// Command devfixtures boots stateful in-memory fakes for qBittorrent,
// Sonarr, and Radarr, pre-seeded from a YAML scenario file. It exists so
// developers can iterate on the React UI (and the Actor in M5+) against
// realistic data without touching the production homelab.
//
// Usage:
//
//	go run ./cmd/devfixtures --scenario fixtures/scenarios/default.yaml
//
// Default ports are 18xxx (sonarr=18989, radarr=17878, qbit=18090) to avoid
// any confusion with a real *arr stack listening on the standard 8xxx ports.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	radarrfake "github.com/Triagearr/Triagearr/internal/clients/arr/radarr/fake"
	sonarrfake "github.com/Triagearr/Triagearr/internal/clients/arr/sonarr/fake"
	qbitfake "github.com/Triagearr/Triagearr/internal/clients/torrent/qbit/fake"
	"github.com/Triagearr/Triagearr/internal/devtools/fakedisk"
)

// Scenario is the on-disk YAML shape that seeds the four fakes.
type Scenario struct {
	Name   string         `yaml:"name"`
	Qbit   QbitScenario   `yaml:"qbit"`
	Sonarr SonarrScenario `yaml:"sonarr"`
	Radarr RadarrScenario `yaml:"radarr"`
	Disks  []DiskScenario `yaml:"disks"`
}

// DiskScenario seeds one volume on the fake disk server.
type DiskScenario struct {
	Name       string `yaml:"name"`
	TotalBytes uint64 `yaml:"total_bytes"`
	FreeBytes  uint64 `yaml:"free_bytes"`
}

// QbitScenario seeds the qBit fake. Empty username/password disables auth.
type QbitScenario struct {
	Username string             `yaml:"username"`
	Password string             `yaml:"password"`
	Torrents []qbitfake.Torrent `yaml:"torrents"`
}

// SonarrScenario seeds the Sonarr fake.
type SonarrScenario struct {
	APIKey  string                     `yaml:"api_key"`
	Tags    []sonarrfake.Tag           `yaml:"tags"`
	Series  []sonarrfake.Series        `yaml:"series"`
	History []sonarrfake.HistoryRecord `yaml:"history"`
}

// RadarrScenario seeds the Radarr fake.
type RadarrScenario struct {
	APIKey  string                     `yaml:"api_key"`
	Tags    []radarrfake.Tag           `yaml:"tags"`
	Movies  []radarrfake.Movie         `yaml:"movies"`
	History []radarrfake.HistoryRecord `yaml:"history"`
}

