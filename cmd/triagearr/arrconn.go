package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// arrKinds lists the *arr flavours in the order they are iterated.
var arrKinds = []triagearr.ArrType{
	triagearr.ArrTypeSonarr,
	triagearr.ArrTypeRadarr,
	triagearr.ArrTypeLidarr,
	triagearr.ArrTypeReadarr,
	triagearr.ArrTypeWhisparrV2,
	triagearr.ArrTypeWhisparrV3,
}

// resolveArrConnections makes the arr_connections table the source of truth
// for cfg.Arrs (ADR-0022). On an empty table it seeds from the YAML `arrs:`
// block once; thereafter it rebuilds cfg.Arrs from the table and re-validates.
func resolveArrConnections(ctx context.Context, s *store.Store, cfg *config.Config) error {
	n, err := s.CountArrConnections(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		seed := flattenArrs(cfg)
		if len(seed) > 0 {
			if err := s.SeedArrConnections(ctx, seed); err != nil {
				return err
			}
			slog.Info("seeded arr_connections from YAML config", "count", len(seed))
		}
	}
	conns, err := s.ListArrConnections(ctx)
	if err != nil {
		return err
	}
	cfg.Arrs = arrsConfigFromConnections(conns)
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating db-resolved arr connections: %w", err)
	}
	return nil
}

// flattenArrs maps each ArrsConfig field to a store.ArrConnection. Zero-value
// entries (no URL) are skipped — they represent unconfigured types.
func flattenArrs(cfg *config.Config) []store.ArrConnection {
	pairs := []struct {
		kind triagearr.ArrType
		inst config.ArrInstanceConfig
	}{
		{triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr},
		{triagearr.ArrTypeRadarr, cfg.Arrs.Radarr},
		{triagearr.ArrTypeLidarr, cfg.Arrs.Lidarr},
		{triagearr.ArrTypeReadarr, cfg.Arrs.Readarr},
		{triagearr.ArrTypeWhisparrV2, cfg.Arrs.WhisparrV2},
		{triagearr.ArrTypeWhisparrV3, cfg.Arrs.WhisparrV3},
	}
	var out []store.ArrConnection
	for _, p := range pairs {
		if p.inst.URL == "" && !p.inst.Enabled {
			continue // zero-value / unconfigured
		}
		out = append(out, instanceToConnection(string(p.kind), p.inst))
	}
	return out
}

// arrsConfigFromConnections regroups a flat connection list back into the
// typed ArrsConfig the registry and pollers consume.
func arrsConfigFromConnections(conns []store.ArrConnection) config.ArrsConfig {
	var ac config.ArrsConfig
	for _, c := range conns {
		inst := connectionToInstance(c)
		switch triagearr.ArrType(c.Kind) {
		case triagearr.ArrTypeSonarr:
			ac.Sonarr = inst
		case triagearr.ArrTypeRadarr:
			ac.Radarr = inst
		case triagearr.ArrTypeLidarr:
			ac.Lidarr = inst
		case triagearr.ArrTypeReadarr:
			ac.Readarr = inst
		case triagearr.ArrTypeWhisparrV2:
			ac.WhisparrV2 = inst
		case triagearr.ArrTypeWhisparrV3:
			ac.WhisparrV3 = inst
		default:
			slog.Warn("ignoring arr_connection with unknown kind", "kind", c.Kind)
		}
	}
	return ac
}

func instanceToConnection(kind string, in config.ArrInstanceConfig) store.ArrConnection {
	return store.ArrConnection{
		Kind:           kind,
		URL:            in.URL,
		APIKey:         in.APIKey,
		Enabled:        in.Enabled,
		Poll:           in.Poll,
		Act:            in.Act,
		TagsExclude:    in.TagsExclude,
		CategoriesOnly: in.CategoriesOnly,
		TimeoutMS:      in.Timeout.Milliseconds(),
	}
}

func connectionToInstance(c store.ArrConnection) config.ArrInstanceConfig {
	return config.ArrInstanceConfig{
		Enabled:        c.Enabled,
		URL:            c.URL,
		APIKey:         c.APIKey,
		Poll:           c.Poll,
		Act:            c.Act,
		TagsExclude:    c.TagsExclude,
		CategoriesOnly: c.CategoriesOnly,
		Timeout:        time.Duration(c.TimeoutMS) * time.Millisecond,
	}
}
