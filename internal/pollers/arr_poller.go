package pollers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ArrStore is the subset of store operations the arr poller needs.
type ArrStore interface {
	UpsertArrInstance(ctx context.Context, name string, typ triagearr.ArrType, url string, healthy bool, lastErr string) error
	UpsertMedia(ctx context.Context, m triagearr.MediaItem) error
}

// ArrPoller iterates the configured *arr instances and refreshes media + health.
// URLs maps "<type>/<name>" → configured URL, used only to record the latest URL
// in the arr_instances table.
type ArrPoller struct {
	Instances []triagearr.ArrInstance
	URLs      map[string]string
	Store     ArrStore
	Interval  time.Duration
}

// URLKey is the canonical "<type>/<name>" key for the URLs map.
func URLKey(name string, typ triagearr.ArrType) string {
	return string(typ) + "/" + name
}

// Name implements Poller.
func (p *ArrPoller) Name() string { return "arr" }

// Run blocks until ctx is cancelled.
func (p *ArrPoller) Run(ctx context.Context) error {
	return tickLoop(ctx, p.Name(), p.Interval, p.tick)
}

func (p *ArrPoller) tick(ctx context.Context) error {
	for _, inst := range p.Instances {
		p.pollOne(ctx, inst)
	}
	return nil
}

func (p *ArrPoller) pollOne(ctx context.Context, inst triagearr.ArrInstance) {
	url := p.URLs[URLKey(inst.Name(), inst.Type())]
	healthErr := inst.HealthCheck(ctx)
	healthy := healthErr == nil
	lastErr := ""
	if healthErr != nil {
		lastErr = healthErr.Error()
	}
	if err := p.Store.UpsertArrInstance(ctx, inst.Name(), inst.Type(), url, healthy, lastErr); err != nil {
		slog.Warn("upsert arr_instance failed", "arr", inst.Type(), "name", inst.Name(), "err", err)
	}
	if !healthy {
		slog.Info("arr unhealthy", "arr", inst.Type(), "name", inst.Name(), "err", healthErr)
		return
	}
	items, err := inst.ListMedia(ctx)
	if err != nil {
		// Stub clients return "not implemented" — log at debug to keep noise down.
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Debug("list media failed", "arr", inst.Type(), "name", inst.Name(), "err", err)
		return
	}
	for _, m := range items {
		if err := p.Store.UpsertMedia(ctx, m); err != nil {
			slog.Warn("upsert media failed", "arr", inst.Type(), "name", inst.Name(), "id", m.ID, "err", err)
		}
	}
	slog.Info("arr tick complete", "arr", inst.Type(), "name", inst.Name(), "media", len(items))
}
