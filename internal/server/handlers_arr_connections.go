package server

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/registry"
	"github.com/Triagearr/Triagearr/internal/store"
)

// arrConnectionDTO is the JSON shape of one *arr connection. api_key is sent
// verbatim (not redacted): the operator opted into UI-managed connections
// (ADR-0022) and editing a key requires reading it back. The endpoint is
// behind auth and the client renders the field as a password input.
type arrConnectionDTO struct {
	ID             int64    `json:"id"`
	Kind           string   `json:"kind"`
	URL            string   `json:"url"`
	APIKey         string   `json:"api_key"`
	Enabled        bool     `json:"enabled"`
	Poll           bool     `json:"poll"`
	Act            bool     `json:"act"`
	TagsExclude    []string `json:"tags_exclude"`
	CategoriesOnly []string `json:"categories_only"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// arrConnectionInput is the writable subset accepted by PUT.
type arrConnectionInput struct {
	URL            string   `json:"url"`
	APIKey         string   `json:"api_key"`
	Enabled        bool     `json:"enabled"`
	Poll           bool     `json:"poll"`
	Act            bool     `json:"act"`
	TagsExclude    []string `json:"tags_exclude"`
	CategoriesOnly []string `json:"categories_only"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// defaultArrTimeoutSeconds mirrors config.defaultArrTimeout — applied when the
// operator leaves the timeout field at zero.
const defaultArrTimeoutSeconds = 30

func connectionToDTO(c store.ArrConnection) arrConnectionDTO {
	return arrConnectionDTO{
		ID: c.ID, Kind: c.Kind, URL: c.URL, APIKey: c.APIKey,
		Enabled: c.Enabled, Poll: c.Poll, Act: c.Act,
		TagsExclude: c.TagsExclude, CategoriesOnly: c.CategoriesOnly,
		TimeoutSeconds: int(c.TimeoutMS / 1000),
	}
}

// validateArrConnInput checks an input the same way config.Validate checks a
// YAML instance, plus a stricter URL host check.
func validateArrConnInput(in arrConnectionInput) (string, bool) {
	if in.Enabled {
		u, err := url.Parse(in.URL)
		if err != nil || in.URL == "" || u.Host == "" {
			return "url must be a valid absolute URL when the connection is enabled", false
		}
		if in.APIKey == "" {
			return "api_key is required when the connection is enabled", false
		}
	}
	if in.TimeoutSeconds < 0 {
		return "timeout_seconds must be zero or positive", false
	}
	return "", true
}

// inputToConnection converts a validated input into a store row for the given kind.
func inputToConnection(kind string, in arrConnectionInput) store.ArrConnection {
	secs := in.TimeoutSeconds
	if secs == 0 {
		secs = defaultArrTimeoutSeconds
	}
	return store.ArrConnection{
		Kind:           kind,
		URL:            strings.TrimRight(in.URL, "/"),
		APIKey:         in.APIKey,
		Enabled:        in.Enabled,
		Poll:           in.Poll,
		Act:            in.Act,
		TagsExclude:    in.TagsExclude,
		CategoriesOnly: in.CategoriesOnly,
		TimeoutMS:      int64(secs) * 1000,
	}
}

func (s *Server) handleListArrConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.opts.Store.ListArrConnections(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]arrConnectionDTO, 0, len(conns))
	for _, c := range conns {
		out = append(out, connectionToDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// handleUpsertArrConnection handles PUT /api/v1/arr-connections/{kind}.
// It creates or replaces the connection for the given kind.
func (s *Server) handleUpsertArrConnection(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !registry.KnownKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of sonarr, radarr, lidarr, readarr, whisparr_v2, whisparr_v3")
		return
	}
	var in arrConnectionInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	// Carry-forward must happen BEFORE validation: the common edit case is
	// "save settings while keeping the existing secret"; rejecting empty
	// api_key when a stored one exists would force the operator to re-enter
	// the key on every save.
	if in.APIKey == "" {
		if existing, err := s.opts.Store.GetArrConnectionByKind(r.Context(), kind); err == nil {
			in.APIKey = existing.APIKey
		}
	}
	if msg, ok := validateArrConnInput(in); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	conn := inputToConnection(kind, in)
	saved, err := s.opts.Store.UpsertArrConnection(r.Context(), conn)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.reloadAfterArrChange()
	writeJSON(w, http.StatusOK, connectionToDTO(saved))
}

func (s *Server) handleDeleteArrConnection(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !registry.KnownKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of sonarr, radarr, lidarr, readarr, whisparr_v2, whisparr_v3")
		return
	}
	if err := s.opts.Store.DeleteArrConnectionByKind(r.Context(), kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no arr connection for kind "+kind)
			return
		}
		writeInternal(w, err)
		return
	}
	s.reloadAfterArrChange()
	w.WriteHeader(http.StatusNoContent)
}

// arrConnectionTestRequest is the body for POST /arr-connections/test. It
// tests the posted credentials directly — no row needs to exist yet, so the
// operator can verify a connection before saving it.
type arrConnectionTestRequest struct {
	Kind           string `json:"kind"`
	URL            string `json:"url"`
	APIKey         string `json:"api_key"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Server) handleTestArrConnection(w http.ResponseWriter, r *http.Request) {
	var body arrConnectionTestRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !registry.KnownKind(body.Kind) {
		writeError(w, http.StatusBadRequest, "unknown arr kind "+body.Kind)
		return
	}
	if body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	timeout := time.Duration(body.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultArrTimeoutSeconds * time.Second
	}
	if err := registry.TestConnection(r.Context(), body.Kind, body.URL, body.APIKey, timeout); err != nil {
		writeError(w, http.StatusBadGateway, "connection test failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reloadAfterArrChange asks the daemon to rebuild itself so the client
// registry picks up the connection change. Mirrors the settings PUT flow.
func (s *Server) reloadAfterArrChange() {
	if s.opts.Reload != nil {
		s.opts.Reload()
		return
	}
	slog.Warn("arr connection changed but no Reload hook is wired — registry will not refresh until next SIGHUP")
}
