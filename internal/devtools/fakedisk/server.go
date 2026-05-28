// Package fakedisk is an in-memory fake disk-usage source for dev. It exposes
// per-volume free/total over HTTP so the daemon's disk poller can sample it
// instead of calling statfs(2) on a real path. This lets developers exercise
// the disk-pressure trigger path end-to-end (and live mode with act:true on
// the fake *arr/qBit) without touching a real disk.
//
// Wire format:
//
//	GET  /disk/{name}        -> {"total_bytes":N,"used_bytes":N,"free_bytes":N,"free_percent":F}
//	POST /disk/{name}        body {"total_bytes":N,"free_bytes":N} — replaces values
//	POST /disk/{name}/fill   body {"bytes":N} — subtracts N from free_bytes (simulates fill)
//	POST /disk/{name}/free   body {"bytes":N} — adds N to free_bytes (simulates cleanup)
//
// Unknown volume names return 404. Path traversal not possible — names are
// matched verbatim against the seeded map.
package fakedisk

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
)

// DiskInfo mirrors triagearr.DiskUsage on the wire (without Path, Timestamp —
// those are filled in by the daemon-side poller).
type DiskInfo struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	FreePercent float64 `json:"free_percent"`
}

// State holds per-volume disk values. All access is mutex-serialized.
type State struct {
	mu      sync.RWMutex
	volumes map[string]volumeState
}

type volumeState struct {
	total uint64
	free  uint64
}

// NewState builds an empty State.
func NewState() *State {
	return &State{volumes: make(map[string]volumeState)}
}

// Set seeds (or replaces) the values for a volume.
func (s *State) Set(name string, total, free uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if free > total {
		free = total
	}
	s.volumes[name] = volumeState{total: total, free: free}
}

// Get returns the current snapshot for a volume.
func (s *State) Get(name string) (DiskInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.volumes[name]
	if !ok {
		return DiskInfo{}, false
	}
	return toInfo(v), true
}

// Fill subtracts n bytes from free_bytes (clamped at zero). Returns the new
// info and false if the volume is unknown.
func (s *State) Fill(name string, n uint64) (DiskInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.volumes[name]
	if !ok {
		return DiskInfo{}, false
	}
	if n >= v.free {
		v.free = 0
	} else {
		v.free -= n
	}
	s.volumes[name] = v
	return toInfo(v), true
}

// Free adds n bytes to free_bytes (clamped at total).
func (s *State) Free(name string, n uint64) (DiskInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.volumes[name]
	if !ok {
		return DiskInfo{}, false
	}
	v.free += n
	if v.free > v.total {
		v.free = v.total
	}
	s.volumes[name] = v
	return toInfo(v), true
}

func toInfo(v volumeState) DiskInfo {
	used := v.total - v.free
	var pct float64
	if v.total > 0 {
		pct = 100.0 * float64(v.free) / float64(v.total)
	}
	return DiskInfo{
		TotalBytes:  v.total,
		UsedBytes:   used,
		FreeBytes:   v.free,
		FreePercent: pct,
	}
}

// Server exposes State over HTTP.
type Server struct {
	state  *State
	logger *slog.Logger
}

// Options configures a Server.
type Options struct {
	Logger *slog.Logger
}

// New constructs a Server with empty state. Seed via Server.State().Set.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{state: NewState(), logger: logger}
}

// State exposes the underlying state for seeding/asserts.
func (s *Server) State() *State { return s.state }

// Handler returns the http.Handler exposing the fake disk API.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /disk/{name}", s.handleGet)
	m.HandleFunc("POST /disk/{name}", s.handleSet)
	m.HandleFunc("POST /disk/{name}/fill", s.handleFill)
	m.HandleFunc("POST /disk/{name}/free", s.handleFree)
	return m
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, ok := s.state.Get(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, info)
}

type setRequest struct {
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body setRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.state.Set(name, body.TotalBytes, body.FreeBytes)
	s.logger.Info("fake-disk: set", "volume", name, "total", body.TotalBytes, "free", body.FreeBytes)
	info, _ := s.state.Get(name)
	writeJSON(w, info)
}

type deltaRequest struct {
	Bytes uint64 `json:"bytes"`
}

func (s *Server) handleFill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body deltaRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	info, ok := s.state.Fill(name, body.Bytes)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.logger.Info("fake-disk: fill", "volume", name, "bytes", body.Bytes, "new_free", info.FreeBytes)
	writeJSON(w, info)
}

func (s *Server) handleFree(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body deltaRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	info, ok := s.state.Free(name, body.Bytes)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.logger.Info("fake-disk: free", "volume", name, "bytes", body.Bytes, "new_free", info.FreeBytes)
	writeJSON(w, info)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
