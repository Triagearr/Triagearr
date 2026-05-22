package scorer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A poller signal drives exactly one scoring pass.
func TestLoopSignalTriggersPass(t *testing.T) {
	var calls atomic.Int32
	sig := make(chan struct{}, 1)
	loop := &Loop{
		Score:    func(context.Context) error { calls.Add(1); return nil },
		Signal:   sig,
		Debounce: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	sig <- struct{}{}
	require.Eventually(t, func() bool { return calls.Load() == 1 },
		time.Second, 5*time.Millisecond)

	cancel()
	<-done
}

// A burst of signals within the debounce window collapses into a single pass.
func TestLoopDebounceCoalesces(t *testing.T) {
	var calls atomic.Int32
	sig := make(chan struct{}, 1)
	loop := &Loop{
		Score:    func(context.Context) error { calls.Add(1); return nil },
		Signal:   sig,
		Debounce: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	for i := 0; i < 10; i++ {
		select {
		case sig <- struct{}{}:
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.Eventually(t, func() bool { return calls.Load() == 1 },
		time.Second, 5*time.Millisecond)
	// Give the loop room to (wrongly) fire a second pass.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), calls.Load(), "burst must coalesce to one pass")

	cancel()
	<-done
}

// Run returns promptly when the context is cancelled while idle.
func TestLoopExitsOnContextCancel(t *testing.T) {
	loop := &Loop{
		Score:    func(context.Context) error { return nil },
		Signal:   make(chan struct{}),
		Debounce: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit on context cancel")
	}
}

// Cancelling mid-debounce aborts the pending pass instead of running it.
func TestLoopCancelDuringDebounce(t *testing.T) {
	var calls atomic.Int32
	sig := make(chan struct{}, 1)
	loop := &Loop{
		Score:    func(context.Context) error { calls.Add(1); return nil },
		Signal:   sig,
		Debounce: time.Hour, // long enough that cancel lands mid-debounce
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = loop.Run(ctx); close(done) }()

	sig <- struct{}{}
	time.Sleep(20 * time.Millisecond) // let the loop enter the debounce wait
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit during debounce")
	}
	require.Equal(t, int32(0), calls.Load(), "no pass should run when cancelled mid-debounce")
}
