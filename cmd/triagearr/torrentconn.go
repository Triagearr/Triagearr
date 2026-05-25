package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
)

// resolveTorrentClientConnections makes the torrent_client_connections table
// the source of truth for cfg.TorrentClients (ADR-0025). On an empty table it
// seeds from the YAML `torrent_clients:` block once; thereafter it rebuilds
// cfg.TorrentClients from the table and re-validates.
func resolveTorrentClientConnections(ctx context.Context, s *store.Store, cfg *config.Config) error {
	return resolveConnections(
		ctx, cfg, "torrent_client",
		s.CountTorrentClientConnections,
		s.SeedTorrentClientConnections,
		s.ListTorrentClientConnections,
		flattenTorrentClients,
		func(conns []store.TorrentClientConnection) {
			cfg.TorrentClients = torrentClientsConfigFromConnections(conns)
		},
	)
}

// flattenTorrentClients maps each TorrentClientsConfig field to a store row.
// Zero-value entries (no URL and not enabled) are skipped — they represent
// unconfigured kinds.
func flattenTorrentClients(cfg *config.Config) []store.TorrentClientConnection {
	var out []store.TorrentClientConnection
	cfg.TorrentClients.EachPtr(func(label string, inst *config.TorrentClientInstanceConfig) {
		if inst.URL == "" && !inst.Enabled {
			return
		}
		out = append(out, torrentInstanceToConnection(label, *inst))
	})
	return out
}

// torrentClientsConfigFromConnections regroups a flat connection list back
// into the typed TorrentClientsConfig the registry consumes.
func torrentClientsConfigFromConnections(conns []store.TorrentClientConnection) config.TorrentClientsConfig {
	var tc config.TorrentClientsConfig
	for _, c := range conns {
		if !tc.SetByKind(c.Kind, torrentConnectionToInstance(c)) {
			slog.Warn("ignoring torrent_client_connection with unknown kind", "kind", c.Kind)
		}
	}
	return tc
}

func torrentInstanceToConnection(kind string, in config.TorrentClientInstanceConfig) store.TorrentClientConnection {
	return store.TorrentClientConnection{
		Kind:            kind,
		URL:             in.URL,
		Username:        in.Username,
		Password:        in.Password,
		Enabled:         in.Enabled,
		CategoryExclude: in.CategoryExclude,
		TagsExclude:     in.TagsExclude,
		DeleteWithFiles: in.DeleteWithFiles,
		TimeoutMS:       in.Timeout.Milliseconds(),
	}
}

func torrentConnectionToInstance(c store.TorrentClientConnection) config.TorrentClientInstanceConfig {
	return config.TorrentClientInstanceConfig{
		Enabled:         c.Enabled,
		URL:             c.URL,
		Username:        c.Username,
		Password:        c.Password,
		CategoryExclude: c.CategoryExclude,
		TagsExclude:     c.TagsExclude,
		DeleteWithFiles: c.DeleteWithFiles,
		Timeout:         time.Duration(c.TimeoutMS) * time.Millisecond,
	}
}
