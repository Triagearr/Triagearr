package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ReplaceTrackers atomically replaces the set of trackers for one torrent.
// Trackers disappear from qBit (user removed one) — without this, stale rows
// would survive forever.
//
// first_seen_dead is preserved across the rewrite when a tracker stays in
// not_working, set to now on the alive→dead transition (or first observation
// of a dead tracker), and cleared back to NULL when the tracker recovers.
// qBit does not expose a "status changed at" field, so the transition must
// be observed here.
func (s *Store) ReplaceTrackers(ctx context.Context, hash triagearr.Hash, infos []triagearr.TrackerInfo) error {
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for trackers %s: %w", hash, err)
	}
	defer func() { _ = tx.Rollback() }()

	type prev struct {
		status        int
		firstSeenDead *time.Time
	}
	prevByURL := map[string]prev{}
	rows, err := tx.QueryxContext(ctx,
		`SELECT tracker_url, status, first_seen_dead FROM torrent_trackers WHERE torrent_hash = ?`,
		string(hash),
	)
	if err != nil {
		return fmt.Errorf("loading prior trackers %s: %w", hash, err)
	}
	for rows.Next() {
		var u string
		var st int
		var fsd *time.Time
		if err := rows.Scan(&u, &st, &fsd); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scanning prior tracker %s: %w", hash, err)
		}
		prevByURL[u] = prev{status: st, firstSeenDead: fsd}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterating prior trackers %s: %w", hash, err)
	}
	_ = rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_trackers WHERE torrent_hash = ?`, string(hash)); err != nil {
		return fmt.Errorf("clearing trackers %s: %w", hash, err)
	}
	now := time.Now().UTC()
	deadStatus := int(triagearr.TrackerNotWorking)
	for _, info := range infos {
		var firstSeenDead any
		if int(info.Status) == deadStatus {
			if p, ok := prevByURL[info.URL]; ok && p.status == deadStatus && p.firstSeenDead != nil {
				firstSeenDead = ts(*p.firstSeenDead)
			} else {
				firstSeenDead = ts(now)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO torrent_trackers(torrent_hash, tracker_url, tracker_host, status, last_msg, last_checked, first_seen_dead)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, string(hash), info.URL, info.Host, int(info.Status), info.Msg, ts(now), firstSeenDead); err != nil {
			return fmt.Errorf("inserting tracker %s/%s: %w", hash, info.URL, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trackers %s: %w", hash, err)
	}
	return nil
}

// TrackerRow is the persisted view used by `inspect trackers`.
type TrackerRow struct {
	TorrentHash   string                  `db:"torrent_hash"`
	URL           string                  `db:"tracker_url"`
	Host          string                  `db:"tracker_host"`
	Status        triagearr.TrackerStatus `db:"status"`
	Msg           string                  `db:"last_msg"`
	LastChecked   time.Time               `db:"last_checked"`
	FirstSeenDead *time.Time              `db:"first_seen_dead"`
}

// ListTrackers returns all trackers attached to a torrent.
func (s *Store) ListTrackers(ctx context.Context, hash triagearr.Hash) ([]TrackerRow, error) {
	var rows []TrackerRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT torrent_hash, tracker_url, tracker_host, status, last_msg, last_checked, first_seen_dead
		FROM torrent_trackers
		WHERE torrent_hash = ?
		ORDER BY tracker_host, tracker_url
	`, string(hash)); err != nil {
		return nil, fmt.Errorf("listing trackers for %s: %w", hash, err)
	}
	return rows, nil
}
