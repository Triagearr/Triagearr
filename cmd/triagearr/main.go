// Package main is the entry point for the triagearr CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/registry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/logging"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
	"github.com/Triagearr/Triagearr/internal/triggers"
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
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config from %q: %w", path, err)
	}
	s, err := openStoreAndMigrate(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	for {
		runCtx, cancel := context.WithCancel(signalCtx)
		done := make(chan error, 1)
		go func(c *config.Config) {
			done <- runDaemon(runCtx, s, c)
		}(cfg)

	inner:
		for {
			select {
			case <-signalCtx.Done():
				cancel()
				return <-done
			case <-hup:
				newCfg, err := config.Load(path)
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

// runDaemon assembles the poller set + HTTP server from cfg and blocks until
// ctx is cancelled. Reused by serveAction's SIGHUP loop to spawn a fresh
// daemon when config changes.
func runDaemon(ctx context.Context, s *store.Store, cfg *config.Config) error {
	reg, err := registry.BuildFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("building client registry: %w", err)
	}

	var ps []pollers.Poller

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
		ps = append(ps, &pollers.QbitPoller{Client: qb, Store: s, Interval: cfg.Polling.QbitInterval})
		ps = append(ps, &pollers.TrackerPoller{Client: qb, Store: s, Interval: cfg.Polling.TrackerInterval})
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
		})
	}

	if vols := enabledVolumes(cfg); len(vols) > 0 {
		ps = append(ps, &pollers.DiskPoller{Volumes: vols, Store: s, Interval: cfg.Polling.DiskInterval})
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
		return errors.New("no pollers enabled — check your config (qbit.enabled, arrs.*.enabled, volumes[].disk_pressure.enabled)")
	}

	sc := scorer.New(scorer.Options{
		Cfg:   cfg.Scoring,
		Qbit:  cfg.Qbit,
		Arrs:  cfg.Arrs,
		Store: s,
	})
	ps = append(ps, &scorer.Loop{Scorer: sc, Interval: cfg.Scoring.Interval})

	dec := decider.New(s)
	if rules := pressureRules(cfg); len(rules) > 0 {
		ps = append(ps, &triggers.DiskWatcher{
			Rules:    rules,
			Decider:  dec,
			Store:    s,
			Interval: cfg.Polling.DiskInterval,
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
			Bind:    cfg.HTTP.Bind,
			APIKey:  apiKey,
			Store:   s,
			Decider: dec,
			Volume:  volumeLookup(cfg),
			Volumes: func() []decider.Volume { return allVolumes(cfg) },
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

func pressureRules(cfg *config.Config) []triggers.VolumeRule {
	var out []triggers.VolumeRule
	for _, v := range cfg.Volumes {
		if !v.DiskPressure.Enabled {
			continue
		}
		if v.DiskPressure.ThresholdFreePercent <= 0 {
			continue
		}
		out = append(out, triggers.VolumeRule{
			Name:                 v.Name,
			Path:                 v.Path,
			ThresholdFreePercent: v.DiskPressure.ThresholdFreePercent,
			TargetFreePercent:    v.DiskPressure.TargetFreePercent,
			MaxRunSizeGB:         v.DiskPressure.MaxRunSizeGB,
		})
	}
	return out
}

func allVolumes(cfg *config.Config) []decider.Volume {
	out := make([]decider.Volume, 0, len(cfg.Volumes))
	for _, v := range cfg.Volumes {
		out = append(out, decider.Volume{
			Name:              v.Name,
			Path:              v.Path,
			TargetFreePercent: v.DiskPressure.TargetFreePercent,
			MaxRunSizeGB:      v.DiskPressure.MaxRunSizeGB,
		})
	}
	return out
}

func volumeLookup(cfg *config.Config) func(string) (decider.Volume, bool) {
	all := allVolumes(cfg)
	return func(name string) (decider.Volume, bool) {
		for _, v := range all {
			if v.Name == name {
				return v, true
			}
		}
		return decider.Volume{}, false
	}
}

func arrURLMap(cfg *config.Config) map[string]string {
	out := map[string]string{}
	add := func(typ triagearr.ArrType, list []config.ArrInstanceConfig) {
		for _, inst := range list {
			out[pollers.URLKey(inst.Name, typ)] = inst.URL
		}
	}
	add(triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr)
	add(triagearr.ArrTypeRadarr, cfg.Arrs.Radarr)
	add(triagearr.ArrTypeLidarr, cfg.Arrs.Lidarr)
	add(triagearr.ArrTypeReadarr, cfg.Arrs.Readarr)
	add(triagearr.ArrTypeWhisparrV2, cfg.Arrs.WhisparrV2)
	add(triagearr.ArrTypeWhisparrV3, cfg.Arrs.WhisparrV3)
	return out
}

func enabledVolumes(cfg *config.Config) []pollers.Volume {
	var out []pollers.Volume
	for _, v := range cfg.Volumes {
		if !v.DiskPressure.Enabled {
			continue
		}
		out = append(out, pollers.Volume{Name: v.Name, Path: v.Path})
	}
	return out
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
			r.Type, r.Name, r.URL, health, optTime(r.LastHealthCheck), lastErr,
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
	if cmd.NArg() < 3 {
		return errors.New("usage: triagearr inspect media <arr-type> <arr-name> <media-id>")
	}
	arrType := triagearr.ArrType(cmd.Args().Get(0))
	arrName := cmd.Args().Get(1)
	mediaID, err := strconv.ParseInt(cmd.Args().Get(2), 10, 64)
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
	files, err := s.ListMediaFilesByMedia(ctx, arrName, arrType, triagearr.MediaID(mediaID))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("no files stored for %s/%s/%d — the *arr poller may not have fanned out yet\n", arrType, arrName, mediaID)
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
	_, _ = fmt.Fprintln(tw, "ARR\tNAME\tFILE_ID\tSIZE\tLIVE_PATH\tDROPPED_PATH")
	for _, l := range links {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
			l.ArrType, l.ArrName, l.FileID, humanBytes(l.Size), l.LivePath, l.DroppedPath,
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
	_, _ = fmt.Fprintln(tw, "ARR\tNAME\tIMPORTS\tMAX_HISTORY_ID")
	for _, group := range []struct {
		typ   triagearr.ArrType
		insts []config.ArrInstanceConfig
	}{
		{triagearr.ArrTypeSonarr, cfg.Arrs.Sonarr},
		{triagearr.ArrTypeRadarr, cfg.Arrs.Radarr},
	} {
		for _, inst := range group.insts {
			if !inst.Enabled {
				continue
			}
			n, err := s.CountArrImports(ctx, inst.Name, group.typ)
			if err != nil {
				return err
			}
			max, err := s.MaxHistoryID(ctx, inst.Name, group.typ)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", group.typ, inst.Name, n, max)
		}
	}
	return tw.Flush()
}
