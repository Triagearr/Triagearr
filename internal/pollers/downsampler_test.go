package pollers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeMaintStore records calls and optionally injects errors per step.
type fakeMaintStore struct {
	downsampleCalls  int
	downsampleBefore time.Time
	retentionCalls   int
	rawHorizon       time.Duration
	dailyHorizon     time.Duration
	pruneCalls       int
	pruneOlderThan   time.Duration
	vacuumCalls      int
	vacuumMin        int64
	checkpointCalls  int
	optimizeCalls    int

	errDownsample error
	errRetention  error
	errPrune      error
	errVacuum     error
}

func (f *fakeMaintStore) DownsampleRange(_ context.Context, before time.Time) (int, int, error) {
	f.downsampleCalls++
	f.downsampleBefore = before
	return 1, 2, f.errDownsample
}

func (f *fakeMaintStore) EnforceRetention(_ context.Context, raw, daily time.Duration) (int, int, error) {
	f.retentionCalls++
	f.rawHorizon, f.dailyHorizon = raw, daily
	return 3, 4, f.errRetention
}

func (f *fakeMaintStore) PruneStaleTorrents(_ context.Context, olderThan time.Duration) (int, error) {
	f.pruneCalls++
	f.pruneOlderThan = olderThan
	return 5, f.errPrune
}

func (f *fakeMaintStore) Vacuum(_ context.Context, minReclaim int64) (bool, int64, error) {
	f.vacuumCalls++
	f.vacuumMin = minReclaim
	return true, 1024, f.errVacuum
}

func (f *fakeMaintStore) CheckpointWAL(_ context.Context) error {
	f.checkpointCalls++
	return nil
}

func (f *fakeMaintStore) Optimize(_ context.Context) error {
	f.optimizeCalls++
	return nil
}

func fullConfig() MaintenanceConfig {
	return MaintenanceConfig{
		Schedule:              "0 3 * * *",
		DownsampleAge:         48 * time.Hour,
		RawRetention:          7 * 24 * time.Hour,
		DailyRetention:        365 * 24 * time.Hour,
		TorrentRetention:      7 * 24 * time.Hour,
		VacuumEnabled:         true,
		VacuumMinReclaimBytes: 4096,
	}
}

func TestMaintenance_runOnce_RunsEveryStep(t *testing.T) {
	fs := &fakeMaintStore{}
	cfg := fullConfig()
	m := &Maintenance{Store: fs, Config: cfg}

	m.runOnce(context.Background())

	require.Equal(t, 1, fs.downsampleCalls)
	require.Equal(t, 1, fs.retentionCalls)
	require.Equal(t, 1, fs.pruneCalls)
	require.Equal(t, 1, fs.vacuumCalls)
	require.Equal(t, 1, fs.checkpointCalls)
	require.Equal(t, 1, fs.optimizeCalls)

	// Args are derived from config.
	require.WithinDuration(t, time.Now().UTC().Add(-cfg.DownsampleAge), fs.downsampleBefore, 5*time.Second)
	require.Equal(t, cfg.RawRetention, fs.rawHorizon)
	require.Equal(t, cfg.DailyRetention, fs.dailyHorizon)
	require.Equal(t, cfg.TorrentRetention, fs.pruneOlderThan)
	require.Equal(t, cfg.VacuumMinReclaimBytes, fs.vacuumMin)
}

func TestMaintenance_runOnce_SkipsPruneWhenRetentionZero(t *testing.T) {
	fs := &fakeMaintStore{}
	cfg := fullConfig()
	cfg.TorrentRetention = 0
	m := &Maintenance{Store: fs, Config: cfg}

	m.runOnce(context.Background())

	require.Zero(t, fs.pruneCalls, "TorrentRetention=0 disables pruning")
}

func TestMaintenance_runOnce_SkipsVacuumWhenDisabled(t *testing.T) {
	fs := &fakeMaintStore{}
	cfg := fullConfig()
	cfg.VacuumEnabled = false
	m := &Maintenance{Store: fs, Config: cfg}

	m.runOnce(context.Background())

	require.Zero(t, fs.vacuumCalls, "VacuumEnabled=false disables vacuum")
}

func TestMaintenance_runOnce_SwallowsErrorsAndContinues(t *testing.T) {
	// Every step errors, yet runOnce must attempt all of them — a failed
	// downsample must not skip retention/prune/vacuum.
	fs := &fakeMaintStore{
		errDownsample: errors.New("d"),
		errRetention:  errors.New("r"),
		errPrune:      errors.New("p"),
		errVacuum:     errors.New("v"),
	}
	m := &Maintenance{Store: fs, Config: fullConfig()}

	require.NotPanics(t, func() { m.runOnce(context.Background()) })
	require.Equal(t, 1, fs.downsampleCalls)
	require.Equal(t, 1, fs.retentionCalls)
	require.Equal(t, 1, fs.pruneCalls)
	require.Equal(t, 1, fs.vacuumCalls)
}

func TestMaintenance_Run_BadScheduleErrors(t *testing.T) {
	m := &Maintenance{Store: &fakeMaintStore{}, Config: MaintenanceConfig{Schedule: "not a cron"}}
	err := m.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "downsample_cron")
}

func TestMaintenance_Run_CancelledCtxReturnsNil(t *testing.T) {
	fs := &fakeMaintStore{}
	m := &Maintenance{Store: fs, Config: fullConfig()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, m.Run(ctx))
	require.Zero(t, fs.downsampleCalls, "no maintenance run before the first scheduled tick")
}

func TestMaintenance_Name(t *testing.T) {
	require.Equal(t, "maintenance", (&Maintenance{}).Name())
}
