package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type arrView struct {
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	URL             string     `json:"url"`
	Healthy         bool       `json:"healthy"`
	LastHealthCheck *time.Time `json:"last_health_check,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

func (s *Server) handleListArrs(w http.ResponseWriter, r *http.Request) {
	out, err := s.buildArrViews(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrs": out})
}

func (s *Server) buildArrViews(ctx context.Context) ([]arrView, error) {
	rows, err := s.opts.Store.ListArrInstances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]arrView, len(rows))
	for i, row := range rows {
		v := arrView{
			Name: row.Kind, Type: row.Kind, URL: row.URL,
			Healthy: row.Healthy, LastHealthCheck: row.LastHealthCheck,
		}
		if row.LastError != nil {
			v.LastError = *row.LastError
		}
		out[i] = v
	}
	return out, nil
}

// arrBaseURL returns the browser-facing base URL for the given arr type, used
// to build deep links in the dashboard. It reads the DB-owned arr_connections
// row (ADR-0022) and prefers public_url when set, falling back to the internal
// url (consumed by API clients). Empty when the kind is unknown or the row is
// absent.
func (s *Server) arrBaseURL(ctx context.Context, t triagearr.ArrType) string {
	if s.opts.Store == nil {
		return ""
	}
	conn, err := s.opts.Store.GetArrConnectionByKind(ctx, string(t))
	if err != nil {
		return ""
	}
	if conn.PublicURL != "" {
		return strings.TrimRight(conn.PublicURL, "/")
	}
	return strings.TrimRight(conn.URL, "/")
}
