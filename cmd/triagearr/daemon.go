package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/runlock"
	"github.com/Triagearr/Triagearr/internal/server"
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

	// One run-lock for the daemon's whole life, shared by the HTTP server and
	// every (reload-rebuilt) disk-pressure watcher so live runs can't overlap.
	// Created here, not in buildEngine: the server is built once and survives
	// reloads, so a per-reload lock would drift apart from it. The backing file
	// also fences the separate `triagearr run --live` process.
	runLock, err := runlock.Open(runLockPath(cfg))
	if err != nil {
		return fmt.Errorf("opening run lock: %w", err)
	}
	defer func() { _ = runLock.Close() }()

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	reloadCh := make(chan reloadRequest, 1)

	// Build the initial engine + pollers. A failure here (e.g. ADR-0023
	// preflight) is fatal at boot, unlike a reload failure which is recoverable.
	eng, ps, err := buildEngine(signalCtx, s, cfg, runLock)
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
		httpSrv, err = newHTTPServer(cfg, s, eng, path, reloadCh, runLock)
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
		newEng, newPs, err := buildEngine(signalCtx, s, newCfg, runLock)
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
