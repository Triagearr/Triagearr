package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/Triagearr/Triagearr/internal/store"
)

func newAuthTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))
	return s
}

func TestRunAuthSetPassword_Generates(t *testing.T) {
	s := newAuthTestStore(t)
	ctx := context.Background()
	_, err := s.InsertAuthUser(ctx, "operator", "old-hash")
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runAuthSetPassword(ctx, s, &out, "", ""))

	// The generated password is printed once; extract it and verify the stored
	// hash matches it.
	printed := out.String()
	require.Contains(t, printed, "not shown again")
	fields := strings.Fields(printed)
	generated := fields[len(fields)-1]

	u, err := s.GetAuthUserByName(ctx, "operator")
	require.NoError(t, err)
	require.NotEqual(t, "old-hash", u.PasswordHash)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(generated)))
}

func TestRunAuthSetPassword_ExplicitPassword(t *testing.T) {
	s := newAuthTestStore(t)
	ctx := context.Background()
	_, err := s.InsertAuthUser(ctx, "operator", "old-hash")
	require.NoError(t, err)

	const pw = "a-deliberate-password"
	var out bytes.Buffer
	require.NoError(t, runAuthSetPassword(ctx, s, &out, "", pw))
	require.NotContains(t, out.String(), "not shown again", "explicit password is not echoed back")

	u, err := s.GetAuthUserByName(ctx, "operator")
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pw)))
}

func TestRunAuthSetPassword_NoAccount(t *testing.T) {
	s := newAuthTestStore(t)
	var out bytes.Buffer
	err := runAuthSetPassword(context.Background(), s, &out, "", "")
	require.ErrorContains(t, err, "not enabled")
}

func TestRunAuthSetPassword_UnknownUser(t *testing.T) {
	s := newAuthTestStore(t)
	ctx := context.Background()
	_, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	var out bytes.Buffer
	err = runAuthSetPassword(ctx, s, &out, "ghost", "a-deliberate-password")
	require.ErrorContains(t, err, `no account named "ghost"`)
}

func TestRunAuthSetPassword_TooShort(t *testing.T) {
	s := newAuthTestStore(t)
	ctx := context.Background()
	_, err := s.InsertAuthUser(ctx, "operator", "hash")
	require.NoError(t, err)

	var out bytes.Buffer
	err = runAuthSetPassword(ctx, s, &out, "", "short")
	require.ErrorContains(t, err, "at least")
}

func TestRunAuthDisable(t *testing.T) {
	ctx := context.Background()

	t.Run("not confirmed leaves the account intact", func(t *testing.T) {
		s := newAuthTestStore(t)
		_, err := s.InsertAuthUser(ctx, "operator", "hash")
		require.NoError(t, err)

		var out bytes.Buffer
		require.NoError(t, runAuthDisable(ctx, s, &out, false))
		require.Contains(t, out.String(), "aborted")

		has, err := s.HasAuthUser(ctx)
		require.NoError(t, err)
		require.True(t, has)
	})

	t.Run("confirmed clears the account", func(t *testing.T) {
		s := newAuthTestStore(t)
		_, err := s.InsertAuthUser(ctx, "operator", "hash")
		require.NoError(t, err)

		var out bytes.Buffer
		require.NoError(t, runAuthDisable(ctx, s, &out, true))
		require.Contains(t, out.String(), "1 account(s) removed")

		has, err := s.HasAuthUser(ctx)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("idempotent on an open store", func(t *testing.T) {
		s := newAuthTestStore(t)
		var out bytes.Buffer
		require.NoError(t, runAuthDisable(ctx, s, &out, true))
		require.Contains(t, out.String(), "already disabled")
	})
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"  y  \n", true},
		{"n\n", false},
		{"\n", false},
		{"nope\n", false},
		{"", false},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		got, err := confirm(strings.NewReader(tt.in), &out, "go?")
		require.NoError(t, err)
		require.Equal(t, tt.want, got, "input %q", tt.in)
	}
}
