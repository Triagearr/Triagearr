package scorer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// Scorer computes DeleteScores from the store state.
//
// ScoreAll walks every torrent twice: pass 1 fetches snapshot stats and builds
// the global velocity normaliser, pass 2 evaluates factors against the cached
// stats. ScoreOne is the per-hash explain path; it walks the library once to
// rebuild the same normaliser (acceptable — a single-hash explain is rare).
type Scorer struct {
	cfg   config.ScoringConfig
	qb    config.QbitConfig
	arrs  config.ArrsConfig
	store *store.Store
	now   func() time.Time
}

// Options configure a Scorer. Now is injected so tests can pin time.
type Options struct {
	Cfg   config.ScoringConfig
	Qbit  config.QbitConfig
	Arrs  config.ArrsConfig
	Store *store.Store
	Now   func() time.Time
}

// New builds a Scorer. When Opts.Now is nil, time.Now is used.
func New(opts Options) *Scorer {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Scorer{
		cfg:   opts.Cfg,
		qb:    opts.Qbit,
		arrs:  opts.Arrs,
		store: opts.Store,
		now:   now,
	}
}

// ScoreOne computes and persists the score for one torrent. Returns the
// breakdown the CLI/UI surface as-is.
func (s *Scorer) ScoreOne(ctx context.Context, hash triagearr.Hash) (Breakdown, error) {
	t, err := s.store.GetTorrentForScoring(ctx, hash)
	if err != nil {
		return Breakdown{}, err
	}
	now := s.now()
	snaps, err := s.store.ScoringSnapshotStats(ctx, hash, now)
	if err != nil {
		return Breakdown{}, fmt.Errorf("loading snapshot stats: %w", err)
	}
	globalAvg, err := s.computeGlobalAvgVelocity(ctx, now, map[string]store.SnapshotStats{t.Hash: snaps})
	if err != nil {
		return Breakdown{}, err
	}
	b, err := s.scoreInputs(ctx, t, snaps, globalAvg)
	if err != nil {
		return Breakdown{}, err
	}
	if err := s.persist(ctx, b); err != nil {
		return b, err
	}
	return b, nil
}

// ScoreAllStats summarises one ScoreAll pass for the loop logger.
type ScoreAllStats struct {
	Scored   int
	Excluded int
	Errors   int
	Duration time.Duration
}

// ScoreAll walks every torrent, persists a score row, and returns counts.
// Individual torrent errors are logged but never abort the pass — one bad
// row should not blind the Decider to the rest of the library.
func (s *Scorer) ScoreAll(ctx context.Context) (ScoreAllStats, error) {
	start := s.now()
	torrents, err := s.store.ListTorrentsForScoring(ctx)
	if err != nil {
		return ScoreAllStats{}, err
	}
	now := start

	// Pass 1: snapshot stats per torrent. The map drives both the global avg
	// computation and the per-torrent factor pass below — each torrent's
	// stats are loaded exactly once.
	statsByHash := make(map[string]store.SnapshotStats, len(torrents))
	var stats ScoreAllStats
	for _, t := range torrents {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		sn, err := s.store.ScoringSnapshotStats(ctx, triagearr.Hash(t.Hash), now)
		if err != nil {
			slog.Warn("loading snapshot stats failed", "hash", t.Hash, "err", err)
			stats.Errors++
			continue
		}
		statsByHash[t.Hash] = sn
	}
	globalAvg := globalAvgFromStats(statsByHash)

	// Batch the trackers + linked-media joins once for the whole library so
	// scoreInputs avoids two SQL round-trips per torrent.
	trackersByHash, err := s.store.ListTrackersAll(ctx)
	if err != nil {
		return stats, fmt.Errorf("prefetch trackers: %w", err)
	}
	linkedByHash, err := s.store.LinkedMediaAll(ctx)
	if err != nil {
		return stats, fmt.Errorf("prefetch linked media: %w", err)
	}

	// Pass 2: evaluate factors against the cached stats.
	for _, t := range torrents {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		sn, ok := statsByHash[t.Hash]
		if !ok {
			// Pass-1 error already counted; skip rather than double-count.
			continue
		}
		b, err := s.scoreWithPrefetched(t, sn, globalAvg, trackersByHash[t.Hash], linkedByHash[t.Hash])
		if err != nil {
			slog.Warn("scoring torrent failed", "hash", t.Hash, "err", err)
			stats.Errors++
			continue
		}
		if err := s.persist(ctx, b); err != nil {
			slog.Warn("persisting score failed", "hash", t.Hash, "err", err)
			stats.Errors++
			continue
		}
		stats.Scored++
		if b.Excluded {
			stats.Excluded++
		}
	}
	stats.Duration = s.now().Sub(start)
	return stats, nil
}

// ScorePass runs one ScoreAll pass and logs its outcome. It is the unit of
// work the event-driven Loop schedules whenever a feeding poller reports
// fresh data.
func (s *Scorer) ScorePass(ctx context.Context) error {
	stats, err := s.ScoreAll(ctx)
	if err != nil {
		return err
	}
	slog.Info("score pass complete",
		"scored", stats.Scored,
		"excluded", stats.Excluded,
		"errors", stats.Errors,
		"duration", stats.Duration.String(),
	)
	return nil
}

