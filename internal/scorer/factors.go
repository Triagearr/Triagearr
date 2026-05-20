package scorer

import (
	"math"
	"strings"
	"time"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// trackerView is the minimal per-tracker shape the factors need. It hides the
// store row type from the factor functions so tests can synthesise inputs
// without round-tripping through SQLite.
type trackerView struct {
	Host          string
	Status        triagearr.TrackerStatus
	LastChecked   time.Time
	FirstSeenDead *time.Time
}

func trackerViewsFromRows(rows []store.TrackerRow) []trackerView {
	out := make([]trackerView, len(rows))
	for i, r := range rows {
		out[i] = trackerView{Host: r.Host, Status: r.Status, LastChecked: r.LastChecked, FirstSeenDead: r.FirstSeenDead}
	}
	return out
}

// anyTrackerAlive returns true if at least one tracker is not in not_working
// status. The mapping is intentional: status=0 (disabled) is treated as alive
// because disabled trackers are a user choice, not infrastructure death.
//
// When the torrent has zero trackers (DHT-only, fresh-from-magnet edge cases)
// the function returns true — without a tracker signal we cannot prove
// graveyard state, so we keep the conservative gates active.
func anyTrackerAlive(trackers []trackerView) bool {
	if len(trackers) == 0 {
		return true
	}
	for _, t := range trackers {
		if t.Status != triagearr.TrackerNotWorking {
			return true
		}
	}
	return false
}

// allTrackersDeadSustained returns true if every tracker has been not_working
// at least `grace` ago. Used by Factor 7 (the tracker_dead bonus) to avoid
// rewarding transient outages.
//
// "Sustained" is measured against first_seen_dead — the moment the tracker
// first reported not_working in a contiguous run. last_checked is rewritten
// every tracker tick (default 6h) and would never cross the grace window;
// first_seen_dead is preserved across polls until the tracker recovers.
func allTrackersDeadSustained(trackers []trackerView, now time.Time, grace time.Duration) bool {
	if len(trackers) == 0 {
		return false
	}
	cutoff := now.Add(-grace)
	for _, t := range trackers {
		if t.Status != triagearr.TrackerNotWorking {
			return false
		}
		if t.FirstSeenDead == nil || t.FirstSeenDead.After(cutoff) {
			return false
		}
	}
	return true
}

// trackerPolicyFor returns the per-tracker overrides for a torrent. When the
// torrent has multiple trackers, the strictest policy wins:
// max(min_seed_days), max(min_ratio), min(rare_threshold). This matches what
// a cautious user would expect: meet the toughest demand to be safe.
func trackerPolicyFor(trackers []trackerView, cfg config.ScoringConfig) config.TrackerPolicy {
	out := config.TrackerPolicy{}
	for _, t := range trackers {
		p, ok := cfg.PerTracker[t.Host]
		if !ok {
			continue
		}
		if p.MinSeedDays > out.MinSeedDays {
			out.MinSeedDays = p.MinSeedDays
		}
		if p.MinRatio > out.MinRatio {
			out.MinRatio = p.MinRatio
		}
		if p.RareThreshold != nil {
			if out.RareThreshold == nil || *p.RareThreshold < *out.RareThreshold {
				v := *p.RareThreshold
				out.RareThreshold = &v
			}
		}
	}
	return out
}

// effectiveRareThreshold falls back to the global default when no per-tracker
// override claims a stricter value.
func effectiveRareThreshold(policy config.TrackerPolicy, cfg config.ScoringConfig) int {
	if policy.RareThreshold != nil {
		return *policy.RareThreshold
	}
	return cfg.RareContentThreshold
}

// seedStart returns CompletionOn when set, falling back to AddedOn. Used by
// both the HnR window and the velocity factor.
func seedStart(t store.ScoringTorrent) time.Time {
	if t.CompletionOn != nil && !t.CompletionOn.IsZero() {
		return *t.CompletionOn
	}
	return t.AddedOn
}

// -----------------------------------------------------------------------------
// Factor implementations — pure functions over already-fetched inputs.
// -----------------------------------------------------------------------------

// factorRatioObligation: gated on private=true. SCORING.md §Factor 1.
func factorRatioObligation(t store.ScoringTorrent, ratio float64, policy config.TrackerPolicy, now time.Time, w float64) Factor {
	f := Factor{Name: FactorRatioObligation, Weight: w}
	if !t.Private {
		f.Gate = GatePublic
		return f
	}
	seedDays := now.Sub(seedStart(t)).Hours() / 24.0
	if ratio >= policy.MinRatio && seedDays >= float64(policy.MinSeedDays) {
		f.Value = 1.0
		f.Contribution = w
	}
	return f
}

// factorVelocityInv: lower velocity → higher delete-safety. SCORING.md §Factor 2.
//
// The window used is whatever snapshots_raw retention permits (default 7d,
// per storage.retention.snapshots_raw) — the store's VelocityBytesPerDay is
// computed over the available window. When the global baseline is 0 (cold
// library, no per-torrent samples ≥ 2) the factor is inert.
func factorVelocityInv(velocityBytesPerDay, globalAvg float64, w float64) Factor {
	f := Factor{Name: FactorVelocityInv, Weight: w}
	if globalAvg <= 0 {
		f.Gate = GateNoSwarmSignal
		return f
	}
	norm := velocityBytesPerDay / globalAvg
	v := 1.0 - norm
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	f.Value = v
	f.Contribution = v * w
	return f
}

// factorAge: tiebreaker, low weight. SCORING.md §Factor 3.
func factorAge(t store.ScoringTorrent, now time.Time, w float64) Factor {
	days := now.Sub(t.AddedOn).Hours() / 24.0
	if days < 0 {
		days = 0
	}
	return Factor{Name: FactorAge, Value: days, Weight: w, Contribution: days * w}
}

// factorSeedersGuard: rare-content veto. SCORING.md §Factor 4. Gated on
// any_tracker_alive — dead infrastructure is not evidence of rarity.
func factorSeedersGuard(seedersAvg float64, threshold int, alive bool, w float64) Factor {
	f := Factor{Name: FactorSeedersGuard, Weight: w}
	if !alive {
		f.Gate = GateAllDead
		return f
	}
	if seedersAvg <= float64(threshold) {
		f.Value = 1.0
		f.Contribution = w
	}
	return f
}

// factorSwarmBonus: log-scaled seeders bonus. SCORING.md §Factor 5.
func factorSwarmBonus(seedersAvg float64, w float64) Factor {
	if seedersAvg < 0 {
		seedersAvg = 0
	}
	v := math.Log10(seedersAvg + 1)
	return Factor{Name: FactorSwarmBonus, Value: v, Weight: w, Contribution: v * w}
}

// factorHnRVeto: hard veto, weight non-configurable. SCORING.md §Factor 6.
// Two gates: public → inert, all_trackers_dead → veto degrades.
func factorHnRVeto(t store.ScoringTorrent, alive bool, windowDays int, now time.Time) Factor {
	f := Factor{Name: FactorHnRVeto, Weight: HnRVetoWeight}
	if !t.Private {
		f.Gate = GatePublic
		return f
	}
	inWindow := now.Sub(seedStart(t)).Hours()/24.0 < float64(windowDays)
	if !inWindow {
		return f
	}
	if !alive {
		f.Gate = GateAllDead
		return f
	}
	f.Value = 1.0
	f.Contribution = HnRVetoWeight
	return f
}

// factorTrackerDead: bubbles up graveyard torrents. SCORING.md §Factor 7.
func factorTrackerDead(trackers []trackerView, now time.Time, grace time.Duration, w float64) Factor {
	f := Factor{Name: FactorTrackerDead, Weight: w}
	if allTrackersDeadSustained(trackers, now, grace) {
		f.Value = 1.0
		f.Contribution = w
	}
	return f
}

// -----------------------------------------------------------------------------
// Exclusions (Factor 8 in SCORING.md — not a factor proper).
// -----------------------------------------------------------------------------

// evaluateExclusions tags a torrent with the reasons it should not be acted on.
// Per the user's decision, the scorer still computes all factors for excluded
// torrents (UI visibility); the Decider (M4) filters them out.
func evaluateExclusions(t store.ScoringTorrent, linkedMedia []store.LinkedMedia, qb config.QbitConfig, arrs config.ArrsConfig) []string {
	var reasons []string

	if t.Category != "" && containsFold(qb.CategoryExclude, t.Category) {
		reasons = append(reasons, "qbit_category:"+t.Category)
	}
	torrentTags := splitTags(t.Tags)
	for _, tag := range torrentTags {
		if containsFold(qb.TagsExclude, tag) {
			reasons = append(reasons, "qbit_tag:"+tag)
		}
	}

	arrIndex := indexArrTagsExclude(arrs)
	for _, m := range linkedMedia {
		key := m.ArrType + "/" + m.ArrName
		excl := arrIndex[key]
		if len(excl) == 0 {
			continue
		}
		for _, mt := range splitTags(m.Tags) {
			if containsFold(excl, mt) {
				reasons = append(reasons, "arr_tag:"+key+":"+mt)
			}
		}
	}

	return reasons
}

func splitTags(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

func indexArrTagsExclude(arrs config.ArrsConfig) map[string][]string {
	out := map[string][]string{}
	add := func(typ string, list []config.ArrInstanceConfig) {
		for _, inst := range list {
			if len(inst.TagsExclude) == 0 {
				continue
			}
			out[typ+"/"+inst.Name] = inst.TagsExclude
		}
	}
	add(string(triagearr.ArrTypeSonarr), arrs.Sonarr)
	add(string(triagearr.ArrTypeRadarr), arrs.Radarr)
	add(string(triagearr.ArrTypeLidarr), arrs.Lidarr)
	add(string(triagearr.ArrTypeReadarr), arrs.Readarr)
	add(string(triagearr.ArrTypeWhisparrV2), arrs.WhisparrV2)
	add(string(triagearr.ArrTypeWhisparrV3), arrs.WhisparrV3)
	return out
}
