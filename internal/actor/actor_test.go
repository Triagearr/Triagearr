package actor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

var _ = noConflictStat // keep helper referenced; tests use it indirectly via custom stats

// fakeSource is an in-memory Source. It mirrors enough of the store contract
// for the actor tests without needing SQLite.
type fakeSource struct {
	mu       sync.Mutex
	run      triagearr.Run
	items    []triagearr.RunItem
	links    map[triagearr.Hash][]triagearr.Link
	runs     map[int64]string                 // id → status
	actions  map[int64]*triagearr.Action      // by action id
	audit    map[int64][]triagearr.AuditEntry // action id → entries
	nextAct  int64
	insErr   error // optional: error on InsertAction
	linksErr error // optional: error on LinksByHash
}

func newFakeSource(run triagearr.Run, items []triagearr.RunItem, links map[triagearr.Hash][]triagearr.Link) *fakeSource {
	run.ID = 1
	return &fakeSource{
		run:     run,
		items:   items,
		links:   links,
		runs:    map[int64]string{1: run.Status},
		actions: map[int64]*triagearr.Action{},
		audit:   map[int64][]triagearr.AuditEntry{},
	}
}

func (f *fakeSource) GetRun(_ context.Context, id int64) (triagearr.Run, []triagearr.RunItem, error) {
	if id != f.run.ID {
		return triagearr.Run{}, nil, errors.New("run not found")
	}
	return f.run, f.items, nil
}

func (f *fakeSource) MarkRunStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[id] = status
	return nil
}

func (f *fakeSource) InsertAction(_ context.Context, a triagearr.Action) (int64, error) {
	if f.insErr != nil {
		return 0, f.insErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextAct++
	a.ID = f.nextAct
	cp := a
	f.actions[a.ID] = &cp
	return a.ID, nil
}

func (f *fakeSource) FinishAction(_ context.Context, id int64, status triagearr.ActionStatus, finishedAt time.Time, freedBytes int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.actions[id]
	if !ok {
		return errors.New("action not found")
	}
	a.Status = status
	a.FinishedAt = finishedAt
	a.FreedBytes = freedBytes
	return nil
}

func (f *fakeSource) AppendAudit(_ context.Context, e triagearr.AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit[e.ActionID] = append(f.audit[e.ActionID], e)
	return nil
}

func (f *fakeSource) LinksByHash(_ context.Context, hash triagearr.Hash) ([]triagearr.Link, error) {
	if f.linksErr != nil {
		return nil, f.linksErr
	}
	return f.links[hash], nil
}

func (f *fakeSource) TorrentSavePath(_ context.Context, _ triagearr.Hash) (string, error) {
	return "/fake", nil
}

// fakeQbit records every Delete call and can be programmed to fail N times.
// Per-hash file lists drive the T3.5 stat sweep.
type fakeQbit struct {
	mu      sync.Mutex
	calls   []triagearr.Hash
	files   map[triagearr.Hash][]triagearr.TorrentFile
	failN   int   // first N calls fail with the given error
	failErr error // if nil, errors.New("boom") wrapped with ErrTransient when transient=true
}

func (q *fakeQbit) TorrentFiles(_ context.Context, h triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return q.files[h], nil
}

func (q *fakeQbit) Delete(_ context.Context, h triagearr.Hash, _ triagearr.DeleteOpts) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls = append(q.calls, h)
	if q.failN > 0 {
		q.failN--
		return q.failErr
	}
	return nil
}

// noConflictStat is the test default: every stat reports nlink=1, i.e. the
// inode is owned by qBit alone (the *arr delete already happened in fanoutArr).
// T3.5 walks through cleanly. Tests for cross-seed scenarios swap this out.
func noConflictStat(_ string) (int64, int64, error) { return 0, 1, nil }

