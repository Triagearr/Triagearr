package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/server"
)

func TestLoadOrGenerateAPIKey_GeneratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_key")

	k1, generated, err := server.LoadOrGenerateAPIKey(path)
	require.NoError(t, err)
	require.True(t, generated)
	require.Len(t, k1, 64) // 32 bytes hex

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	k2, generated2, err := server.LoadOrGenerateAPIKey(path)
	require.NoError(t, err)
	require.False(t, generated2)
	require.Equal(t, k1, k2)
}

func TestLoadOrGenerateAPIKey_TrimsAndAcceptsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_key")
	require.NoError(t, os.WriteFile(path, []byte("  preset-value-123  \n"), 0o600))

	k, generated, err := server.LoadOrGenerateAPIKey(path)
	require.NoError(t, err)
	require.False(t, generated)
	require.Equal(t, "preset-value-123", k)
}

func TestLoadOrGenerateAPIKey_EmptyFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_key")
	require.NoError(t, os.WriteFile(path, []byte("\n  \n"), 0o600))

	_, _, err := server.LoadOrGenerateAPIKey(path)
	require.Error(t, err)
}
