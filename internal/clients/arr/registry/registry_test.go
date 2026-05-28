package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/registry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestBuildFromConfig_EnabledOnly(t *testing.T) {
	cfg := &config.Config{}
	cfg.Arrs.Sonarr = config.ArrInstanceConfig{Enabled: true, URL: "http://s", APIKey: "k", Poll: true}
	cfg.Arrs.Radarr = config.ArrInstanceConfig{Enabled: true, URL: "http://r", APIKey: "k", Poll: true, Act: true}
	cfg.Arrs.Lidarr = config.ArrInstanceConfig{Enabled: false, URL: "http://l"} // dropped

	r, err := registry.BuildFromConfig(cfg)
	require.NoError(t, err)
	require.Len(t, r.All(), 2, "disabled instances are dropped")

	types := map[triagearr.ArrType]bool{}
	for _, inst := range r.All() {
		types[inst.Type()] = true
	}
	require.True(t, types[triagearr.ArrTypeSonarr])
	require.True(t, types[triagearr.ArrTypeRadarr])
}

func TestBuildFromConfig_ConstructionErrorFailsFast(t *testing.T) {
	cfg := &config.Config{}
	// Enabled but missing the required APIKey → sonarr.New fails, registry aborts.
	cfg.Arrs.Sonarr = config.ArrInstanceConfig{Enabled: true, URL: "http://s"}

	_, err := registry.BuildFromConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sonarr")
}

func TestBuildFromConfig_Empty(t *testing.T) {
	r, err := registry.BuildFromConfig(&config.Config{})
	require.NoError(t, err)
	require.Empty(t, r.All())
}

func TestBuildFromConfig_AllKinds(t *testing.T) {
	cfg := &config.Config{}
	cfg.Arrs.Sonarr = config.ArrInstanceConfig{Enabled: true, URL: "http://s", APIKey: "k"}
	cfg.Arrs.Radarr = config.ArrInstanceConfig{Enabled: true, URL: "http://r", APIKey: "k"}
	cfg.Arrs.Lidarr = config.ArrInstanceConfig{Enabled: true, URL: "http://l"}
	cfg.Arrs.Readarr = config.ArrInstanceConfig{Enabled: true, URL: "http://b"}
	cfg.Arrs.WhisparrV2 = config.ArrInstanceConfig{Enabled: true, URL: "http://w2"}
	cfg.Arrs.WhisparrV3 = config.ArrInstanceConfig{Enabled: true, URL: "http://w3"}

	r, err := registry.BuildFromConfig(cfg)
	require.NoError(t, err)
	require.Len(t, r.All(), 6, "one instance per enabled kind, including stubs")
}

func TestAllPolling(t *testing.T) {
	cfg := &config.Config{}
	cfg.Arrs.Sonarr = config.ArrInstanceConfig{Enabled: true, URL: "http://s", APIKey: "k", Poll: true}
	cfg.Arrs.Radarr = config.ArrInstanceConfig{Enabled: true, URL: "http://r", APIKey: "k", Poll: false}

	r, err := registry.BuildFromConfig(cfg)
	require.NoError(t, err)
	polling := r.AllPolling()
	require.Len(t, polling, 1)
	require.Equal(t, triagearr.ArrTypeSonarr, polling[0].Type())
}

func TestDeleter(t *testing.T) {
	cfg := &config.Config{}
	cfg.Arrs.Sonarr = config.ArrInstanceConfig{Enabled: true, URL: "http://s", APIKey: "k", Act: true}
	cfg.Arrs.Radarr = config.ArrInstanceConfig{Enabled: true, URL: "http://r", APIKey: "k", Act: false}
	cfg.Arrs.Lidarr = config.ArrInstanceConfig{Enabled: true, URL: "http://l"} // stub: not a FileDeleter

	r, err := registry.BuildFromConfig(cfg)
	require.NoError(t, err)

	t.Run("act-enabled implementer resolves", func(t *testing.T) {
		d, ok := r.Deleter(string(triagearr.ArrTypeSonarr))
		require.True(t, ok)
		require.NotNil(t, d)
	})
	t.Run("act:false is rejected", func(t *testing.T) {
		_, ok := r.Deleter(string(triagearr.ArrTypeRadarr))
		require.False(t, ok)
	})
	t.Run("stub kind is not a FileDeleter", func(t *testing.T) {
		_, ok := r.Deleter(string(triagearr.ArrTypeLidarr))
		require.False(t, ok)
	})
	t.Run("unknown kind", func(t *testing.T) {
		_, ok := r.Deleter("nope")
		require.False(t, ok)
	})
}

func TestKnownKind(t *testing.T) {
	require.True(t, registry.KnownKind("sonarr"))
	require.True(t, registry.KnownKind("whisparr_v3"))
	require.False(t, registry.KnownKind("plex"))
	require.False(t, registry.KnownKind(""))
}

func TestTestConnection(t *testing.T) {
	t.Run("unknown kind", func(t *testing.T) {
		err := registry.TestConnection(context.Background(), "plex", "http://h", "k", 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown arr kind")
	})
	t.Run("construction error surfaces", func(t *testing.T) {
		// sonarr requires an APIKey; an empty one fails before any HTTP call.
		err := registry.TestConnection(context.Background(), "sonarr", "http://h", "", 0)
		require.Error(t, err)
	})
	t.Run("radarr construction error surfaces", func(t *testing.T) {
		err := registry.TestConnection(context.Background(), "radarr", "http://h", "", 0)
		require.Error(t, err)
	})
	for _, kind := range []string{"lidarr", "readarr", "whisparr_v2", "whisparr_v3"} {
		t.Run(kind+" stub returns not-implemented", func(t *testing.T) {
			// Stub kinds build fine but their HealthCheck reports "not implemented" — no network.
			err := registry.TestConnection(context.Background(), kind, "http://h", "", 0)
			require.Error(t, err)
		})
	}
}
