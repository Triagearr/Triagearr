// Package actor performs the destructive side of Triagearr: it consumes a
// Decider run that has been resolved to mode "live" and executes the
// arr-first, qBit-second deletion pipeline described in
// docs/HARDLINK_TOPOLOGY.md and ADR-0003.
//
// One Execute call processes the run's items in order: for each candidate
// it fans out per-file *arr DELETEs, runs the T3.5 atomic nlink re-check
// (HARDLINK_TOPOLOGY.md), then issues the whole-torrent qBit DELETE. Failures
// are logged per-file in audit_log so a post-mortem can reconstruct partial
// states (8 OK + 1 failed + 1 not-attempted on a season pack). Cross-seed
// safety is enforced at action time by the T3.5 stat; ADR-0023 makes the
// stat namespace-safe.
package actor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Source is the subset of *store.Store the Actor reads and writes. Tests
// pass an in-memory fake satisfying this interface.
type Source interface {
	GetRun(ctx context.Context, id int64) (triagearr.Run, []triagearr.RunItem, error)
	MarkRunStatus(ctx context.Context, id int64, status string) error
	InsertAction(ctx context.Context, a triagearr.Action) (int64, error)
	FinishAction(ctx context.Context, id int64, status triagearr.ActionStatus, finishedAt time.Time, freedBytes int64) error
	AppendAudit(ctx context.Context, e triagearr.AuditEntry) error
	LinksByHash(ctx context.Context, hash triagearr.Hash) ([]triagearr.Link, error)
	// TorrentSavePath fetches the volume-relative root for the torrent so the
	// T3.5 step can resolve absolute file paths in Triagearr's namespace
	// (ADR-0023).
	TorrentSavePath(ctx context.Context, hash triagearr.Hash) (string, error)
}

// QbitClient is the qBit subset the Actor uses: enumerating the torrent's
// files (for the T3.5 stat sweep) and the destructive whole-torrent delete.
type QbitClient interface {
	TorrentFiles(ctx context.Context, h triagearr.Hash) ([]triagearr.TorrentFile, error)
	Delete(ctx context.Context, h triagearr.Hash, opts triagearr.DeleteOpts) error
}

// DeleterResolver returns the *arr FileDeleter for an instance name, or
// (nil, false) when the instance has `act: false`, is unknown, or doesn't
// implement FileDeleter (stub *arr types).
type DeleterResolver func(arrName string) (triagearr.FileDeleter, bool)

// Options configures an Actor.
type Options struct {
	Source             Source
	Qbit               QbitClient
	Deleter            DeleterResolver
	MaxDeletionsPerRun int           // 0 → unlimited (not recommended outside tests)
	InterActionDelay   time.Duration // sleep between two whole-torrent qBit deletes
	// AddImportExclusion forwards to *arr — when true, deleted releases are
	// added to the import exclusion list so *arr won't re-grab them.
	AddImportExclusion bool
	// Stat is the T3.5 hardlink-count probe. nil falls back to
	// pollers.DefaultStatNlink (Linux syscall.Stat_t). Tests inject a fake to
	// drive nlink scenarios without touching the filesystem.
	Stat pollers.StatNlink

	now   func() time.Time      // injected in tests
	sleep func(d time.Duration) // injected in tests
}

// Actor executes destructive runs.
type Actor struct {
	opts Options
}

// New constructs an Actor. nil values for the now/sleep hooks fall back to
// time.Now / time.Sleep.
func New(opts Options) *Actor {
	if opts.Source == nil {
		panic("actor: Source is required")
	}
	if opts.Qbit == nil {
		panic("actor: Qbit is required")
	}
	if opts.Deleter == nil {
		panic("actor: Deleter resolver is required")
	}
	if opts.now == nil {
		opts.now = func() time.Time { return time.Now().UTC() }
	}
	if opts.sleep == nil {
		opts.sleep = time.Sleep
	}
	if opts.Stat == nil {
		opts.Stat = pollers.DefaultStatNlink
	}
	return &Actor{opts: opts}
}

// Execute drives the destructive pipeline for runID. A run not in mode
// "live" is a no-op (defense in depth against caller mistakes). Runs from
// triggers outside the allowed set (cron, future schedulers) are refused
// per ADR-0015.
func (a *Actor) Execute(ctx context.Context, runID int64) error {
	run, items, err := a.opts.Source.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("actor: loading run %d: %w", runID, err)
	}
	if run.Mode != string(triagearr.RunModeLive) {
		slog.Debug("actor: run is not live, skipping", "run_id", runID, "mode", run.Mode)
		return nil
	}
	switch run.TriggeredBy {
	case triagearr.RunTriggerDiskPressure, triagearr.RunTriggerHTTP, triagearr.RunTriggerCLI:
	default:
		// ADR-0015: cron and any future scheduled trigger never execute.
		return fmt.Errorf("actor: trigger %q is not allowed to execute (ADR-0015)", run.TriggeredBy)
	}

	if err := a.opts.Source.MarkRunStatus(ctx, runID, "running"); err != nil {
		return fmt.Errorf("actor: marking run running: %w", err)
	}

	limit := a.opts.MaxDeletionsPerRun
	for i, item := range items {
		if limit > 0 && i >= limit {
			slog.Info("actor: rate cap reached", "run_id", runID, "cap", limit, "processed", i)
			break
		}
		if err := a.processCandidate(ctx, runID, item); err != nil {
			// processCandidate already persisted its terminal state.
			slog.Warn("actor: candidate aborted", "run_id", runID, "hash", item.TorrentHash, "err", err)
		}
		if i+1 < len(items) && a.opts.InterActionDelay > 0 {
			a.opts.sleep(a.opts.InterActionDelay)
		}
	}

	if err := a.opts.Source.MarkRunStatus(ctx, runID, "completed"); err != nil {
		return fmt.Errorf("actor: marking run completed: %w", err)
	}
	return nil
}

