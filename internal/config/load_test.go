package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoad_MinimalDefaults(t *testing.T) {
	path := writeConfig(t, `
storage:
  sqlite_path: /tmp/test.db
volume:
  path: /data
`)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.ModeDryRun, cfg.Mode)
	require.Equal(t, "127.0.0.1:9494", cfg.HTTP.Bind)
	require.Equal(t, 30*time.Minute, cfg.Polling.QbitInterval)
	require.Equal(t, time.Hour, cfg.Polling.ArrInterval)
	require.Equal(t, 5*time.Minute, cfg.Polling.DiskInterval)
}

func TestLoad_EnvSubstitution(t *testing.T) {
	t.Setenv("MY_KEY", "secret-value")
	path := writeConfig(t, `
mode: dry-run
volume:
  path: /data
arrs:
  sonarr:
    enabled: true
    url: ${SONARR_URL:-http://sonarr:8989}
    api_key: ${MY_KEY}
    poll: true
`)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "secret-value", cfg.Arrs.Sonarr.APIKey)
	require.Equal(t, "http://sonarr:8989", cfg.Arrs.Sonarr.URL)
}

func TestLoad_RequiredEnvMissing(t *testing.T) {
	path := writeConfig(t, `
arrs:
  sonarr:
    enabled: true
    url: http://sonarr:8989
    api_key: ${DEFINITELY_NOT_SET_12345}
    poll: true
`)
	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DEFINITELY_NOT_SET_12345")
}

func TestLoad_ValidatesMode(t *testing.T) {
	path := writeConfig(t, `mode: bogus`)
	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mode")
}

func TestLoad_MissingAPIKeyWhenEnabled(t *testing.T) {
	path := writeConfig(t, `
arrs:
  sonarr:
    enabled: true
    url: http://sonarr:8989
    poll: true
`)
	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

func TestLoad_FullExample(t *testing.T) {
	t.Setenv("TRIAGEARR_API_KEY", "k1")
	t.Setenv("SONARR_API_KEY", "k2")
	t.Setenv("RADARR_API_KEY", "k3")
	t.Setenv("TELEGRAM_CHAT_ID", "chat")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")

	// Load the real config.example.yml from the repo root.
	abs, err := filepath.Abs("../../config.example.yml")
	require.NoError(t, err)
	cfg, err := config.Load(abs)
	require.NoError(t, err)
	require.True(t, cfg.AnyArrEnabledForPolling())
	require.Equal(t, "/config/triagearr.db", cfg.Storage.SQLitePath)
}
