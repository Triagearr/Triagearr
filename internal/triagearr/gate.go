package triagearr

// RunMode is the resolved deletion mode persisted in `runs.mode`.
type RunMode string

// Supported RunMode values.
const (
	RunModeDryRun RunMode = "dry-run"
	RunModeLive   RunMode = "live"
)

// ResolveRunMode applies ADR-0015's trigger × opt-in × daemon-mode table.
//
//   - The daemon's global mode is the hard ceiling: a dry-run daemon never
//     produces live runs, regardless of trigger or caller opt-in.
//   - Disk-pressure runs auto-execute live when the daemon is live (the
//     pressure threshold is itself the human-defined opt-in).
//   - HTTP and CLI triggers always require an explicit per-request opt-in
//     (request body `"mode":"live"` or `--live` flag) to go live.
//   - Any future scheduled trigger (cron) is forced to dry-run forever,
//     regardless of daemon mode or opt-in.
//
// daemonLive reflects config.Mode == ModeLive. requestedLive is whatever the
// caller signalled (body field, CLI flag); ignored for pressure and cron.
func ResolveRunMode(daemonLive bool, trigger RunTrigger, requestedLive bool) RunMode {
	if !daemonLive {
		return RunModeDryRun
	}
	switch trigger {
	case RunTriggerDiskPressure:
		return RunModeLive
	case RunTriggerHTTP, RunTriggerCLI:
		if requestedLive {
			return RunModeLive
		}
		return RunModeDryRun
	default:
		// Unknown trigger (e.g. a future scheduled "cron") stays dry-run.
		return RunModeDryRun
	}
}
