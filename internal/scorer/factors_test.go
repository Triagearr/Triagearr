package scorer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// fixedNow anchors every table-driven test so seedStart/age math is deterministic.
var fixedNow = time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

func ptr(t time.Time) *time.Time { return &t }

func TestFactor_RatioObligation(t *testing.T) {
	const w = 50.0
	policy := config.TrackerPolicy{MinSeedDays: 7, MinRatio: 1.0}

	cases := []struct {
		name   string
		t      store.ScoringTorrent
		ratio  float64
		policy config.TrackerPolicy
		want   Factor
	}{
		{
			name:  "public_gates_to_zero",
			t:     store.ScoringTorrent{Hash: "h", Private: false, AddedOn: fixedNow.Add(-30 * 24 * time.Hour)},
			ratio: 5.0,
			want:  Factor{Name: FactorRatioObligation, Weight: w, Gate: GatePublic},
		},
		{
			name:   "private_obligation_met",
			t:      store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-30 * 24 * time.Hour)},
			ratio:  1.5,
			policy: policy,
			want:   Factor{Name: FactorRatioObligation, Value: 1.0, Weight: w, Contribution: w},
		},
		{
			name:   "private_ratio_unmet",
			t:      store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-30 * 24 * time.Hour)},
			ratio:  0.5,
			policy: policy,
			want:   Factor{Name: FactorRatioObligation, Weight: w},
		},
		{
			name:   "private_seed_days_short",
			t:      store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-3 * 24 * time.Hour)},
			ratio:  2.0,
			policy: policy,
			want:   Factor{Name: FactorRatioObligation, Weight: w},
		},
		{
			name:   "completion_on_used_when_set",
			t:      store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-30 * 24 * time.Hour), CompletionOn: ptr(fixedNow.Add(-3 * 24 * time.Hour))},
			ratio:  2.0,
			policy: policy,
			want:   Factor{Name: FactorRatioObligation, Weight: w},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := factorRatioObligation(c.t, c.ratio, c.policy, fixedNow, w)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFactor_VelocityInv(t *testing.T) {
	const w = 30.0
	cases := []struct {
		name             string
		velocity, global float64
		want             Factor
	}{
		{"global_zero_inert", 0, 0, Factor{Name: FactorVelocityInv, Weight: w, Gate: GateNoSwarmSignal}},
		{"zero_velocity_full_bonus", 0, 1_000_000, Factor{Name: FactorVelocityInv, Value: 1.0, Weight: w, Contribution: w}},
		{"average_neutralised", 500_000, 1_000_000, Factor{Name: FactorVelocityInv, Value: 0.5, Weight: w, Contribution: 0.5 * w}},
		{"above_average_clamped", 5_000_000, 1_000_000, Factor{Name: FactorVelocityInv, Value: 0, Weight: w}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := factorVelocityInv(c.velocity, c.global, w)
			require.InDelta(t, c.want.Contribution, got.Contribution, 1e-9)
			require.Equal(t, c.want.Gate, got.Gate)
		})
	}
}

func TestFactor_Age(t *testing.T) {
	tor := store.ScoringTorrent{Hash: "h", AddedOn: fixedNow.Add(-180 * 24 * time.Hour)}
	got := factorAge(tor, fixedNow, 0.1)
	require.InDelta(t, 180.0, got.Value, 1e-9)
	require.InDelta(t, 18.0, got.Contribution, 1e-9)
}

func TestFactor_SeedersGuard(t *testing.T) {
	const w = -1000.0
	cases := []struct {
		name      string
		seeders   float64
		threshold int
		alive     bool
		want      Factor
	}{
		{"rare_and_alive_vetoes", 2, 3, true, Factor{Name: FactorSeedersGuard, Value: 1, Weight: w, Contribution: w}},
		{"rare_but_all_dead_degraded", 0, 3, false, Factor{Name: FactorSeedersGuard, Weight: w, Gate: GateAllDead}},
		{"above_threshold_inert", 10, 3, true, Factor{Name: FactorSeedersGuard, Weight: w}},
		{"equal_threshold_triggers", 3, 3, true, Factor{Name: FactorSeedersGuard, Value: 1, Weight: w, Contribution: w}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := factorSeedersGuard(c.seeders, c.threshold, c.alive, w)
			require.Equal(t, c.want, got)
		})
	}
}

