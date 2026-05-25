package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

const baseYAML = `
mode: dry-run
http:
  bind: "127.0.0.1:9494"
storage:
  sqlite_path: /tmp/triagearr-test.db
arrs:
  sonarr:
    enabled: true
    url: http://sonarr:8989
    api_key: test-key
    poll: true
    act: false
torrent_clients:
  qbittorrent:
    enabled: true
    url: http://qbit:8090
    username: ""
    password: ""
volume:
  name: media
  path: /tmp
  disk_pressure:
    enabled: true
    threshold_free_percent: 15
    target_free_percent: 25
    max_run_size_gb: 50
scoring:
  weights:
    ratio_obligation_met: 50
    upload_velocity_inv: 30
    age_days: 0.1
    seeders_low_guard: -1000
    swarm_health_bonus: 5
  rare_content_threshold: 3
  hnr_window_days: 14
polling:
  qbit_interval: 30m
  arr_interval: 1h
  arr_file_min_interval: 200ms
  tracker_interval: 6h
  disk_interval: 5m
  maintainerr_interval: 1h
  downsample_cron: "0 3 * * *"
`

func writeBaseYAML(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(p, []byte(baseYAML), 0o600))
	return p
}

func TestLoadWithOverrides_NoOverrides_MatchesLoad(t *testing.T) {
	p := writeBaseYAML(t)
	base, err := config.Load(p)
	require.NoError(t, err)
	merged, err := config.LoadWithOverrides(p, nil)
	require.NoError(t, err)
	require.Equal(t, base.Scoring.HnRWindowDays, merged.Scoring.HnRWindowDays)
	require.Equal(t, base.Polling.QbitInterval, merged.Polling.QbitInterval)
}

func TestLoadWithOverrides_ScalarOverride(t *testing.T) {
	p := writeBaseYAML(t)
	cfg, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "scoring.hnr_window_days", ValueJSON: `21`},
	})
	require.NoError(t, err)
	require.Equal(t, 21, cfg.Scoring.HnRWindowDays)
}

func TestLoadWithOverrides_NestedWeight(t *testing.T) {
	p := writeBaseYAML(t)
	cfg, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "scoring.weights.ratio_obligation_met", ValueJSON: `100`},
	})
	require.NoError(t, err)
	require.InDelta(t, 100.0, cfg.Scoring.Weights.RatioObligationMet, 0.001)
}

func TestLoadWithOverrides_DurationOverride(t *testing.T) {
	p := writeBaseYAML(t)
	cfg, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "polling.qbit_interval", ValueJSON: `"5m"`},
	})
	require.NoError(t, err)
	require.Equal(t, "5m0s", cfg.Polling.QbitInterval.String())
}

func TestLoadWithOverrides_InvalidJSON(t *testing.T) {
	p := writeBaseYAML(t)
	_, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "scoring.hnr_window_days", ValueJSON: `not-json`},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid JSON")
}

func TestLoadWithOverrides_InvalidValueFailsValidation(t *testing.T) {
	p := writeBaseYAML(t)
	// threshold > target is rejected by Validate
	_, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "volume.disk_pressure.threshold_free_percent", ValueJSON: `90`},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "threshold_free_percent")
}

func TestLoadWithOverrides_VolumeDiskPressure(t *testing.T) {
	p := writeBaseYAML(t)
	cfg, err := config.LoadWithOverrides(p, []config.Override{
		{Key: "volume.disk_pressure.threshold_free_percent", ValueJSON: `10`},
		{Key: "volume.disk_pressure.target_free_percent", ValueJSON: `20`},
	})
	require.NoError(t, err)
	// Overriding a nested leaf must leave the volume's other fields intact.
	require.Equal(t, "media", cfg.Volume.Name)
	require.Equal(t, "/tmp", cfg.Volume.Path)
	require.InDelta(t, 10.0, cfg.Volume.DiskPressure.ThresholdFreePercent, 0.001)
	require.InDelta(t, 20.0, cfg.Volume.DiskPressure.TargetFreePercent, 0.001)
	require.Equal(t, 50, cfg.Volume.DiskPressure.MaxRunSizeGB)
}

func TestIsEditableKey(t *testing.T) {
	editable := []string{
		"scoring.hnr_window_days",
		"scoring.weights.ratio_obligation_met",
		"polling.qbit_interval",
		"volume.disk_pressure.threshold_free_percent",
	}
	forbidden := []string{
		"mode",
		"arrs.sonarr.act",
		"arrs.sonarr.api_key",
		"qbit.password",
		"http.bind",
		"storage.sqlite_path",
		// volume.{path,name,source} are boot-critical (preflight reads them):
		// they must stay YAML-only so a hot-reload can't rebind the watched
		// mount to an out-of-scope path.
		"volume.path",
		"volume.name",
		"volume.source",
	}
	for _, k := range editable {
		require.True(t, config.IsEditableKey(k), "expected editable: %s", k)
	}
	for _, k := range forbidden {
		require.False(t, config.IsEditableKey(k), "expected forbidden: %s", k)
	}
}