// fakeDeleter is a per-arr FileDeleter that records the file_ids it sees and
// can be programmed to fail on specific calls.
type fakeDeleter struct {
	name     string
	mu       sync.Mutex
	calls    []int64
	failOn   map[int64]error // file_id → error to return
	failOnce map[int64]int   // how many remaining failures per file_id (decrement to 0)
}

func newFakeDeleter(name string) *fakeDeleter {
	return &fakeDeleter{name: name, failOn: map[int64]error{}, failOnce: map[int64]int{}}
}

func (d *fakeDeleter) DeleteMediaFile(_ context.Context, fileID int64, _ triagearr.DeleteOpts) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, fileID)
	if remaining, ok := d.failOnce[fileID]; ok && remaining > 0 {
		d.failOnce[fileID] = remaining - 1
		return d.failOn[fileID]
	}
	if err, ok := d.failOn[fileID]; ok {
		if _, transient := d.failOnce[fileID]; !transient {
			return err
		}
	}
	return nil
}

func resolverFor(deleters ...*fakeDeleter) actor.DeleterResolver {
	return func(name string) (triagearr.FileDeleter, bool) {
		for _, d := range deleters {
			if d.name == name {
				return d, true
			}
		}
		return nil, false
	}
}

func liveRun(items []triagearr.RunItem) triagearr.Run {
	return triagearr.Run{
		TriggeredBy: triagearr.RunTriggerDiskPressure,
		TriggeredAt: time.Now().UTC(),
		Mode:        string(triagearr.RunModeLive),
		StopReason:  triagearr.StopTargetReached,
		Status:      "pending",
	}
}

func TestActor_HappyPath_singleFile(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h1", SizeBytes: 1000}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h1": {{ArrName: "sonarr-main", FileID: 42}},
	})
	q := &fakeQbit{}
	d := newFakeDeleter("sonarr-main")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	require.NoError(t, a.Execute(context.Background(), 1))

	require.Equal(t, []int64{42}, d.calls)
	require.Equal(t, []triagearr.Hash{"h1"}, q.calls)
	require.Equal(t, "completed", src.runs[1])
	require.Len(t, src.actions, 1)
	act := src.actions[1]
	require.Equal(t, triagearr.ActionSucceeded, act.Status)
	require.Equal(t, int64(1000), act.FreedBytes)

	rows := src.audit[1]
	require.Len(t, rows, 3)
	require.Equal(t, triagearr.AuditStepArrDelete, rows[0].Step)
	require.Equal(t, triagearr.AuditOutcomeOK, rows[0].Outcome)
	require.Equal(t, triagearr.AuditStepNlinkCheck, rows[1].Step)
	require.Equal(t, triagearr.AuditOutcomeOK, rows[1].Outcome)
	require.Equal(t, triagearr.AuditStepQbitDelete, rows[2].Step)
	require.Equal(t, triagearr.AuditOutcomeOK, rows[2].Outcome)
}

func TestActor_SeasonPack_8files_allOK(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "pack", SizeBytes: 80000}}
	var links []triagearr.Link
	for i := int64(1); i <= 8; i++ {
		links = append(links, triagearr.Link{ArrName: "sonarr-main", FileID: i})
	}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{"pack": links})
	q := &fakeQbit{}
	d := newFakeDeleter("sonarr-main")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	require.NoError(t, a.Execute(context.Background(), 1))

	require.Len(t, d.calls, 8)
	require.Equal(t, []triagearr.Hash{"pack"}, q.calls)
	require.Equal(t, triagearr.ActionSucceeded, src.actions[1].Status)
	require.Len(t, src.audit[1], 10) // 8 arr + 1 nlink check + 1 qbit
}

