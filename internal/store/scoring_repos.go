package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ScoringTorrent is the per-torrent state the M3 scorer consumes. It bundles
// the columns the scorer cares about (private regime, age/HnR anchors, tags
// for exclusions) into one struct so the scorer does not run separate queries
// per field.
type ScoringTorrent struct {
	Hash         string     `db:"hash"`
	Name         string     `db:"name"`
	Category     string     `db:"category"`
	Tags         string     `db:"tags"`
	Size         int64      `db:"size"`
	AddedOn      time.Time  `db:"added_on"`
	CompletionOn *time.Time `db:"completion_on"`
	Private      bool       `db:"private"`
	Protected    bool       `db:"protected"`
}

// GetTorrentForScoring loads one torrent's scoring fields. Returns sql.ErrNoRows
// if the hash is unknown.
func (s *Store) GetTorrentForScoring(ctx context.Context, hash triagearr.Hash) (ScoringTorrent, error) {
	var row ScoringTorrent
	err := s.reader.GetContext(ctx, &row, `
		SELECT hash, name, category, tags, size, added_on, completion_on, private, protected
		FROM torrents WHERE hash = ?
	`, string(hash))
	if err != nil {
		return ScoringTorrent{}, fmt.Errorf("loading torrent %s for scoring: %w", hash, err)
	}
	return row, nil
}

