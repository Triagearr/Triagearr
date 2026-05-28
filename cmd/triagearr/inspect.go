package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/linker"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

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
