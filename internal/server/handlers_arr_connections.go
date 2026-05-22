package server

import (
	"database/sql"
	"encoding/json"
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
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	APIKey         string   `json:"api_key"`
	Enabled        bool     `json:"enabled"`
	Poll           bool     `json:"poll"`
	Act            bool     `json:"act"`
	TagsExclude    []string `json:"tags_exclude"`
	CategoriesOnly []string `json:"categories_only"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// arrConnectionInput is the writable subset accepted by POST and PUT.
type arrConnectionInput struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
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
		ID: c.ID, Kind: c.Kind, Name: c.Name, URL: c.URL, APIKey: c.APIKey,
		Enabled: c.Enabled, Poll: c.Poll, Act: c.Act,
		TagsExclude: c.TagsExclude, CategoriesOnly: c.CategoriesOnly,
		TimeoutSeconds: int(c.TimeoutMS / 1000),
	}
}

// validateArrConnInput checks an input the same way config.Validate checks a
// YAML instance, plus a stricter URL host check. Being stricter than
// config.Validate is safe: anything this accepts also passes the re-validation
// resolveArrConnections runs on reload.
func validateArrConnInput(in arrConnectionInput) (string, bool) {
	if !registry.KnownKind(in.Kind) {
		return "kind must be one of sonarr, radarr, lidarr, readarr, whisparr_v2, whisparr_v3", false
	}
	if strings.TrimSpace(in.Name) == "" {
		return "name is required", false
	}
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

// inputToConnection converts a validated input into a store row. id is set by
// the caller (0 for create).
func inputToConnection(id int64, in arrConnectionInput) store.ArrConnection {
	secs := in.TimeoutSeconds
	if secs == 0 {
		secs = defaultArrTimeoutSeconds
	}
	return store.ArrConnection{
		ID:             id,
		Kind:           in.Kind,
		Name:           strings.TrimSpace(in.Name),
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

// nameTaken reports whether (kind, name) already exists among existing rows,
// ignoring the row identified by excludeID (0 ignores nothing).
func nameTaken(existing []store.ArrConnection, kind, name string, excludeID int64) bool {
	for _, c := range existing {
		if c.ID == excludeID {
			continue
		}
		if c.Kind == kind && c.Name == name {
			return true
		}
	}
	return false
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

func (s *Server) handleCreateArrConnection(w http.ResponseWriter, r *http.Request) {
	var in arrConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if msg, ok := validateArrConnInput(in); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	conn := inputToConnection(0, in)

	existing, err := s.opts.Store.ListArrConnections(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if nameTaken(existing, conn.Kind, conn.Name, 0) {
		writeError(w, http.StatusConflict, "a "+conn.Kind+" connection named "+conn.Name+" already exists")
		return
	}

	id, err := s.opts.Store.CreateArrConnection(r.Context(), conn)
	if err != nil {
		writeInternal(w, err)
		return
	}
	conn.ID = id
	s.reloadAfterArrChange()
	writeJSON(w, http.StatusCreated, connectionToDTO(conn))
}

func (s *Server) handleUpdateArrConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	var in arrConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if msg, ok := validateArrConnInput(in); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	conn := inputToConnection(id, in)

	existing, err := s.opts.Store.ListArrConnections(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	if nameTaken(existing, conn.Kind, conn.Name, id) {
		writeError(w, http.StatusConflict, "a "+conn.Kind+" connection named "+conn.Name+" already exists")
		return
	}

	if err := s.opts.Store.UpdateArrConnection(r.Context(), conn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no arr connection with that id")
			return
		}
		writeInternal(w, err)
		return
	}
	s.reloadAfterArrChange()
	writeJSON(w, http.StatusOK, connectionToDTO(conn))
}

func (s *Server) handleDeleteArrConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	if err := s.opts.Store.DeleteArrConnection(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no arr connection with that id")
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
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