// audit appends one audit_log row for the given action/step. Errors are
// swallowed because audit failures must never abort the destructive
// pipeline — the action's terminal status is still recorded by finish().
func (a *Actor) audit(ctx context.Context, actionID int64, step triagearr.AuditStep, outcome triagearr.AuditOutcome, arrName string, fileID int64, detail string) {
	_ = a.opts.Source.AppendAudit(ctx, triagearr.AuditEntry{
		ActionID:  actionID,
		Timestamp: a.opts.now(),
		Step:      step,
		Outcome:   outcome,
		ArrName:   arrName,
		ArrFileID: fileID,
		Detail:    detail,
	})
}

// processCandidate runs one candidate end-to-end. Errors are non-fatal at
// the run level — the caller continues to the next candidate — but they
// drive the action's terminal status and the audit_log narrative.
func (a *Actor) processCandidate(ctx context.Context, runID int64, item triagearr.RunItem) error {
	started := a.opts.now()
	actionID, err := a.opts.Source.InsertAction(ctx, triagearr.Action{
		RunID:       runID,
		Rank:        item.Rank,
		TorrentHash: item.TorrentHash,
		StartedAt:   started,
		Status:      triagearr.ActionRunning,
	})
	if err != nil {
		return fmt.Errorf("insert action: %w", err)
	}

	links, err := a.opts.Source.LinksByHash(ctx, item.TorrentHash)
	if err != nil {
		a.audit(ctx, actionID, triagearr.AuditStepArrDelete, triagearr.AuditOutcomeFailed, "", 0, fmt.Sprintf("resolving links: %v", err))
		return a.finish(ctx, actionID, triagearr.ActionAbortedArrFail, 0, err)
	}

	if err := a.fanoutArr(ctx, actionID, links); err != nil {
		return a.finish(ctx, actionID, triagearr.ActionAbortedArrFail, 0, err)
	}

	// T3.5 — atomic per-file hardlink re-check (HARDLINK_TOPOLOGY.md). The
	// Decider already filtered cross-seed candidates at election time, but a
	// new cross-seed peer can appear in the TOCTOU window between scoring
	// and action. If ANY file still has nlink > 1 after the *arr deletes, the
	// qBit delete would either free no disk (cross-seed) or break a peer.
	// We abort the qBit step; *arr deletes are NOT rolled back — *arr will
	// re-monitor and re-grab, and the surviving nlink protects the disk.
	if skip, err := a.checkNlink(ctx, actionID, item.TorrentHash); err != nil {
		return a.finish(ctx, actionID, triagearr.ActionAbortedArrFail, 0, err)
	} else if skip {
		return a.finish(ctx, actionID, triagearr.ActionSkippedCrossSeed, 0, nil)
	}

	delOpts := triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: a.opts.AddImportExclusion}
	if err := withRetry(ctx, a.opts.sleep, func() error {
		return a.opts.Qbit.Delete(ctx, item.TorrentHash, delOpts)
	}); err != nil {
		a.audit(ctx, actionID, triagearr.AuditStepQbitDelete, triagearr.AuditOutcomeFailed, "", 0, truncate(err.Error()))
		return a.finish(ctx, actionID, triagearr.ActionFailedQbit, 0, err)
	}
	a.audit(ctx, actionID, triagearr.AuditStepQbitDelete, triagearr.AuditOutcomeOK, "", 0, "")

	return a.finish(ctx, actionID, triagearr.ActionSucceeded, item.SizeBytes, nil)
}

