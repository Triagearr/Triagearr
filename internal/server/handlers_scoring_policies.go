package server

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// scoringDefaultsDTO is the body of GET/PUT /api/v1/scoring/defaults. The
// fields mirror triagearr.ScoringDefaults with explicit json tags so the
// wire format does not depend on Go field-name casing.
type scoringDefaultsDTO struct {
	MinRatio      float64 `json:"min_ratio"`
	MinSeedDays   int     `json:"min_seed_days"`
	RareThreshold int     `json:"rare_threshold"`
}

// trackerPolicyDTO is one row in the per-tracker policy list. RareThreshold
// is a pointer to preserve the "inherit default" semantics — null means the
// tracker uses the global default for rare_threshold.
type trackerPolicyDTO struct {
	TrackerHost   string  `json:"tracker_host"`
	MinRatio      float64 `json:"min_ratio"`
	MinSeedDays   int     `json:"min_seed_days"`
	RareThreshold *int    `json:"rare_threshold"`
	Enabled       bool    `json:"enabled"`
}

// trackerHostStatDTO enriches one entry of the policy list with operational
// signals from torrent_trackers (count of torrents using this host, whether
// any tracker is currently alive). Used by the UI to badge dead trackers and
// surface "configure this one next".
type trackerHostStatDTO struct {
	TrackerHost  string            `json:"tracker_host"`
	TorrentCount int               `json:"torrent_count"`
	AnyAlive     bool              `json:"any_alive"`
	AllDead      bool              `json:"all_dead"`
	Policy       *trackerPolicyDTO `json:"policy,omitempty"`
}

func defaultsToDTO(d triagearr.ScoringDefaults) scoringDefaultsDTO {
	return scoringDefaultsDTO{
		MinRatio:      d.MinRatio,
		MinSeedDays:   d.MinSeedDays,
		RareThreshold: d.RareThreshold,
	}
}

func policyToDTO(p triagearr.TrackerPolicy) trackerPolicyDTO {
	return trackerPolicyDTO{
		TrackerHost:   p.TrackerHost,
		MinRatio:      p.MinRatio,
		MinSeedDays:   p.MinSeedDays,
		RareThreshold: p.RareThreshold,
		Enabled:       p.Enabled,
	}
}

func statToDTO(s store.TrackerHostStat) trackerHostStatDTO {
	out := trackerHostStatDTO{
		TrackerHost:  s.Host,
		TorrentCount: s.TorrentCount,
		AnyAlive:     s.AnyAlive,
		AllDead:      s.AllDead,
	}
	if s.Policy != nil {
		p := policyToDTO(*s.Policy)
		out.Policy = &p
	}
	return out
}

func (s *Server) handleGetScoringDefaults(w http.ResponseWriter, r *http.Request) {
	d, err := s.opts.Store.GetScoringDefaults(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading scoring defaults: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, defaultsToDTO(d))
}