// ListTorrentsForScoring streams every torrent currently observed.
func (s *Store) ListTorrentsForScoring(ctx context.Context) ([]ScoringTorrent, error) {
	var rows []ScoringTorrent
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT hash, name, category, tags, size, added_on, completion_on, private, protected
		FROM torrents ORDER BY hash
	`); err != nil {
		return nil, fmt.Errorf("listing torrents for scoring: %w", err)
	}
	return rows, nil
}

// SnapshotStats summarises the recent-window aggregates the scorer needs for
// one torrent: seeders average (Factor 4/5), the latest known ratio (Factor 1),
// and the upload velocity in bytes/day (Factor 2) computed over up to 30 days
// by blending snapshots_raw (recent) with snapshots_daily.uploaded_max (older).
type SnapshotStats struct {
	SeedersAvg7d        float64
	VelocityBytesPerDay float64
	LatestRatio         float64
}

// velocityWindowDays is the SCORING.md §Factor 2 window. The query returns
// zero velocity when the available history is shorter (the scorer treats this
// as a documented "insufficient data" case and zeroes the factor).
const velocityWindowDays = 30

// ScoringSnapshotStats computes the recent-window aggregates for one hash.
// Returns zeroed values when no snapshots exist.
func (s *Store) ScoringSnapshotStats(ctx context.Context, hash triagearr.Hash, now time.Time) (SnapshotStats, error) {
	cutoff7d := ts(now.Add(-7 * 24 * time.Hour))
	cutoff30d := ts(now.Add(-velocityWindowDays * 24 * time.Hour))

	// Seeders average over the 7-day window: blends snapshots_raw (recent) with
	// snapshots_daily (whatever overlaps the window if raw retention < 7d).
	var seedersAvg sql.NullFloat64
	if err := s.reader.GetContext(ctx, &seedersAvg, `
		WITH combined AS (
			SELECT CAST(seeders AS REAL) AS s
			FROM snapshots_raw
			WHERE torrent_hash = ? AND ts >= ?
			UNION ALL
			SELECT seeders_avg AS s
			FROM snapshots_daily
			WHERE torrent_hash = ? AND day >= date(?)
		)
		SELECT AVG(s) FROM combined
	`, string(hash), cutoff7d, string(hash), cutoff7d); err != nil {
		return SnapshotStats{}, fmt.Errorf("computing seeders_avg_7d for %s: %w", hash, err)
	}

	// Latest ratio: most recent snapshots_raw row, falling back to the most
	// recent snapshots_daily aggregate.
	var latestRatio sql.NullFloat64
	if err := s.reader.GetContext(ctx, &latestRatio, `
		SELECT COALESCE(
			(SELECT ratio FROM snapshots_raw WHERE torrent_hash = ? ORDER BY ts DESC LIMIT 1),
			(SELECT ratio_avg FROM snapshots_daily WHERE torrent_hash = ? ORDER BY day DESC LIMIT 1)
		)
	`, string(hash), string(hash)); err != nil {
		return SnapshotStats{}, fmt.Errorf("loading latest ratio for %s: %w", hash, err)
	}

	// Velocity newest + anchor across raw + daily. snapshots_daily.uploaded_max
	// is filtered > 0 to skip pre-migration rows that carry the default zero —
	// distinguishing legacy zero from a genuine first-day zero requires a
	// marker we don't store; treating zero as "no data" loses at most one day
	// of velocity signal on freshly-added torrents.
	type velPoint struct {
		Ts sql.NullString `db:"ts"`
		Up sql.NullInt64  `db:"uploaded"`
	}

	var newest velPoint
	if err := s.reader.GetContext(ctx, &newest, `
		SELECT ts, uploaded FROM (
			SELECT ts, uploaded FROM snapshots_raw WHERE torrent_hash = ?
			UNION ALL
			SELECT day || 'T00:00:00Z' AS ts, uploaded_max AS uploaded
			FROM snapshots_daily WHERE torrent_hash = ? AND uploaded_max > 0
		) ORDER BY ts DESC LIMIT 1
	`, string(hash), string(hash)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SnapshotStats{}, fmt.Errorf("loading newest velocity point for %s: %w", hash, err)
	}

	var anchor velPoint
	if err := s.reader.GetContext(ctx, &anchor, `
		SELECT ts, uploaded FROM (
			SELECT ts, uploaded FROM snapshots_raw WHERE torrent_hash = ? AND ts >= ?
			UNION ALL
			SELECT day || 'T00:00:00Z' AS ts, uploaded_max AS uploaded
			FROM snapshots_daily
			WHERE torrent_hash = ? AND day >= date(?) AND uploaded_max > 0
		) ORDER BY ts ASC LIMIT 1
	`, string(hash), cutoff30d, string(hash), cutoff30d); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SnapshotStats{}, fmt.Errorf("loading anchor velocity point for %s: %w", hash, err)
	}

	out := SnapshotStats{}
	if seedersAvg.Valid {
		out.SeedersAvg7d = seedersAvg.Float64
	}
	if latestRatio.Valid {
		out.LatestRatio = latestRatio.Float64
	}
	if newest.Ts.Valid && anchor.Ts.Valid && newest.Up.Valid && anchor.Up.Valid {
		newT, errN := parseTS(newest.Ts.String)
		anchT, errA := parseTS(anchor.Ts.String)
		if errN == nil && errA == nil {
			span := newT.Sub(anchT).Hours() / 24.0
			if span > velocityWindowDays {
				span = velocityWindowDays
			}
			if span > 0 {
				delta := float64(newest.Up.Int64 - anchor.Up.Int64)
				if delta < 0 {
					delta = 0
				}
				out.VelocityBytesPerDay = delta / span
			}
		}
	}
	return out, nil
}

// parseTS reads back a timestamp written by ts(). modernc.org/sqlite returns
// raw text for aggregate expressions (MIN/MAX) instead of going through the
// time-value codec, so we re-parse the RFC3339Nano string we wrote.
func parseTS(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing timestamp %q: %w", s, err)
	}
	return t, nil
}

// LinkedMedia is one *arr-side media linked to a torrent via arr_imports.
// Tags are the comma-separated string stored in media.tags; the scorer
// splits them when matching against arrs.<type>.tags_exclude.
type LinkedMedia struct {
	ArrType string `db:"arr_type"`
	MediaID int64  `db:"media_id"`
	Tags    string `db:"tags"`
}

// LinkedMediaForHash returns the distinct media items linked to one download_id
// (qBit hash) through arr_imports.
func (s *Store) LinkedMediaForHash(ctx context.Context, hash triagearr.Hash) ([]LinkedMedia, error) {
	var rows []LinkedMedia
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT DISTINCT m.arr_type, m.id AS media_id, m.tags
		FROM arr_imports ai
		JOIN media_files mf
		  ON mf.arr_type = ai.arr_type
		 AND mf.file_id  = ai.file_id
		JOIN media m
		  ON m.arr_type = mf.arr_type
		 AND m.id       = mf.media_id
		WHERE ai.download_id = ?
		ORDER BY m.arr_type, m.id
	`, string(hash)); err != nil {
		return nil, fmt.Errorf("listing linked media for %s: %w", hash, err)
	}
	return rows, nil
}

