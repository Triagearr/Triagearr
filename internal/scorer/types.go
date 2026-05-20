// Package scorer implements Triagearr's DeleteScore engine (M3).
//
// The scorer reads the passive observations produced by the pollers (torrents,
// trackers, snapshots, arr_imports) and emits a per-torrent breakdown of the
// eight factors defined in docs/SCORING.md. Higher score = safer to delete.
//
// Two gates run before factor math:
//   - Private vs public: ratio-obligation and HnR-window factors are inert on
//     public trackers (no account, no penalty).
//   - any_tracker_alive: when every tracker for a torrent reports
//     status=not_working sustained beyond tracker_dead_grace, the rare-content
//     guard and HnR veto degrade to 0 — the swarm/HnR signal becomes
//     meaningless. The tracker_dead bonus picks up the slack.
//
// Both gates are recorded explicitly on every Factor whose value they zeroed,
// so the explain output names the reason rather than silently emitting 0.
package scorer

import "time"

// Factor names, stable identifiers used in factors_json. The CLI/UI relies on
// these for layout so renaming is a breaking change.
const (
	FactorRatioObligation = "ratio_obligation_met"
	FactorVelocityInv     = "upload_velocity_inv"
	FactorAge             = "age_days"
	FactorSeedersGuard    = "seeders_low_guard"
	FactorSwarmBonus      = "swarm_health_bonus"
	FactorHnRVeto         = "hnr_window_veto"
	FactorTrackerDead     = "tracker_dead_bonus"
)

// HnRVetoWeight is the hard-coded veto magnitude for in-window private torrents
// served by a live tracker. SCORING.md §Factor 6: "non-configurable".
const HnRVetoWeight = -10000.0

// Factor is one contribution to the final score. Contribution = Value × Weight,
// unless Gate is set in which case Contribution is forced to 0 and the gate
// label explains why.
type Factor struct {
	Name         string  `json:"name"`
	Value        float64 `json:"value"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
	Gate         string  `json:"gate,omitempty"`
}

// Breakdown is the full scoring verdict for one torrent. Persisted to the
// scores table (factors_json holds Factors).
type Breakdown struct {
	Hash             string    `json:"torrent_hash"`
	Score            float64   `json:"score"`
	Private          bool      `json:"private"`
	AnyTrackerAlive  bool      `json:"any_tracker_alive"`
	Excluded         bool      `json:"excluded"`
	ExclusionReasons []string  `json:"exclusion_reasons,omitempty"`
	Factors          []Factor  `json:"factors"`
	ComputedAt       time.Time `json:"computed_at"`
}

// Gate label constants, surfaced verbatim in factors_json.
const (
	GatePublic        = "public — inert"
	GateAllDead       = "all_trackers_dead"
	GateNoSwarmSignal = "no_swarm_signal"
)
