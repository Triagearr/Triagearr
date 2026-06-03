package main

import (
	"errors"
	"fmt"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/clients/arr/registry"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/torrentregistry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// registryDeleter adapts the registry's typed accessor into the
// actor.DeleterResolver function shape.
func registryDeleter(reg *registry.Registry) actor.DeleterResolver {
	return func(name string) (triagearr.FileDeleter, bool) {
		return reg.Deleter(name)
	}
}

// buildActor constructs an Actor for one-shot CLI invocations. It mirrors
// the lifecycle the daemon performs in buildEngine, but stays scoped to the
// caller (no goroutines, no SIGHUP). Returns the qbit client too so callers
// can keep it alive for the duration of Execute.
func buildActor(cfg *config.Config, s *store.Store) (*actor.Actor, *qbit.Client, error) {
	treg, err := torrentregistry.BuildFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building torrent client registry: %w", err)
	}
	active, ok := treg.Active()
	if !ok {
		return nil, nil, errors.New("a torrent client must be enabled to run --live actions")
	}
	qb, ok := active.(*qbit.Client)
	if !ok {
		return nil, nil, errors.New("only qbittorrent is supported as torrent client today")
	}
	reg, err := registry.BuildFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building registry: %w", err)
	}
	act := actor.New(actor.Options{
		Source:             s,
		Client:             qb,
		Deleter:            registryDeleter(reg),
		MaxDeletionsPerRun: cfg.Action.MaxDeletionsPerRun,
		InterActionDelay:   cfg.Action.InterActionDelay,
		AddImportExclusion: cfg.Action.AddImportExclusion,
	})
	return act, qb, nil
}
