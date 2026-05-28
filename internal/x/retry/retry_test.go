package retry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
	"github.com/Triagearr/Triagearr/internal/x/retry"
)

// transient wraps an error so errors.Is(err, triagearr.ErrTransient) is true,
// matching how the clients mark retryable failures.
func transient(inner error) error {
	return fmt.Errorf("%w: %w", triagearr.ErrTransient, inner)
}

func TestDo(t *testing.T) {
	tests := []struct {
		name       string
		results    []error // one per attempt; op pops the next on each call
		opts       []retry.Option
		wantCalls  int
		wantSleeps int
		wantErr    bool
	}{
		{
			name:      "success first try",
			results:   []error{nil},
			wantCalls: 1,
		},
		{
			name:       "transient then success",
			results:    []error{transient(errors.New("503")), nil},
			wantCalls:  2,
			wantSleeps: 1,
		},
		{
			name:       "transient exhausts budget",
			results:    []error{transient(errors.New("a")), transient(errors.New("b")), transient(errors.New("c"))},
			wantCalls:  3, // default maxAttempts
			wantSleeps: 2, // no sleep after the final attempt
			wantErr:    true,
		},
		{
			name:      "hard failure returns immediately",
			results:   []error{errors.New("404")},
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:       "respects WithMaxAttempts",
			results:    []error{transient(errors.New("a")), transient(errors.New("b"))},
			opts:       []retry.Option{retry.WithMaxAttempts(2)},
			wantCalls:  2,
			wantSleeps: 1,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls, sleeps int
			results := tc.results
			opts := append([]retry.Option{retry.WithSleep(func(time.Duration) { sleeps++ })}, tc.opts...)

			err := retry.Do(context.Background(), func() error {
				err := results[calls]
				calls++
				return err
			}, opts...)

			require.Equal(t, tc.wantCalls, calls)
			require.Equal(t, tc.wantSleeps, sleeps)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDo_CancelledCtx_ShortCircuits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	err := retry.Do(ctx, func() error {
		calls++
		return transient(errors.New("never reached"))
	}, retry.WithSleep(func(time.Duration) {}))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls, "op must not run once ctx is already cancelled")
}

func TestDo_BackoffSchedule(t *testing.T) {
	var delays []time.Duration
	_ = retry.Do(context.Background(), func() error {
		return transient(errors.New("x"))
	},
		retry.WithSleep(func(d time.Duration) { delays = append(delays, d) }),
		retry.WithMaxAttempts(4),
		retry.WithBaseDelay(100*time.Millisecond),
		retry.WithMaxDelay(250*time.Millisecond),
	)
	// 3 sleeps for 4 attempts; base doubles (100,200,400) and is capped at 250.
	// Jitter adds up to half the pre-cap delay, so assert lower/upper bounds.
	require.Len(t, delays, 3)
	require.GreaterOrEqual(t, delays[0], 100*time.Millisecond)
	require.Less(t, delays[0], 150*time.Millisecond)
	require.GreaterOrEqual(t, delays[1], 200*time.Millisecond)
	require.Less(t, delays[1], 300*time.Millisecond)
	require.GreaterOrEqual(t, delays[2], 250*time.Millisecond) // capped base + jitter
}
