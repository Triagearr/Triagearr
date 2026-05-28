package torrentregistry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/torrent/torrentregistry"
	"github.com/Triagearr/Triagearr/internal/config"
)

func TestBuildFromConfig_Qbittorrent(t *testing.T) {
	cfg := &config.Config{}
	cfg.TorrentClients.Qbittorrent = config.TorrentClientInstanceConfig{
		Enabled: true, URL: "http://qbit:8080", Username: "admin", Password: "pw",
	}
	r, err := torrentregistry.BuildFromConfig(cfg)
	require.NoError(t, err)

	c, ok := r.Active()
	require.True(t, ok)
	require.NotNil(t, c)
	kind, ok := r.ActiveKind()
	require.True(t, ok)
	require.Equal(t, torrentregistry.KindQbittorrent, kind)
}

func TestBuildFromConfig_NoneEnabled(t *testing.T) {
	r, err := torrentregistry.BuildFromConfig(&config.Config{})
	require.NoError(t, err)

	_, ok := r.Active()
	require.False(t, ok, "no torrent client → observation-only mode")
	_, ok = r.ActiveKind()
	require.False(t, ok)
}

func TestBuildFromConfig_ScaffoldedKindErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.TorrentClients.Transmission = config.TorrentClientInstanceConfig{Enabled: true, URL: "http://t"}

	_, err := torrentregistry.BuildFromConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scaffolded")
}

func TestBuildFromConfig_QbitConstructionError(t *testing.T) {
	cfg := &config.Config{}
	// Enabled but missing the required URL → qbit.New fails.
	cfg.TorrentClients.Qbittorrent = config.TorrentClientInstanceConfig{Enabled: true}

	_, err := torrentregistry.BuildFromConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "qbittorrent")
}

func TestKnownAndImplementedKind(t *testing.T) {
	require.True(t, torrentregistry.KnownKind("qbittorrent"))
	require.True(t, torrentregistry.KnownKind("transmission"), "scaffolded kinds are known")
	require.False(t, torrentregistry.KnownKind("aria2"))

	require.True(t, torrentregistry.ImplementedKind("qbittorrent"))
	require.False(t, torrentregistry.ImplementedKind("transmission"), "scaffolded but not implemented")
}

func TestTestConnection_ErrorBranches(t *testing.T) {
	t.Run("unknown kind", func(t *testing.T) {
		err := torrentregistry.TestConnection(context.Background(), "aria2", "http://h", "", "", 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown torrent client kind")
	})
	t.Run("scaffolded kind", func(t *testing.T) {
		err := torrentregistry.TestConnection(context.Background(), "transmission", "http://h", "", "", 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not implemented")
	})
}
