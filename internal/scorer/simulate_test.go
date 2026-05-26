package scorer

import (
	"testing"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func defaultSimInput() SimInput {
	return SimInput{
		Weights: config.ScoringWeights{
			RatioObligationMet: 50,
			UploadVelocityInv:  30,
			AgeDays:            0.1,
			SeedersLowGuard:    -1000,
			SwarmHealthBonus:   5,
			TrackerDeadBonus:   40,
		},
		HnRWindowDays:    14,
		TrackerDeadGrace: 7 * 24 * time.Hour,
		Defaults:         triagearr.ScoringDefaults{MinRatio: 1.0, MinSeedDays: 30, RareThreshold: 3},
	}
}

func resultByName(t *testing.T, res []SimResult, name string) SimResult {
	t.Helper()
	for _, r := range res {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("archetype %q not found in simulation results", name)
	return SimResult{}
}

func factorByName(t *testing.T, b Breakdown, name string) Factor {
	t.Helper()
	for _, f := range b.Factors {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("factor %q not found", name)
	return Factor{}
}

func TestSimulateArchetypeGatesAndSigns(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	res := simulateAt(defaultSimInput(), now)

	tests := []struct {
		archetype    string
		dominantGate string // gate expected on a specific factor
		gatedFactor  string
		wantPositive bool // overall score sign expectation
		checkVetoNeg bool // expect a large negative score (protected)
	}{
		{archetype: "public_well_seeded", gatedFactor: FactorRatioObligation, dominantGate: GatePublic, wantPositive: true},
		{archetype: "private_obligation_met", wantPositive: true},
		{archetype: "private_in_hnr_window", checkVetoNeg: true},
		{archetype: "rare_content", checkVetoNeg: true},
		{archetype: "dead_tracker_library", gatedFactor: FactorRatioObligation, dominantGate: GateAllDead, wantPositive: true},
	}

	for _, tc := range tests {
		t.Run(tc.archetype, func(t *testing.T) {
			r := resultByName(t, res, tc.archetype)
			if tc.dominantGate != "" {
				f := factorByName(t, r.Breakdown, tc.gatedFactor)
				if f.Gate != tc.dominantGate {
					t.Errorf("factor %s gate = %q, want %q", tc.gatedFactor, f.Gate, tc.dominantGate)
				}
				if f.Contribution != 0 {
					t.Errorf("gated factor %s contribution = %v, want 0", tc.gatedFactor, f.Contribution)
				}
			}
			if tc.checkVetoNeg && r.Breakdown.Score > -100 {
				t.Errorf("%s score = %v, want strongly negative (protected)", tc.archetype, r.Breakdown.Score)
			}
			if tc.wantPositive && r.Breakdown.Score <= 0 {
				t.Errorf("%s score = %v, want positive", tc.archetype, r.Breakdown.Score)
			}
		})
	}
}

func TestSimulateDeadTrackerBonusFires(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	res := simulateAt(defaultSimInput(), now)
	dead := resultByName(t, res, "dead_tracker_library")
	f := factorByName(t, dead.Breakdown, FactorTrackerDead)
	if f.Contribution != 40 {
		t.Errorf("tracker_dead_bonus contribution = %v, want 40", f.Contribution)
	}
}

// TestSimulateMinRatioFlipsObligation proves a threshold change (not just a
// weight) changes the factor value: relaxing min_ratio below the unmet
// archetype's ratio turns Factor 1 on.
func TestSimulateMinRatioFlipsObligation(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	strict := defaultSimInput() // min_ratio 1.0, min_seed_days 30
	unmet := factorByName(t, resultByName(t, simulateAt(strict, now), "private_obligation_unmet").Breakdown, FactorRatioObligation)
	if unmet.Contribution != 0 {
		t.Fatalf("under strict defaults, obligation factor should be 0, got %v", unmet.Contribution)
	}

	relaxed := defaultSimInput()
	relaxed.Defaults.MinRatio = 0.1
	relaxed.Defaults.MinSeedDays = 5
	met := factorByName(t, resultByName(t, simulateAt(relaxed, now), "private_obligation_unmet").Breakdown, FactorRatioObligation)
	if met.Contribution != relaxed.Weights.RatioObligationMet {
		t.Errorf("after relaxing thresholds, obligation factor = %v, want %v", met.Contribution, relaxed.Weights.RatioObligationMet)
	}
}
