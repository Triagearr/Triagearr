package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// writeInternal returns a 500 with the error's message, used everywhere a
// handler can't recover from an upstream/storage failure.
func writeInternal(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, err.Error())
}

// parseIDPath extracts a positive int64 from the {id} path segment, writing
// a 400 on failure. Returns (id, true) on success.
func parseIDPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

type postRunRequest struct {
	Mode string `json:"mode,omitempty"`
}

type runResponse struct {
	RunID               int64             `json:"run_id"`
	TriggeredBy         string            `json:"triggered_by"`
	TriggeredAt         time.Time         `json:"triggered_at"`
	Mode                string            `json:"mode"`
	FreePctAtFire       float64           `json:"free_pct_at_fire,omitempty"`
	TargetFreePct       float64           `json:"target_free_pct,omitempty"`
	EstimatedFreedBytes int64             `json:"estimated_freed_bytes"`
	StopReason          string            `json:"stop_reason"`
	Status              string            `json:"status"`
	Candidates          []runItemResponse `json:"candidates,omitempty"`
}

type runItemResponse struct {
	Rank           int     `json:"rank"`
	TorrentHash    string  `json:"torrent_hash"`
	Score          float64 `json:"score"`
	SizeBytes      int64   `json:"size_bytes"`
	WouldFreeBytes int64   `json:"would_free_bytes"`
}

func (s *Server) handlePostRun(w http.ResponseWriter, r *http.Request) {
	var req postRunRequest
	if r.ContentLength > 0 && !decodeJSONBody(w, r, &req) {
		return
	}
	vol := s.opts.Volume()
	plan, err := s.opts.Decider.Plan(r.Context(), vol)
	if err != nil {
		writeInternal(w, err)
		return
	}
	mode := triagearr.ResolveRunMode(s.opts.DaemonLive, triagearr.RunTriggerHTTP, req.Mode == "live")
	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerHTTP,
		TriggeredAt:         time.Now().UTC(),
		Mode:                string(mode),
		FreePctAtFire:       plan.FreePctAtFire,
		TargetFreePct:       vol.TargetFreePercent,
		EstimatedFreedBytes: plan.EstimatedFreedBytes,
		StopReason:          plan.StopReason,
		Status:              "completed",
	}
	id, err := s.opts.Store.InsertRun(r.Context(), run)
	if err != nil {
		writeInternal(w, fmt.Errorf("persisting run: %w", err))
		return
	}
	if err := s.opts.Store.InsertRunItems(r.Context(), id, plan.Items); err != nil {
		writeInternal(w, fmt.Errorf("persisting items: %w", err))
		return
	}
	if mode == triagearr.RunModeLive && s.opts.Actor != nil {
		if err := s.opts.Actor.Execute(r.Context(), id); err != nil {
			slog.Warn("actor execute failed", "run_id", id, "err", err)
		}
	}
	// Always re-read the run so the response carries persisted state
	// (Actor may have updated status, freed_bytes, ordering).
	refreshed, items, err := s.opts.Store.GetRun(r.Context(), id)
	if err != nil {
		run.ID = id
		writeJSON(w, http.StatusOK, buildResponse(run, plan.Items))
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(refreshed, items))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r.URL.Query(), "limit", 50, 1, 500)
	rows, err := s.opts.Store.ListRuns(r.Context(), store.ListRunsOpts{Limit: limit})
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]runResponse, len(rows))
	for i, r := range rows {
		out[i] = buildResponse(r, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDPath(w, r)
	if !ok {
		return
	}
	run, items, err := s.opts.Store.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(run, items))
}

func buildResponse(r triagearr.Run, items []triagearr.RunItem) runResponse {
	out := runResponse{
		RunID:               r.ID,
		TriggeredBy:         string(r.TriggeredBy),
		TriggeredAt:         r.TriggeredAt,
		Mode:                r.Mode,
		FreePctAtFire:       r.FreePctAtFire,
		TargetFreePct:       r.TargetFreePct,
		EstimatedFreedBytes: r.EstimatedFreedBytes,
		StopReason:          string(r.StopReason),
		Status:              r.Status,
	}
	for _, it := range items {
		out.Candidates = append(out.Candidates, runItemResponse{
			Rank:           it.Rank,
			TorrentHash:    string(it.TorrentHash),
			Score:          it.Score,
			SizeBytes:      it.SizeBytes,
			WouldFreeBytes: it.WouldFreeBytes,
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
