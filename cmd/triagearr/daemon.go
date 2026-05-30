package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/clients/arr/registry"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/torrent/torrentregistry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/notify/telegram"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/preflight"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
	"github.com/Triagearr/Triagearr/internal/triggers"
	"github.com/Triagearr/Triagearr/web"
)

// reloadRequest is sent to serveAction's reload controller by the HTTP
// server's Reload hook. done carries the build/swap result back so the
// settings handler can report success or failure synchronously.
type reloadRequest struct{ done chan error }

func serveAction(ctx context.Context, cmd *cli.Command) error {
	path := cmd.String("config")
	// Boot in two passes so SQLite overrides can layer on top of the YAML
	// baseline: load YAML once to find sqlite_path, migrate the store, then
	// reload with the persisted overrides applied. Anything in
	// settings_overrides becomes part of the effective config from tick zero.
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config from %q: %w", path, err)
	}
	s, err := openStoreAndMigrate(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	cfg, err = loadWithOverrides(ctx, path, s)
	if err != nil {
		return fmt.Errorf("applying settings_overrides: %w", err)
	}

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	reloadCh := make(chan reloadRequest, 1)

	// Build the initial engine + pollers. A failure here (e.g. ADR-0023
	// preflight) is fatal at boot, unlike a reload failure which is recoverable.
	eng, ps, err := buildEngine(signalCtx, s, cfg)
	if err != nil {
		return fmt.Errorf("building engine: %w", err)
	}

	// The HTTP server is long-lived (Option B): built once, it survives every
	// reload. Only the engine + pollers are rebuilt and swapped in. Infra knobs
	// (http.bind, rate limits, api_key, sqlite_path) are YAML-only and need a
	// real process restart — they're never reloadable this way.
	var httpSrv *server.Server
	httpErrCh := make(chan error, 1)
	if cfg.HTTP.Bind != "" {
		httpSrv, err = newHTTPServer(cfg, s, eng, path, reloadCh)
		if err != nil {
			return err
		}
		go func() { httpErrCh <- httpSrv.Start(signalCtx) }()
	}

	// Engine pollers run under their own context so a reload can stop just them
	// without touching the listener. engineCancel is reassigned on every reload.
	engineCtx, engineCancel := context.WithCancel(signalCtx)
	defer func() { engineCancel() }()
	mgrDone := make(chan error, 1)
	go func() { mgrDone <- pollers.NewManager(ps...).Run(engineCtx) }()

	slog.Info("daemon starting",
		"mode", string(cfg.Mode),
		"pollers", len(ps),
		"http", cfg.HTTP.Bind,
		"sqlite", cfg.Storage.SQLitePath,
		"version", version,
	)

	// reload rebuilds the engine + pollers from the freshly persisted overrides
	// and swaps them in. It drains the old pollers before starting the new set
	// so two DiskWatchers can't both act during the swap. On any failure it
	// keeps the current engine and returns the error (no partial swap).
	reload := func() error {
		newCfg, err := loadWithOverrides(signalCtx, path, s)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		newEng, newPs, err := buildEngine(signalCtx, s, newCfg)
		if err != nil {
			return fmt.Errorf("building engine: %w", err)
		}
		engineCancel()
		<-mgrDone
		engineCtx, engineCancel = context.WithCancel(signalCtx)
		mgrDone = make(chan error, 1)
		go func() { mgrDone <- pollers.NewManager(newPs...).Run(engineCtx) }()
		if httpSrv != nil {
			httpSrv.SwapEngine(newEng)
		}
		slog.Info("config reloaded", "mode", string(newCfg.Mode), "pollers", len(newPs))
		return nil
	}

	for {
		select {
		case <-signalCtx.Done():
			engineCancel()
			<-mgrDone
			if httpSrv != nil {
				return <-httpErrCh
			}
			return nil
		case err := <-httpErrCh:
			// The listener died (bind failure or fatal serve error). Tear the
			// pollers down and surface it.
			engineCancel()
			<-mgrDone
			return err
		case req := <-reloadCh:
			req.done <- reload()
		case <-hup:
			// Manual YAML reload (kill -HUP). Same path, fire and forget.
			if err := reload(); err != nil {
				slog.Error("SIGHUP reload failed; keeping current config", "err", err)
			}
		}
	}
}

