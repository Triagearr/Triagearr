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
	UpsertMediaFile(ctx context.Context, f triagearr.MediaFile) error
}

// ArrPoller iterates the configured *arr instances and refreshes media + health.
// URLs maps "<type>/<name>" → configured URL, used only to record the latest URL
// in the arr_instances table.
//
// When an instance implements triagearr.FileLister, the poller fans out one
// per-file API call per media item (Sonarr: episodefile, Radarr: moviefile),
// spaced by FileFanoutMinInterval to avoid hammering the *arr.
type ArrPoller struct {
	Instances             []triagearr.ArrInstance
	URLs                  map[string]string
	Store                 ArrStore
	Interval              time.Duration
	FileFanoutMinInterval time.Duration
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
	lister, hasFileLister := inst.(triagearr.FileLister)
	filesTotal, filesFailed := 0, 0
	minInterval := p.FileFanoutMinInterval
	lastFileCall := time.Time{}
	for _, m := range items {
		if err := p.Store.UpsertMedia(ctx, m); err != nil {
			slog.Warn("upsert media failed", "arr", inst.Type(), "name", inst.Name(), "id", m.ID, "err", err)
			continue
		}
		if !hasFileLister {
			continue
		}
		if minInterval > 0 && !lastFileCall.IsZero() {
			elapsed := time.Since(lastFileCall)
			if wait := minInterval - elapsed; wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
		}
		lastFileCall = time.Now()
		files, err := lister.ListMediaFiles(ctx, m.ID)
		if err != nil {
			slog.Debug("list media files failed", "arr", inst.Type(), "name", inst.Name(), "id", m.ID, "err", err)
			filesFailed++
			continue
		}
		for _, f := range files {
			if err := p.Store.UpsertMediaFile(ctx, f); err != nil {
				slog.Warn("upsert media_file failed", "arr", inst.Type(), "name", inst.Name(), "file_id", f.FileID, "err", err)
				continue
			}
			filesTotal++
		}
	}
	slog.Info("arr tick complete",
		"arr", inst.Type(), "name", inst.Name(),
		"media", len(items), "files", filesTotal, "files_failed", filesFailed)
}
