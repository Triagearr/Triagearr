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
	scores   []store.ScoreRow
	torrents []store.TorrentBasic
	disks    []triagearr.DiskUsage
}

func (f *fakeSrc) ListScores(_ context.Context, _ store.ListScoresOpts) ([]store.ScoreRow, error) {
	return f.scores, nil
}
func (f *fakeSrc) ListTorrentsBasic(_ context.Context) ([]store.TorrentBasic, error) {
	return f.torrents, nil
}
func (f *fakeSrc) LatestDiskUsage(_ context.Context) ([]triagearr.DiskUsage, error) {
	return f.disks, nil
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
		disks: []triagearr.DiskUsage{
			{VolumeName: "data", TotalBytes: 100 * uint64(oneGiB), FreePercent: 5},
		},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 10, MaxRunSizeGB: 50,
	})
	require.NoError(t, err)
	require.Equal(t, triagearr.StopTargetReached, plan.StopReason)
	// need = (10-5)% of 100GiB = 5 GiB ; a(3) + b(2) = 5 → target met after 2 items
	require.Len(t, plan.Items, 2)
	require.Equal(t, triagearr.Hash("a"), plan.Items[0].TorrentHash)
	require.Equal(t, triagearr.Hash("b"), plan.Items[1].TorrentHash)
}

func TestPlan_SizeCap(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{
			{Hash: "a", Score: 100},
			{Hash: "b", Score: 90},
		},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data/x", Size: 6 * oneGiB},
			{Hash: "b", SavePath: "/data/x", Size: 6 * oneGiB},
		},
		disks: []triagearr.DiskUsage{
			{VolumeName: "data", TotalBytes: 1000 * uint64(oneGiB), FreePercent: 0},
		},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 50, MaxRunSizeGB: 5,
	})
	require.NoError(t, err)
	require.Equal(t, triagearr.StopSizeCap, plan.StopReason)
	// cap = 5 GiB ; first item (6 GiB) already exceeds it → 1 item
	require.Len(t, plan.Items, 1)
}

func TestPlan_NoMoreCandidates(t *testing.T) {
	const oneGiB = int64(1024 * 1024 * 1024)
	src := &fakeSrc{
		scores: []store.ScoreRow{{Hash: "a", Score: 1}},
		torrents: []store.TorrentBasic{
			{Hash: "a", SavePath: "/data/x", Size: 1 * oneGiB},
		},
		disks: []triagearr.DiskUsage{
			{VolumeName: "data", TotalBytes: 1000 * uint64(oneGiB), FreePercent: 0},
		},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 50, MaxRunSizeGB: 100,
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
		disks: []triagearr.DiskUsage{
			{VolumeName: "data", TotalBytes: 100 * uint64(oneGiB), FreePercent: 0},
		},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 1, MaxRunSizeGB: 100,
	})
	require.NoError(t, err)
	// 'a' filtered out (different volume) ; 'b' brings 4 GiB > need (1% of 100GiB = 1GiB)
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
		disks: []triagearr.DiskUsage{
			{VolumeName: "data", TotalBytes: 100 * uint64(oneGiB), FreePercent: 80},
		},
	}
	d := decider.New(src)
	plan, err := d.Plan(context.Background(), decider.Volume{
		Name: "data", Path: "/data", TargetFreePercent: 20, MaxRunSizeGB: 100,
	})
	require.NoError(t, err)
	// need = 0 → first candidate already meets target
	require.Equal(t, triagearr.StopTargetReached, plan.StopReason)
	require.Len(t, plan.Items, 1)
}

func TestPlan_UnknownVolume(t *testing.T) {
	src := &fakeSrc{disks: []triagearr.DiskUsage{{VolumeName: "other"}}}
	d := decider.New(src)
	_, err := d.Plan(context.Background(), decider.Volume{Name: "data", Path: "/data"})
	require.Error(t, err)
}

// keep time import used in build matrix where stdlib gets flagged unused.
var _ = time.Now