// LinkedMediaAll returns the distinct media items linked to every download_id
// present in arr_imports, grouped by lowercased hash. Used by the scoring
// pass to avoid a per-torrent round-trip (N+1).
func (s *Store) LinkedMediaAll(ctx context.Context) (map[string][]LinkedMedia, error) {
	type row struct {
		Hash    string `db:"download_id"`
		ArrType string `db:"arr_type"`
		MediaID int64  `db:"media_id"`
		Tags    string `db:"tags"`
	}
	var rows []row
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT DISTINCT LOWER(ai.download_id) AS download_id,
		       m.arr_type, m.id AS media_id, m.tags
		FROM arr_imports ai
		JOIN media_files mf
		  ON mf.arr_type = ai.arr_type
		 AND mf.file_id  = ai.file_id
		JOIN media m
		  ON m.arr_type = mf.arr_type
		 AND m.id       = mf.media_id
		ORDER BY ai.download_id, m.arr_type, m.id
	`); err != nil {
		return nil, fmt.Errorf("listing all linked media: %w", err)
	}
	out := make(map[string][]LinkedMedia, len(rows))
	for _, r := range rows {
		out[r.Hash] = append(out[r.Hash], LinkedMedia{
			ArrType: r.ArrType, MediaID: r.MediaID, Tags: r.Tags,
		})
	}
	return out, nil
}

// ListTrackersAll returns every torrent's trackers grouped by torrent_hash.
// Used by the scoring pass to avoid one ListTrackers call per torrent.
func (s *Store) ListTrackersAll(ctx context.Context) (map[string][]TrackerRow, error) {
	var rows []TrackerRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT torrent_hash, tracker_url, tracker_host, status, last_msg, last_checked, first_seen_dead
		FROM torrent_trackers
		ORDER BY torrent_hash, tracker_host, tracker_url
	`); err != nil {
		return nil, fmt.Errorf("listing all trackers: %w", err)
	}
	out := make(map[string][]TrackerRow)
	for _, r := range rows {
		out[r.TorrentHash] = append(out[r.TorrentHash], r)
	}
	return out, nil
}

// ScoreRow is the persisted scoring verdict per torrent. Name is only populated
// by ListScores (which joins the torrents table); GetScore leaves it empty.
type ScoreRow struct {
	Hash             string    `db:"torrent_hash"`
	Name             string    `db:"name"`
	Score            float64   `db:"score"`
	Private          bool      `db:"private"`
	AnyTrackerAlive  bool      `db:"any_tracker_alive"`
	Excluded         bool      `db:"excluded"`
	ExclusionReasons string    `db:"exclusion_reasons"`
	FactorsJSON      string    `db:"factors_json"`
	ComputedAt       time.Time `db:"computed_at"`
}

// UpsertScore writes (or replaces) one score row. The verdict and its factor
// breakdown live in separate tables (scores / score_factors), so the write is
// wrapped in a transaction to keep the two in sync.
func (s *Store) UpsertScore(ctx context.Context, row ScoreRow) error {
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert score %s: %w", row.Hash, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scores(torrent_hash, score, private, any_tracker_alive, excluded, exclusion_reasons, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(torrent_hash) DO UPDATE SET
			score=excluded.score,
			private=excluded.private,
			any_tracker_alive=excluded.any_tracker_alive,
			excluded=excluded.excluded,
			exclusion_reasons=excluded.exclusion_reasons,
			computed_at=excluded.computed_at
	`, row.Hash, row.Score, row.Private, row.AnyTrackerAlive, row.Excluded, row.ExclusionReasons, ts(row.ComputedAt)); err != nil {
		return fmt.Errorf("upserting score %s: %w", row.Hash, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO score_factors(torrent_hash, factors_json)
		VALUES (?, ?)
		ON CONFLICT(torrent_hash) DO UPDATE SET factors_json=excluded.factors_json
	`, row.Hash, row.FactorsJSON); err != nil {
		return fmt.Errorf("upserting score factors %s: %w", row.Hash, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert score %s: %w", row.Hash, err)
	}
	return nil
}

