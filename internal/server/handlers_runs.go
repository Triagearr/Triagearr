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

	"github.com/Triagearr/Triagearr/internal/actor"
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
	TorrentName    string  `json:"torrent_name,omitempty"`
	Score          float64 `json:"score"`
	SizeBytes      int64   `json:"size_bytes"`
	WouldFreeBytes int64   `json:"would_free_bytes"`
}

func (s *Server) handlePostRun(w http.ResponseWriter, r *http.Request) {
	var req postRunRequest
	if r.ContentLength > 0 && !decodeJSONBody(w, r, &req) {
		return
	}
	// Snapshot the engine once: a reload may swap it mid-handler, and a live
	// run must execute against the Actor that was current when it was armed.
	eng := s.engine()
	vol := eng.Volume()
	if vol.Path == "" {
		writeError(w, http.StatusBadRequest, "no volume configured")
		return
	}
	plan, err := eng.Decider.Plan(r.Context(), vol)
	if err != nil {
		writeInternal(w, err)
		return
	}
	mode := triagearr.ResolveRunMode(eng.DaemonLive, triagearr.RunTriggerHTTP, req.Mode == "live")
	live := mode == triagearr.RunModeLive && eng.Actor != nil

	// A live run executes asynchronously (see executeRunAsync); reserve the
	// single-run slot before persisting anything so a concurrent trigger gets
	// a clean 409 instead of a half-created run that never executes.
	if live {
		select {
		case s.liveRun <- struct{}{}:
		default:
			writeError(w, http.StatusConflict, "a live run is already in progress")
			return
		}
	}

	// Live runs start "pending" and are driven to a terminal state by the
	// background goroutine; dry-runs have no Actor and are terminal at once.
	status := "completed"
	if live {
		status = "pending"
	}
	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerHTTP,
		TriggeredAt:         time.Now().UTC(),
		Mode:                string(mode),
		FreePctAtFire:       plan.FreePctAtFire,
		TargetFreePct:       vol.TargetFreePercent,
		EstimatedFreedBytes: plan.EstimatedFreedBytes,
		StopReason:          plan.StopReason,
		Status:              status,
	}
	id, err := s.opts.Store.InsertRun(r.Context(), run)
	if err != nil {
		if live {
			<-s.liveRun
		}
		writeInternal(w, fmt.Errorf("persisting run: %w", err))
		return
	}
	if err := s.opts.Store.InsertRunItems(r.Context(), id, plan.Items); err != nil {
		if live {
			<-s.liveRun
		}
		writeInternal(w, fmt.Errorf("persisting items: %w", err))
		return
	}
	if live {
		go s.executeRunAsync(id, eng.Actor)
	}
	// Re-read so the response carries persisted state. The live goroutine may
	// not have started yet, so this typically returns the "pending" run plus
	// its candidates — the UI polls /runs/{id} for the live progression.
	refreshed, items, err := s.opts.Store.GetRun(r.Context(), id)
	if err != nil {
		run.ID = id
		writeJSON(w, http.StatusOK, buildResponse(run, plan.Items))
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(refreshed, items))
}

// executeRunAsync drives a live run's destructive pipeline detached from the
// HTTP request. It runs on the daemon-lifetime baseCtx so a long deletion
// outlives the request, and always releases the single-run slot. On Actor
// failure the run is marked "aborted" so the UI stops showing it in-flight.
func (s *Server) executeRunAsync(id int64, act *actor.Actor) {
	defer func() { <-s.liveRun }()
	if err := act.Execute(s.baseCtx, id); err != nil {
		slog.Warn("actor execute failed", "run_id", id, "err", err)
		if mErr := s.opts.Store.MarkRunStatus(s.baseCtx, id, "aborted"); mErr != nil {
			slog.Error("marking aborted run failed", "run_id", id, "err", mErr)
		}
	}
}

// handlePreviewRun returns the deletion plan the Decider would produce right
// now, without persisting a run. It backs the live-confirmation modal so the
// operator sees what a live run would delete before arming it. Read-only.
func (s *Server) handlePreviewRun(w http.ResponseWriter, r *http.Request) {
	eng := s.engine()
	vol := eng.Volume()
	if vol.Path == "" {
		writeError(w, http.StatusBadRequest, "no volume configured")
		return
	}
	plan, err := eng.Decider.Plan(r.Context(), vol)
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"estimated_freed_bytes": plan.EstimatedFreedBytes,
		"stop_reason":           string(plan.StopReason),
		"candidates":            buildRunItems(plan.Items),
	})
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
	out.Candidates = buildRunItems(items)
	return out
}

// buildRunItems maps deletion-plan items to their wire shape. The torrent name
// rides on the item itself (set by the Decider, persisted in run_items per the
// 0003 migration) so a reaped torrent still renders its title even after it has
// left the torrents table. Shared by the persisted-run view and the preview.
func buildRunItems(items []triagearr.RunItem) []runItemResponse {
	out := make([]runItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, runItemResponse{
			Rank:           it.Rank,
			TorrentHash:    string(it.TorrentHash),
			TorrentName:    it.TorrentName,
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
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Status is already on the wire; we can only log. Most callers don't
		// observe writeJSON's outcome anyway.
		slog.Warn("encoding HTTP response body failed", "status", status, "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
