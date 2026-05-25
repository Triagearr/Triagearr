// Package main is the entry point for the triagearr CLI.
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
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/registry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/logging"
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

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const defaultConfigPath = "/config/config.yml"

func main() {
	logging.Setup()

	configFlag := &cli.StringFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "path to triagearr config file",
		Sources: cli.EnvVars("TRIAGEARR_CONFIG"),
		Value:   defaultConfigPath,
	}

	app := &cli.Command{
		Name:    "triagearr",
		Usage:   "disk-pressure-aware media reaper for Plex/*arr/qBittorrent",
		Version: version,
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "print version information",
				Action: func(_ context.Context, _ *cli.Command) error {
					fmt.Printf("triagearr %s\n", version)
					fmt.Printf("  commit: %s\n", commit)
					fmt.Printf("  built:  %s\n", date)
					return nil
				},
			},
			{
				Name:   "serve",
				Usage:  "run the observation daemon",
				Flags:  []cli.Flag{configFlag},
				Action: serveAction,
			},
			{
				Name:   "migrate",
				Usage:  "apply pending database migrations and exit",
				Flags:  []cli.Flag{configFlag},
				Action: migrateAction,
			},
			scoreCommand(configFlag),
			runCommand(configFlag),
			{
				Name:  "inspect",
				Usage: "inspect the local Triagearr state",
				Commands: []*cli.Command{
					{
						Name:  "torrents",
						Usage: "list current torrents with their latest snapshot",
						Flags: []cli.Flag{
							configFlag,
							&cli.IntFlag{Name: "limit", Value: 50, Usage: "maximum number of rows"},
							&cli.StringFlag{Name: "sort", Value: "name", Usage: "sort key: name|seeders|ratio|size|last_seen"},
						},
						Action: inspectTorrentsAction,
					},
					{
						Name:   "arrs",
						Usage:  "list configured *arr instances with their last health check",
						Flags:  []cli.Flag{configFlag},
						Action: inspectArrsAction,
					},
					{
						Name:      "trackers",
						Usage:     "show qBit-reported tracker statuses for a torrent",
						ArgsUsage: "<torrent-hash>",
						Flags:     []cli.Flag{configFlag},
						Action:    inspectTrackersAction,
					},
					{
						Name:      "media",
						Usage:     "show one *arr media item with its per-file breakdown",
						ArgsUsage: "<arr-type> <arr-name> <media-id>",
						Flags:     []cli.Flag{configFlag},
						Action:    inspectMediaAction,
					},
					{
						Name:      "mapping",
						Usage:     "show *arr-side files linked to a torrent (per ADR-0012 import history)",
						ArgsUsage: "<torrent-hash>",
						Flags:     []cli.Flag{configFlag},
						Action:    inspectMappingAction,
					},
					{
						Name:   "imports",
						Usage:  "summary of arr_imports cached per *arr instance",
						Flags:  []cli.Flag{configFlag},
						Action: inspectImportsAction,
					},
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "err", err)
		os.Exit(1)
	}
}

func loadConfigFromCmd(cmd *cli.Command) (*config.Config, error) {
	path := cmd.String("config")
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading config from %q: %w", path, err)
	}
	return cfg, nil
}

func openStoreAndMigrate(cfg *config.Config) (*store.Store, error) {
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("applying migrations: %w", err)
	}
	return s, nil
}

func migrateAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := openStoreAndMigrate(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	slog.Info("migrations applied", "db", cfg.Storage.SQLitePath)
	return nil
}

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
	s, err := openStoreAndMigrate(cfg)
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

	for {
		runCtx, cancel := context.WithCancel(signalCtx)
		done := make(chan error, 1)
		go func(c *config.Config) {
			done <- runDaemon(runCtx, s, c, path)
		}(cfg)

	inner:
		for {
			select {
			case <-signalCtx.Done():
				cancel()
				return <-done
			case <-hup:
				newCfg, err := loadWithOverrides(signalCtx, path, s)
				if err != nil {
					slog.Error("SIGHUP reload failed; keeping current config", "err", err)
					continue inner
				}
				slog.Info("SIGHUP — restarting daemon with new config")
				cancel()
				<-done
				cfg = newCfg
				break inner
			case err := <-done:
				cancel()
				return err
			}
		}
	}
}

// selfReload sends SIGHUP to the current process so the serveAction boot
// loop tears down the daemon and re-loads with fresh settings_overrides.
// Called by the settings HTTP handler after a successful PUT.
func selfReload() {
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		slog.Error("self-SIGHUP failed", "err", err)
	}
}

