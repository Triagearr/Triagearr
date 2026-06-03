package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/runlock"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/web"
)

// newHTTPServer wires the long-lived HTTP server. The API key lives in
// `${data_dir}/api_key` (Sonarr-style), auto-generated if absent. The Reload
// hook funnels settings/connection saves into serveAction's reload controller
// and blocks until the swap is live, so the handler reports the real outcome.
func newHTTPServer(cfg *config.Config, s *store.Store, eng *server.Engine, cfgPath string, reloadCh chan<- reloadRequest, runLock *runlock.Lock) (*server.Server, error) {
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
		RunLock: runLock,
	}, eng), nil
}

// runLockPath is the file backing the cross-process run-lock, alongside the
// SQLite DB and api_key in the data dir (mirrors the api_key path derivation).
func runLockPath(cfg *config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Storage.SQLitePath), "run.lock")
}
