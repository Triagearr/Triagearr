package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// UpsertMedia records a media item from an *arr.
func (s *Store) UpsertMedia(ctx context.Context, m triagearr.MediaItem) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO media(id, arr_type, title, title_slug, path, size, tags, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, arr_type) DO UPDATE SET
			title=excluded.title,
			title_slug=excluded.title_slug,
			path=excluded.path,
			size=excluded.size,
			tags=excluded.tags,
			last_seen=excluded.last_seen
	`, int64(m.ID), string(m.ArrType), m.Title, m.TitleSlug, m.Path, m.Size, strings.Join(m.Tags, ","), ts(now))
	if err != nil {
		return fmt.Errorf("upserting media %s/%d: %w", m.ArrType, m.ID, err)
	}
	return nil
}

// CountMedia returns the number of media rows for the given *arr (for testing/inspect).
func (s *Store) CountMedia(ctx context.Context, arrType triagearr.ArrType) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM media WHERE arr_type = ?`,
		string(arrType),
	); err != nil {
		return 0, fmt.Errorf("counting media: %w", err)
	}
	return n, nil
}
