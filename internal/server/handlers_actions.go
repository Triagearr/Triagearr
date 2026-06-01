package server

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type actionView struct {
	ID          int64      `json:"id"`
	RunID       int64      `json:"run_id"`
	Rank        int        `json:"rank"`
	TorrentHash string     `json:"torrent_hash"`
	TorrentName string     `json:"torrent_name,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	FreedBytes  int64      `json:"freed_bytes"`
}

type actionListResponse struct {
	Actions []actionView `json:"actions"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
}

func (s *Server) handleListActions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := intParam(q, "limit", 50, 1, 500)
	offset := intParam(q, "offset", 0, 0, 1_000_000)
	rows, err := s.opts.Store.ListActionsRecent(r.Context(), limit, offset)
	if err != nil {
		writeInternal(w, err)
		return
	}
	total, err := s.opts.Store.CountActions(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actionListResponse{
		Actions: viewsFromActions(rows), Total: total, Limit: limit, Offset: offset,
	})
}

// actionToView maps a persisted action to its wire shape. The torrent name is
// read straight off the row (snapshotted at action time, 0003 migration) so a
// reaped torrent — already evicted from the torrents table — still shows its
// title in the post-mortem.
func actionToView(a triagearr.Action) actionView {
	v := actionView{
		ID: a.ID, RunID: a.RunID, Rank: a.Rank,
		TorrentHash: string(a.TorrentHash), TorrentName: a.TorrentName,
		Status:    string(a.Status),
		StartedAt: a.StartedAt, FreedBytes: a.FreedBytes,
	}
	if !a.FinishedAt.IsZero() {
		finished := a.FinishedAt
		v.FinishedAt = &finished
	}
	return v
}

func viewsFromActions(rows []triagearr.Action) []actionView {
	out := make([]actionView, len(rows))
	for i, a := range rows {
		out[i] = actionToView(a)
	}
	return out
}

func (s *Server) handleRunActions(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	rows, err := s.opts.Store.ListActionsByRun(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": viewsFromActions(rows)})
}

type auditView struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"ts"`
	Step      string    `json:"step"`
	Outcome   string    `json:"outcome"`
	ArrType   string    `json:"arr_type,omitempty"`
	ArrFileID int64     `json:"arr_file_id,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

type actionDetailResponse struct {
	Action actionView  `json:"action"`
	Audit  []auditView `json:"audit"`
}

func (s *Server) handleGetAction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	a, err := s.opts.Store.GetAction(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "action not found")
			return
		}
		writeInternal(w, err)
		return
	}
	audit, err := s.opts.Store.ListAuditByAction(r.Context(), id)
	if err != nil {
		writeInternal(w, err)
		return
	}
	resp := actionDetailResponse{
		Action: actionToView(a),
		Audit:  make([]auditView, len(audit)),
	}
	for i, e := range audit {
		resp.Audit[i] = auditView{
			ID: e.ID, Timestamp: e.Timestamp,
			Step: string(e.Step), Outcome: string(e.Outcome),
			ArrType: e.ArrType, ArrFileID: e.ArrFileID, Detail: e.Detail,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
