package server

import (
	"context"
	"net/http"
	"time"
)

type volumeView struct {
	Name                 string     `json:"name"`
	Path                 string     `json:"path"`
	TargetFreePercent    float64    `json:"target_free_percent,omitempty"`
	ThresholdFreePercent float64    `json:"threshold_free_percent,omitempty"`
	TotalBytes           uint64     `json:"total_bytes,omitempty"`
	UsedBytes            uint64     `json:"used_bytes,omitempty"`
	FreeBytes            uint64     `json:"free_bytes,omitempty"`
	FreePercent          float64    `json:"free_percent,omitempty"`
	MeasuredAt           *time.Time `json:"measured_at,omitempty"`
}

func (s *Server) handleVolume(w http.ResponseWriter, r *http.Request) {
	vv, err := s.buildVolumeView(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"volume": vv})
}

func (s *Server) buildVolumeView(ctx context.Context) (volumeView, error) {
	latest, err := s.opts.Store.LatestDiskUsage(ctx)
	if err != nil {
		return volumeView{}, err
	}
	var vv volumeView
	if s.opts.Config != nil {
		v := s.opts.Config.Volume
		vv = volumeView{
			Name: v.Name, Path: v.Path,
			TargetFreePercent:    v.DiskPressure.TargetFreePercent,
			ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
		}
	}
	if latest != nil {
		vv.Path = latest.Path
		vv.TotalBytes = latest.TotalBytes
		vv.UsedBytes = latest.UsedBytes
		vv.FreeBytes = latest.FreeBytes
		vv.FreePercent = latest.FreePercent
		t := latest.Timestamp
		vv.MeasuredAt = &t
	}
	return vv, nil
}

type volumeHistoryPoint struct {
	Timestamp   time.Time `json:"ts"`
	TotalBytes  int64     `json:"total_bytes"`
	UsedBytes   int64     `json:"used_bytes"`
	FreeBytes   int64     `json:"free_bytes"`
	FreePercent float64   `json:"free_percent"`
}

func (s *Server) handleVolumeHistory(w http.ResponseWriter, r *http.Request) {
	since := sinceParam(r, 24*time.Hour)
	limit := intParam(r.URL.Query(), "limit", 2000, 1, 10000)
	pts, err := s.opts.Store.ListDiskUsageHistory(r.Context(), since, limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]volumeHistoryPoint, len(pts))
	for i, p := range pts {
		out[i] = volumeHistoryPoint{
			Timestamp: p.Timestamp, TotalBytes: p.TotalBytes,
			UsedBytes: p.UsedBytes, FreeBytes: p.FreeBytes,
			FreePercent: p.FreePercent,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}