func TestFactor_HnRVeto(t *testing.T) {
	freshPrivate := store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-3 * 24 * time.Hour)}
	oldPrivate := store.ScoringTorrent{Hash: "h", Private: true, AddedOn: fixedNow.Add(-30 * 24 * time.Hour)}
	freshPublic := store.ScoringTorrent{Hash: "h", Private: false, AddedOn: fixedNow.Add(-3 * 24 * time.Hour)}

	cases := []struct {
		name     string
		t        store.ScoringTorrent
		alive    bool
		window   int
		wantVeto bool
		wantGate string
	}{
		{"in_window_alive_vetoes", freshPrivate, true, 14, true, ""},
		{"in_window_all_dead_degraded", freshPrivate, false, 14, false, GateAllDead},
		{"out_of_window_inert", oldPrivate, true, 14, false, ""},
		{"public_inert", freshPublic, true, 14, false, GatePublic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := factorHnRVeto(c.t, c.alive, c.window, fixedNow)
			require.Equal(t, c.wantGate, got.Gate)
			if c.wantVeto {
				require.Equal(t, HnRVetoWeight, got.Contribution)
			} else {
				require.Equal(t, 0.0, got.Contribution)
			}
		})
	}
}

func TestFactor_TrackerDead(t *testing.T) {
	const w = 40.0
	const grace = 7 * 24 * time.Hour
	old := fixedNow.Add(-30 * 24 * time.Hour)
	recent := fixedNow.Add(-time.Hour)

	sustained := []trackerView{
		{Host: "a", Status: triagearr.TrackerNotWorking, FirstSeenDead: &old},
	}
	mixed := []trackerView{
		{Host: "a", Status: triagearr.TrackerNotWorking, FirstSeenDead: &old},
		{Host: "b", Status: triagearr.TrackerWorking},
	}
	recentDead := []trackerView{
		{Host: "a", Status: triagearr.TrackerNotWorking, FirstSeenDead: &recent},
	}
	// status=4 observed but first_seen_dead never recorded (e.g. pre-0007 row
	// that wasn't backfilled because last_checked was missing): treat as
	// not-yet-sustained.
	deadNoTimestamp := []trackerView{
		{Host: "a", Status: triagearr.TrackerNotWorking, FirstSeenDead: nil},
	}

	require.Equal(t, w, factorTrackerDead(sustained, fixedNow, grace, w).Contribution)
	require.Equal(t, 0.0, factorTrackerDead(mixed, fixedNow, grace, w).Contribution)
	require.Equal(t, 0.0, factorTrackerDead(recentDead, fixedNow, grace, w).Contribution)
	require.Equal(t, 0.0, factorTrackerDead(deadNoTimestamp, fixedNow, grace, w).Contribution)
	require.Equal(t, 0.0, factorTrackerDead(nil, fixedNow, grace, w).Contribution)
}

func TestAnyTrackerAlive(t *testing.T) {
	require.True(t, anyTrackerAlive(nil), "no-tracker torrent must default to alive (conservative)")
	require.True(t, anyTrackerAlive([]trackerView{
		{Status: triagearr.TrackerNotWorking}, {Status: triagearr.TrackerWorking},
	}))
	require.False(t, anyTrackerAlive([]trackerView{{Status: triagearr.TrackerNotWorking}}))
	require.True(t, anyTrackerAlive([]trackerView{{Status: triagearr.TrackerDisabled}}), "disabled is a user choice, not death")
}

func TestTrackerPolicyFor_StrictestWins(t *testing.T) {
	threshold5 := 5
	threshold2 := 2
	cfg := config.ScoringConfig{
		PerTracker: map[string]config.TrackerPolicy{
			"a": {MinSeedDays: 7, MinRatio: 1.0, RareThreshold: &threshold5},
			"b": {MinSeedDays: 14, MinRatio: 0.5, RareThreshold: &threshold2},
		},
	}
	got := trackerPolicyFor([]trackerView{{Host: "a"}, {Host: "b"}}, cfg)
	require.Equal(t, 14, got.MinSeedDays)
	require.Equal(t, 1.0, got.MinRatio)
	require.NotNil(t, got.RareThreshold)
	require.Equal(t, 2, *got.RareThreshold, "min(rare_threshold) is strictest")
}

func TestEvaluateExclusions(t *testing.T) {
	qb := config.QbitConfig{
		CategoryExclude: []string{"keep"},
		TagsExclude:     []string{"protected"},
	}
	arrs := config.ArrsConfig{
		Sonarr: []config.ArrInstanceConfig{{Name: "main", TagsExclude: []string{"favourite"}}},
	}
	tor := store.ScoringTorrent{
		Hash: "h", Category: "keep", Tags: "hd,protected",
	}
	linked := []store.LinkedMedia{
		{ArrName: "main", ArrType: string(triagearr.ArrTypeSonarr), MediaID: 1, Tags: "favourite,other"},
	}
	reasons := evaluateExclusions(tor, linked, qb, arrs)
	require.Contains(t, reasons, "qbit_category:keep")
	require.Contains(t, reasons, "qbit_tag:protected")
	require.Contains(t, reasons, "arr_tag:sonarr/main:favourite")

	noReason := evaluateExclusions(store.ScoringTorrent{Hash: "h2"}, nil, qb, arrs)
	require.Empty(t, noReason)
}
