package decider_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeSrc struct {
	scores     []store.ScoreRow
	torrents   []store.TorrentBasic
	disk       *triagearr.DiskUsage
	maxN       map[triagearr.Hash]int64
	arrImports map[triagearr.Hash]struct{}
}

func (f *fakeSrc) ListScores(_ context.Context, _ store.ListScoresOpts) ([]store.ScoreRow, error) {
	return f.scores, nil
}
func (f *fakeSrc) ListTorrentsBasic(_ context.Context) ([]store.TorrentBasic, error) {
	return f.torrents, nil
}
func (f *fakeSrc) LatestDiskUsage(_ context.Context) (*triagearr.DiskUsage, error) {
	return f.disk, nil
}
func (f *fakeSrc) MaxNlinkByHashes(_ context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]int64, error) {
	if f.maxN == nil {
		return map[triagearr.Hash]int64{}, nil
	}
	out := map[triagearr.Hash]int64{}
	for _, h := range hashes {
		if n, ok := f.maxN[h]; ok {
			out[h] = n
		}
	}
	return out, nil
}
func (f *fakeSrc) HashesWithArrImports(_ context.Context) (map[triagearr.Hash]struct{}, error) {
	if f.arrImports == nil {
		return map[triagearr.Hash]struct{}{}, nil
	}
	return f.arrImports, nil
}

func TestPlan_TargetReached(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{
			{Hash: "a", Score: 100},
			{Hash: "b", Score: 90},
			{Hash: "c", Score: 80},
		},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data/dl/movies", Size: 3 * oneGiB},
			{Hash: "b", SavePath: "/data/dl/tv", Size: 2 * oneGiB},
			{Hash: "c", SavePath: "/data/dl/tv", Size: 10 * oneGiB},
		},
		disk: &triagearr.DiskUsage{TotalBytes: 100 * uint64(oneGiB), FreePercent: 5},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 10,
	})
	require.NoError(t, err)
	require.Equal(t, triagearr.StopTargetReached, plan.StopReason)
	// need = (10-5)% of 100GiB = 5 GiB ; a(3) + b(2) = 5 → target met after 2 items
	require.Len(t, plan.Items, 2)
	require.Equal(t, triagearr.Hash("a"), plan.Items[0].TorrentHash)
	require.Equal(t, triagearr.Hash("b"), plan.Items[1].TorrentHash)
}

func TestPlan_NoMoreCandidates(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{{Hash: "a", Score: 1}},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data/x", Size: 1 * oneGiB},
		},
		disk: &triagearr.DiskUsage{TotalBytes: 1000 * uint64(oneGiB), FreePercent: 0},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 50,
	})
	require.NoError(t, err)
	require.Equal(t, triagearr.StopNoMoreCandidates, plan.StopReason)
	require.Len(t, plan.Items, 1)
}

func TestPlan_VolumeFilterByPrefix(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{
			{Hash: "a", Score: 100},
			{Hash: "b", Score: 90},
			{Hash: "c", Score: 80},
		},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/other/volume/movies", Size: 10 * oneGiB},
			{Hash: "b", SavePath: "/data/dl", Size: 4 * oneGiB},
			{Hash: "c", SavePath: "/data", Size: 1 * oneGiB},
		},
		disk: &triagearr.DiskUsage{TotalBytes: 100 * uint64(oneGiB), FreePercent: 0},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 1,
	})
	require.NoError(t, err)
	// 'a' filtered out (outside the volume path) ; 'b' brings 4 GiB > need
	// (1% of 100GiB = 1GiB)
	require.Equal(t, triagearr.StopTargetReached, plan.StopReason)
	require.Len(t, plan.Items, 1)
	require.Equal(t, triagearr.Hash("b"), plan.Items[0].TorrentHash)
}

func TestPlan_AlreadyAboveTarget(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{{Hash: "a", Score: 100}},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data", Size: 1 * oneGiB},
		},
		disk: &triagearr.DiskUsage{TotalBytes: 100 * uint64(oneGiB), FreePercent: 80},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 20,
	})
	require.NoError(t, err)
	// need = 0 → target already met → empty plan, no candidate elected
	require.Equal(t, triagearr.StopTargetReached, plan.StopReason)
	require.Empty(t, plan.Items)
	require.Zero(t, plan.EstimatedFreedBytes)
}

func TestPlan_NoSnapshot(t *testing.T) {
	src := &fakeSrc{} // disk nil — no disk_usage recorded yet
	d := decider.New(src)
	_, err := d.Plan(context.Background(), decider.Volume{Name: "data", Path: "/data"})
	require.Error(t, err)
}

func TestPlan_FiltersCrossSeed(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{
			{Hash: "a", Score: 100}, // cross-seed (nlink=3) → filtered
			{Hash: "b", Score: 90},  // arr-managed, nlink=2 (qBit+*arr) → kept
			{Hash: "c", Score: 80},  // unsampled (no nlink row) → kept, T3.5 will catch
			{Hash: "d", Score: 70},  // qbit-only nlink=2 → filtered (stricter ceiling)
		},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data", Size: 5 * oneGiB},
			{Hash: "b", SavePath: "/data", Size: 5 * oneGiB},
			{Hash: "c", SavePath: "/data", Size: 5 * oneGiB},
			{Hash: "d", SavePath: "/data", Size: 5 * oneGiB},
		},
		disk:       &triagearr.DiskUsage{TotalBytes: 100 * uint64(oneGiB), FreePercent: 0},
		maxN:       map[triagearr.Hash]int64{"a": 3, "b": 2, "d": 2},
		arrImports: map[triagearr.Hash]struct{}{"a": {}, "b": {}}, // d is qbit-only
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 100,
	})
	require.NoError(t, err)
	require.Equal(t, 2, plan.FilteredCrossSeed, "a (nlink=3) and d (qbit-only nlink=2) filtered")
	require.Len(t, plan.Items, 2)
	require.Equal(t, triagearr.Hash("b"), plan.Items[0].TorrentHash)
	require.Equal(t, triagearr.Hash("c"), plan.Items[1].TorrentHash)
}

// keep time import used in build matrix where stdlib gets flagged unused.
var _ = time.Now
