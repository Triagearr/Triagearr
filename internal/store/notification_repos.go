package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// GetNotificationState returns the last-sent timestamp for a logical alert
// (event_key) and whether a row exists. A missing row (ok == false) means the
// alert has never been sent, so the caller should emit immediately. See
// ADR-0032.
func (s *Store) GetNotificationState(ctx context.Context, eventKey string) (time.Time, bool, error) {
	var last time.Time
	err := s.reader.GetContext(ctx, &last,
		`SELECT last_sent_at FROM notification_state WHERE event_key = ?`, eventKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("getting notification_state %q: %w", eventKey, err)
	}
	return last, true, nil
}

// MarkNotificationSent records that the alert keyed by eventKey was just
// dispatched, upserting last_sent_at so the next reminder is rate-limited.
func (s *Store) MarkNotificationSent(ctx context.Context, eventKey string, at time.Time) error {
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO notification_state(event_key, last_sent_at)
		VALUES (?, ?)
		ON CONFLICT(event_key) DO UPDATE SET last_sent_at = excluded.last_sent_at
	`, eventKey, ts(at))
	if err != nil {
		return fmt.Errorf("marking notification_state %q sent: %w", eventKey, err)
	}
	return nil
}

// ClearNotificationState drops the throttle row for eventKey, so the next
// occurrence of the condition alerts immediately rather than waiting out the
// reminder window. Idempotent: a no-op when no row exists.
func (s *Store) ClearNotificationState(ctx context.Context, eventKey string) error {
	if _, err := s.writer.ExecContext(ctx,
		`DELETE FROM notification_state WHERE event_key = ?`, eventKey); err != nil {
		return fmt.Errorf("clearing notification_state %q: %w", eventKey, err)
	}
	return nil
}
