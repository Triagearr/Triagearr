// Package main is the entry point for the triagearr CLI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/logging"
	"github.com/Triagearr/Triagearr/internal/store"
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
		Usage:   "disk-pressure-aware media reaper for *arr + torrent-client stacks",
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

func openStoreAndMigrate(ctx context.Context, cfg *config.Config) (*store.Store, error) {
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("applying migrations: %w", err)
	}
	return s, nil
}

func migrateAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := openStoreAndMigrate(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	slog.Info("migrations applied", "db", cfg.Storage.SQLitePath)
	return nil
}
