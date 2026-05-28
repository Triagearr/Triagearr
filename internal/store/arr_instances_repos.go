package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// ArrInstanceRow is the persisted view of an *arr instance health.
type ArrInstanceRow struct {
	Kind            string     `db:"kind"`
	URL             string     `db:"url"`
	Healthy         bool       `db:"healthy"`
	LastHealthCheck *time.Time `db:"last_health_check"`
	LastError       *string    `db:"last_error"`
}

// UpsertArrInstance records the last-known health for an *arr instance.
func (s *Store) UpsertArrInstance(ctx context.Context, typ triagearr.ArrType, url string, healthy bool, lastErr string) error {
	now := time.Now().UTC()
	var lastErrCol any
	if lastErr != "" {
		lastErrCol = lastErr
	}
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO arr_instances(kind, url, healthy, last_health_check, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			url=excluded.url,
			healthy=excluded.healthy,
			last_health_check=excluded.last_health_check,
			last_error=excluded.last_error
	`, string(typ), url, healthy, ts(now), lastErrCol)
	if err != nil {
		return fmt.Errorf("upserting arr_instance %s: %w", typ, err)
	}
	return nil
}

// ListArrInstances returns every recorded *arr instance.
func (s *Store) ListArrInstances(ctx context.Context) ([]ArrInstanceRow, error) {
	var rows []ArrInstanceRow
	if err := s.reader.SelectContext(ctx, &rows,
		`SELECT kind, url, healthy, last_health_check, last_error FROM arr_instances ORDER BY kind`,
	); err != nil {
		return nil, fmt.Errorf("listing arr_instances: %w", err)
	}
	return rows, nil
}
