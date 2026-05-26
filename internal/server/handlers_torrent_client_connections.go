package server

import (
	"context"
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
	PublicURL       string   `json:"public_url"`
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
	PublicURL       string   `json:"public_url"`
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Enabled         bool     `json:"enabled"`
	CategoryExclude []string `json:"category_exclude"`
	TagsExclude     []string `json:"tags_exclude"`
	DeleteWithFiles bool     `json:"delete_with_files"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
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

const defaultTorrentClientTimeoutSeconds = 30

const torrentClientKnownKindMsg = "kind must be one of qbittorrent, transmission, deluge, rtorrent"

func torrentClientConnectionToDTO(c store.TorrentClientConnection) torrentClientConnectionDTO {
	return torrentClientConnectionDTO{
		ID: c.ID, Kind: c.Kind, URL: c.URL, PublicURL: c.PublicURL,
		Username: c.Username, Password: c.Password,
		Enabled:         c.Enabled,
		CategoryExclude: c.CategoryExclude,
		TagsExclude:     c.TagsExclude,
		DeleteWithFiles: c.DeleteWithFiles,
		TimeoutSeconds:  int(c.TimeoutMS / 1000),
	}
}

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
	if in.PublicURL != "" {
		u, err := url.Parse(in.PublicURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "public_url must be an absolute http(s) URL when set", false
		}
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
		PublicURL:       strings.TrimRight(in.PublicURL, "/"),
		Username:        in.Username,
		Password:        in.Password,
		Enabled:         in.Enabled,
		CategoryExclude: in.CategoryExclude,
		TagsExclude:     in.TagsExclude,
		DeleteWithFiles: in.DeleteWithFiles,
		TimeoutMS:       int64(secs) * 1000,
	}
}

func (s *Server) torrentClientConnCRUD() *connectionCRUD[store.TorrentClientConnection, torrentClientConnectionInput, torrentClientConnectionDTO, torrentClientConnectionTestRequest] {
	return &connectionCRUD[store.TorrentClientConnection, torrentClientConnectionInput, torrentClientConnectionDTO, torrentClientConnectionTestRequest]{
		label:           "torrent client connection",
		knownKind:       torrentregistry.KnownKind,
		knownKindMsg:    torrentClientKnownKindMsg,
		implementedKind: torrentregistry.ImplementedKind,
		implementedKindMsg: func(k string) string {
			return "kind " + k + " is scaffolded but has no backend yet"
		},
		list:         s.opts.Store.ListTorrentClientConnections,
		getByKind:    s.opts.Store.GetTorrentClientConnectionByKind,
		upsert:       s.opts.Store.UpsertTorrentClientConnection,
		deleteByKind: s.opts.Store.DeleteTorrentClientConnectionByKind,
		carryForwardInput: func(in *torrentClientConnectionInput, existing store.TorrentClientConnection) {
			if in.Password == "" {
				in.Password = existing.Password
			}
		},
		validateInput:      validateTorrentClientConnInput,
		inputToConn:        torrentClientInputToConnection,
		connToDTO:          torrentClientConnectionToDTO,
		testKind:           func(r *torrentClientConnectionTestRequest) string { return r.Kind },
		testURL:            func(r *torrentClientConnectionTestRequest) string { return r.URL },
		testTimeoutSeconds: func(r *torrentClientConnectionTestRequest) int { return r.TimeoutSeconds },
		carryForwardTest: func(r *torrentClientConnectionTestRequest, existing store.TorrentClientConnection) {
			if r.Password == "" {
				r.Password = existing.Password
			}
		},
		runTest: func(ctx context.Context, req torrentClientConnectionTestRequest, timeout time.Duration) error {
			return torrentregistry.TestConnection(ctx, req.Kind, req.URL, req.Username, req.Password, timeout)
		},
		defaultTimeoutSecs: defaultTorrentClientTimeoutSeconds,
		reload:             s.opts.Reload,
	}
}

func (s *Server) handleListTorrentClientConnections(w http.ResponseWriter, r *http.Request) {
	s.torrentClientConnCRUD().handleList(w, r)
}

func (s *Server) handleUpsertTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	s.torrentClientConnCRUD().handleUpsert(w, r)
}

func (s *Server) handleDeleteTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	s.torrentClientConnCRUD().handleDelete(w, r)
}

func (s *Server) handleTestTorrentClientConnection(w http.ResponseWriter, r *http.Request) {
	s.torrentClientConnCRUD().handleTest(w, r)
}
