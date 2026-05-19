// Package main is the entry point for the triagearr CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/clients/qbit"
	"github.com/Triagearr/Triagearr/internal/clients/registry"
	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/logging"
	"github.com/Triagearr/Triagearr/internal/mapper"
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
						Usage:     "show per-file path translation, inode and hardlink count for a torrent",
						ArgsUsage: "<torrent-hash>",
						Flags:     []cli.Flag{configFlag},
						Action:    inspectMappingAction,
					},
					{
						Name:   "remap",
						Usage:  "print the active path_remap rules per volume",
						Flags:  []cli.Flag{configFlag},
						Action: inspectRemapAction,
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
			VacuumEnabled:         cfg.Storage.Vacuum.Enabled,
			VacuumMinReclaimBytes: cfg.Storage.Vacuum.MinReclaimMB * 1024 * 1024,
		},
	})

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

	// Inference waits on the first *arr fan-out to populate media_files (its
	// sample source), so it can't block startup.
	go runBootInference(signalCtx, cfg, s)

	mgr := pollers.NewManager(ps...)
	return mgr.Run(signalCtx)
}

// runBootInference waits for the first *arr fan-out to populate media_files,
// then runs the ADR-0010 boot procedure per configured volume and logs the
// result. Manual overrides are validated immediately.
func runBootInference(ctx context.Context, cfg *config.Config, s *store.Store) {
	const (
		pollEvery  = 5 * time.Second
		maxWait    = 10 * time.Minute
		minSamples = 5
	)

	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for _, v := range cfg.Volumes {
		manual := manualRulesFor(v.PathRemap)
		if len(manual) > 0 {
			rules, err := mapper.ValidateManual(manual)
			if err != nil {
				slog.Error("mapper manual path_remap invalid", "volume", v.Name, "err", err)
				continue
			}
			for _, r := range rules {
				slog.Info("path_remap_active", "volume", v.Name, "origin", string(r.Origin), "from", r.From, "to", r.To)
			}
			continue
		}

		// Wait for samples to be available, then run inference.
		for {
			if ctx.Err() != nil {
				return
			}
			samples, err := s.SampleMediaFilePaths(ctx, cfg.Mapper.SampleCount)
			if err != nil {
				slog.Warn("mapper sampling failed", "volume", v.Name, "err", err)
			}
			if len(samples) >= minSamples || time.Now().After(deadline) {
				bootRunInference(ctx, cfg, v, samples)
				break
			}
			slog.Debug("mapper waiting for samples", "volume", v.Name, "got", len(samples), "want", minSamples)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}

func bootRunInference(ctx context.Context, cfg *config.Config, v config.VolumeConfig, samples []string) {
	in := mapper.BootInputs{
		VolumeName:      v.Name,
		Root:            v.Path,
		IndexMaxEntries: cfg.Mapper.IndexMaxEntries,
	}
	for _, p := range samples {
		in.ArrSamples = append(in.ArrSamples, mapper.Sample{SourcePath: p, Size: -1})
	}
	res, err := mapper.Boot(ctx, in)
	if err != nil {
		slog.Error("path_remap_inference_failed", "volume", v.Name, "err", err)
		if res.Inference != nil {
			for _, c := range res.Inference.Candidates {
				slog.Warn("inference_candidate", "volume", v.Name, "from", c.From, "to", c.To, "votes", c.Votes)
			}
		}
		return
	}
	for _, r := range res.Rules {
		slog.Info("path_remap_inferred",
			"volume", v.Name,
			"origin", string(r.Origin),
			"from", r.From,
			"to", r.To,
			"samples_matched", r.SampleMatches,
			"samples_total", r.SampleTotal,
		)
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
	hash := triagearr.Hash(cmd.Args().Get(0))
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
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
	hash := triagearr.Hash(cmd.Args().Get(0))
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	resolver, err := buildResolverFromStore(ctx, cfg, s)
	if err != nil {
		return fmt.Errorf("building mapper: %w", err)
	}

	if !cfg.Qbit.Enabled {
		return errors.New("qbit.enabled must be true to run inspect mapping (need live torrent files)")
	}
	qb, err := qbit.New(qbit.Options{
		BaseURL:  cfg.Qbit.URL,
		Username: cfg.Qbit.Username,
		Password: cfg.Qbit.Password,
		Timeout:  cfg.Qbit.Timeout,
	})
	if err != nil {
		return fmt.Errorf("building qbit client: %w", err)
	}
	files, err := qb.TorrentFiles(ctx, hash)
	if err != nil {
		return fmt.Errorf("fetching torrent files: %w", err)
	}
	// qBit's file.name is relative to save_path. Resolve absolute paths by
	// joining with the torrent's save_path (read from the latest qBit poll).
	tors, err := qb.ListTorrents(ctx)
	if err != nil {
		return fmt.Errorf("fetching torrents: %w", err)
	}
	var savePath string
	for _, t := range tors {
		if t.Hash == hash {
			savePath = t.SavePath
			break
		}
	}
	if savePath == "" {
		return fmt.Errorf("torrent %s not found in current qBit list", hash)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "QBIT_PATH\tLOCAL_PATH\tINODE\tNLINK\tRULE\tNOTE")
	for _, f := range files {
		ref := resolver.StatFile(path.Join(savePath, f.Name))
		ruleDesc := "<none>"
		if ref.Rule.Origin != "" {
			ruleDesc = mapper.Describe(ref.Rule)
		}
		note := ref.StatErr
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\n",
			ref.QbitPath, ref.LocalPath, ref.Inode, ref.Nlink, ruleDesc, note,
		)
	}
	return tw.Flush()
}

func inspectRemapAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	resolver, err := buildResolverFromStore(ctx, cfg, s)
	if err != nil {
		return fmt.Errorf("building mapper: %w", err)
	}
	for _, v := range resolver.VolumeRules() {
		fmt.Printf("volume %q:\n", v.VolumeName)
		if len(v.Rules) == 0 {
			fmt.Println("  (no rules)")
			continue
		}
		for _, r := range v.Rules {
			fmt.Printf("  - %s\n", mapper.Describe(r))
		}
	}
	return nil
}

// buildResolverFromStore re-runs the boot procedure synchronously (manual
// override OR boot inference from snapshots in the store). Used by the CLI
// inspect commands so they don't depend on the running daemon.
func buildResolverFromStore(ctx context.Context, cfg *config.Config, s *store.Store) (*mapper.Resolver, error) {
	resolver := mapper.NewResolver()
	var allVolumes []mapper.VolumeRules
	for _, v := range cfg.Volumes {
		in := mapper.BootInputs{
			VolumeName:      v.Name,
			Root:            v.Path,
			ManualRules:     manualRulesFor(v.PathRemap),
			IndexMaxEntries: cfg.Mapper.IndexMaxEntries,
		}
		if len(in.ManualRules) == 0 {
			paths, err := s.SampleMediaFilePaths(ctx, cfg.Mapper.SampleCount)
			if err != nil {
				return nil, fmt.Errorf("sampling media_files: %w", err)
			}
			for _, p := range paths {
				in.ArrSamples = append(in.ArrSamples, mapper.Sample{SourcePath: p, Size: -1})
			}
		}
		res, err := mapper.Boot(ctx, in)
		if err != nil {
			if res.Inference != nil {
				logInferenceCandidates(v.Name, res.Inference)
			}
			return nil, err
		}
		allVolumes = append(allVolumes, mapper.VolumeRules{VolumeName: v.Name, Rules: res.Rules})
	}
	resolver.Set(allVolumes)
	return resolver, nil
}

func manualRulesFor(entries []config.PathRemapEntry) []mapper.ManualRule {
	out := make([]mapper.ManualRule, len(entries))
	for i, e := range entries {
		out[i] = mapper.ManualRule{From: e.From, To: e.To}
	}
	return out
}

func logInferenceCandidates(volume string, inf *mapper.InferenceResult) {
	slog.Error("path_remap_inference_failed",
		"volume", volume,
		"samples_total", inf.SamplesTotal,
		"samples_matched", inf.SamplesMatched,
	)
}
