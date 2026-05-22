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
	UpsertArrImport(ctx context.Context, arrName string, arrType triagearr.ArrType, rec triagearr.ImportRecord) error
	MaxHistoryID(ctx context.Context, arrName string, arrType triagearr.ArrType) (int64, error)
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
	// Notify, when non-nil, is signalled after each successful tick so the
	// scorer can re-score against the freshly persisted *arr state.
	Notify chan<- struct{}
}

// URLKey is the canonical "<type>/<name>" key for the URLs map.
func URLKey(name string, typ triagearr.ArrType) string {
	return string(typ) + "/" + name
}

// Name implements Poller.
func (p *ArrPoller) Name() string { return "arr" }

// Run blocks until ctx is cancelled.
func (p *ArrPoller) Run(ctx context.Context) error {
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, p.Notify)
}

func (p *ArrPoller) tick(ctx context.Context) error {
	for _, inst := range p.Instances {
		p.pollOne(ctx, inst)
	}
	return nil
}

func (p *ArrPoller) pollOne(ctx context.Context, inst triagearr.ArrInstance) {
	logger := slog.With("arr", inst.Type(), "name", inst.Name())
	url := p.URLs[URLKey(inst.Name(), inst.Type())]
	healthErr := inst.HealthCheck(ctx)
	healthy := healthErr == nil
	lastErr := ""
	if healthErr != nil {
		lastErr = healthErr.Error()
	}
	if err := p.Store.UpsertArrInstance(ctx, inst.Name(), inst.Type(), url, healthy, lastErr); err != nil {
		logger.Warn("upsert arr_instance failed", "err", err)
	}
	if !healthy {
		logger.Info("arr unhealthy", "err", healthErr)
		return
	}
	items, err := inst.ListMedia(ctx)
	if err != nil {
		// Stub clients return "not implemented" — log at debug to keep noise down.
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.Debug("list media failed", "err", err)
		return
	}
	lister, hasFileLister := inst.(triagearr.FileLister)
	filesTotal, filesFailed := 0, 0
	minInterval := p.FileFanoutMinInterval
	lastFileCall := time.Time{}
	for _, m := range items {
		if err := p.Store.UpsertMedia(ctx, m); err != nil {
			logger.Warn("upsert media failed", "id", m.ID, "err", err)
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
			logger.Debug("list media files failed", "id", m.ID, "err", err)
			filesFailed++
			continue
		}
		for _, f := range files {
			if err := p.Store.UpsertMediaFile(ctx, f); err != nil {
				logger.Warn("upsert media_file failed", "file_id", f.FileID, "err", err)
				continue
			}
			filesTotal++
		}
	}
	imports, importsFailed := p.syncImports(ctx, inst, logger)
	logger.Info("arr tick complete",
		"media", len(items),
		"files", filesTotal, "files_failed", filesFailed,
		"imports_new", imports, "imports_failed", importsFailed)
}

// syncImports pulls the *arr-side import history delta (since the highest
// history_id we've already stored) and upserts arr_imports. Skipped silently
// when the client doesn't implement ImportLister (stub clients).
func (p *ArrPoller) syncImports(ctx context.Context, inst triagearr.ArrInstance, logger *slog.Logger) (ok, failed int) {
	lister, hasImportLister := inst.(triagearr.ImportLister)
	if !hasImportLister {
		return 0, 0
	}
	since, err := p.Store.MaxHistoryID(ctx, inst.Name(), inst.Type())
	if err != nil {
		logger.Warn("max history_id failed", "err", err)
		return 0, 0
	}
	recs, err := lister.ListImports(ctx, since)
	if err != nil {
		logger.Warn("list imports failed", "err", err)
		return 0, 0
	}
	for _, r := range recs {
		if err := p.Store.UpsertArrImport(ctx, inst.Name(), inst.Type(), r); err != nil {
			logger.Warn("upsert arr_import failed", "file_id", r.FileID, "err", err)
			failed++
			continue
		}
		ok++
	}
	return ok, failed
}