// runDaemon assembles the poller set + HTTP server from cfg and blocks until
// ctx is cancelled. Reused by serveAction's SIGHUP loop to spawn a fresh
// daemon when config changes. cfgPath is captured for the ReloadValidate
// closure given to the HTTP server.
func runDaemon(ctx context.Context, s *store.Store, cfg *config.Config, cfgPath string) error {
	reg, err := registry.BuildFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("building client registry: %w", err)
	}

	var ps []pollers.Poller

	// scoreSignal carries "fresh data persisted" pings from the feeding
	// pollers (qbit/tracker/arr) to the scorer Loop. Buffered at 1 so a burst
	// of poller ticks coalesces and senders never block.
	scoreSignal := make(chan struct{}, 1)

	var qb *qbit.Client
	if cfg.Qbit.Enabled {
		qbBuilt, qbErr := qbit.New(qbit.Options{
			BaseURL:  cfg.Qbit.URL,
			Username: cfg.Qbit.Username,
			Password: cfg.Qbit.Password,
			Timeout:  cfg.Qbit.Timeout,
		})
		if qbErr != nil {
			return fmt.Errorf("building qbit client: %w", qbErr)
		}
		qb = qbBuilt
		ps = append(ps, &pollers.QbitPoller{Client: qb, Store: s, Interval: cfg.Polling.QbitInterval, Notify: scoreSignal})
		ps = append(ps, &pollers.TrackerPoller{Client: qb, Store: s, Interval: cfg.Polling.TrackerInterval, Notify: scoreSignal})
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
		ps = append(ps, &pollers.FilesPoller{Store: s, Qbit: qb, Interval: cfg.Polling.QbitInterval})
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
			return err
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
		return errors.New("no pollers enabled — check your config (qbit.enabled, arrs.*.enabled, volume.disk_pressure.enabled)")
	}

	sc := scorer.New(scorer.Options{
		Cfg:   cfg.Scoring,
		Qbit:  cfg.Qbit,
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

	// HTTP server runs as a sibling goroutine to the Manager; cancellation
	// of ctx triggers shutdown on both sides. The API key lives in
	// `${data_dir}/api_key` (Sonarr-style), auto-generated if absent.
	var httpSrv *server.Server
	if cfg.HTTP.Bind != "" {
		keyPath := filepath.Join(filepath.Dir(cfg.Storage.SQLitePath), "api_key")
		apiKey, generated, err := server.LoadOrGenerateAPIKey(keyPath)
		if err != nil {
			return fmt.Errorf("loading api_key: %w", err)
		}
		if generated {
			slog.Warn("api_key generated — read it from the file to access the API",
				"path", keyPath)
		}
		httpSrv = server.New(server.Options{
			Bind:          cfg.HTTP.Bind,
			APIKey:        apiKey,
			RunsPerMinute: cfg.HTTP.RateLimits.RunsPerMinute,
			AuthPerMinute: cfg.HTTP.RateLimits.AuthPerMinute,
			Store:         s,
			Linker:        linker.New(s),
			Config:        cfg,
			Version:       server.VersionInfo{Version: version, Commit: commit, Date: date},
			UIHandler:     web.Handler(),
			Decider:       dec,
			Volume:        func() decider.Volume { return theVolume(cfg) },
			DaemonLive:    daemonLive,
			Actor:         act,
			Notifier:      notifier,
			Reload:        selfReload,
			ReloadValidate: func(ovs []config.Override) error {
				_, err := config.LoadWithOverrides(cfgPath, ovs)
				return err
			},
		})
	}

	slog.Info("daemon starting",
		"mode", string(cfg.Mode),
		"pollers", len(ps),
		"http", cfg.HTTP.Bind,
		"sqlite", cfg.Storage.SQLitePath,
		"version", version,
	)

	mgr := pollers.NewManager(ps...)
	errCh := make(chan error, 2)
	go func() { errCh <- mgr.Run(ctx) }()
	if httpSrv != nil {
		go func() { errCh <- httpSrv.Start(ctx) }()
	}

	var firstErr error
	expect := 1
	if httpSrv != nil {
		expect = 2
	}
	for i := 0; i < expect; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
// the lifecycle the daemon performs in runDaemon, but stays scoped to the
// caller (no goroutines, no SIGHUP). Returns the qbit client too so callers
// can keep it alive for the duration of Execute.
func buildActor(cfg *config.Config, s *store.Store) (*actor.Actor, *qbit.Client, error) {
	if !cfg.Qbit.Enabled {
		return nil, nil, errors.New("qbit must be enabled to run --live actions")
	}
	qb, err := qbit.New(qbit.Options{
		BaseURL:  cfg.Qbit.URL,
		Username: cfg.Qbit.Username,
		Password: cfg.Qbit.Password,
		Timeout:  cfg.Qbit.Timeout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("building qbit client: %w", err)
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
		MaxRunSizeGB:         v.DiskPressure.MaxRunSizeGB,
	}, true
}

// theVolume is the single watched volume in the shape the Decider plans against.
func theVolume(cfg *config.Config) decider.Volume {
	v := cfg.Volume
	return decider.Volume{
		Name:              v.Name,
		Path:              v.Path,
		TargetFreePercent: v.DiskPressure.TargetFreePercent,
		MaxRunSizeGB:      v.DiskPressure.MaxRunSizeGB,
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

func inspectTorrentsAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.ListTorrentsLatest(ctx, cmd.String("sort"), cmd.Int("limit"))
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HASH\tNAME\tCATEGORY\tSIZE\tRATIO\tSEEDERS\tSTATE\tLAST_SNAPSHOT")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			short(r.Hash, 12),
			r.Name,
			r.Category,
			humanBytes(r.Size),
			optFloat(r.Ratio),
			optInt(r.Seeders),
			optStr(r.State),
			optTime(r.SnapshotAt),
		)
	}
	return tw.Flush()
}

func inspectArrsAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.ListArrInstances(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TYPE\tNAME\tURL\tHEALTHY\tLAST_CHECK\tLAST_ERROR")
	for _, r := range rows {
		health := "no"
		if r.Healthy {
			health = "yes"
		}
		lastErr := ""
		if r.LastError != nil {
			lastErr = *r.LastError
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Kind, r.Kind, r.URL, health, optTime(r.LastHealthCheck), lastErr,
		)
	}
	return tw.Flush()
}

// -----------------------------------------------------------------------------
// formatting helpers
// -----------------------------------------------------------------------------

func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func optFloat(f *float64) string {
	if f == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f", *f)
}

func optInt(i *int) string {
	if i == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *i)
}

func optStr(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func optTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func inspectTrackersAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return errors.New("usage: triagearr inspect trackers <torrent-hash>")
	}
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	hash, err := s.ResolveTorrentHash(ctx, cmd.Args().Get(0))
	if err != nil {
		return err
	}
	rows, err := s.ListTrackers(ctx, hash)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Printf("no trackers stored for %s — the tracker poller may not have run yet\n", hash)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HOST\tSTATUS\tURL\tLAST_CHECK\tMESSAGE")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			r.Host, r.Status, r.URL, r.LastChecked.Format(time.RFC3339), r.Msg,
		)
	}
	return tw.Flush()
}

func inspectMediaAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 2 {
		return errors.New("usage: triagearr inspect media <arr-type> <media-id>")
	}
	arrType := triagearr.ArrType(cmd.Args().Get(0))
	mediaID, err := strconv.ParseInt(cmd.Args().Get(1), 10, 64)
	if err != nil {
		return fmt.Errorf("media-id: %w", err)
	}
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	files, err := s.ListMediaFilesByMedia(ctx, arrType, triagearr.MediaID(mediaID))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("no files stored for %s/%d — the *arr poller may not have fanned out yet\n", arrType, mediaID)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FILE_ID\tSIZE\tLAST_SEEN\tPATH")
	for _, f := range files {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
			f.FileID, humanBytes(f.Size), f.LastSeen.Format(time.RFC3339), f.Path,
		)
	}
	return tw.Flush()
}

func inspectMappingAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return errors.New("usage: triagearr inspect mapping <torrent-hash>")
	}
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	hash, err := s.ResolveTorrentHash(ctx, cmd.Args().Get(0))
	if err != nil {
		return err
	}
	links, err := linker.New(s).Links(ctx, hash)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		fmt.Printf("no *arr-side links for %s — orphan qbit-only torrent or import history not synced yet\n", hash)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ARR\tFILE_ID\tSIZE\tLIVE_PATH\tDROPPED_PATH")
	for _, l := range links {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n",
			l.ArrType, l.FileID, humanBytes(l.Size), l.LivePath, l.DroppedPath,
		)
	}
	return tw.Flush()
}

func inspectImportsAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ARR\tIMPORTS\tMAX_HISTORY_ID")
	for _, pair := range []struct {
		typ  triagearr.ArrType
		inst config.ArrInstanceConfig
	}{
		{triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr},
		{triagearr.ArrTypeRadarr, cfg.Arrs.Radarr},
	} {
		if !pair.inst.Enabled {
			continue
		}
		n, err := s.CountArrImports(ctx, pair.typ)
		if err != nil {
			return err
		}
		max, err := s.MaxHistoryID(ctx, pair.typ)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\n", pair.typ, n, max)
	}
	return tw.Flush()
}
