package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// UpsertArrImport records one *arr-side import (downloadFolderImported event).
// PK is (arr_type, file_id) — re-imports under the same fileId update the row;
// *arr's behaviour is to allocate a fresh fileId on every import, so collisions
// in practice are rare.
func (s *Store) UpsertArrImport(ctx context.Context, arrType triagearr.ArrType, rec triagearr.ImportRecord) error {
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO arr_imports(arr_type, file_id, download_id, dropped_path, imported_path, size, history_id, imported_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(arr_type, file_id) DO UPDATE SET
			download_id=excluded.download_id,
			dropped_path=excluded.dropped_path,
			imported_path=excluded.imported_path,
			size=excluded.size,
			history_id=excluded.history_id,
			imported_at=excluded.imported_at
	`, string(arrType), rec.FileID, string(rec.DownloadID),
		rec.DroppedPath, rec.ImportedPath, rec.Size, rec.HistoryID, ts(rec.ImportedAt))
	if err != nil {
		return fmt.Errorf("upserting arr_import %s/%d: %w", arrType, rec.FileID, err)
	}
	return nil
}

// MaxHistoryID returns the highest history.id we've ingested for one *arr
// instance, so the next poll can fetch only the delta.
func (s *Store) MaxHistoryID(ctx context.Context, arrType triagearr.ArrType) (int64, error) {
	var v *int64
	if err := s.reader.GetContext(ctx, &v, `
		SELECT MAX(history_id) FROM arr_imports WHERE arr_type = ?
	`, string(arrType)); err != nil {
		return 0, fmt.Errorf("max history_id for %s: %w", arrType, err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// LinksByHash returns the per-file links for a torrent: every *arr file that
// was imported from this download_id AND still exists in media_files. The
// JOIN drops imports whose fileId no longer matches a current media_files
// entry (post-upgrade, manual delete) — keeping the linker output aligned
// with what M5 actor can actually act on.
func (s *Store) LinksByHash(ctx context.Context, hash triagearr.Hash) ([]triagearr.Link, error) {
	type row struct {
		ArrType      string `db:"arr_type"`
		FileID       int64  `db:"file_id"`
		DownloadID   string `db:"download_id"`
		DroppedPath  string `db:"dropped_path"`
		ImportedPath string `db:"imported_path"`
		Size         int64  `db:"size"`
		LivePath     string `db:"live_path"`
		TitleSlug    string `db:"title_slug"`
	}
	var rows []row
	if err := s.reader.SelectContext(ctx, &rows, `
		SELECT ai.arr_type, ai.file_id, ai.download_id,
		       ai.dropped_path, ai.imported_path, ai.size, mf.path AS live_path,
		       COALESCE(m.title_slug, '') AS title_slug
		FROM arr_imports ai
		JOIN media_files mf
		  ON mf.arr_type = ai.arr_type
		 AND mf.file_id  = ai.file_id
		LEFT JOIN media m
		  ON m.arr_type = mf.arr_type
		 AND m.id       = mf.media_id
		WHERE ai.download_id = ?
		ORDER BY ai.arr_type, ai.file_id
	`, strings.ToLower(string(hash))); err != nil {
		return nil, fmt.Errorf("listing links for %s: %w", hash, err)
	}
	out := make([]triagearr.Link, len(rows))
	for i, r := range rows {
		out[i] = triagearr.Link{
			ArrType:      triagearr.ArrType(r.ArrType),
			FileID:       r.FileID,
			DownloadID:   triagearr.Hash(r.DownloadID),
			TitleSlug:    r.TitleSlug,
			DroppedPath:  r.DroppedPath,
			ImportedPath: r.ImportedPath,
			LivePath:     r.LivePath,
			Size:         r.Size,
		}
	}
	return out, nil
}

// CountArrImports returns the number of imports stored for one *arr instance,
// surfaced by `inspect imports`.
func (s *Store) CountArrImports(ctx context.Context, arrType triagearr.ArrType) (int, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM arr_imports WHERE arr_type = ?`,
		string(arrType),
	); err != nil {
		return 0, fmt.Errorf("counting arr_imports: %w", err)
	}
	return n, nil
}
