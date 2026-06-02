// Package runlock provides a single-run guard ensuring at most one destructive
// run executes at a time across every trigger (HTTP, disk pressure, CLI).
//
// It layers two mechanisms behind one TryAcquire/Release pair:
//
//   - a capacity-1 channel — serializes the in-daemon goroutines (the HTTP
//     handler and the disk-pressure watcher share one Lock instance);
//   - an optional OS file lock (flock) — excludes the separate `triagearr run
//     --live` process, which an in-memory channel cannot see.
//
// flock is released by the kernel when the holding process dies, so a crashed
// run never leaves a stale lock behind. Acquisition is always non-blocking:
// callers fail fast (HTTP 409, pressure skip, CLI error) rather than queue.
package runlock

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// Lock is a non-reentrant, capacity-1 guard. A file-backed Lock (via Open) also
// excludes other processes locking the same path; a memory-only Lock (via New)
// guards a single process. The zero value is unusable — construct with New or
// Open.
type Lock struct {
	ch   chan struct{}
	file *os.File // nil for memory-only locks

	// mu guards the active-run registry below. Arm records the in-flight run's
	// id and its cancel func so RequestStop can interrupt it cleanly; both are
	// cleared on Release. This is in-process only — a cross-process CLI run
	// holds the flock but never arms, so RequestStop can't reach it.
	mu     sync.Mutex
	runID  int64
	cancel context.CancelFunc
}

// New returns a ready, memory-only Lock. It guards goroutines within one
// process but not other processes. Used by tests and by callers that have no
// data dir to anchor a file lock.
func New() *Lock {
	return &Lock{ch: make(chan struct{}, 1)}
}

// Open returns a ready Lock backed by an OS file lock at path, adding
// cross-process exclusion on top of the in-process channel. The file is created
// if absent and kept open for the lifetime of the Lock; callers must Close it.
func Open(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path is the operator-configured data dir.
	if err != nil {
		return nil, fmt.Errorf("opening run lock %s: %w", path, err)
	}
	return &Lock{ch: make(chan struct{}, 1), file: f}, nil
}

// TryAcquire grabs the slot without blocking, returning false if a run is
// already in progress (in this process or, for a file-backed Lock, any other).
// A successful acquire must be paired with exactly one Release.
func (l *Lock) TryAcquire() bool {
	select {
	case l.ch <- struct{}{}:
	default:
		return false
	}
	if l.file != nil {
		if err := tryFlock(l.file); err != nil {
			<-l.ch // roll back the in-process slot so it isn't stuck held
			return false
		}
	}
	return true
}

// Arm registers the in-flight run and its cancel func, so a later RequestStop
// can interrupt it. Called by the holder right after a successful TryAcquire,
// before executing. Replaces any prior registration (the capacity-1 slot means
// there is at most one).
func (l *Lock) Arm(runID int64, cancel context.CancelFunc) {
	l.mu.Lock()
	l.runID = runID
	l.cancel = cancel
	l.mu.Unlock()
}

// RequestStop cancels the in-flight run when runID matches the armed run,
// returning true. It returns false when no run is armed, the id doesn't match,
// or the run belongs to another process (which holds the flock but never armed
// this in-memory registry). The cancellation is cooperative: the running Actor
// observes it between candidates and stops cleanly.
func (l *Lock) RequestStop(runID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel == nil || l.runID != runID {
		return false
	}
	l.cancel()
	return true
}

// Release frees the slot. It must be called only after a successful TryAcquire.
func (l *Lock) Release() {
	l.mu.Lock()
	l.runID = 0
	l.cancel = nil
	l.mu.Unlock()
	if l.file != nil {
		_ = unflock(l.file)
	}
	<-l.ch
}

// Close releases the underlying file descriptor. It does not release a held
// lock — call Release first. Safe to call on a memory-only Lock.
func (l *Lock) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
