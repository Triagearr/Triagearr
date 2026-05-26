package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ScoringDefaultsRow is the DB shape of the singleton row. The struct mirrors
// triagearr.ScoringDefaults but is the SELECT target.
type scoringDefaultsRow struct {
	ID            int       `db:"id"`
	MinRatio      float64   `db:"min_ratio"`
	MinSeedDays   int       `db:"min_seed_days"`
	RareThreshold int       `db:"rare_threshold"`
	UpdatedAt     time.Time `db:"updated_at"`
}

// GetScoringDefaults reads the singleton row. The row is seeded at migration
// time with conservative defaults (ADR-0026), so this never returns
// sql.ErrNoRows under normal operation.
func (s *Store) GetScoringDefaults(ctx context.Context) (triagearr.ScoringDefaults, error) {
	var r scoringDefaultsRow
	err := s.reader.GetContext(ctx, &r, `
		SELECT id, min_ratio, min_seed_days, rare_threshold, updated_at
		FROM scoring_defaults WHERE id = 1
	`)
	if err != nil {
		return triagearr.ScoringDefaults{}, fmt.Errorf("loading scoring_defaults: %w", err)
	}
	return triagearr.ScoringDefaults{
		MinRatio:      r.MinRatio,
		MinSeedDays:   r.MinSeedDays,
		RareThreshold: r.RareThreshold,
		UpdatedAt:     r.UpdatedAt,
	}, nil
}

// SetScoringDefaults updates the singleton row. The row is created at
// migration time so this is always an UPDATE — the WHERE id=1 clause guards
// against accidentally writing multiple rows.
func (s *Store) SetScoringDefaults(ctx context.Context, d triagearr.ScoringDefaults) error {
	now := ts(time.Now().UTC())
	_, err := s.writer.ExecContext(ctx, `
		UPDATE scoring_defaults
		   SET min_ratio = ?, min_seed_days = ?, rare_threshold = ?, updated_at = ?
		 WHERE id = 1
	`, d.MinRatio, d.MinSeedDays, d.RareThreshold, now)
	if err != nil {
		return fmt.Errorf("updating scoring_defaults: %w", err)
	}
	return nil
}

type trackerPolicyRow struct {
	TrackerHost   string        `db:"tracker_host"`
	MinRatio      float64       `db:"min_ratio"`
	MinSeedDays   int           `db:"min_seed_days"`
	RareThreshold sql.NullInt64 `db:"rare_threshold"`
	Enabled       bool          `db:"enabled"`
	UpdatedAt     time.Time     `db:"updated_at"`
}

func (r trackerPolicyRow) toPolicy() triagearr.TrackerPolicy {
	p := triagearr.TrackerPolicy{
		TrackerHost: r.TrackerHost,
		MinRatio:    r.MinRatio,
		MinSeedDays: r.MinSeedDays,
		Enabled:     r.Enabled,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.RareThreshold.Valid {
		v := int(r.RareThreshold.Int64)
		p.RareThreshold = &v
	}
	return p
}

// ListTrackerPolicies returns every configured policy row, ordered by host
// for a stable UI listing.
func (s *Store) ListTrackerPolicies(ctx context.Context) ([]triagearr.TrackerPolicy, error) {
	var rows []trackerPolicyRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT tracker_host, min_ratio, min_seed_days, rare_threshold, enabled, updated_at
		FROM tracker_policies
		ORDER BY tracker_host
	`); err != nil {
		return nil, fmt.Errorf("listing tracker_policies: %w", err)
	}
	out := make([]triagearr.TrackerPolicy, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toPolicy())
	}
	return out, nil
}

// GetTrackerPolicy returns one row by host. sql.ErrNoRows when absent.
func (s *Store) GetTrackerPolicy(ctx context.Context, host string) (triagearr.TrackerPolicy, error) {
	var r trackerPolicyRow
	err := s.reader.GetContext(ctx, &r, `
		SELECT tracker_host, min_ratio, min_seed_days, rare_threshold, enabled, updated_at
		FROM tracker_policies WHERE tracker_host = ?
	`, host)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return triagearr.TrackerPolicy{}, err
		}
		return triagearr.TrackerPolicy{}, fmt.Errorf("loading tracker_policy %s: %w", host, err)
	}
	return r.toPolicy(), nil
}

// UpsertTrackerPolicy inserts or replaces one row. The host is the natural key.
func (s *Store) UpsertTrackerPolicy(ctx context.Context, p triagearr.TrackerPolicy) (triagearr.TrackerPolicy, error) {
	if p.TrackerHost == "" {
		return triagearr.TrackerPolicy{}, fmt.Errorf("tracker_policy: tracker_host required")
	}
	var rareCol any
	if p.RareThreshold != nil {
		rareCol = *p.RareThreshold
	}
	now := ts(time.Now().UTC())
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO tracker_policies(tracker_host, min_ratio, min_seed_days, rare_threshold, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tracker_host) DO UPDATE SET
			min_ratio      = excluded.min_ratio,
			min_seed_days  = excluded.min_seed_days,
			rare_threshold = excluded.rare_threshold,
			enabled        = excluded.enabled,
			updated_at     = excluded.updated_at
	`, p.TrackerHost, p.MinRatio, p.MinSeedDays, rareCol, boolToInt(p.Enabled), now)
	if err != nil {
		return triagearr.TrackerPolicy{}, fmt.Errorf("upserting tracker_policy %s: %w", p.TrackerHost, err)
	}
	return s.GetTrackerPolicy(ctx, p.TrackerHost)
}

