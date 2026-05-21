// Package server exposes Triagearr's HTTP API and serves the embedded React UI.
//
// Auth is Sonarr-style: a loopback bind defaults to "none" so a fresh daemon
// greets the operator without prompts (canonical setup behind a reverse proxy
// like TinyAuth/Authelia/Caddy). Any non-loopback bind requires "apikey",
// compared in constant time. The SPA is served from / with a fallback to
// index.html so client-side routes survive full-page reloads.
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/store"
)

// VolumeLookup resolves a volume name to the rule the Decider plan needs.
// The daemon supplies a closure over the parsed config.
type VolumeLookup func(name string) (decider.Volume, bool)

// VersionInfo is the build metadata surfaced through GET /api/v1/version.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Options bundles everything the server needs at construction time.
type Options struct {
	Bind   string
	APIKey string
	// Auth selects the authentication strategy: "none" or "apikey".
	// Empty defaults to "apikey".
	Auth      string
	Store     *store.Store
	Linker    *linker.Linker
	Config    *config.Config
	Version   VersionInfo
	UIHandler http.Handler

	Decider *decider.Decider
	Volume  VolumeLookup
	Volumes func() []decider.Volume
	// DaemonLive mirrors config.Mode == "live". Without it, per-request live
	// opt-ins are forced back to dry-run (ADR-0015).
	DaemonLive bool
	Actor      *actor.Actor
}

// Server is a wired HTTP server ready to be Started.
type Server struct {
	opts    Options
	srv     *http.Server
	runRate *ipRateLimiter
}

// New builds a Server. Does not start listening.
func New(opts Options) *Server {
	if opts.Auth == "" {
		opts.Auth = config.HTTPAuthAPIKey
	}
	s := &Server{
		opts:    opts,
		runRate: newIPRateLimiter(1, time.Minute),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/runs", s.security(s.auth(s.runRateLimit(s.handlePostRun))))
	mux.HandleFunc("GET /api/v1/runs", s.security(s.auth(s.handleListRuns)))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.security(s.auth(s.handleGetRun)))
	mux.HandleFunc("GET /api/v1/runs/{id}/actions", s.security(s.auth(s.handleRunActions)))
	mux.HandleFunc("GET /api/v1/actions", s.security(s.auth(s.handleListActions)))
	mux.HandleFunc("GET /api/v1/actions/{id}", s.security(s.auth(s.handleGetAction)))
	mux.HandleFunc("GET /api/v1/torrents", s.security(s.auth(s.handleListTorrents)))
	mux.HandleFunc("GET /api/v1/torrents/{hash}", s.security(s.auth(s.handleGetTorrent)))
	mux.HandleFunc("GET /api/v1/torrents/{hash}/snapshots", s.security(s.auth(s.handleTorrentSnapshots)))
	mux.HandleFunc("GET /api/v1/scores", s.security(s.auth(s.handleListScores)))
	mux.HandleFunc("GET /api/v1/volumes", s.security(s.auth(s.handleListVolumes)))
	mux.HandleFunc("GET /api/v1/volumes/{name}/history", s.security(s.auth(s.handleVolumeHistory)))
	mux.HandleFunc("GET /api/v1/arrs", s.security(s.auth(s.handleListArrs)))
	mux.HandleFunc("GET /api/v1/summary", s.security(s.auth(s.handleSummary)))
	mux.HandleFunc("GET /api/v1/config", s.security(s.auth(s.handleConfig)))
	mux.HandleFunc("GET /api/v1/version", s.security(s.auth(s.handleVersion)))
	mux.HandleFunc("GET /api/v1/auth-mode", s.security(s.handleAuthMode))
	mux.HandleFunc("GET /healthz", s.security(s.handleHealth))

	// SPA fallback must register last so API routes win the pattern match.
	if opts.UIHandler != nil {
		mux.Handle("/", s.security(opts.UIHandler.ServeHTTP))
	}

	s.srv = &http.Server{
		Addr:              opts.Bind,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Handler exposes the wired http.Handler. Useful for httptest-driven tests.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Start serves until ctx is cancelled, then shuts down with a 5s grace.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "bind", s.opts.Bind, "auth", s.opts.Auth)
		err := s.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	if s.opts.Auth == config.HTTPAuthNone {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid X-API-Key")
			return
		}
		h(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAuthMode(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"auth": s.opts.Auth})
}
