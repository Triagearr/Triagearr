package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
)

// loadWithOverrides reads YAML from path, layers persisted settings_overrides
// on top, and returns the effective config. Called at boot and on SIGHUP.
func loadWithOverrides(ctx context.Context, path string, s *store.Store) (*config.Config, error) {
	rows, err := s.ListSettingsOverrides(ctx)
	if err != nil {
		return nil, err
	}
	ovs := make([]config.Override, len(rows))
	for i, r := range rows {
		ovs[i] = config.Override{Key: r.Key, ValueJSON: r.ValueJSON}
	}
	cfg, err := config.LoadWithOverrides(path, ovs)
	if err != nil {
		return nil, err
	}
	// arr_connections (the DB table) is the source of truth for *arr
	// instances; the YAML `arrs:` block only seeds it on first boot (ADR-0022).
	if err := resolveArrConnections(ctx, s, cfg); err != nil {
		return nil, err
	}
	// Same pattern for torrent clients (ADR-0025).
	if err := resolveTorrentClientConnections(ctx, s, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// resolveConnections is the shared boot path that promotes a DB-owned
// connections table to be the source of truth for a slice of cfg (ADR-0022 /
// ADR-0025). It seeds from YAML once on first run, then rebuilds the typed
// config slot from the table and re-validates.
//
// label is used in log messages — e.g. "arr", "torrent client".
func resolveConnections[Conn any](
	ctx context.Context,
	cfg *config.Config,
	label string,
	count func(context.Context) (int, error),
	seed func(context.Context, []Conn) error,
	list func(context.Context) ([]Conn, error),
	flatten func(*config.Config) []Conn,
	rebuild func([]Conn),
) error {
	n, err := count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		s := flatten(cfg)
		if len(s) > 0 {
			if err := seed(ctx, s); err != nil {
				return err
			}
			slog.Info("seeded "+label+"_connections from YAML config", "count", len(s))
		}
	}
	conns, err := list(ctx)
	if err != nil {
		return err
	}
	rebuild(conns)
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating db-resolved %s connections: %w", label, err)
	}
	return nil
}
