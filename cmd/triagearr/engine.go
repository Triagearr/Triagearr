package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/clients/arr/registry"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/torrentregistry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/preflight"
	"github.com/Triagearr/Triagearr/internal/runlock"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triggers"
)

// buildEngine assembles the config-derived subsystems (registry, pollers,
// scorer, decider, actor, notifier, disk-pressure trigger) from cfg and runs
// the ADR-0023 preflight. The returned Engine backs the HTTP handlers; the
// poller set is run by the caller under an engine-scoped context. An error
// (including preflight failure) means nothing was started — the caller keeps
// the previous engine on reload, or fails to boot.
func buildEngine(ctx context.Context, s *store.Store, cfg *config.Config, runLock *runlock.Lock) (*server.Engine, []pollers.Poller, error) {
	reg, err := registry.BuildFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building client registry: %w", err)
	}

	var ps []pollers.Poller

	// scoreSignal carries "fresh data persisted" pings from the feeding
	// pollers (qbit/tracker/arr) to the scorer Loop. Buffered at 1 so a burst
	// of poller ticks coalesces and senders never block.
	scoreSignal := make(chan struct{}, 1)
	// trackerCatchup signals the TrackerPoller to fetch trackers for
	// freshly-seen hashes immediately (event-driven catchup) instead of
	// waiting for the next 6h periodic sweep.
	trackerCatchup := make(chan struct{}, 1)

	treg, err := torrentregistry.BuildFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building torrent client registry: %w", err)
	}
	var qb *qbit.Client
	if active, ok := treg.Active(); ok {
		// In V1 the only implemented torrent client is qBittorrent; the
		// pollers and FilesPoller still require the concrete *qbit.Client
		// for now (their fields are typed against it).
		if qc, isQbit := active.(*qbit.Client); isQbit {
			qb = qc
			ps = append(ps, &pollers.TorrentClientPoller{Client: qb, Kind: string(torrentregistry.KindQbittorrent), URL: cfg.TorrentClients.Qbittorrent.URL, Store: s, Interval: cfg.Polling.TorrentClientInterval, Notify: scoreSignal, TrackerCatchup: trackerCatchup})
			ps = append(ps, &pollers.TrackerPoller{Client: qb, Store: s, Interval: cfg.Polling.TrackerInterval, Signal: trackerCatchup, Notify: scoreSignal})
		}
	}

	pollingArrs := reg.AllPolling()
	if len(pollingArrs) > 0 {
		urls := arrURLMap(cfg)
		ps = append(ps, &pollers.ArrPoller{
			Instances:             pollingArrs,
			URLs:                  urls,
			Store:                 s,
			Interval:              cfg.Polling.ArrInterval,
			FileFanoutMinInterval: cfg.Polling.ArrFileMinInterval,
			Notify:                scoreSignal,
		})
	}

	if vol, ok := enabledVolume(cfg); ok {
		ps = append(ps, &pollers.DiskPoller{Volume: vol, Store: s, Interval: cfg.Polling.DiskInterval})
	}

	if qb != nil {
		ps = append(ps, &pollers.FilesPoller{Store: s, Qbit: qb, Interval: cfg.Polling.TorrentClientInterval})
	}

	// ADR-0023 boot validation: refuse to start if the TRaSH single-shared-mount
	// convention is violated (qBit save_paths don't resolve in our namespace).
	// Skipped only when there's nothing to validate (qBit + volume both off).
	if cfg.Volume.Path != "" || qb != nil {
		var pqb preflight.TorrentClient
		if qb != nil {
			pqb = qb
		}
		if err := preflight.Validate(ctx, pqb, cfg.Volume.Path, nil); err != nil {
			return nil, nil, err
		}
	}

	ps = append(ps, &pollers.Maintenance{
		Store: s,
		Config: pollers.MaintenanceConfig{
			Schedule:              cfg.Polling.DownsampleCron,
			DownsampleAge:         48 * time.Hour,
			RawRetention:          cfg.Storage.Retention.SnapshotsRaw,
			DailyRetention:        cfg.Storage.Retention.SnapshotsDaily,
			TorrentRetention:      cfg.Storage.Retention.Torrents,
			VacuumEnabled:         cfg.Storage.Vacuum.Enabled,
			VacuumMinReclaimBytes: cfg.Storage.Vacuum.MinReclaimMB * 1024 * 1024,
		},
	})

	if len(ps) == 0 {
		return nil, nil, errors.New("no pollers enabled — check your config (qbit.enabled, arrs.*.enabled, volume.disk_pressure.enabled)")
	}

	sc := scorer.New(scorer.Options{
		Cfg:   cfg.Scoring,
		Qbit:  cfg.TorrentClients.Qbittorrent,
		Arrs:  cfg.Arrs,
		Store: s,
	})
	ps = append(ps, &scorer.Loop{Score: sc.ScorePass, Signal: scoreSignal})

	dec := decider.New(s)
	daemonLive := cfg.Mode == config.ModeLive

	// Actor needs the qBit client; if qBit isn't enabled nothing destructive
	// can happen anyway, so leave it nil.
	var act *actor.Actor
	if qb != nil {
		act = actor.New(actor.Options{
			Source:             s,
			Client:             qb,
			Deleter:            registryDeleter(reg),
			MaxDeletionsPerRun: cfg.Action.MaxDeletionsPerRun,
			InterActionDelay:   cfg.Action.InterActionDelay,
			AddImportExclusion: cfg.Action.AddImportExclusion,
		})
	}

	// One dispatcher, shared by the disk-pressure trigger (run notifications)
	// and the HTTP server (the "send test" endpoint).
	notifier := buildNotifier(cfg.Notifications)

	if rule, ok := pressureRule(cfg); ok {
		ps = append(ps, &triggers.DiskWatcher{
			Rule:                      rule,
			Decider:                   dec,
			Store:                     s,
			Interval:                  cfg.Polling.DiskInterval,
			DaemonLive:                daemonLive,
			Actor:                     act,
			Notifier:                  notifier,
			Sampler:                   pollers.Statfs,
			RunLock:                   runLock,
			TargetUnreachableReminder: cfg.Notifications.TargetUnreachable.ReminderInterval,
		})
	}

	// The health watcher emits connection-health transitions (ADR-0033). It
	// reads health the arr/torrent pollers persist, so it is only useful when
	// notifications are configured.
	if !notifier.Empty() {
		ps = append(ps, &triggers.HealthWatcher{
			Store:    s,
			Notifier: notifier,
			Interval: cfg.Polling.HealthInterval,
		})
	}

	eng := &server.Engine{
		Config:     cfg,
		Scorer:     sc,
		Decider:    dec,
		Volume:     func() decider.Volume { return theVolume(cfg) },
		DaemonLive: daemonLive,
		Actor:      act,
		Notifier:   notifier,
	}
	return eng, ps, nil
}
