package runlock_test

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/runlock"
)

func TestLock_AcquireReleaseCycle(t *testing.T) {
	l := runlock.New()

	require.True(t, l.TryAcquire(), "first acquire succeeds")
	require.False(t, l.TryAcquire(), "second acquire fails while held")

	l.Release()
	require.True(t, l.TryAcquire(), "acquire succeeds again after release")
	l.Release()
}

// A file-backed lock excludes a second, independent Lock on the same path —
// the daemon-vs-CLI scenario, where the two processes each open their own
// handle to ${data_dir}/run.lock.
func TestLock_FileBackedCrossInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")

	a, err := runlock.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })

	b, err := runlock.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	require.True(t, a.TryAcquire(), "first instance acquires the file lock")
	require.False(t, b.TryAcquire(), "second instance is blocked while the first holds it")

	a.Release()
	require.True(t, b.TryAcquire(), "second instance acquires once the first releases")
	b.Release()
}

func TestOpen_CreatesLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.lock")
	l, err := runlock.Open(path)
	require.NoError(t, err)
	require.NoError(t, l.Close())
	require.FileExists(t, path)
}

func TestLock_RequestStop(t *testing.T) {
	l := runlock.New()
	require.True(t, l.TryAcquire())

	var stopped atomic.Bool
	l.Arm(7, func() { stopped.Store(true) })

	require.False(t, l.RequestStop(99), "wrong run id does not cancel")
	require.False(t, stopped.Load())

	require.True(t, l.RequestStop(7), "matching run id cancels")
	require.True(t, stopped.Load())
}

// Release clears the armed run so a stale stop after a finished run is a no-op.
func TestLock_RequestStopAfterRelease(t *testing.T) {
	l := runlock.New()
	require.True(t, l.TryAcquire())

	var stopped atomic.Bool
	l.Arm(7, func() { stopped.Store(true) })
	l.Release()

	require.False(t, l.RequestStop(7), "no run is armed after release")
	require.False(t, stopped.Load())
}

// Under -race, exactly one of many concurrent TryAcquire calls may win.
func TestLock_ConcurrentSingleWinner(t *testing.T) {
	l := runlock.New()
	const goroutines = 50

	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if l.TryAcquire() {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(1), wins.Load(), "exactly one goroutine may hold the slot")
}