// checkNlink runs the T3.5 stat sweep. It fetches the torrent's current file
// list from qBit (authoritative — the in-store torrent_files snapshot can lag
// up to one files-poller interval) and stats each in Triagearr's namespace
// (safe per ADR-0023). Returns (skip=true) on the first file with nlink > 1.
// ENOENT is not a conflict: someone (cleanup script, the user, a prior failed
// run) already removed the inode — proceed with the qBit delete to keep state
// consistent. A genuine FS error returns an err so the caller aborts the run.
func (a *Actor) checkNlink(ctx context.Context, actionID int64, hash triagearr.Hash) (bool, error) {
	savePath, err := a.opts.Source.TorrentSavePath(ctx, hash)
	if err != nil {
		a.audit(ctx, actionID, triagearr.AuditStepNlinkCheck, triagearr.AuditOutcomeFailed, "", 0, fmt.Sprintf("save_path: %v", err))
		return false, fmt.Errorf("save_path %s: %w", hash, err)
	}
	files, err := a.opts.Qbit.TorrentFiles(ctx, hash)
	if err != nil {
		a.audit(ctx, actionID, triagearr.AuditStepNlinkCheck, triagearr.AuditOutcomeFailed, "", 0, fmt.Sprintf("qbit files: %v", err))
		return false, fmt.Errorf("qbit files %s: %w", hash, err)
	}
	var checked, gone int
	for _, f := range files {
		abs := filepath.Join(savePath, f.Name)
		_, nlink, statErr := a.opts.Stat(abs)
		if errors.Is(statErr, os.ErrNotExist) {
			gone++
			continue
		}
		if statErr != nil {
			a.audit(ctx, actionID, triagearr.AuditStepNlinkCheck, triagearr.AuditOutcomeFailed, "", 0,
				truncate(fmt.Sprintf("stat %s: %v", abs, statErr)))
			return false, fmt.Errorf("stat %s: %w", abs, statErr)
		}
		checked++
		if nlink > 1 {
			a.audit(ctx, actionID, triagearr.AuditStepNlinkCheck, triagearr.AuditOutcomeSkipped, "", 0,
				truncate(fmt.Sprintf("nlink=%d %s", nlink, abs)))
			return true, nil
		}
	}
	a.audit(ctx, actionID, triagearr.AuditStepNlinkCheck, triagearr.AuditOutcomeOK, "", 0,
		fmt.Sprintf("checked=%d gone=%d", checked, gone))
	return false, nil
}

// fanoutArr issues per-file DELETEs against the *arr instances that own each
// link. On the first failure (or an instance with act=false) the remaining
// links are recorded as not_attempted and the function returns an error.
// Already-deleted *arr files are NOT rolled back (see HARDLINK_TOPOLOGY case 4).
func (a *Actor) fanoutArr(ctx context.Context, actionID int64, links []triagearr.Link) error {
	delOpts := triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: a.opts.AddImportExclusion}
	for i, lk := range links {
		deleter, ok := a.opts.Deleter(lk.ArrName)
		if !ok {
			a.audit(ctx, actionID, triagearr.AuditStepArrDelete, triagearr.AuditOutcomeSkipped, lk.ArrName, lk.FileID, "instance act=false or no deleter")
			a.markRemaining(ctx, actionID, links[i+1:])
			return fmt.Errorf("instance %q is not actable", lk.ArrName)
		}
		err := withRetry(ctx, a.opts.sleep, func() error {
			return deleter.DeleteMediaFile(ctx, lk.FileID, delOpts)
		})
		if err != nil {
			a.audit(ctx, actionID, triagearr.AuditStepArrDelete, triagearr.AuditOutcomeFailed, lk.ArrName, lk.FileID, truncate(err.Error()))
			a.markRemaining(ctx, actionID, links[i+1:])
			return fmt.Errorf("arr delete for file %d on %q: %w", lk.FileID, lk.ArrName, err)
		}
		a.audit(ctx, actionID, triagearr.AuditStepArrDelete, triagearr.AuditOutcomeOK, lk.ArrName, lk.FileID, "")
	}
	return nil
}

func (a *Actor) markRemaining(ctx context.Context, actionID int64, rest []triagearr.Link) {
	for _, lk := range rest {
		a.audit(ctx, actionID, triagearr.AuditStepArrDelete, triagearr.AuditOutcomeNotAttempted, lk.ArrName, lk.FileID, "")
	}
}

func (a *Actor) finish(ctx context.Context, actionID int64, status triagearr.ActionStatus, freed int64, cause error) error {
	finishedAt := a.opts.now()
	if err := a.opts.Source.FinishAction(ctx, actionID, status, finishedAt, freed); err != nil {
		// Persistence failure is logged but we still return the original cause
		// (if any) — the caller already knows the action's terminal state.
		slog.Error("actor: finishing action", "action_id", actionID, "status", status, "err", err)
	}
	return cause
}

// withRetry runs op with exponential backoff (+ jitter) for triagearr.ErrTransient
// errors. Hard failures (4xx without 408/429, etc.) return immediately. Total
// budget capped at ~10s to keep a run from stalling on one bad torrent.
func withRetry(ctx context.Context, sleep func(time.Duration), op func() error) error {
	const maxAttempts = 3
	const baseDelay = 500 * time.Millisecond
	var lastErr error
	delay := baseDelay
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = op()
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, triagearr.ErrTransient) {
			return lastErr
		}
		if attempt == maxAttempts-1 {
			break
		}
		sleep(delay + jitter(delay/2))
		delay *= 2
		if delay > 4*time.Second {
			delay = 4 * time.Second
		}
	}
	return lastErr
}

func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max))) //nolint:gosec // G404: jitter is timing noise, not security-sensitive
}

func truncate(s string) string {
	const limit = 500
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