func TestActor_ArrFailMidway_NotAttemptedRecorded(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "pack", SizeBytes: 80000}}
	var links []triagearr.Link
	for i := int64(1); i <= 10; i++ {
		links = append(links, triagearr.Link{ArrName: "sonarr-main", FileID: i})
	}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{"pack": links})
	q := &fakeQbit{}
	d := newFakeDeleter("sonarr-main")
	d.failOn[5] = errors.New("HTTP 404") // hard fail (not transient)
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	require.NoError(t, a.Execute(context.Background(), 1))

	// 4 OK (1..4) + 1 failed (5) + 5 not_attempted (6..10) ; qBit not called
	require.Empty(t, q.calls)
	rows := src.audit[1]
	require.Len(t, rows, 10)
	var ok, failed, notAttempted int
	for _, r := range rows {
		switch r.Outcome {
		case triagearr.AuditOutcomeOK:
			ok++
		case triagearr.AuditOutcomeFailed:
			failed++
		case triagearr.AuditOutcomeNotAttempted:
			notAttempted++
		}
	}
	require.Equal(t, 4, ok)
	require.Equal(t, 1, failed)
	require.Equal(t, 5, notAttempted)
	require.Equal(t, triagearr.ActionAbortedArrFail, src.actions[1].Status)
	require.Equal(t, int64(0), src.actions[1].FreedBytes)
}

func TestActor_QbitFail_ArrAlreadyDone(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h1", SizeBytes: 500}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h1": {{ArrName: "sonarr-main", FileID: 1}},
	})
	q := &fakeQbit{failN: 5, failErr: errors.New("connection refused")} // not transient → no retry
	d := newFakeDeleter("sonarr-main")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Equal(t, triagearr.ActionFailedQbit, src.actions[1].Status)
	// arr was called (1 OK) before qBit failed
	require.Equal(t, []int64{1}, d.calls)
	// qBit called exactly once (no retry on non-transient)
	require.Len(t, q.calls, 1)
}

func TestActor_QbitTransientRetry(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h1", SizeBytes: 500}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h1": {{ArrName: "sonarr-main", FileID: 1}},
	})
	q := &fakeQbit{failN: 2, failErr: errTransient(errors.New("502"))}
	d := newFakeDeleter("sonarr-main")
	a := actor.New(actor.Options{
		Source:  src,
		Qbit:    q,
		Deleter: resolverFor(d),
	})
	require.NoError(t, a.Execute(context.Background(), 1))
	require.Equal(t, triagearr.ActionSucceeded, src.actions[1].Status)
	require.Len(t, q.calls, 3) // 2 transient fails + 1 success
}

func errTransient(inner error) error {
	return wrapTransient{inner: inner}
}

type wrapTransient struct{ inner error }

func (w wrapTransient) Error() string { return w.inner.Error() }
func (w wrapTransient) Is(target error) bool {
	return target == triagearr.ErrTransient
}

func TestActor_RateCap_StopsEarly(t *testing.T) {
	items := []triagearr.RunItem{
		{Rank: 0, TorrentHash: "a", SizeBytes: 1},
		{Rank: 1, TorrentHash: "b", SizeBytes: 1},
		{Rank: 2, TorrentHash: "c", SizeBytes: 1},
	}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"a": {{ArrName: "s", FileID: 1}},
		"b": {{ArrName: "s", FileID: 2}},
		"c": {{ArrName: "s", FileID: 3}},
	})
	q := &fakeQbit{}
	d := newFakeDeleter("s")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d), MaxDeletionsPerRun: 2})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Len(t, src.actions, 2) // c never inserted
	require.Equal(t, []triagearr.Hash{"a", "b"}, q.calls)
}

func TestActor_DryRunMode_NoCallsMade(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h", SizeBytes: 1}}
	run := liveRun(items)
	run.Mode = "dry-run"
	src := newFakeSource(run, items, map[triagearr.Hash][]triagearr.Link{
		"h": {{ArrName: "s", FileID: 1}},
	})
	q := &fakeQbit{}
	d := newFakeDeleter("s")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Empty(t, q.calls)
	require.Empty(t, d.calls)
	require.Empty(t, src.actions)
}

