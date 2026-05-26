import { Field, SectionShell, Subsection } from "./SettingsField";
import { TrackerPoliciesPanel } from "./TrackerPoliciesPanel";
import { ScoringSimulator } from "./ScoringSimulator";
import { m } from "@/paraglide/messages";

const WEIGHTS = [
  "ratio_obligation_met",
  "upload_velocity_inv",
  "age_days",
  "seeders_low_guard",
  "swarm_health_bonus",
  "tracker_dead_bonus",
] as const;

// WEIGHT_TIP maps each weight to its short hover explanation. Paraglide emits
// one function per message key, so a static map resolves one by runtime name.
const WEIGHT_TIP: Record<(typeof WEIGHTS)[number], () => string> = {
  ratio_obligation_met: m.settings_scoring_tip_ratio_obligation_met,
  upload_velocity_inv: m.settings_scoring_tip_upload_velocity_inv,
  age_days: m.settings_scoring_tip_age_days,
  seeders_low_guard: m.settings_scoring_tip_seeders_low_guard,
  swarm_health_bonus: m.settings_scoring_tip_swarm_health_bonus,
  tracker_dead_bonus: m.settings_scoring_tip_tracker_dead_bonus,
};

export function ScoringSection() {
  return (
    <div className="space-y-4">
      <SectionShell
        title={m.settings_scoring_title()}
        description={m.settings_scoring_description()}
        render={(h) => {
          const sc = h.settings.values.scoring;
          return (
            <div className="flex flex-wrap gap-6 items-start">
              <div className="shrink-0 space-y-2">
                <Field
                  label={m.settings_scoring_hnr_window_label()}
                  keyName="scoring.hnr_window_days"
                  type="number"
                  compact
                  tooltip={m.settings_scoring_tip_hnr_window_days()}
                  value={h.fieldValue("scoring.hnr_window_days", sc.hnr_window_days)}
                  onChange={(v) => h.setField("scoring.hnr_window_days", v)}
                  overridden={h.isOverridden("scoring.hnr_window_days")}
                  dirty={h.isDirty("scoring.hnr_window_days")}
                  onRevert={() => h.revert("scoring.hnr_window_days")}
                />
                <Subsection title={m.settings_scoring_weights()}>
                  {WEIGHTS.map((w) => {
                    const k = `scoring.weights.${w}`;
                    return (
                      <Field
                        key={k}
                        label={w}
                        keyName={k}
                        type="number"
                        compact
                        tooltip={WEIGHT_TIP[w]()}
                        value={h.fieldValue(k, sc.weights?.[w])}
                        onChange={(v) => h.setField(k, v)}
                        overridden={h.isOverridden(k)}
                        dirty={h.isDirty(k)}
                        onRevert={() => h.revert(k)}
                      />
                    );
                  })}
                </Subsection>
              </div>
              <div className="flex-1 min-w-[18rem] lg:sticky lg:top-4">
                <ScoringSimulator h={h} />
              </div>
            </div>
          );
        }}
      />
      <TrackerPoliciesPanel />
    </div>
  );
}
