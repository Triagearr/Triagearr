package pollers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// FilesStore is the subset of *store.Store the files poller needs.
type FilesStore interface {
	ListTorrentsBasic(ctx context.Context) ([]store.TorrentBasic, error)
	UpsertTorrentFile(ctx context.Context, hash triagearr.Hash, relPath string, size int64, nlink *int64, sampledAt time.Time) error
}

// FilesQbit is the qBit subset the files poller needs.
type FilesQbit interface {
	TorrentFiles(ctx context.Context, h triagearr.Hash) ([]triagearr.TorrentFile, error)
}

// StatNlink reports a file's size and hardlink count. Returned (-1, nil) is
// reserved for "file gone" — translated to a nil nlink upsert (ENOENT is not
// an error: *arr renames, atomic-move replacements and torrent removals all
// race the poller).
type StatNlink func(path string) (size int64, nlink int64, err error)

// FilesPoller stats each file of each torrent and persists (size, nlink)
// into torrent_files. Enables the Decider's cross-seed pre-filter
// (max(nlink)>2 ⇒ deleting frees no disk; see docs/SCORING.md) and feeds the
// Actor's T3.5 sanity-check seed data. Reliant on ADR-0023's shared-mount
// convention: the qBit save_path resolves in Triagearr's namespace.
type FilesPoller struct {
	Store    FilesStore
	Qbit     FilesQbit
	Interval time.Duration
	// Stat defaults to DefaultStatNlink (Linux syscall.Stat_t). Tests inject
	// a fake to drive nlink scenarios without touching the filesystem.
	Stat StatNlink
}

// Name implements Poller.
func (p *FilesPoller) Name() string { return "files" }

// Run blocks until ctx is cancelled.
func (p *FilesPoller) Run(ctx context.Context) error {
	if p.Stat == nil {
		p.Stat = DefaultStatNlink
	}
	return TickLoop(ctx, p.Name(), p.Interval, p.tick, nil)
}

func (p *FilesPoller) tick(ctx context.Context) error {
	torrents, err := p.Store.ListTorrentsBasic(ctx)
	if err != nil {
		return fmt.Errorf("listing torrents: %w", err)
	}
	now := time.Now().UTC()
	var totalFiles, missing, conflicts, statErrors int
	for _, t := range torrents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.SavePath == "" {
			continue
		}
		files, err := p.Qbit.TorrentFiles(ctx, triagearr.Hash(t.Hash))
		if err != nil {
			slog.Warn("files poller: TorrentFiles failed", "hash", t.Hash, "err", err)
			continue
		}
		for _, f := range files {
			abs := filepath.Join(t.SavePath, f.Name)
			size, nlink, statErr := p.Stat(abs)
			var nlinkArg *int64
			storedSize := f.Size
			switch {
			case statErr == nil:
				v := nlink
				nlinkArg = &v
				storedSize = size
				if v > 2 {
					conflicts++
				}
			case errors.Is(statErr, os.ErrNotExist):
				// File genuinely gone (rename race, *arr atomic-move replacement,
				// manual delete). NULL nlink is the truthful value to persist —
				// the Decider treats NULL as "unknown, keep eligible" so the
				// Actor's T3.5 will catch any TOCTOU re-appearance.
				missing++
			default:
				// Transient FS error (EIO, EACCES, NFS hiccup). Skip the upsert
				// to preserve the previously-good nlink — overwriting with NULL
				// would silently bypass the Decider's cross-seed pre-filter for
				// this file until the next successful tick.
				statErrors++
				slog.Warn("files poller: stat failed",
					"hash", t.Hash, "path", abs, "err", statErr)
				continue
			}
			if err := p.Store.UpsertTorrentFile(ctx, triagearr.Hash(t.Hash), f.Name, storedSize, nlinkArg, now); err != nil {
				slog.Warn("files poller: upsert failed", "hash", t.Hash, "path", f.Name, "err", err)
				continue
			}
			totalFiles++
		}
	}
	slog.Info("files tick complete",
		"torrents", len(torrents),
		"files", totalFiles,
		"missing", missing,
		"stat_errors", statErrors,
		"cross_seed_candidates", conflicts,
	)
	return nil
}