// computeGlobalAvgVelocity rebuilds the normaliser for the ScoreOne path. It
// walks every torrent's snapshot stats once. The seed map lets the caller
// avoid re-fetching the target hash that ScoreOne already loaded.
func (s *Scorer) computeGlobalAvgVelocity(ctx context.Context, now time.Time, seed map[string]store.SnapshotStats) (float64, error) {
	torrents, err := s.store.ListTorrentsForScoring(ctx)
	if err != nil {
		return 0, err
	}
	by := make(map[string]store.SnapshotStats, len(torrents))
	for k, v := range seed {
		by[k] = v
	}
	for _, t := range torrents {
		if _, ok := by[t.Hash]; ok {
			continue
		}
		sn, err := s.store.ScoringSnapshotStats(ctx, triagearr.Hash(t.Hash), now)
		if err != nil {
			return 0, fmt.Errorf("loading snapshot stats for %s: %w", t.Hash, err)
		}
		by[t.Hash] = sn
	}
	return globalAvgFromStats(by), nil
}

// globalAvgFromStats averages the per-torrent VelocityBytesPerDay across the
// library. Torrents with zero velocity (insufficient history, or genuinely
// idle) are excluded from the average so the normaliser reflects the active
// signal rather than being dragged toward zero.
func globalAvgFromStats(by map[string]store.SnapshotStats) float64 {
	var sum float64
	var n int
	for _, sn := range by {
		if sn.VelocityBytesPerDay > 0 {
			sum += sn.VelocityBytesPerDay
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// scoreInputs runs the seven factors over already-fetched torrent metadata.
// Snapshot stats are passed in so ScoreAll's two-pass flow doesn't re-fetch
// them per torrent — the caller is responsible for loading them once. This
// fetches trackers + linked media individually; ScoreAll prefers
// scoreWithPrefetched to avoid the per-torrent round-trip.
func (s *Scorer) scoreInputs(ctx context.Context, t store.ScoringTorrent, snaps store.SnapshotStats, globalAvg float64) (Breakdown, error) {
	trackerRows, err := s.store.ListTrackers(ctx, triagearr.Hash(t.Hash))
	if err != nil {
		return Breakdown{}, fmt.Errorf("loading trackers: %w", err)
	}
	linked, err := s.store.LinkedMediaForHash(ctx, triagearr.Hash(t.Hash))
	if err != nil {
		return Breakdown{}, fmt.Errorf("loading linked media: %w", err)
	}
	return s.scoreWithPrefetched(t, snaps, globalAvg, trackerRows, linked)
}

// scoreWithPrefetched is the pure factor-evaluation path used by ScoreAll
// once trackers + linked media have been bulk-loaded.
func (s *Scorer) scoreWithPrefetched(t store.ScoringTorrent, snaps store.SnapshotStats, globalAvg float64, trackerRows []store.TrackerRow, linked []store.LinkedMedia) (Breakdown, error) {
	now := s.now()
	trackers := trackerViewsFromRows(trackerRows)

	alive := anyTrackerAlive(trackers)
	policy := trackerPolicyFor(trackers, s.cfg)
	rareThreshold := effectiveRareThreshold(policy, s.cfg)

	w := s.cfg.Weights
	factors := []Factor{
		factorRatioObligation(t, snaps.LatestRatio, policy, now, w.RatioObligationMet),
		factorVelocityInv(snaps.VelocityBytesPerDay, globalAvg, w.UploadVelocityInv),
		factorAge(t, now, w.AgeDays),
		factorSeedersGuard(snaps.SeedersAvg7d, rareThreshold, alive, w.SeedersLowGuard),
		factorSwarmBonus(snaps.SeedersAvg7d, w.SwarmHealthBonus),
		factorHnRVeto(t, alive, s.cfg.HnRWindowDays, now),
		factorTrackerDead(trackers, now, s.cfg.TrackerDeadGrace, w.TrackerDeadBonus),
	}

	var total float64
	for _, f := range factors {
		total += f.Contribution
	}

	reasons := evaluateExclusions(t, linked, s.qb, s.arrs)
	return Breakdown{
		Hash:             t.Hash,
		Score:            total,
		Private:          t.Private,
		AnyTrackerAlive:  alive,
		Excluded:         len(reasons) > 0,
		ExclusionReasons: reasons,
		Factors:          factors,
		ComputedAt:       now,
	}, nil
}

func (s *Scorer) persist(ctx context.Context, b Breakdown) error {
	payload, err := json.Marshal(b.Factors)
	if err != nil {
		return fmt.Errorf("marshalling factors: %w", err)
	}
	return s.store.UpsertScore(ctx, store.ScoreRow{
		Hash:             b.Hash,
		Score:            b.Score,
		Private:          b.Private,
		AnyTrackerAlive:  b.AnyTrackerAlive,
		Excluded:         b.Excluded,
		ExclusionReasons: strings.Join(b.ExclusionReasons, ","),
		FactorsJSON:      string(payload),
		ComputedAt:       b.ComputedAt,
	})
}
