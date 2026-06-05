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
	"github.com/Triagearr/Triagearr/internal/runlock"
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
			&cli.BoolFlag{Name: "json", Usage: "emit JSON instead of a table"},
		},
		Action: runAction,
	}
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	live := cmd.Bool("live")
	dryRun := cmd.Bool("dry-run")
	if live && dryRun {
		return errors.New("--live and --dry-run are mutually exclusive")
	}
	if !live && !dryRun {
		return errors.New("exactly one of --live or --dry-run is required")
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

	daemonLive := cfg.Mode == config.ModeLive
	mode := triagearr.ResolveRunMode(daemonLive, triagearr.RunTriggerCLI, live)
	if live && mode != triagearr.RunModeLive {
		return errors.New("--live requires the daemon's mode: live (current config is dry-run)")
	}

	// A live CLI run shares the cross-process run-lock with the daemon (HTTP +
	// disk-pressure triggers). Claim it before planning/persisting so a contended
	// run writes nothing and can't race the daemon's destructive pipeline.
	if mode == triagearr.RunModeLive {
		lock, err := runlock.Open(runLockPath(cfg))
		if err != nil {
			return fmt.Errorf("opening run lock: %w", err)
		}
		defer func() { _ = lock.Close() }()
		if !lock.TryAcquire() {
			return errors.New("a live run is already in progress (daemon or another CLI); try again later")
		}
		defer lock.Release()
	}

	v := theVolume(cfg)

	dec := decider.New(s)
	plan, err := dec.Plan(ctx, v)
	if err != nil {
		return err
	}

	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerCLI,
		TriggeredAt:         time.Now().UTC(),
		Mode:                string(mode),
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

	if mode == triagearr.RunModeLive {
		act, qbitClient, err := buildActor(cfg, s)
		if err != nil {
			return err
		}
		_ = qbitClient // referenced via closure inside buildActor for lifecycle
		if err := act.Execute(ctx, id); err != nil {
			return fmt.Errorf("actor execute: %w", err)
		}
		refreshed, items, err := s.GetRun(ctx, id)
		if err == nil {
			run = refreshed
			plan.Items = items
		}
	}

	if cmd.Bool("json") {
		return emitRunJSON(run, plan.Items)
	}
	return emitRunTable(run, plan.Items)
}

func emitRunJSON(r triagearr.Run, items []triagearr.RunItem) error {
	out := map[string]any{
		"run_id":                r.ID,
		"triggered_by":          string(r.TriggeredBy),
		"triggered_at":          r.TriggeredAt,
		"mode":                  r.Mode,
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
	fmt.Printf("run #%d  free=%.2f%%→%.2f%%  est.freed=%s  stop=%s  candidates=%d\n",
		r.ID, r.FreePctAtFire, r.TargetFreePct,
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
