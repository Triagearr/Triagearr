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

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func runCommand(configFlag cli.Flag) *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "trigger a one-shot decision (dry-run only in M4)",
		Flags: []cli.Flag{
			configFlag,
			&cli.BoolFlag{Name: "now", Usage: "trigger immediately (currently required)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "produce a plan without acting (required in M4)"},
			&cli.BoolFlag{Name: "live", Usage: "destructive mode (arrives in M5)"},
			&cli.StringFlag{Name: "volume", Usage: "target volume name; defaults to the most pressed volume"},
			&cli.BoolFlag{Name: "json", Usage: "emit JSON instead of a table"},
		},
		Action: runAction,
	}
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("live") {
		return errors.New("live mode arrives in M5; use --dry-run for now")
	}
	if !cmd.Bool("dry-run") {
		return errors.New("--dry-run is required (live mode arrives in M5)")
	}
	if !cmd.Bool("now") {
		return errors.New("--now is required for one-shot triggers")
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

	v, err := pickVolume(ctx, cfg, s, cmd.String("volume"))
	if err != nil {
		return err
	}

	dec := decider.New(s)
	plan, err := dec.Plan(ctx, v)
	if err != nil {
		return err
	}

	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerCLI,
		TriggeredAt:         time.Now().UTC(),
		Mode:                "dry-run",
		VolumeName:          v.Name,
		FreePctAtFire:       plan.FreePctAtFire,
		TargetFreePct:       v.TargetFreePercent,
		EstimatedFreedBytes: plan.EstimatedFreedBytes,
		StopReason:          plan.StopReason,
		Status:              "completed",
	}
	id, err := s.InsertRun(ctx, run)
	if err != nil {
		return fmt.Errorf("persisting run: %w", err)
	}
	if err := s.InsertRunItems(ctx, id, plan.Items); err != nil {
		return fmt.Errorf("persisting run items: %w", err)
	}
	run.ID = id

	if cmd.Bool("json") {
		return emitRunJSON(run, plan.Items)
	}
	return emitRunTable(run, plan.Items)
}

func pickVolume(ctx context.Context, cfg *config.Config, s *store.Store, name string) (decider.Volume, error) {
	all := allVolumes(cfg)
	if len(all) == 0 {
		return decider.Volume{}, errors.New("no volumes configured")
	}
	if name != "" {
		for _, v := range all {
			if v.Name == name {
				return v, nil
			}
		}
		return decider.Volume{}, fmt.Errorf("unknown volume %q", name)
	}
	// No volume given: pick the lowest-free% volume from latest disk_usage.
	disks, err := s.LatestDiskUsage(ctx)
	if err != nil {
		return decider.Volume{}, err
	}
	if len(disks) == 0 {
		return all[0], nil
	}
	worst := disks[0]
	for _, d := range disks[1:] {
		if d.FreePercent < worst.FreePercent {
			worst = d
		}
	}
	for _, v := range all {
		if v.Name == worst.VolumeName {
			return v, nil
		}
	}
	return all[0], nil
}

func emitRunJSON(r triagearr.Run, items []triagearr.RunItem) error {
	out := map[string]any{
		"run_id":                r.ID,
		"triggered_by":          string(r.TriggeredBy),
		"triggered_at":          r.TriggeredAt,
		"mode":                  r.Mode,
		"volume":                r.VolumeName,
		"free_pct_at_fire":      r.FreePctAtFire,
		"target_free_pct":       r.TargetFreePct,
		"estimated_freed_bytes": r.EstimatedFreedBytes,
		"stop_reason":           string(r.StopReason),
		"status":                r.Status,
		"candidates":            items,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func emitRunTable(r triagearr.Run, items []triagearr.RunItem) error {
	fmt.Printf("run #%d  volume=%s  free=%.2f%%→%.2f%%  est.freed=%s  stop=%s  candidates=%d\n",
		r.ID, r.VolumeName, r.FreePctAtFire, r.TargetFreePct,
		humanBytes(r.EstimatedFreedBytes), r.StopReason, len(items))
	if len(items) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RANK\tHASH\tSCORE\tSIZE\tWOULD_FREE")
	for _, it := range items {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%.2f\t%s\t%s\n",
			it.Rank, short(string(it.TorrentHash), 12), it.Score,
			humanBytes(it.SizeBytes), humanBytes(it.WouldFreeBytes))
	}
	return tw.Flush()
}
