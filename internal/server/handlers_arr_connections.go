package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/clients/arr/registry"
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
	PublicURL      string   `json:"public_url"`
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
	PublicURL      string   `json:"public_url"`
	APIKey         string   `json:"api_key"`
	Enabled        bool     `json:"enabled"`
	Poll           bool     `json:"poll"`
	Act            bool     `json:"act"`
	TagsExclude    []string `json:"tags_exclude"`
	CategoriesOnly []string `json:"categories_only"`
	TimeoutSeconds int      `json:"timeout_seconds"`
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

// defaultArrTimeoutSeconds mirrors config.defaultArrTimeout — applied when the
// operator leaves the timeout field at zero.
const defaultArrTimeoutSeconds = 30

const arrKnownKindMsg = "kind must be one of sonarr, radarr, lidarr, readarr, whisparr_v2, whisparr_v3"

func arrConnectionToDTO(c store.ArrConnection) arrConnectionDTO {
	return arrConnectionDTO{
		ID: c.ID, Kind: c.Kind, URL: c.URL, PublicURL: c.PublicURL, APIKey: c.APIKey,
		Enabled: c.Enabled, Poll: c.Poll, Act: c.Act,
		TagsExclude: c.TagsExclude, CategoriesOnly: c.CategoriesOnly,
		TimeoutSeconds: int(c.TimeoutMS / 1000),
	}
}

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
	if in.PublicURL != "" {
		u, err := url.Parse(in.PublicURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "public_url must be an absolute http(s) URL when set", false
		}
	}
	return "", true
}

func arrInputToConnection(kind string, in arrConnectionInput) store.ArrConnection {
	secs := in.TimeoutSeconds
	if secs == 0 {
		secs = defaultArrTimeoutSeconds
	}
	return store.ArrConnection{
		Kind:           kind,
		URL:            strings.TrimRight(in.URL, "/"),
		PublicURL:      strings.TrimRight(in.PublicURL, "/"),
		APIKey:         in.APIKey,
		Enabled:        in.Enabled,
		Poll:           in.Poll,
		Act:            in.Act,
		TagsExclude:    in.TagsExclude,
		CategoriesOnly: in.CategoriesOnly,
		TimeoutMS:      int64(secs) * 1000,
	}
}

func (s *Server) arrConnCRUD() *connectionCRUD[store.ArrConnection, arrConnectionInput, arrConnectionDTO, arrConnectionTestRequest] {
	return &connectionCRUD[store.ArrConnection, arrConnectionInput, arrConnectionDTO, arrConnectionTestRequest]{
		label:        "arr connection",
		knownKind:    registry.KnownKind,
		knownKindMsg: arrKnownKindMsg,
		list:         s.opts.Store.ListArrConnections,
		getByKind:    s.opts.Store.GetArrConnectionByKind,
		upsert:       s.opts.Store.UpsertArrConnection,
		deleteByKind: s.opts.Store.DeleteArrConnectionByKind,
		carryForwardInput: func(in *arrConnectionInput, existing store.ArrConnection) {
			if in.APIKey == "" {
				in.APIKey = existing.APIKey
			}
		},
		validateInput:      validateArrConnInput,
		inputToConn:        arrInputToConnection,
		connToDTO:          arrConnectionToDTO,
		testKind:           func(r *arrConnectionTestRequest) string { return r.Kind },
		testURL:            func(r *arrConnectionTestRequest) string { return r.URL },
		testTimeoutSeconds: func(r *arrConnectionTestRequest) int { return r.TimeoutSeconds },
		runTest: func(ctx context.Context, req arrConnectionTestRequest, timeout time.Duration) error {
			return registry.TestConnection(ctx, req.Kind, req.URL, req.APIKey, timeout)
		},
		defaultTimeoutSecs: defaultArrTimeoutSeconds,
		reload:             s.opts.Reload,
	}
}

func (s *Server) handleListArrConnections(w http.ResponseWriter, r *http.Request) {
	s.arrConnCRUD().handleList(w, r)
}

func (s *Server) handleUpsertArrConnection(w http.ResponseWriter, r *http.Request) {
	s.arrConnCRUD().handleUpsert(w, r)
}

func (s *Server) handleDeleteArrConnection(w http.ResponseWriter, r *http.Request) {
	s.arrConnCRUD().handleDelete(w, r)
}

func (s *Server) handleTestArrConnection(w http.ResponseWriter, r *http.Request) {
	s.arrConnCRUD().handleTest(w, r)
}
