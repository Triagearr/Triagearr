package scorer

import (
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// SimInput is the proposed scoring config the simulator scores the archetypes
// against. It carries exactly the knobs surfaced on /settings/scoring: the
// per-factor weights, the HnR window, the tracker-dead grace, and the global
// threshold defaults (min_ratio / min_seed_days / rare_threshold). The
// simulator never touches the database — it replays the real factor functions
// over fixed fixtures so the operator sees the effect of a change before
// saving it.
type SimInput struct {
	Weights          config.ScoringWeights
	HnRWindowDays    int
	TrackerDeadGrace time.Duration
	Defaults         triagearr.ScoringDefaults
}

// SimResult is one archetype's verdict. Name and Description are stable keys
// the UI maps to localized strings; Breakdown is the same shape the live
// scorer emits so the front-end renders simulator and real scores identically.
type SimResult struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Breakdown   Breakdown `json:"breakdown"`
}

// archetype is a synthetic torrent crafted to exercise one facet of the
// scoring model. The fixtures intentionally cover every factor and gate so the
// operator can watch each knob bite.
type archetype struct {
	name      string
	desc      string
	torrent   store.ScoringTorrent
	snaps     store.SnapshotStats
	trackers  []trackerView
	globalAvg float64
}

// simGlobalAvg is the fixed upload-velocity normaliser used across archetypes.
// 1 MiB/day keeps Factor 2 active (a zero baseline would gate it as
// no_swarm_signal) and gives the velocities below sensible normalised values.
const simGlobalAvg = 1 << 20

// archetypes builds the fixture set relative to now so age- and window-based
// factors evaluate deterministically regardless of when Simulate runs.
func archetypes(now time.Time) []archetype {
	const day = 24 * time.Hour
	at := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }

	aliveTracker := func(host string) trackerView {
		return trackerView{Host: host, Status: triagearr.TrackerWorking, LastChecked: now}
	}
	deadTracker := func(host string, deadFor time.Duration) trackerView {
		return trackerView{Host: host, Status: triagearr.TrackerNotWorking, LastChecked: now, FirstSeenDead: at(deadFor)}
	}

	return []archetype{
		{
			name: "public_well_seeded",
			desc: "Public tracker, fully seeded, old. Ratio/HnR factors are inert on public — high score, safe to reap.",
			torrent: store.ScoringTorrent{
				Hash: "sim-public", Name: "Public well-seeded", Private: false,
				AddedOn: now.Add(-200 * day), CompletionOn: at(200 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 500, VelocityBytesPerDay: 0, LatestRatio: 5.0},
			trackers:  []trackerView{aliveTracker("opentracker.example.org")},
			globalAvg: simGlobalAvg,
		},
		{
			name: "private_obligation_met",
			desc: "Private, ratio + seed-time obligation met, past the HnR window. Reapable.",
			torrent: store.ScoringTorrent{
				Hash: "sim-met", Name: "Private obligation met", Private: true,
				AddedOn: now.Add(-60 * day), CompletionOn: at(60 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 20, VelocityBytesPerDay: 100 << 10, LatestRatio: 2.0},
			trackers:  []trackerView{aliveTracker("private.example.org")},
			globalAvg: simGlobalAvg,
		},
		{
			name: "private_in_hnr_window",
			desc: "Private, completed 3 days ago — inside the HnR window. Hard veto, never deleted.",
			torrent: store.ScoringTorrent{
				Hash: "sim-hnr", Name: "Private in HnR window", Private: true,
				AddedOn: now.Add(-3 * day), CompletionOn: at(3 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 50, VelocityBytesPerDay: 2 << 20, LatestRatio: 0.5},
			trackers:  []trackerView{aliveTracker("private.example.org")},
			globalAvg: simGlobalAvg,
		},
		{
			name: "private_obligation_unmet",
			desc: "Private, ratio below min_ratio and too young — obligation not met. Lower score until you relax the threshold.",
			torrent: store.ScoringTorrent{
				Hash: "sim-unmet", Name: "Private obligation unmet", Private: true,
				AddedOn: now.Add(-10 * day), CompletionOn: at(10 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 30, VelocityBytesPerDay: 0, LatestRatio: 0.3},
			trackers:  []trackerView{aliveTracker("private.example.org")},
			globalAvg: simGlobalAvg,
		},
		{
			name: "rare_content",
			desc: "Private, obligation met, but only ~1 seeder on a live tracker — rare-content guard protects it.",
			torrent: store.ScoringTorrent{
				Hash: "sim-rare", Name: "Rare content", Private: true,
				AddedOn: now.Add(-90 * day), CompletionOn: at(90 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 1, VelocityBytesPerDay: 0, LatestRatio: 2.0},
			trackers:  []trackerView{aliveTracker("private.example.org")},
			globalAvg: simGlobalAvg,
		},
		{
			name: "dead_tracker_library",
			desc: "Private, every tracker dead well past the grace window. Gates lift, tracker-dead bonus fires — primary graveyard target.",
			torrent: store.ScoringTorrent{
				Hash: "sim-dead", Name: "Dead tracker library", Private: true,
				AddedOn: now.Add(-300 * day), CompletionOn: at(300 * day),
			},
			snaps:     store.SnapshotStats{SeedersAvg7d: 0, VelocityBytesPerDay: 0, LatestRatio: 0.1},
			trackers:  []trackerView{deadTracker("defunct.example.org", 30*day)},
			globalAvg: simGlobalAvg,
		},
	}
}

// Simulate scores the built-in archetypes against the proposed config and
// returns one result per archetype, preserving the archetype order so the UI
// can keep stable identities while re-ranking by score client-side. It is a
// pure function: no store access, no persistence, no time dependence beyond the
// reference now used to anchor the fixtures.
func Simulate(in SimInput) []SimResult {
	return simulateAt(in, time.Now().UTC())
}

func simulateAt(in SimInput, now time.Time) []SimResult {
	cfg := config.ScoringConfig{
		HnRWindowDays:    in.HnRWindowDays,
		TrackerDeadGrace: in.TrackerDeadGrace,
		Weights:          in.Weights,
	}
	arcs := archetypes(now)
	out := make([]SimResult, 0, len(arcs))
	for _, a := range arcs {
		alive := anyTrackerAlive(a.trackers)
		// Archetypes carry no per-tracker overrides; resolve against the
		// proposed defaults only, matching an unconfigured tracker host.
		policy := trackerPolicyFor(a.trackers, in.Defaults, nil)
		factors := evalFactors(a.torrent, a.snaps, a.globalAvg, a.trackers, cfg, policy, alive, now)

		var total float64
		for _, f := range factors {
			total += f.Contribution
		}
		out = append(out, SimResult{
			Name:        a.name,
			Description: a.desc,
			Breakdown: Breakdown{
				Hash:            a.torrent.Hash,
				Score:           total,
				Private:         a.torrent.Private,
				AnyTrackerAlive: alive,
				Factors:         factors,
				ComputedAt:      now,
			},
		})
	}
	return out
}
