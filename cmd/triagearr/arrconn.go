package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
)

// resolveArrConnections makes the arr_connections table the source of truth
// for cfg.Arrs (ADR-0022). On an empty table it seeds from the YAML `arrs:`
// block once; thereafter it rebuilds cfg.Arrs from the table and re-validates.
func resolveArrConnections(ctx context.Context, s *store.Store, cfg *config.Config) error {
	return resolveConnections(
		ctx, cfg, "arr",
		s.CountArrConnections,
		s.SeedArrConnections,
		s.ListArrConnections,
		flattenArrs,
		func(conns []store.ArrConnection) { cfg.Arrs = arrsConfigFromConnections(conns) },
	)
}

// flattenArrs maps each ArrsConfig field to a store.ArrConnection. Zero-value
// entries (no URL) are skipped — they represent unconfigured types.
func flattenArrs(cfg *config.Config) []store.ArrConnection {
	var out []store.ArrConnection
	cfg.Arrs.EachPtr(func(label string, inst *config.ArrInstanceConfig) {
		if inst.URL == "" && !inst.Enabled {
			return
		}
		out = append(out, arrInstanceToConnection(label, *inst))
	})
	return out
}

// arrsConfigFromConnections regroups a flat connection list back into the
// typed ArrsConfig the registry and pollers consume.
func arrsConfigFromConnections(conns []store.ArrConnection) config.ArrsConfig {
	var ac config.ArrsConfig
	for _, c := range conns {
		if !ac.SetByKind(c.Kind, arrConnectionToInstance(c)) {
			slog.Warn("ignoring arr_connection with unknown kind", "kind", c.Kind)
		}
	}
	return ac
}

func arrInstanceToConnection(kind string, in config.ArrInstanceConfig) store.ArrConnection {
	return store.ArrConnection{
		Kind:           kind,
		URL:            in.URL,
		PublicURL:      in.PublicURL,
		APIKey:         in.APIKey,
		Enabled:        in.Enabled,
		Poll:           in.Poll,
		Act:            in.Act,
		TagsExclude:    in.TagsExclude,
		CategoriesOnly: in.CategoriesOnly,
		TimeoutMS:      in.Timeout.Milliseconds(),
	}
}

func arrConnectionToInstance(c store.ArrConnection) config.ArrInstanceConfig {
	return config.ArrInstanceConfig{
		Enabled:        c.Enabled,
		URL:            c.URL,
		PublicURL:      c.PublicURL,
		APIKey:         c.APIKey,
		Poll:           c.Poll,
		Act:            c.Act,
		TagsExclude:    c.TagsExclude,
		CategoriesOnly: c.CategoriesOnly,
		Timeout:        time.Duration(c.TimeoutMS) * time.Millisecond,
	}
}