// buildEngine assembles the config-derived subsystems (registry, pollers,
// scorer, decider, actor, notifier, disk-pressure trigger) from cfg and runs
// the ADR-0023 preflight. The returned Engine backs the HTTP handlers; the
// poller set is run by the caller under an engine-scoped context. An error
// (including preflight failure) means nothing was started — the caller keeps
// the previous engine on reload, or fails to boot.
func buildEngine(ctx context.Context, s *store.Store, cfg *config.Config) (*server.Engine, []pollers.Poller, error) {
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
			Rule:       rule,
			Decider:    dec,
			Store:      s,
			Interval:   cfg.Polling.DiskInterval,
			DaemonLive: daemonLive,
			Actor:      act,
			Notifier:   notifier,
			Sampler:    pollers.Statfs,
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

// newHTTPServer wires the long-lived HTTP server. The API key lives in
// `${data_dir}/api_key` (Sonarr-style), auto-generated if absent. The Reload
// hook funnels settings/connection saves into serveAction's reload controller
// and blocks until the swap is live, so the handler reports the real outcome.
func newHTTPServer(cfg *config.Config, s *store.Store, eng *server.Engine, cfgPath string, reloadCh chan<- reloadRequest) (*server.Server, error) {
	keyPath := filepath.Join(filepath.Dir(cfg.Storage.SQLitePath), "api_key")
	apiKey, generated, err := server.LoadOrGenerateAPIKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading api_key: %w", err)
	}
	if generated {
		slog.Warn("api_key generated — read it from the file to access the API",
			"path", keyPath)
	}
	reload := func(reqCtx context.Context) error {
		req := reloadRequest{done: make(chan error, 1)}
		select {
		case reloadCh <- req:
		case <-reqCtx.Done():
			return reqCtx.Err()
		}
		select {
		case err := <-req.done:
			return err
		case <-reqCtx.Done():
			return reqCtx.Err()
		}
	}
	return server.New(server.Options{
		Bind:          cfg.HTTP.Bind,
		APIKey:        apiKey,
		RunsPerMinute: cfg.HTTP.RateLimits.RunsPerMinute,
		AuthPerMinute: cfg.HTTP.RateLimits.AuthPerMinute,
		Store:         s,
		Linker:        linker.New(s),
		ConfigPath:    cfgPath,
		Version:       server.VersionInfo{Version: version, Commit: commit, Date: date},
		UIHandler:     web.Handler(),
		Reload:        reload,
		ReloadValidate: func(ovs []config.Override) error {
			_, err := config.LoadWithOverrides(cfgPath, ovs)
			return err
		},
	}, eng), nil
}

// registryDeleter adapts the registry's typed accessor into the
// actor.DeleterResolver function shape.
func registryDeleter(reg *registry.Registry) actor.DeleterResolver {
	return func(name string) (triagearr.FileDeleter, bool) {
		return reg.Deleter(name)
	}
}

// buildNotifier assembles the notification dispatcher from config. A provider
// that fails to construct is logged and skipped — a bad bot token must not
// prevent the daemon from starting. An all-disabled config yields an empty
// dispatcher whose Dispatch is a no-op.
func buildNotifier(cfg config.NotificationsConfig) *notify.Dispatcher {
	var notifiers []notify.Notifier
	if cfg.Telegram.Enabled {
		tg, err := telegram.New(telegram.Options{
			BotToken: cfg.Telegram.BotToken,
			ChatID:   cfg.Telegram.ChatID,
		})
		if err != nil {
			slog.Error("notifications: telegram provider disabled", "err", err)
		} else {
			notifiers = append(notifiers, tg)
			slog.Info("notifications: telegram provider enabled")
		}
	}
	return notify.NewDispatcher(notifiers...)
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

// pressureRule builds the disk-pressure rule for the watched volume. The
// second return is false when disk pressure is disabled or has no threshold.
func pressureRule(cfg *config.Config) (triggers.VolumeRule, bool) {
	v := cfg.Volume
	if !v.DiskPressure.Enabled || v.DiskPressure.ThresholdFreePercent <= 0 {
		return triggers.VolumeRule{}, false
	}
	return triggers.VolumeRule{
		Name:                 v.Name,
		Path:                 v.Path,
		ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
		TargetFreePercent:    v.DiskPressure.TargetFreePercent,
	}, true
}

// theVolume is the single watched volume in the shape the Decider plans against.
func theVolume(cfg *config.Config) decider.Volume {
	v := cfg.Volume
	return decider.Volume{
		Name:              v.Name,
		Path:              v.Path,
		TargetFreePercent: v.DiskPressure.TargetFreePercent,
	}
}

func arrURLMap(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for _, pair := range []struct {
		typ  triagearr.ArrType
		inst config.ArrInstanceConfig
	}{
		{triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr},
		{triagearr.ArrTypeRadarr, cfg.Arrs.Radarr},
		{triagearr.ArrTypeLidarr, cfg.Arrs.Lidarr},
		{triagearr.ArrTypeReadarr, cfg.Arrs.Readarr},
		{triagearr.ArrTypeWhisparrV2, cfg.Arrs.WhisparrV2},
		{triagearr.ArrTypeWhisparrV3, cfg.Arrs.WhisparrV3},
	} {
		out[pollers.URLKey(pair.typ)] = pair.inst.URL
	}
	return out
}

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

// enabledVolume builds the disk poller's view of the watched volume. The
// second return is false when disk pressure is disabled.
func enabledVolume(cfg *config.Config) (pollers.Volume, bool) {
	v := cfg.Volume
	if !v.DiskPressure.Enabled {
		return pollers.Volume{}, false
	}
	vol := pollers.Volume{Path: v.Path}
	if v.Source != "" {
		vol.Sample = httpDiskSampler(v.Source)
	}
	return vol, true
}

// httpDiskSampler returns a Sampler that fetches DiskUsage from a URL serving
// the fakedisk JSON shape. Used by dev configs (config.dev.yml) to drive the
// pressure trigger off a fake disk without touching a real filesystem.
func httpDiskSampler(url string) pollers.Sampler {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(ctx context.Context) (triagearr.DiskUsage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: %w", url, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: %w", url, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: HTTP %d", url, resp.StatusCode)
		}
		var body struct {
			TotalBytes  uint64  `json:"total_bytes"`
			UsedBytes   uint64  `json:"used_bytes"`
			FreeBytes   uint64  `json:"free_bytes"`
			FreePercent float64 `json:"free_percent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return triagearr.DiskUsage{}, fmt.Errorf("disk source %q: decode: %w", url, err)
		}
		return triagearr.DiskUsage{
			TotalBytes:  body.TotalBytes,
			UsedBytes:   body.UsedBytes,
			FreeBytes:   body.FreeBytes,
			FreePercent: body.FreePercent,
		}, nil
	}
}
