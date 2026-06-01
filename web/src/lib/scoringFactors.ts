import { m } from "@/paraglide/messages";

// Maps the scorer's stable factor identifiers (emitted by the Go factors) to
// localized strings. Paraglide generates one function per message key, so a
// static map is the idiomatic way to resolve one from a runtime string.
// Shared by the score breakdown, the scoring-weights form, and the simulator.

export const FACTOR_LABEL: Record<string, () => string> = {
  ratio_obligation_met: m.settings_scoring_factor_ratio_obligation_met,
  upload_velocity_inv: m.settings_scoring_factor_upload_velocity_inv,
  age_days: m.settings_scoring_factor_age_days,
  seeders_low_guard: m.settings_scoring_factor_seeders_low_guard,
  swarm_health_bonus: m.settings_scoring_factor_swarm_health_bonus,
  hnr_window_veto: m.settings_scoring_factor_hnr_window_veto,
  tracker_dead_bonus: m.settings_scoring_factor_tracker_dead_bonus,
  candidate_boost: m.settings_scoring_factor_candidate_boost,
};

export const FACTOR_TIP: Record<string, () => string> = {
  ratio_obligation_met: m.settings_scoring_tip_ratio_obligation_met,
  upload_velocity_inv: m.settings_scoring_tip_upload_velocity_inv,
  age_days: m.settings_scoring_tip_age_days,
  seeders_low_guard: m.settings_scoring_tip_seeders_low_guard,
  swarm_health_bonus: m.settings_scoring_tip_swarm_health_bonus,
  hnr_window_veto: m.settings_scoring_tip_hnr_window_veto,
  tracker_dead_bonus: m.settings_scoring_tip_tracker_dead_bonus,
  candidate_boost: m.settings_scoring_tip_candidate_boost,
};
