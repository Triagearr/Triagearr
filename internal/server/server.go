// Package server exposes Triagearr's HTTP API. In M4 the surface is minimal:
//   - POST /api/v1/runs       — trigger a dry-run Decider pass and persist it
//   - GET  /api/v1/runs       — list recent runs
//   - GET  /api/v1/runs/{id}  — fetch a run + its items
//
// Auth is a single X-API-Key header compared in constant time. M6 will layer
// rate-limiting and the static UI on top.
package server

import (
	"context"
	"crypto/subtle"
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

// VolumeLookup resolves a volume name to the rule M4 needs for a Decider plan.
// The daemon supplies a closure over the parsed config.
type VolumeLookup func(name string) (decider.Volume, bool)

// Options bundles everything the server needs at construction time.
type Options struct {
	Bind    string
	APIKey  string
	Store   *store.Store
	Decider *decider.Decider
	// Volume resolves a name to the {path, target%, max_run_size_gb} that
	// the Decider expects. Must return ok=false for unknown volumes.
	Volume VolumeLookup
	// Volumes returns every configured volume (used when the caller omits the
	// volume name — picks the most pressed one).
	Volumes func() []decider.Volume
}

// Server is a wired HTTP server ready to be Started.
type Server struct {
	opts Options
	srv  *http.Server
}

// New builds a Server. Does not start listening.
func New(opts Options) *Server {
	s := &Server{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/runs", s.auth(s.handlePostRun))
	mux.HandleFunc("GET /api/v1/runs", s.auth(s.handleListRuns))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.auth(s.handleGetRun))
	mux.HandleFunc("GET /healthz", s.handleHealth)
	s.srv = &http.Server{
		Addr:              opts.Bind,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Handler exposes the wired http.Handler. Useful for httptest-driven tests.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Start serves until ctx is cancelled, then shuts down with a 5s grace.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "bind", s.opts.Bind)
		err := s.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid X-API-Key")
			return
		}
		h(w, r)
	}
}

type postRunRequest struct {
	Volume string `json:"volume,omitempty"`
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
	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerHTTP,
		TriggeredAt:         time.Now().UTC(),
		Mode:                "dry-run",
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
	writeJSON(w, http.StatusOK, buildResponse(run, plan.Items))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit must be 1..500")
			return
		}
		limit = n
	}
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

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resolveVolume returns the requested volume, or — when none is named —
// picks the most-pressed one from the configured set (lowest free%).
func (s *Server) resolveVolume(name string) (decider.Volume, bool) {
	if name != "" {
		return s.opts.Volume(name)
	}
	all := s.opts.Volumes()
	if len(all) == 0 {
		return decider.Volume{}, false
	}
	// Without a disk snapshot here, fall back to the first configured volume.
	// Callers wanting smart selection should pass an explicit name.
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