// GetScore returns the persisted score row for one hash, including the factor
// breakdown. Returns sql.ErrNoRows when the scorer has not produced a verdict
// yet.
func (s *Store) GetScore(ctx context.Context, hash triagearr.Hash) (ScoreRow, error) {
	var row ScoreRow
	err := s.reader.GetContext(ctx, &row, `
		SELECT sc.torrent_hash, sc.score, sc.private, sc.any_tracker_alive,
		       sc.excluded, sc.exclusion_reasons, sc.computed_at,
		       COALESCE(sf.factors_json, '') AS factors_json
		FROM scores sc
		LEFT JOIN score_factors sf ON sf.torrent_hash = sc.torrent_hash
		WHERE sc.torrent_hash = ?
	`, string(hash))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ScoreRow{}, err
		}
		return ScoreRow{}, fmt.Errorf("loading score %s: %w", hash, err)
	}
	return row, nil
}

// ListScoresOpts tunes ListScores.
type ListScoresOpts struct {
	// IncludeExcluded leaves rows flagged excluded=1 in the result. The
	// default (false) is what M4's Decider wants: only eligible candidates.
	IncludeExcluded bool
	// WithFactors joins score_factors to populate FactorsJSON. The Decider
	// ranks by score alone, so it leaves this false to avoid reading the
	// breakdown blob for every candidate; the explain/UI paths set it true.
	WithFactors bool
	// Limit caps the number of rows. <= 0 means no limit.
	Limit int
}

// ListScores returns score rows ordered by score descending (most-deletable
// first), joined to the torrents table so callers get a human-readable name.
func (s *Store) ListScores(ctx context.Context, opts ListScoresOpts) ([]ScoreRow, error) {
	factorsCol := "'' AS factors_json"
	factorsJoin := ""
	if opts.WithFactors {
		factorsCol = "COALESCE(sf.factors_json, '') AS factors_json"
		factorsJoin = "LEFT JOIN score_factors sf ON sf.torrent_hash = sc.torrent_hash"
	}
	q := fmt.Sprintf(`
		SELECT sc.torrent_hash, sc.score, sc.private, sc.any_tracker_alive,
		       sc.excluded, sc.exclusion_reasons, sc.computed_at,
		       %s,
		       COALESCE(t.name, sc.torrent_hash) AS name
		FROM scores sc
		LEFT JOIN torrents t ON t.hash = sc.torrent_hash
		%s
	`, factorsCol, factorsJoin)
	if !opts.IncludeExcluded {
		q += ` WHERE sc.excluded = 0`
	}
	q += ` ORDER BY sc.score DESC, sc.torrent_hash ASC`
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	var rows []ScoreRow
	if err := s.reader.SelectContext(ctx, &rows, q); err != nil {
		return nil, fmt.Errorf("listing scores: %w", err)
	}
	return rows, nil
}

// HashesWithArrImports returns the set of download_id hashes that have at
// least one row in arr_imports. Used by the Decider to distinguish qbit-only
// torrents (no *arr hardlink partner) from arr-managed ones when applying
// the nlink cross-seed pre-filter.
func (s *Store) HashesWithArrImports(ctx context.Context) (map[triagearr.Hash]struct{}, error) {
	var hashes []string
	if err := s.reader.SelectContext(ctx, &hashes,
		`SELECT DISTINCT LOWER(download_id) FROM arr_imports`,
	); err != nil {
		return nil, fmt.Errorf("listing arr_imports hashes: %w", err)
	}
	out := make(map[triagearr.Hash]struct{}, len(hashes))
	for _, h := range hashes {
		out[triagearr.Hash(h)] = struct{}{}
	}
	return out, nil
}
