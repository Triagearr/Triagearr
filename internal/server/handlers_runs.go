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

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type postRunRequest struct {
	Volume string `json:"volume,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

type runResponse struct {
	RunID               int64             `json:"run_id"`
	TriggeredBy         string            `json:"triggered_by"`
	TriggeredAt         time.Time         `json:"triggered_at"`
	Mode                string            `json:"mode"`
	Volume              string            `json:"volume,omitempty"`
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
	if r.ContentLength > 0 {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
	}
	vol, ok := s.resolveVolume(req.Volume)
	if !ok {
		if req.Volume == "" {
			writeError(w, http.StatusBadRequest, "no volume configured")
		} else {
			writeError(w, http.StatusNotFound, fmt.Sprintf("unknown volume %q", req.Volume))
		}
		return
	}
	plan, err := s.opts.Decider.Plan(r.Context(), vol)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mode := triagearr.ResolveRunMode(s.opts.DaemonLive, triagearr.RunTriggerHTTP, req.Mode == "live")
	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerHTTP,
		TriggeredAt:         time.Now().UTC(),
		Mode:                string(mode),
		VolumeName:          vol.Name,
		FreePctAtFire:       plan.FreePctAtFire,
		TargetFreePct:       vol.TargetFreePercent,
		EstimatedFreedBytes: plan.EstimatedFreedBytes,
		StopReason:          plan.StopReason,
		Status:              "completed",
	}
	id, err := s.opts.Store.InsertRun(r.Context(), run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "persisting run: "+err.Error())
		return
	}
	if err := s.opts.Store.InsertRunItems(r.Context(), id, plan.Items); err != nil {
		writeError(w, http.StatusInternalServerError, "persisting items: "+err.Error())
		return
	}
	run.ID = id
	if mode == triagearr.RunModeLive && s.opts.Actor != nil {
		if err := s.opts.Actor.Execute(r.Context(), id); err != nil {
			slog.Warn("actor execute failed", "run_id", id, "err", err)
		} else {
			if refreshed, items, err := s.opts.Store.GetRun(r.Context(), id); err == nil {
				writeJSON(w, http.StatusOK, buildResponse(refreshed, items))
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, buildResponse(run, plan.Items))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r.URL.Query(), "limit", 50, 1, 500)
	rows, err := s.opts.Store.ListRuns(r.Context(), store.ListRunsOpts{Limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]runResponse, len(rows))
	for i, r := range rows {
		out[i] = buildResponse(r, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	run, items, err := s.opts.Store.GetRun(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildResponse(run, items))
}

// resolveVolume returns the requested volume, or — when none is named —
// picks the first configured one.
func (s *Server) resolveVolume(name string) (decider.Volume, bool) {
	if name != "" {
		return s.opts.Volume(name)
	}
	all := s.opts.Volumes()
	if len(all) == 0 {
		return decider.Volume{}, false
	}
	return all[0], true
}

func buildResponse(r triagearr.Run, items []triagearr.RunItem) runResponse {
	out := runResponse{
		RunID:               r.ID,
		TriggeredBy:         string(r.TriggeredBy),
		TriggeredAt:         r.TriggeredAt,
		Mode:                r.Mode,
		Volume:              r.VolumeName,
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
