package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SettingsOverride is one persisted override row. Value is opaque JSON.
type SettingsOverride struct {
	Key       string    `db:"key"`
	ValueJSON string    `db:"value_json"`
	UpdatedAt time.Time `db:"updated_at"`
}

// ListSettingsOverrides returns every override row, suitable for merging on
// top of the YAML baseline at boot. Ordering is by key for deterministic
// merge behaviour when two overrides target overlapping paths.
func (s *Store) ListSettingsOverrides(ctx context.Context) ([]SettingsOverride, error) {
	rows, err := s.reader.QueryxContext(ctx, `
		SELECT key, value_json, updated_at
		FROM settings_overrides
		ORDER BY key
	`)
	if err != nil {
		return nil, fmt.Errorf("listing settings_overrides: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SettingsOverride
	for rows.Next() {
		var r SettingsOverride
		if err := rows.StructScan(&r); err != nil {
			return nil, fmt.Errorf("scanning settings_overrides: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSettingsOverride returns one row by key. sql.ErrNoRows when absent.
func (s *Store) GetSettingsOverride(ctx context.Context, key string) (SettingsOverride, error) {
	var r SettingsOverride
	err := s.reader.GetContext(ctx, &r, `
		SELECT key, value_json, updated_at
		FROM settings_overrides
		WHERE key = ?
	`, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SettingsOverride{}, err
		}
		return SettingsOverride{}, fmt.Errorf("getting settings_override %q: %w", key, err)
	}
	return r, nil
}

// UpsertSettingsOverride writes (or replaces) one override. valueJSON must
// be valid JSON — validation is the caller's responsibility (the HTTP layer
// runs the whitelist + config.Validate before persisting).
func (s *Store) UpsertSettingsOverride(ctx context.Context, key, valueJSON string) error {
	now := ts(time.Now())
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO settings_overrides(key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, key, valueJSON, now)
	if err != nil {
		return fmt.Errorf("upserting settings_override %q: %w", key, err)
	}
	return nil
}

// DeleteSettingsOverride removes one override, reverting to the YAML default.
// Returns nil even when the key didn't exist (idempotent).
func (s *Store) DeleteSettingsOverride(ctx context.Context, key string) error {
	_, err := s.writer.ExecContext(ctx, `DELETE FROM settings_overrides WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("deleting settings_override %q: %w", key, err)
	}
	return nil
}
