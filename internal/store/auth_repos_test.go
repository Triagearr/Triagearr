package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthUser_Lifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	has, err := s.HasAuthUser(ctx)
	require.NoError(t, err)
	require.False(t, has, "fresh store has no operator account")

	id, err := s.InsertAuthUser(ctx, "operator", "hash-v1")
	require.NoError(t, err)
	require.Positive(t, id)

	has, err = s.HasAuthUser(ctx)
	require.NoError(t, err)
	require.True(t, has)

	// Duplicate username violates the UNIQUE constraint.
	_, err = s.InsertAuthUser(ctx, "operator", "hash-other")
	require.Error(t, err)

	byName, err := s.GetAuthUserByName(ctx, "operator")
	require.NoError(t, err)
	require.Equal(t, id, byName.ID)
	require.Equal(t, "hash-v1", byName.PasswordHash)

	byID, err := s.GetAuthUserByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "operator", byID.Username)
	require.Equal(t, byName.PasswordChangedAt, byID.PasswordChangedAt)

	require.NoError(t, s.UpdateAuthPassword(ctx, id, "hash-v2"))
	after, err := s.GetAuthUserByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "hash-v2", after.PasswordHash)
	require.False(t, after.PasswordChangedAt.Before(byID.PasswordChangedAt),
		"password_changed_at must advance on rotation")

	require.NoError(t, s.DeleteAuthUser(ctx, id))
	_, err = s.GetAuthUserByID(ctx, id)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestAuthUser_GetMissing(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetAuthUserByName(ctx, "nobody")
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = s.GetAuthUserByID(ctx, 999)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestGetSoleAuthUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.GetSoleAuthUser(ctx)
	require.ErrorIs(t, err, sql.ErrNoRows, "no account → ErrNoRows (auth disabled)")

	id, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	sole, err := s.GetSoleAuthUser(ctx)
	require.NoError(t, err)
	require.Equal(t, id, sole.ID)
	require.Equal(t, "operator", sole.Username)
}

func TestClearAuthUsers(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Empty table → idempotent no-op reporting zero rows.
	n, err := s.ClearAuthUsers(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), n)

	uid, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)
	require.NoError(t, s.InsertAuthSession(ctx, "tok", uid, time.Now().UTC().Add(time.Hour)))

	n, err = s.ClearAuthUsers(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	has, err := s.HasAuthUser(ctx)
	require.NoError(t, err)
	require.False(t, has, "clearing returns the store to open mode")

	_, err = s.LookupAuthSession(ctx, "tok")
	require.ErrorIs(t, err, sql.ErrNoRows, "clearing users must cascade their sessions")
}

func TestAuthSession_LookupAndExpiry(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	uid, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, s.InsertAuthSession(ctx, "live-token", uid, now.Add(time.Hour)))
	require.NoError(t, s.InsertAuthSession(ctx, "dead-token", uid, now.Add(-time.Hour)))

	sess, err := s.LookupAuthSession(ctx, "live-token")
	require.NoError(t, err)
	require.Equal(t, uid, sess.UserID)
	require.True(t, sess.ExpiresAt.After(now))

	// Expired sessions read as a miss without being deleted.
	_, err = s.LookupAuthSession(ctx, "dead-token")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Unknown token is a miss.
	_, err = s.LookupAuthSession(ctx, "ghost")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestAuthSession_TouchExtendsWindow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	uid, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	// Session expiring imminently; Touch slides it forward by the TTL.
	require.NoError(t, s.InsertAuthSession(ctx, "tok", uid, time.Now().UTC().Add(time.Second)))
	require.NoError(t, s.TouchAuthSession(ctx, "tok", time.Hour))

	sess, err := s.LookupAuthSession(ctx, "tok")
	require.NoError(t, err)
	require.True(t, sess.ExpiresAt.After(time.Now().UTC().Add(30*time.Minute)),
		"touch must extend expires_at by the ttl")
}

func TestAuthSession_DeleteAndSweep(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	uid, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, s.InsertAuthSession(ctx, "logout-me", uid, now.Add(time.Hour)))
	require.NoError(t, s.DeleteAuthSession(ctx, "logout-me"))
	_, err = s.LookupAuthSession(ctx, "logout-me")
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Deleting an already-gone token is a no-op.
	require.NoError(t, s.DeleteAuthSession(ctx, "logout-me"))

	require.NoError(t, s.InsertAuthSession(ctx, "keep", uid, now.Add(time.Hour)))
	require.NoError(t, s.InsertAuthSession(ctx, "stale-1", uid, now.Add(-time.Hour)))
	require.NoError(t, s.InsertAuthSession(ctx, "stale-2", uid, now.Add(-2*time.Hour)))

	removed, err := s.SweepExpiredAuthSessions(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), removed)

	// The live session survives the sweep.
	_, err = s.LookupAuthSession(ctx, "keep")
	require.NoError(t, err)
}

func TestAuthUser_DeleteCascadesSessions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	uid, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)
	require.NoError(t, s.InsertAuthSession(ctx, "tok", uid, time.Now().UTC().Add(time.Hour)))

	require.NoError(t, s.DeleteAuthUser(ctx, uid))

	_, err = s.LookupAuthSession(ctx, "tok")
	require.ErrorIs(t, err, sql.ErrNoRows, "deleting the user must cascade its sessions")
}
