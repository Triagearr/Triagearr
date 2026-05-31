package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// UpsertMediaFile records (or refreshes) one *arr-owned file. The file_id is
// the *arr-side primary key (episodeFile.id / movieFile.id), reused by M5
// Actor for granular DELETEs.
func (s *Store) UpsertMediaFile(ctx context.Context, f triagearr.MediaFile) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO media_files(arr_type, file_id, media_id, path, size, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(arr_type, file_id) DO UPDATE SET
			media_id=excluded.media_id,
			path=excluded.path,
			size=excluded.size,
			last_seen=excluded.last_seen
	`, string(f.ArrType), f.FileID, int64(f.MediaID), f.Path, f.Size, ts(now))
	if err != nil {
		return fmt.Errorf("upserting media_file %s/%d: %w", f.ArrType, f.FileID, err)
	}
	return nil
}

// UpsertMediaFiles batches UpsertMediaFile for one media item's file fanout in
// a single transaction with one prepared statement.
func (s *Store) UpsertMediaFiles(ctx context.Context, files []triagearr.MediaFile) error {
	if len(files) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for media_files batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PreparexContext(ctx, `
		INSERT INTO media_files(arr_type, file_id, media_id, path, size, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(arr_type, file_id) DO UPDATE SET
			media_id=excluded.media_id,
			path=excluded.path,
			size=excluded.size,
			last_seen=excluded.last_seen
	`)
	if err != nil {
		return fmt.Errorf("prepare media_files upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	now := ts(time.Now().UTC())
	for _, f := range files {
		if _, err := stmt.ExecContext(ctx,
			string(f.ArrType), f.FileID, int64(f.MediaID), f.Path, f.Size, now,
		); err != nil {
			return fmt.Errorf("upserting media_file %s/%d: %w", f.ArrType, f.FileID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media_files batch: %w", err)
	}
	return nil
}

// MediaFileRow is the persisted view used by `inspect media` and the linker.
type MediaFileRow struct {
	ArrType  string    `db:"arr_type"`
	FileID   int64     `db:"file_id"`
	MediaID  int64     `db:"media_id"`
	Path     string    `db:"path"`
	Size     int64     `db:"size"`
	LastSeen time.Time `db:"last_seen"`
}

// ListMediaFilesByMedia returns the files attached to one media item.
func (s *Store) ListMediaFilesByMedia(ctx context.Context, arrType triagearr.ArrType, mediaID triagearr.MediaID) ([]MediaFileRow, error) {
	var rows []MediaFileRow
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT arr_type, file_id, media_id, path, size, last_seen
		FROM media_files
		WHERE arr_type = ? AND media_id = ?
		ORDER BY path
	`, string(arrType), int64(mediaID)); err != nil {
		return nil, fmt.Errorf("listing media_files: %w", err)
	}
	return rows, nil
}
