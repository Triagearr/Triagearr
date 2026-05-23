// Package decider turns the M3 scorer's verdicts into an ordered run plan:
// the set of torrents to delete to bring a volume back above its
// target_free_percent, capped by max_run_size_gb. M4 only emits dry-run
// plans; M5's Actor consumes the same shape and executes deletions.
package decider

import (
	"context"
	"fmt"
	"strings"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Volume binds a watched filesystem mount to its pressure thresholds.
// Triagearr's config.VolumeConfig already carries this — Decider re-declares
// the shape to keep the package decoupled.
type Volume struct {
	Name              string
	Path              string
	TargetFreePercent float64
	MaxRunSizeGB      int
}

// Source is the data contract the Decider reads from. *store.Store satisfies it.
type Source interface {
	ListScores(ctx context.Context, opts store.ListScoresOpts) ([]store.ScoreRow, error)
	ListTorrentsBasic(ctx context.Context) ([]store.TorrentBasic, error)
	LatestDiskUsage(ctx context.Context) (*triagearr.DiskUsage, error)
}

// RunPlan is the Decider's output: an ordered candidate list plus the volume
// snapshot that justified it.
type RunPlan struct {
	Volume              Volume
	FreePctAtFire       float64
	EstimatedFreedBytes int64
	StopReason          triagearr.RunStopReason
	Items               []triagearr.RunItem
}

// Decider produces RunPlans. Stateless; safe for concurrent use.
type Decider struct {
	src Source
}

// New returns a Decider backed by src.
func New(src Source) *Decider {
	return &Decider{src: src}
}

// Plan computes a run plan for v. It reads the latest disk_usage to determine
// need_bytes (the gap to target_free_percent), then walks scores in DESC
// order, keeping only torrents whose save_path is under v.Path, until the
// budget is met or the size cap is reached.
func (d *Decider) Plan(ctx context.Context, v Volume) (RunPlan, error) {
	snap, err := d.src.LatestDiskUsage(ctx)
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: reading disk usage: %w", err)
	}
	if snap == nil {
		return RunPlan{}, fmt.Errorf("decider: no disk_usage snapshot recorded yet")
	}

	needBytes := neededBytes(snap.TotalBytes, snap.FreePercent, v.TargetFreePercent)
	capBytes := int64(v.MaxRunSizeGB) * 1024 * 1024 * 1024

	scores, err := d.src.ListScores(ctx, store.ListScoresOpts{IncludeExcluded: false})
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: listing scores: %w", err)
	}
	torrents, err := d.src.ListTorrentsBasic(ctx)
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: listing torrents: %w", err)
	}
	byHash := make(map[string]store.TorrentBasic, len(torrents))
	for _, t := range torrents {
		byHash[t.Hash] = t
	}

	volumePath := strings.TrimRight(v.Path, "/")
	prefix := volumePath + "/"

	plan := RunPlan{Volume: v, FreePctAtFire: snap.FreePercent}
	var rank int
	for _, sc := range scores {
		t, ok := byHash[sc.Hash]
		if !ok {
			continue
		}
		sp := strings.TrimRight(t.SavePath, "/")
		if sp != volumePath && !strings.HasPrefix(sp, prefix) {
			continue
		}
		plan.Items = append(plan.Items, triagearr.RunItem{
			Rank:           rank,
			TorrentHash:    triagearr.Hash(sc.Hash),
			Score:          sc.Score,
			SizeBytes:      t.Size,
			WouldFreeBytes: t.Size,
		})
		plan.EstimatedFreedBytes += t.Size
		rank++

		if capBytes > 0 && plan.EstimatedFreedBytes >= capBytes {
			plan.StopReason = triagearr.StopSizeCap
			return plan, nil
		}
		if plan.EstimatedFreedBytes >= needBytes {
			plan.StopReason = triagearr.StopTargetReached
			return plan, nil
		}
	}
	plan.StopReason = triagearr.StopNoMoreCandidates
	return plan, nil
}

// neededBytes returns how many bytes must be freed to reach targetPct.
// Zero when current free% already meets or exceeds target.
func neededBytes(totalBytes uint64, freePct, targetPct float64) int64 {
	if freePct >= targetPct {
		return 0
	}
	gap := targetPct - freePct
	return int64(float64(totalBytes) * gap / 100.0)
}
