package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

func TestLoad_AuthDefaults_LoopbackNone(t *testing.T) {
	path := writeConfig(t, `
storage:
  sqlite_path: /tmp/test.db
`)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.HTTPAuthNone, cfg.HTTP.Auth, "loopback bind should default to auth=none")
}

func TestLoad_AuthDefaults_NonLoopbackApikey(t *testing.T) {
	path := writeConfig(t, `
storage:
  sqlite_path: /tmp/test.db
http:
  bind: "0.0.0.0:9494"
`)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, config.HTTPAuthAPIKey, cfg.HTTP.Auth, "non-loopback bind should default to auth=apikey")
}

func TestLoad_AuthNone_NonLoopback_Rejected(t *testing.T) {
	path := writeConfig(t, `
storage:
  sqlite_path: /tmp/test.db
http:
  bind: "0.0.0.0:9494"
  auth: none
`)
	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "loopback")
}

func TestRedacted_ScrubsArrAPIKey(t *testing.T) {
	cfg := config.Config{
		Arrs: config.ArrsConfig{
			Sonarr: []config.ArrInstanceConfig{
				{Name: "main", APIKey: "secret-key", URL: "http://sonarr"},
			},
		},
		Qbit: config.QbitConfig{Password: "qbit-pass"},
	}
	r := cfg.Redacted()
	require.Equal(t, config.RedactedPlaceholder, r.Arrs.Sonarr[0].APIKey)
	require.Equal(t, config.RedactedPlaceholder, r.Qbit.Password)
	// Original config untouched.
	require.Equal(t, "secret-key", cfg.Arrs.Sonarr[0].APIKey)
	require.Equal(t, "qbit-pass", cfg.Qbit.Password)
}