func main() {
	var (
		scenarioPath = flag.String("scenario", "fixtures/scenarios/default.yaml", "path to a YAML scenario file")
		sonarrPort   = flag.Int("sonarr-port", 18989, "TCP port for the fake Sonarr")
		radarrPort   = flag.Int("radarr-port", 17878, "TCP port for the fake Radarr")
		qbitPort     = flag.Int("qbit-port", 18090, "TCP port for the fake qBit")
		diskPort     = flag.Int("disk-port", 18091, "TCP port for the fake disk source")
		bindHost     = flag.String("host", "127.0.0.1", "host to bind on (use 0.0.0.0 to expose to LAN)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	sc, err := loadScenario(*scenarioPath)
	if err != nil {
		logger.Error("loading scenario", "path", *scenarioPath, "err", err)
		os.Exit(1)
	}
	logger.Info("scenario loaded",
		"name", sc.Name,
		"path", *scenarioPath,
		"qbit_torrents", len(sc.Qbit.Torrents),
		"sonarr_series", len(sc.Sonarr.Series),
		"radarr_movies", len(sc.Radarr.Movies),
	)

	qbitSrv, qbitHTTP := buildQbit(sc.Qbit, *bindHost, *qbitPort, logger)
	sonarrSrv, sonarrHTTP := buildSonarr(sc.Sonarr, *bindHost, *sonarrPort, logger)
	radarrSrv, radarrHTTP := buildRadarr(sc.Radarr, *bindHost, *radarrPort, logger)
	diskSrv, diskHTTP := buildDisk(sc.Disks, *bindHost, *diskPort, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	runServer(&wg, "qbit", qbitHTTP, logger)
	runServer(&wg, "sonarr", sonarrHTTP, logger)
	runServer(&wg, "radarr", radarrHTTP, logger)
	runServer(&wg, "disk", diskHTTP, logger)

	logger.Info("fakes ready",
		"sonarr", fmt.Sprintf("http://%s:%d", *bindHost, *sonarrPort),
		"radarr", fmt.Sprintf("http://%s:%d", *bindHost, *radarrPort),
		"qbit", fmt.Sprintf("http://%s:%d", *bindHost, *qbitPort),
		"disk", fmt.Sprintf("http://%s:%d", *bindHost, *diskPort),
		"sonarr_api_key", sc.Sonarr.APIKey,
		"radarr_api_key", sc.Radarr.APIKey,
	)
	_ = diskSrv

	<-ctx.Done()
	logger.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	for _, srv := range []*http.Server{qbitHTTP, sonarrHTTP, radarrHTTP, diskHTTP} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("shutdown", "err", err)
		}
	}
	wg.Wait()

	// Final stats so the operator can confirm what happened during the session.
	logger.Info("final state",
		"qbit_torrents", qbitSrv.State().Len(),
		"qbit_delete_calls", qbitSrv.State().DeleteCalls(),
		"sonarr_episodefile_deletes", sonarrSrv.State().EpisodeFileDeletes(),
		"radarr_moviefile_deletes", radarrSrv.State().MovieFileDeletes(),
	)
}

func loadScenario(path string) (*Scenario, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: scenario path is an operator-supplied dev-tool flag
	if err != nil {
		return nil, fmt.Errorf("reading scenario: %w", err)
	}
	var sc Scenario
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	return &sc, nil
}

func buildQbit(sc QbitScenario, host string, port int, logger *slog.Logger) (*qbitfake.Server, *http.Server) {
	srv := qbitfake.New(qbitfake.Options{
		Username: sc.Username,
		Password: sc.Password,
		Logger:   logger.With("component", "qbit"),
	})
	srv.State().AddMany(sc.Torrents)
	return srv, &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func buildSonarr(sc SonarrScenario, host string, port int, logger *slog.Logger) (*sonarrfake.Server, *http.Server) {
	srv := sonarrfake.New(sonarrfake.Options{
		APIKey: sc.APIKey,
		Logger: logger.With("component", "sonarr"),
	})
	for _, t := range sc.Tags {
		srv.State().AddTag(t)
	}
	for _, s := range sc.Series {
		srv.State().AddSeries(s)
	}
	for _, h := range sc.History {
		srv.State().AddHistory(h)
	}
	return srv, &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func buildDisk(scenarios []DiskScenario, host string, port int, logger *slog.Logger) (*fakedisk.Server, *http.Server) {
	srv := fakedisk.New(fakedisk.Options{Logger: logger.With("component", "disk")})
	for _, d := range scenarios {
		srv.State().Set(d.Name, d.TotalBytes, d.FreeBytes)
	}
	return srv, &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func buildRadarr(sc RadarrScenario, host string, port int, logger *slog.Logger) (*radarrfake.Server, *http.Server) {
	srv := radarrfake.New(radarrfake.Options{
		APIKey: sc.APIKey,
		Logger: logger.With("component", "radarr"),
	})
	for _, t := range sc.Tags {
		srv.State().AddTag(t)
	}
	for _, m := range sc.Movies {
		srv.State().AddMovie(m)
	}
	for _, h := range sc.History {
		srv.State().AddHistory(h)
	}
	return srv, &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func runServer(wg *sync.WaitGroup, name string, srv *http.Server, logger *slog.Logger) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("listening", "service", name, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "service", name, "err", err)
		}
	}()
}
