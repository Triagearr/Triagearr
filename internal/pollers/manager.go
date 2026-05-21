package pollers

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Manager runs a set of pollers concurrently and blocks until the context is
// cancelled. Pollers run independently — one poller's failure does not stop
// the others.
type Manager struct {
	pollers []Poller
}

// NewManager returns a manager wired with the given pollers.
func NewManager(pollers ...Poller) *Manager {
	return &Manager{pollers: pollers}
}

// Run starts every poller. It returns when the context is cancelled, after
// waiting for all pollers to exit. If any poller returns a non-nil error,
// the first such error is reported as the aggregate result.
func (m *Manager) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errs := make([]error, len(m.pollers))
	for i, p := range m.pollers {
		wg.Add(1)
		go func(i int, p Poller) {
			defer wg.Done()
			errs[i] = p.Run(ctx)
		}(i, p)
	}
	wg.Wait()
	wrapped := make([]error, 0, len(errs))
	for i, err := range errs {
		if err != nil {
			wrapped = append(wrapped, fmt.Errorf("poller %s: %w", m.pollers[i].Name(), err))
		}
	}
	return errors.Join(wrapped...)
}
