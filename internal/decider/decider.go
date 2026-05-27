// Package decider turns the M3 scorer's verdicts into an ordered run plan:
// the set of torrents to delete to bring a volume back above its
// target_free_percent. M4 only emits dry-run plans; M5's Actor consumes
// the same shape and executes deletions.
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
}

// Source is the data contract the Decider reads from. *store.Store satisfies it.
type Source interface {
	ListScores(ctx context.Context, opts store.ListScoresOpts) ([]store.ScoreRow, error)
	ListTorrentsBasic(ctx context.Context) ([]store.TorrentBasic, error)
	LatestDiskUsage(ctx context.Context) (*triagearr.DiskUsage, error)
	// MaxNlinkByHashes feeds the cross-seed pre-filter (SCORING.md): the
	// Decider drops candidates whose max(nlink) > 2 because their on-disk
	// inode is referenced by an additional non-arr peer (cross-seed), so
	// deleting the torrent frees zero bytes. Hashes absent from the result
	// are conservatively kept eligible — the Actor's T3.5 still re-checks
	// atomically at action time.
	MaxNlinkByHashes(ctx context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]int64, error)
	// HashesWithArrImports returns the set of hashes that have at least one
	// arr_imports row. Drives the qbit-only nlink ceiling (Finding 6).
	HashesWithArrImports(ctx context.Context) (map[triagearr.Hash]struct{}, error)
}

// MaxAllowedNlink is the per-file hardlink-count ceiling enforced at election
// time. 2 = qBit + *arr import, the only "healthy" topology. 3+ means a
// cross-seed peer or another holder of the inode — deleting frees no disk.
const MaxAllowedNlink = 2

// RunPlan is the Decider's output: an ordered candidate list plus the volume
// snapshot that justified it.
type RunPlan struct {
	Volume              Volume
	FreePctAtFire       float64
	EstimatedFreedBytes int64
	StopReason          triagearr.RunStopReason
	Items               []triagearr.RunItem
	// FilteredCrossSeed counts candidates removed by the cross-seed pre-filter
	// (max(nlink) > MaxAllowedNlink). Surfaced for logging and the dashboard
	// so the user can see why the plan is shorter than the score list suggests.
	FilteredCrossSeed int
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
// budget is met or candidates are exhausted.
func (d *Decider) Plan(ctx context.Context, v Volume) (RunPlan, error) {
	snap, err := d.src.LatestDiskUsage(ctx)
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: reading disk usage: %w", err)
	}
	if snap == nil {
		return RunPlan{}, fmt.Errorf("decider: no disk_usage snapshot recorded yet")
	}

	needBytes := neededBytes(snap.TotalBytes, snap.FreePercent, v.TargetFreePercent)

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

	// Cross-seed pre-filter: load max(nlink) for every torrent in scope (one
	// query). Hashes with no sampled file stay eligible — see SCORING.md.
	scopedHashes := make([]triagearr.Hash, 0, len(scores))
	for _, sc := range scores {
		if t, ok := byHash[sc.Hash]; ok {
			sp := strings.TrimRight(t.SavePath, "/")
			if sp == volumePath || strings.HasPrefix(sp, prefix) {
				scopedHashes = append(scopedHashes, triagearr.Hash(sc.Hash))
			}
		}
	}
	maxNlink, err := d.src.MaxNlinkByHashes(ctx, scopedHashes)
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: max nlink: %w", err)
	}
	arrLinked, err := d.src.HashesWithArrImports(ctx)
	if err != nil {
		return RunPlan{}, fmt.Errorf("decider: arr imports set: %w", err)
	}

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
		// For qbit-only torrents (no arr_imports) a successful fanoutArr is a
		// no-op, so nlink will still be 2 after *arr deletes and T3.5 will veto
		// the qBit delete. Apply a stricter ceiling of 1 for these candidates.
		_, hasArrLink := arrLinked[triagearr.Hash(sc.Hash)]
		ceiling := MaxAllowedNlink
		if !hasArrLink {
			ceiling = 1
		}
		if n, ok := maxNlink[triagearr.Hash(sc.Hash)]; ok && n > int64(ceiling) {
			plan.FilteredCrossSeed++
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
