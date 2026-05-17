// Package main is the entry point for the triagearr CLI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

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
				Name:  "health",
				Usage: "report process health and exit",
				Action: func(_ context.Context, _ *cli.Command) error {
					slog.Info("health check ok", "version", version)
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "err", err)
		os.Exit(1)
	}
}
