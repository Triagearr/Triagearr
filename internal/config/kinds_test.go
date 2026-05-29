package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

func TestArrsConfig_SetByKind(t *testing.T) {
	var c config.ArrsConfig
	inst := config.ArrInstanceConfig{URL: "http://sonarr:8989", APIKey: "k"}

	require.True(t, c.SetByKind("sonarr", inst))
	require.Equal(t, "http://sonarr:8989", c.Sonarr.URL)

	require.True(t, c.SetByKind("whisparr_v3", config.ArrInstanceConfig{URL: "http://w3"}))
	require.Equal(t, "http://w3", c.WhisparrV3.URL)

	require.False(t, c.SetByKind("bogus", inst), "unknown kind reports false")

	// EachPtr is the source of truth for supported kinds; every label it yields
	// must round-trip through SetByKind.
	c.EachPtr(func(label string, _ *config.ArrInstanceConfig) {
		require.True(t, c.SetByKind(label, config.ArrInstanceConfig{}), "kind %q must be settable", label)
	})
}

func TestTorrentClientsConfig_SetByKindAndBackend(t *testing.T) {
	var c config.TorrentClientsConfig

	require.True(t, c.SetByKind("qbittorrent", config.TorrentClientInstanceConfig{URL: "http://qbit:8080"}))
	require.Equal(t, "http://qbit:8080", c.Qbittorrent.URL)
	require.True(t, c.SetByKind("deluge", config.TorrentClientInstanceConfig{URL: "http://deluge"}))
	require.False(t, c.SetByKind("bogus", config.TorrentClientInstanceConfig{}))

	require.True(t, c.HasBackend("qbittorrent"))
	require.False(t, c.HasBackend("transmission"))

	c.EachPtr(func(label string, _ *config.TorrentClientInstanceConfig) {
		require.True(t, c.SetByKind(label, config.TorrentClientInstanceConfig{}), "kind %q must be settable", label)
	})
}

func TestEditableKeys_SortedWhitelist(t *testing.T) {
	keys := config.EditableKeys()
	require.NotEmpty(t, keys)
	require.Contains(t, keys, "scoring")
	require.Contains(t, keys, "notifications")
	require.IsIncreasing(t, keys, "EditableKeys must be sorted")
}

func TestConfig_Redacted(t *testing.T) {
	var c config.Config
	c.Arrs.Sonarr.APIKey = "super-secret-key"
	c.TorrentClients.Qbittorrent.Password = "hunter2"
	c.Notifications.Telegram.BotToken = "bot-token"

	red := c.Redacted()
	require.Equal(t, config.RedactedPlaceholder, red.Arrs.Sonarr.APIKey)
	require.Equal(t, config.RedactedPlaceholder, red.TorrentClients.Qbittorrent.Password)
	require.Equal(t, config.RedactedPlaceholder, red.Notifications.Telegram.BotToken)

	// The original is untouched (Redacted returns a copy).
	require.Equal(t, "super-secret-key", c.Arrs.Sonarr.APIKey)

	// Empty secrets stay empty rather than being masked.
	require.Empty(t, red.Arrs.Radarr.APIKey)
}
