package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AuthUser is the single operator account when built-in auth is enabled.
type AuthUser struct {
	ID                int64     `db:"id"`
	Username          string    `db:"username"`
	PasswordHash      string    `db:"password_hash"`
	CreatedAt         time.Time `db:"created_at"`
	PasswordChangedAt time.Time `db:"password_changed_at"`
}

// AuthSession is one logged-in browser. token_hash = sha256(cookie value).
type AuthSession struct {
	TokenHash  string    `db:"token_hash"`
	UserID     int64     `db:"user_id"`
	CreatedAt  time.Time `db:"created_at"`
	ExpiresAt  time.Time `db:"expires_at"`
	LastSeenAt time.Time `db:"last_seen_at"`
}

// HasAuthUser reports whether built-in auth is enabled (≥1 row in auth_users).
func (s *Store) HasAuthUser(ctx context.Context) (bool, error) {
	var n int
	if err := s.reader.GetContext(ctx, &n, `SELECT COUNT(*) FROM auth_users`); err != nil {
		return false, fmt.Errorf("counting auth_users: %w", err)
	}
	return n > 0, nil
}

// GetAuthUserByName loads the user by username. Returns sql.ErrNoRows if missing.
func (s *Store) GetAuthUserByName(ctx context.Context, username string) (AuthUser, error) {
	var u AuthUser
	err := s.reader.GetContext(ctx, &u, `
		SELECT id, username, password_hash, created_at, password_changed_at
		FROM auth_users WHERE username = ?
	`, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthUser{}, err
		}
		return AuthUser{}, fmt.Errorf("loading auth_user %q: %w", username, err)
	}
	return u, nil
}

// GetAuthUserByID loads the user by ID. Returns sql.ErrNoRows if missing.
func (s *Store) GetAuthUserByID(ctx context.Context, id int64) (AuthUser, error) {
	var u AuthUser
	err := s.reader.GetContext(ctx, &u, `
		SELECT id, username, password_hash, created_at, password_changed_at
		FROM auth_users WHERE id = ?
	`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthUser{}, err
		}
		return AuthUser{}, fmt.Errorf("loading auth_user #%d: %w", id, err)
	}
	return u, nil
}

// InsertAuthUser creates the user. Fails with a unique-constraint error if
// the username already exists.
func (s *Store) InsertAuthUser(ctx context.Context, username, passwordHash string) (int64, error) {
	now := time.Now().UTC()
	res, err := s.writer.ExecContext(ctx, `
		INSERT INTO auth_users (username, password_hash, created_at, password_changed_at)
		VALUES (?, ?, ?, ?)
	`, username, passwordHash, ts(now), ts(now))
	if err != nil {
		return 0, fmt.Errorf("inserting auth_user: %w", err)
	}
	return res.LastInsertId()
}

// UpdateAuthPassword rotates the password hash and bumps password_changed_at.
func (s *Store) UpdateAuthPassword(ctx context.Context, id int64, passwordHash string) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		UPDATE auth_users SET password_hash = ?, password_changed_at = ? WHERE id = ?
	`, passwordHash, ts(now), id)
	if err != nil {
		return fmt.Errorf("updating auth_user password: %w", err)
	}
	return nil
}

// DeleteAuthUser removes the user (cascades sessions).
func (s *Store) DeleteAuthUser(ctx context.Context, id int64) error {
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM auth_users WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting auth_user: %w", err)
	}
	return nil
}

// InsertAuthSession stores a fresh session for a user.
func (s *Store) InsertAuthSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO auth_sessions (token_hash, user_id, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, tokenHash, userID, ts(now), ts(expiresAt.UTC()), ts(now))
	if err != nil {
		return fmt.Errorf("inserting auth_session: %w", err)
	}
	return nil
}

// LookupAuthSession returns the session if present and not expired; refreshes
// last_seen_at as a side-effect (sliding window). Returns sql.ErrNoRows on miss.
func (s *Store) LookupAuthSession(ctx context.Context, tokenHash string) (AuthSession, error) {
	var sess AuthSession
	err := s.reader.GetContext(ctx, &sess, `
		SELECT token_hash, user_id, created_at, expires_at, last_seen_at
		FROM auth_sessions WHERE token_hash = ?
	`, tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthSession{}, err
		}
		return AuthSession{}, fmt.Errorf("looking up auth_session: %w", err)
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return AuthSession{}, sql.ErrNoRows
	}
	return sess, nil
}

// TouchAuthSession updates last_seen_at and extends expires_at by ttl.
func (s *Store) TouchAuthSession(ctx context.Context, tokenHash string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.writer.ExecContext(ctx, `
		UPDATE auth_sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?
	`, ts(now), ts(now.Add(ttl)), tokenHash)
	if err != nil {
		return fmt.Errorf("touching auth_session: %w", err)
	}
	return nil
}

// DeleteAuthSession removes a single session (logout).
func (s *Store) DeleteAuthSession(ctx context.Context, tokenHash string) error {
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("deleting auth_session: %w", err)
	}
	return nil
}

// SweepExpiredAuthSessions removes all sessions past their expires_at.
// Returns the number of rows removed.
func (s *Store) SweepExpiredAuthSessions(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at < ?`, ts(time.Now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("sweeping expired auth_sessions: %w", err)
	}
	return res.RowsAffected()
}