func TestActor_CronTriggered_Refused(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h", SizeBytes: 1}}
	run := liveRun(items)
	run.TriggeredBy = triagearr.RunTrigger("cron")
	src := newFakeSource(run, items, map[triagearr.Hash][]triagearr.Link{
		"h": {{ArrName: "s", FileID: 1}},
	})
	q := &fakeQbit{}
	d := newFakeDeleter("s")
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d)})

	err := a.Execute(context.Background(), 1)
	require.Error(t, err)
	require.Empty(t, q.calls)
}

// TestActor_T35_SkipsCrossSeed exercises the atomic nlink re-check: a file
// reporting nlink>1 after the *arr fan-out indicates a cross-seed peer (or
// some other inode-sharing holder) appeared between scoring and action. The
// qBit delete must be aborted so the peer keeps seeding.
func TestActor_T35_SkipsCrossSeed(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h", SizeBytes: 1000}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h": {{ArrName: "s", FileID: 1}},
	})
	q := &fakeQbit{files: map[triagearr.Hash][]triagearr.TorrentFile{
		"h": {{Name: "ep01.mkv"}, {Name: "ep02.mkv"}},
	}}
	d := newFakeDeleter("s")
	// Second file reports nlink=2 → cross-seed conflict, abort qBit step.
	stat := func(path string) (int64, int64, error) {
		if filepath.Base(path) == "ep02.mkv" {
			return 0, 2, nil
		}
		return 0, 1, nil
	}
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d), Stat: stat})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Empty(t, q.calls, "qBit delete must not run when T3.5 sees nlink>1")
	require.Equal(t, []int64{1}, d.calls, "*arr deletes already happened (not rolled back)")
	require.Equal(t, triagearr.ActionSkippedCrossSeed, src.actions[1].Status)
	require.Equal(t, int64(0), src.actions[1].FreedBytes)

	rows := src.audit[1]
	require.Len(t, rows, 2) // 1 arr_delete OK + 1 nlink_check Skipped
	require.Equal(t, triagearr.AuditStepArrDelete, rows[0].Step)
	require.Equal(t, triagearr.AuditStepNlinkCheck, rows[1].Step)
	require.Equal(t, triagearr.AuditOutcomeSkipped, rows[1].Outcome)
	require.Contains(t, rows[1].Detail, "nlink=2")
}

// TestActor_T35_EnoentProceeds: a stale qBit file list pointing at an inode
// already removed (cleanup script, manual rm, prior crash) is not a conflict
// — nothing to protect on the *arr side. Proceed with qBit delete.
func TestActor_T35_EnoentProceeds(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h", SizeBytes: 1000}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h": {{ArrName: "s", FileID: 1}},
	})
	q := &fakeQbit{files: map[triagearr.Hash][]triagearr.TorrentFile{
		"h": {{Name: "ep01.mkv"}},
	}}
	d := newFakeDeleter("s")
	stat := func(_ string) (int64, int64, error) { return 0, 0, os.ErrNotExist }
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor(d), Stat: stat})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Equal(t, []triagearr.Hash{"h"}, q.calls)
	require.Equal(t, triagearr.ActionSucceeded, src.actions[1].Status)
}

func TestActor_ActFalse_Skips(t *testing.T) {
	items := []triagearr.RunItem{{Rank: 0, TorrentHash: "h", SizeBytes: 100}}
	src := newFakeSource(liveRun(items), items, map[triagearr.Hash][]triagearr.Link{
		"h": {{ArrName: "sonarr-readonly", FileID: 1}},
	})
	q := &fakeQbit{}
	// No deleter registered for "sonarr-readonly" → resolver returns false.
	a := actor.New(actor.Options{Source: src, Qbit: q, Deleter: resolverFor()})

	require.NoError(t, a.Execute(context.Background(), 1))
	require.Empty(t, q.calls)
	require.Equal(t, triagearr.ActionAbortedArrFail, src.actions[1].Status)
	rows := src.audit[1]
	require.Len(t, rows, 1)
	require.Equal(t, triagearr.AuditOutcomeSkipped, rows[0].Outcome)
	require.Contains(t, rows[0].Detail, "act=false")
}
