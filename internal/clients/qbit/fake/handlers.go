package fake

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const sessionCookieName = "SID"

// mux returns a http.ServeMux pre-wired with all supported endpoints.
func (s *Server) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("POST /api/v2/auth/login", s.handleLogin)
	m.HandleFunc("GET /api/v2/torrents/info", s.requireSession(s.handleTorrentsInfo))
	m.HandleFunc("GET /api/v2/torrents/files", s.requireSession(s.handleTorrentsFiles))
	m.HandleFunc("GET /api/v2/torrents/trackers", s.requireSession(s.handleTorrentsTrackers))
	m.HandleFunc("POST /api/v2/torrents/delete", s.requireSession(s.handleTorrentsDelete))
	m.HandleFunc("/", s.handleUnknown)
	return m
}

// requireSession wraps a handler with the SID-cookie check, mirroring qBit's
// 403 response when the session is missing or stale.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.state.authBypassed() {
			next(w, r)
			return
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !s.state.sessionValid(c.Value) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user := r.FormValue("username")
	pass := r.FormValue("password")
	if !s.state.checkCredentials(user, pass) {
		// Real qBit returns 200 with body "Fails." on bad creds. Match that
		// so the client's loose parsing (`!= "Ok."` → error) trips correctly.
		_, _ = w.Write([]byte("Fails."))
		return
	}
	cookie := newSessionID()
	s.state.registerSession(cookie)
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: fake qBit server runs over plain HTTP; this session cookie is not a security boundary
		Name:     sessionCookieName,
		Value:    cookie,
		Path:     "/",
		HttpOnly: true,
	})
	_, _ = w.Write([]byte("Ok."))
}

func (s *Server) handleTorrentsInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.state.List())
}

func (s *Server) handleTorrentsFiles(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	t, ok := s.state.Get(hash)
	if !ok {
		// qBit returns 404 when the hash is unknown.
		http.NotFound(w, r)
		return
	}
	if t.Files == nil {
		writeJSON(w, []File{})
		return
	}
	writeJSON(w, t.Files)
}

func (s *Server) handleTorrentsTrackers(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	t, ok := s.state.Get(hash)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Mirror qBit's three synthetic DHT/PEX/LSD pseudo-trackers. The real
	// client filters these out (looksLikeURL); keeping them here exercises
	// that filter path.
	out := make([]Tracker, 0, 3+len(t.Trackers))
	out = append(out,
		Tracker{URL: "** [DHT] **", Status: 2},
		Tracker{URL: "** [PeX] **", Status: 2},
		Tracker{URL: "** [LSD] **", Status: 2},
	)
	out = append(out, t.Trackers...)
	writeJSON(w, out)
}

func (s *Server) handleTorrentsDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	s.state.deleteCalls.Add(1)
	hashes := strings.Split(r.FormValue("hashes"), "|")
	deleteFiles := r.FormValue("deleteFiles") == "true"
	for _, h := range hashes {
		if h == "" {
			continue
		}
		removed := s.state.Delete(h)
		s.logger.Info("fake-qbit: delete",
			slog.String("hash", h),
			slog.Bool("delete_files", deleteFiles),
			slog.Bool("removed", removed),
		)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	s.logger.Warn("fake-qbit: unimplemented endpoint",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails in practice; falling back to a constant is
		// fine for a dev fake.
		return "fakefakefakefake"
	}
	return hex.EncodeToString(b[:])
}
