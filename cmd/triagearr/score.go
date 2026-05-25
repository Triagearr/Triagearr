package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func scoreCommand(configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "score",
		Usage: "compute, inspect, and persist DeleteScores (M3)",
		Commands: []*cli.Command{
			{
				Name:      "explain",
				Usage:     "show the per-factor breakdown for one torrent",
				ArgsUsage: "<torrent-hash>",
				Flags: []cli.Flag{
					configFlag,
					&cli.BoolFlag{Name: "json", Usage: "emit the raw breakdown JSON"},
					&cli.BoolFlag{Name: "recompute", Usage: "score the torrent now instead of reading the persisted row"},
				},
				Action: scoreExplainAction,
			},
			{
				Name:      "recompute",
				Usage:     "force a single-hash recompute and persist the result",
				ArgsUsage: "<torrent-hash>",
				Flags:     []cli.Flag{configFlag},
				Action:    scoreRecomputeAction,
			},
			{
				Name:  "top",
				Usage: "list torrents ordered by score (most-deletable first)",
				Flags: []cli.Flag{
					configFlag,
					&cli.IntFlag{Name: "limit", Value: 20, Usage: "maximum number of rows"},
					&cli.BoolFlag{Name: "include-excluded", Usage: "show torrents matching exclusion filters"},
				},
				Action: scoreTopAction,
			},
		},
	}
}

func scoreExplainAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return errors.New("usage: triagearr score explain <torrent-hash>")
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

	if cmd.Bool("recompute") {
		sc := scorer.New(scorer.Options{Cfg: cfg.Scoring, Qbit: cfg.TorrentClients.Qbittorrent, Arrs: cfg.Arrs, Store: s})
		b, err := sc.ScoreOne(ctx, hash)
		if err != nil {
			return err
		}
		return renderBreakdown(b, cmd.Bool("json"))
	}

	row, err := s.GetScore(ctx, hash)
	if err != nil {
		return fmt.Errorf("loading score for %s (run with --recompute if the scorer has not yet seen this hash): %w", hash, err)
	}
	b, err := breakdownFromRow(row)
	if err != nil {
		return err
	}
	return renderBreakdown(b, cmd.Bool("json"))
}

func scoreRecomputeAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() < 1 {
		return errors.New("usage: triagearr score recompute <torrent-hash>")
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
	sc := scorer.New(scorer.Options{Cfg: cfg.Scoring, Qbit: cfg.TorrentClients.Qbittorrent, Arrs: cfg.Arrs, Store: s})
	b, err := sc.ScoreOne(ctx, hash)
	if err != nil {
		return err
	}
	return renderBreakdown(b, false)
}

func scoreTopAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := loadConfigFromCmd(cmd)
	if err != nil {
		return err
	}
	s, err := store.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	rows, err := s.ListScores(ctx, store.ListScoresOpts{
		IncludeExcluded: cmd.Bool("include-excluded"),
		Limit:           cmd.Int("limit"),
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("no scores stored — the scorer may not have run yet (start `triagearr serve` or use `score recompute <hash>`)")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HASH\tSCORE\tPRIVATE\tTRACKERS\tEXCLUDED\tCOMPUTED_AT")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%.2f\t%s\t%s\t%s\t%s\n",
			short(r.Hash, 12),
			r.Score,
			yesNo(r.Private),
			trackerLabel(r.AnyTrackerAlive),
			excludedLabel(r.Excluded, r.ExclusionReasons),
			r.ComputedAt.Format(time.RFC3339),
		)
	}
	return tw.Flush()
}

func breakdownFromRow(row store.ScoreRow) (scorer.Breakdown, error) {
	var factors []scorer.Factor
	if err := json.Unmarshal([]byte(row.FactorsJSON), &factors); err != nil {
		return scorer.Breakdown{}, fmt.Errorf("decoding factors_json: %w", err)
	}
	var reasons []string
	if row.ExclusionReasons != "" {
		reasons = splitCSV(row.ExclusionReasons)
	}
	return scorer.Breakdown{
		Hash:             row.Hash,
		Score:            row.Score,
		Private:          row.Private,
		AnyTrackerAlive:  row.AnyTrackerAlive,
		Excluded:         row.Excluded,
		ExclusionReasons: reasons,
		Factors:          factors,
		ComputedAt:       row.ComputedAt,
	}, nil
}

func renderBreakdown(b scorer.Breakdown, asJSON bool) error {
	if asJSON {
		out, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding breakdown: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("Hash:             %s\n", b.Hash)
	fmt.Printf("Score:            %.3f\n", b.Score)
	fmt.Printf("Private:          %s\n", yesNo(b.Private))
	fmt.Printf("AnyTrackerAlive:  %s\n", trackerLabel(b.AnyTrackerAlive))
	fmt.Printf("Excluded:         %s\n", excludedLabel(b.Excluded, joinCSV(b.ExclusionReasons)))
	fmt.Printf("ComputedAt:       %s\n\n", b.ComputedAt.Format(time.RFC3339))
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FACTOR\tVALUE\tWEIGHT\tCONTRIBUTION\tGATE")
	for _, f := range b.Factors {
		_, _ = fmt.Fprintf(tw, "%s\t%.3f\t%.2f\t%+.3f\t%s\n", f.Name, f.Value, f.Weight, f.Contribution, f.Gate)
	}
	return tw.Flush()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func trackerLabel(alive bool) string {
	if alive {
		return "alive"
	}
	return "all_dead"
}

func excludedLabel(excluded bool, reasons string) string {
	if !excluded {
		return "no"
	}
	if reasons == "" {
		return "yes"
	}
	return "yes (" + reasons + ")"
}

func splitCSV(csv string) []string {
	if csv == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i <= len(csv); i++ {
		if i == len(csv) || csv[i] == ',' {
			if i > start {
				out = append(out, csv[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func joinCSV(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "," + p
	}
	return out
}
