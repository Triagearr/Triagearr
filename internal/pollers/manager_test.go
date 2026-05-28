package pollers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
)

// scriptedPoller blocks until ctx is cancelled, then returns the configured
// error. ran flips to true once Run is entered.
type scriptedPoller struct {
	name string
	err  error
	ran  atomic.Bool
}

func (p *scriptedPoller) Name() string { return p.name }

func (p *scriptedPoller) Run(ctx context.Context) error {
	p.ran.Store(true)
	<-ctx.Done()
	return p.err
}

func TestManager_RunsAllPollersUntilCancel(t *testing.T) {
	a := &scriptedPoller{name: "a"}
	b := &scriptedPoller{name: "b"}
	m := pollers.NewManager(a, b)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	require.Eventually(t, func() bool { return a.ran.Load() && b.ran.Load() }, time.Second, 5*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "clean cancel → no aggregate error")
	case <-time.After(time.Second):
		t.Fatal("Manager.Run did not return after cancel")
	}
}

func TestManager_AggregatesErrorsAndIsolatesFailures(t *testing.T) {
	boom := errors.New("boom")
	failing := &scriptedPoller{name: "failing", err: boom}
	healthy := &scriptedPoller{name: "healthy"}
	m := pollers.NewManager(failing, healthy)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	require.Eventually(t, func() bool { return failing.ran.Load() && healthy.ran.Load() },
		time.Second, 5*time.Millisecond, "one poller's failure must not stop the others starting")

	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "failing", "error is tagged with the poller name")
	case <-time.After(time.Second):
		t.Fatal("Manager.Run did not return after cancel")
	}
}

func TestManager_NoPollers(t *testing.T) {
	require.NoError(t, pollers.NewManager().Run(context.Background()))
}