// DeleteTrackerPolicy removes one row. sql.ErrNoRows when the host has no
// policy configured (i.e. the UI tried to reset an already-default tracker).
func (s *Store) DeleteTrackerPolicy(ctx context.Context, host string) error {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM tracker_policies WHERE tracker_host = ?`, host)
	if err != nil {
		return fmt.Errorf("deleting tracker_policy %s: %w", host, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reading delete result for tracker_policy %s: %w", host, err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// TrackerHostStat is one row in the UI's tracker list: a host observed in
// torrent_trackers, with operational signals (counts + dead flag) and the
// configured policy (when any).
type TrackerHostStat struct {
	Host         string
	TorrentCount int
	AnyAlive     bool
	AllDead      bool
	Policy       *triagearr.TrackerPolicy
}

// ListTrackerHostStats walks torrent_trackers, groups by host, and joins the
// tracker_policies row when present. Used by the Settings → Scoring UI to
// drive the per-tracker table. Hosts with no torrents do not appear; the user
// can only configure trackers their library has actually seen (ADR-0026).
func (s *Store) ListTrackerHostStats(ctx context.Context) ([]TrackerHostStat, error) {
	type aggRow struct {
		Host       string `db:"tracker_host"`
		Total      int    `db:"total"`
		AliveCount int    `db:"alive_count"`
	}
	var rows []aggRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT tracker_host,
		       COUNT(DISTINCT torrent_hash) AS total,
		       SUM(CASE WHEN status != ? THEN 1 ELSE 0 END) AS alive_count
		FROM torrent_trackers
		GROUP BY tracker_host
		ORDER BY tracker_host
	`, int(triagearr.TrackerNotWorking)); err != nil {
		return nil, fmt.Errorf("aggregating tracker_host stats: %w", err)
	}

	policies, err := s.ListTrackerPolicies(ctx)
	if err != nil {
		return nil, err
	}
	byHost := make(map[string]triagearr.TrackerPolicy, len(policies))
	for _, p := range policies {
		byHost[p.TrackerHost] = p
	}

	out := make([]TrackerHostStat, 0, len(rows))
	for _, r := range rows {
		stat := TrackerHostStat{
			Host:         r.Host,
			TorrentCount: r.Total,
			AnyAlive:     r.AliveCount > 0,
			AllDead:      r.AliveCount == 0,
		}
		if p, ok := byHost[r.Host]; ok {
			pol := p
			stat.Policy = &pol
		}
		out = append(out, stat)
	}

	// Surface configured policies whose host has zero torrents — the user may
	// have configured them speculatively then deleted matching torrents. The
	// UI shows them with TorrentCount=0 so they can still be cleaned up.
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		seen[r.Host] = struct{}{}
	}
	for _, p := range policies {
		if _, ok := seen[p.TrackerHost]; ok {
			continue
		}
		pol := p
		out = append(out, TrackerHostStat{Host: p.TrackerHost, Policy: &pol})
	}
	return out, nil
}
