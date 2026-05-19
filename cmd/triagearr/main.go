// Package main is the entry point for the triagearr CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/registry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/logging"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
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
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := openStoreAndMigrate(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	reg, err := registry.BuildFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("building client registry: %w", err)
	}

	var ps []pollers.Poller

	if cfg.Qbit.Enabled {
		qb, qbErr := qbit.New(qbit.Options{
			BaseURL:  cfg.Qbit.URL,
			Username: cfg.Qbit.Username,
			Password: cfg.Qbit.Password,
			Timeout:  cfg.Qbit.Timeout,
		})
		if qbErr != nil {
			return fmt.Errorf("building qbit client: %w", qbErr)
		}
		ps = append(ps, &pollers.QbitPoller{Client: qb, Store: s, Interval: cfg.Polling.QbitInterval})
	}

	pollingArrs := reg.AllPolling()
	if len(pollingArrs) > 0 {
		urls := arrURLMap(cfg)
		ps = append(ps, &pollers.ArrPoller{
			Instances: pollingArrs,
			URLs:      urls,
			Store:     s,
			Interval:  cfg.Polling.ArrInterval,
		})
	}

	if vols := enabledVolumes(cfg); len(vols) > 0 {
		ps = append(ps, &pollers.DiskPoller{Volumes: vols, Store: s, Interval: cfg.Polling.DiskInterval})
	}

	if len(ps) == 0 {
		return errors.New("no pollers enabled — check your config (qbit.enabled, arrs.*.enabled, volumes[].disk_pressure.enabled)")
	}

	slog.Info("daemon starting",
		"mode", string(cfg.Mode),
		"pollers", len(ps),
		"sqlite", cfg.Storage.SQLitePath,
		"version", version,
	)

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mgr := pollers.NewManager(ps...)
	return mgr.Run(signalCtx)
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
