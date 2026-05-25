package server

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/torrentregistry"
	"github.com/Triagearr/Triagearr/internal/store"
)

// torrentClientConnectionDTO is the JSON shape of one torrent client
// connection. password is sent verbatim (not redacted): the operator opted
// into UI-managed connections (ADR-0025) and editing a password requires
// reading it back. The endpoint is behind auth and the client renders the
// field as a password input.
type torrentClientConnectionDTO struct {
	ID              int64    `json:"id"`
	Kind            string   `json:"kind"`
	URL             string   `json:"url"`
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Enabled         bool     `json:"enabled"`
	CategoryExclude []string `json:"category_exclude"`
	TagsExclude     []string `json:"tags_exclude"`
	DeleteWithFiles bool     `json:"delete_with_files"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
}

// torrentClientConnectionInput is the writable subset accepted by PUT.
type torrentClientConnectionInput struct {
	URL             string   `json:"url"`
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Enabled         bool     `json:"enabled"`
	CategoryExclude []string `json:"category_exclude"`
	TagsExclude     []string `json:"tags_exclude"`
	DeleteWithFiles bool     `json:"delete_with_files"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
}

const defaultTorrentClientTimeoutSeconds = 30

func torrentClientConnectionToDTO(c store.TorrentClientConnection) torrentClientConnectionDTO {
	return torrentClientConnectionDTO{
		ID: c.ID, Kind: c.Kind, URL: c.URL,
		Username: c.Username, Password: c.Password,
		Enabled:         c.Enabled,
		CategoryExclude: c.CategoryExclude,
		TagsExclude:     c.TagsExclude,
		DeleteWithFiles: c.DeleteWithFiles,
		TimeoutSeconds:  int(c.TimeoutMS / 1000),
	}
}

// validateTorrentClientConnInput checks an input the same way config.Validate
// checks a YAML instance, plus a stricter URL host check.
func validateTorrentClientConnInput(in torrentClientConnectionInput) (string, bool) {
	if in.Enabled {
		u, err := url.Parse(in.URL)
		if err != nil || in.URL == "" || u.Host == "" {
			return "url must be a valid absolute URL when the connection is enabled", false
		}
	}
	if in.TimeoutSeconds < 0 {
		return "timeout_seconds must be zero or positive", false
	}
	return "", true
}

func torrentClientInputToConnection(kind string, in torrentClientConnectionInput) store.TorrentClientConnection {
	secs := in.TimeoutSeconds
	if secs == 0 {
		secs = defaultTorrentClientTimeoutSeconds
	}
	return store.TorrentClientConnection{
		Kind:            kind,
		URL:             strings.TrimRight(in.URL, "/"),
		Username:        in.Username,
		Password:        in.Password,
		Enabled:         in.Enabled,
		CategoryExclude: in.CategoryExclude,
		TagsExclude:     in.TagsExclude,
		DeleteWithFiles: in.DeleteWithFiles,
		TimeoutMS:       int64(secs) * 1000,
	}
}

func (s *Server) handleListTorrentClientConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.opts.Store.ListTorrentClientConnections(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]torrentClientConnectionDTO, 0, len(conns))
	for _, c := range conns {
		out = append(out, torrentClientConnectionToDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// handleUpsertTorrentClientConnection handles PUT
// /api/v1/torrent-client-connections/{kind}.
func (s *Server) handleUpsertTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !torrentregistry.KnownKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of qbittorrent, transmission, deluge, rtorrent")
		return
	}
	if !torrentregistry.ImplementedKind(kind) {
		writeError(w, http.StatusBadRequest, "kind "+kind+" is scaffolded but has no backend yet")
		return
	}
	var in torrentClientConnectionInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	// Carry-forward: don't force the operator to re-enter the password on
	// every save. Empty password in the request means "keep the stored one".
	if in.Password == "" {
		if existing, err := s.opts.Store.GetTorrentClientConnectionByKind(r.Context(), kind); err == nil {
			in.Password = existing.Password
		}
	}
	if msg, ok := validateTorrentClientConnInput(in); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	conn := torrentClientInputToConnection(kind, in)
	saved, err := s.opts.Store.UpsertTorrentClientConnection(r.Context(), conn)
	if err != nil {
		writeInternal(w, err)
		return
	}
	s.reloadAfterTorrentClientChange()
	writeJSON(w, http.StatusOK, torrentClientConnectionToDTO(saved))
}

func (s *Server) handleDeleteTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !torrentregistry.KnownKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of qbittorrent, transmission, deluge, rtorrent")
		return
	}
	if err := s.opts.Store.DeleteTorrentClientConnectionByKind(r.Context(), kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no torrent client connection for kind "+kind)
			return
		}
		writeInternal(w, err)
		return
	}
	s.reloadAfterTorrentClientChange()
	w.WriteHeader(http.StatusNoContent)
}

// torrentClientConnectionTestRequest is the body for POST
// /torrent-client-connections/test. Tests the posted credentials directly —
// no row needs to exist yet, so the operator can verify before saving.
type torrentClientConnectionTestRequest struct {
	Kind           string `json:"kind"`
	URL            string `json:"url"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (s *Server) handleTestTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	var body torrentClientConnectionTestRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !torrentregistry.KnownKind(body.Kind) {
		writeError(w, http.StatusBadRequest, "unknown torrent client kind "+body.Kind)
		return
	}
	if body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	// Carry-forward stored password when the test request has none — same
	// rationale as the PUT handler.
	if body.Password == "" {
		if existing, err := s.opts.Store.GetTorrentClientConnectionByKind(r.Context(), body.Kind); err == nil {
			body.Password = existing.Password
		}
	}
	timeout := time.Duration(body.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTorrentClientTimeoutSeconds * time.Second
	}
	if err := torrentregistry.TestConnection(r.Context(), body.Kind, body.URL, body.Username, body.Password, timeout); err != nil {
		writeError(w, http.StatusBadGateway, "connection test failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reloadAfterTorrentClientChange asks the daemon to rebuild itself so the
// torrent registry picks up the connection change. Mirrors the *arr flow.
func (s *Server) reloadAfterTorrentClientChange() {
	if s.opts.Reload != nil {
		s.opts.Reload()
		return
	}
	slog.Warn("torrent client connection changed but no Reload hook is wired — registry will not refresh until next SIGHUP")
}
