package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/triagearr"
	"github.com/Triagearr/Triagearr/internal/triggers"
)

// pressureRule builds the disk-pressure rule for the watched volume. The
// second return is false when disk pressure is disabled or has no threshold.
func pressureRule(cfg *config.Config) (triggers.VolumeRule, bool) {
	v := cfg.Volume
	if !v.DiskPressure.Enabled || v.DiskPressure.ThresholdFreePercent <= 0 {
		return triggers.VolumeRule{}, false
	}
	return triggers.VolumeRule{
		Name:                 v.Name,
		Path:                 v.Path,
		ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
		TargetFreePercent:    v.DiskPressure.TargetFreePercent,
	}, true
}

// theVolume is the single watched volume in the shape the Decider plans against.
func theVolume(cfg *config.Config) decider.Volume {
	v := cfg.Volume
	return decider.Volume{
		Name:              v.Name,
		Path:              v.Path,
		TargetFreePercent: v.DiskPressure.TargetFreePercent,
	}
}

func arrURLMap(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for _, pair := range []struct {
		typ  triagearr.ArrType
		inst config.ArrInstanceConfig
	}{
		{triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr},
		{triagearr.ArrTypeRadarr, cfg.Arrs.Radarr},
		{triagearr.ArrTypeLidarr, cfg.Arrs.Lidarr},
		{triagearr.ArrTypeWhisparrV2, cfg.Arrs.WhisparrV2},
		{triagearr.ArrTypeWhisparrV3, cfg.Arrs.WhisparrV3},
	} {
		out[pollers.URLKey(pair.typ)] = pair.inst.URL
	}
	return out
}

// enabledVolume builds the disk poller's view of the watched volume. The
// second return is false when disk pressure is disabled.
func enabledVolume(cfg *config.Config) (pollers.Volume, bool) {
	v := cfg.Volume
	if !v.DiskPressure.Enabled {
		return pollers.Volume{}, false
	}
	vol := pollers.Volume{Path: v.Path}
	if v.Source != "" {
		vol.Sample = httpDiskSampler(v.Source)
	}
	return vol, true
}

// httpDiskSampler returns a Sampler that fetches DiskUsage from a URL serving
// the fakedisk JSON shape. Used by dev configs (config.dev.yml) to drive the
// pressure trigger off a fake disk without touching a real filesystem.
func httpDiskSampler(url string) pollers.Sampler {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(ctx context.Context) (triagearr.DiskUsage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: %w", url, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: %w", url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: HTTP %d", url, resp.StatusCode)
		}
		var body struct {
			TotalBytes  uint64  `json:"total_bytes"`
			UsedBytes   uint64  `json:"used_bytes"`
			FreeBytes   uint64  `json:"free_bytes"`
			FreePercent float64 `json:"free_percent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: decode: %w", url, err)
		}
		return triagearr.DiskUsage{
			TotalBytes:  body.TotalBytes,
			UsedBytes:   body.UsedBytes,
			FreeBytes:   body.FreeBytes,
			FreePercent: body.FreePercent,
		}, nil
	}
}
