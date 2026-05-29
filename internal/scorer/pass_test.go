package scorer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestScorePass_EmptyStore(t *testing.T) {
	s := openTestStore(t)
	sc := scorer.New(scorer.Options{
		Cfg:   testConfig(),
		Store: s,
		Now:   func() time.Time { return time.Now().UTC() },
	})
	// ScorePass wraps ScoreAll + logs the summary; an empty store is a valid
	// no-op pass that must not error.
	require.NoError(t, sc.ScorePass(context.Background()))
}

func TestSimulate_PublicWrapper(t *testing.T) {
	// Simulate is the now()-anchored wrapper around simulateAt; it scores a
	// fixed set of archetypes with the supplied weights and must return them.
	res := scorer.Simulate(scorer.SimInput{
		Weights: config.ScoringWeights{
			RatioObligationMet: 50, UploadVelocityInv: 30, AgeDays: 0.1,
			SeedersLowGuard: -1000, SwarmHealthBonus: 5, TrackerDeadBonus: 40,
		},
		HnRWindowDays:    14,
		TrackerDeadGrace: 7 * 24 * time.Hour,
		Defaults:         triagearr.ScoringDefaults{MinRatio: 1.0, MinSeedDays: 30, RareThreshold: 3},
	})
	require.NotEmpty(t, res)
}

func TestLoop_Name(t *testing.T) {
	require.Equal(t, "scorer", (&scorer.Loop{}).Name())
}

func TestLoop_ScoreErrorIsLoggedNotFatal(t *testing.T) {
	sig := make(chan struct{}, 1)
	called := make(chan struct{}, 4)
	loop := &scorer.Loop{
		Score: func(context.Context) error {
			called <- struct{}{}
			return errors.New("boom")
		},
		Signal:   sig,
		Debounce: time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	sig <- struct{}{}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("a failing Score must still be invoked")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after cancel")
	}
}