func (s *Server) handlePutScoringDefaults(w http.ResponseWriter, r *http.Request) {
	var body scoringDefaultsDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if err := validateDefaults(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.opts.Store.SetScoringDefaults(r.Context(), triagearr.ScoringDefaults{
		MinRatio:      body.MinRatio,
		MinSeedDays:   body.MinSeedDays,
		RareThreshold: body.RareThreshold,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "updating scoring defaults: "+err.Error())
		return
	}
	// No daemon reload — the next ScoreAll pass picks the new values up from
	// the DB on its own, so verdicts refresh on the following scorer tick.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTrackerPolicies(w http.ResponseWriter, r *http.Request) {
	stats, err := s.opts.Store.ListTrackerHostStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing tracker_policies: "+err.Error())
		return
	}
	out := make([]trackerHostStatDTO, 0, len(stats))
	for _, st := range stats {
		out = append(out, statToDTO(st))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutTrackerPolicy(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	if host == "" {
		writeError(w, http.StatusBadRequest, "host required")
		return
	}
	var body trackerPolicyDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if err := validatePolicy(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.opts.Store.UpsertTrackerPolicy(r.Context(), triagearr.TrackerPolicy{
		TrackerHost:   host,
		MinRatio:      body.MinRatio,
		MinSeedDays:   body.MinSeedDays,
		RareThreshold: body.RareThreshold,
		Enabled:       body.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upserting tracker_policy: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policyToDTO(saved))
}

func (s *Server) handleDeleteTrackerPolicy(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	if host == "" {
		writeError(w, http.StatusBadRequest, "host required")
		return
	}
	if err := s.opts.Store.DeleteTrackerPolicy(r.Context(), host); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no policy configured for this tracker")
			return
		}
		writeError(w, http.StatusInternalServerError, "deleting tracker_policy: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// simWeightsDTO mirrors config.ScoringWeights on the wire.
type simWeightsDTO struct {
	RatioObligationMet float64 `json:"ratio_obligation_met"`
	UploadVelocityInv  float64 `json:"upload_velocity_inv"`
	AgeDays            float64 `json:"age_days"`
	SeedersLowGuard    float64 `json:"seeders_low_guard"`
	SwarmHealthBonus   float64 `json:"swarm_health_bonus"`
	TrackerDeadBonus   float64 `json:"tracker_dead_bonus"`
}

// simulateRequestDTO is the body of POST /api/v1/scoring/simulate. Every group
// is optional: an absent group falls back to the daemon's current effective
// config, so the UI can send only the fields the operator is editing. The
// tracker-dead grace is not editable on the scoring page, so it is always taken
// from the live config.
type simulateRequestDTO struct {
	Weights       *simWeightsDTO      `json:"weights"`
	HnRWindowDays *int                `json:"hnr_window_days"`
	Defaults      *scoringDefaultsDTO `json:"defaults"`
}

// handleSimulateScoring scores the built-in archetypes against the proposed
// config and returns one breakdown per archetype. It is read-only and never
// touches the scores table — it exists purely so the settings UI can show the
// impact of a weight/threshold change before it is saved.
func (s *Server) handleSimulateScoring(w http.ResponseWriter, r *http.Request) {
	var body simulateRequestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}

	var cur config.ScoringConfig
	if s.opts.Config != nil {
		cur = s.opts.Config.Scoring
	}

	curDefaults, err := s.opts.Store.GetScoringDefaults(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading scoring defaults: "+err.Error())
		return
	}

	in := scorer.SimInput{
		Weights:          cur.Weights,
		HnRWindowDays:    cur.HnRWindowDays,
		TrackerDeadGrace: cur.TrackerDeadGrace,
		Defaults:         curDefaults,
	}
	if body.Weights != nil {
		in.Weights = config.ScoringWeights{
			RatioObligationMet: body.Weights.RatioObligationMet,
			UploadVelocityInv:  body.Weights.UploadVelocityInv,
			AgeDays:            body.Weights.AgeDays,
			SeedersLowGuard:    body.Weights.SeedersLowGuard,
			SwarmHealthBonus:   body.Weights.SwarmHealthBonus,
			TrackerDeadBonus:   body.Weights.TrackerDeadBonus,
		}
	}
	if body.HnRWindowDays != nil {
		in.HnRWindowDays = *body.HnRWindowDays
	}
	if body.Defaults != nil {
		in.Defaults = triagearr.ScoringDefaults{
			MinRatio:      body.Defaults.MinRatio,
			MinSeedDays:   body.Defaults.MinSeedDays,
			RareThreshold: body.Defaults.RareThreshold,
		}
	}

	writeJSON(w, http.StatusOK, scorer.Simulate(in))
}

// validateDefaults guards against obviously broken values. The UI rejects
// these client-side too but the API is the source of truth.
func validateDefaults(d scoringDefaultsDTO) error {
	if d.MinRatio < 0 {
		return errBadField("min_ratio must be >= 0")
	}
	if d.MinSeedDays < 0 {
		return errBadField("min_seed_days must be >= 0")
	}
	if d.RareThreshold < 0 {
		return errBadField("rare_threshold must be >= 0")
	}
	return nil
}

func validatePolicy(p trackerPolicyDTO) error {
	if p.MinRatio < 0 {
		return errBadField("min_ratio must be >= 0")
	}
	if p.MinSeedDays < 0 {
		return errBadField("min_seed_days must be >= 0")
	}
	if p.RareThreshold != nil && *p.RareThreshold < 0 {
		return errBadField("rare_threshold must be >= 0 when set")
	}
	return nil
}

type badFieldError string

func (e badFieldError) Error() string { return string(e) }

func errBadField(msg string) error { return badFieldError(msg) }
