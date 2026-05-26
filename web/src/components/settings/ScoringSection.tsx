import { Field, SectionShell, Subsection } from "./SettingsField";
import { TrackerPoliciesPanel } from "./TrackerPoliciesPanel";

const WEIGHTS = [
  "ratio_obligation_met",
  "upload_velocity_inv",
  "age_days",
  "seeders_low_guard",
  "swarm_health_bonus",
] as const;

export function ScoringSection() {
  return (
    <div className="space-y-4">
      <SectionShell
        title="Scoring"
        description="Tunes the DeleteScore algorithm. The HnR-window veto (-10000) is hard-coded and not editable here."
        render={(h) => {
          const sc = h.settings.values.scoring;
          return (
            <>
              <Field
                label="HnR window (days)"
                keyName="scoring.hnr_window_days"
                type="number"
                value={h.fieldValue("scoring.hnr_window_days", sc.hnr_window_days)}
                onChange={(v) => h.setField("scoring.hnr_window_days", v)}
                overridden={h.isOverridden("scoring.hnr_window_days")}
                dirty={h.isDirty("scoring.hnr_window_days")}
                onRevert={() => h.revert("scoring.hnr_window_days")}
              />
              <Subsection title="Weights">
                {WEIGHTS.map((w) => {
                  const k = `scoring.weights.${w}`;
                  return (
                    <Field
                      key={k}
                      label={w}
                      keyName={k}
                      type="number"
                      value={h.fieldValue(k, sc.weights?.[w])}
                      onChange={(v) => h.setField(k, v)}
                      overridden={h.isOverridden(k)}
                      dirty={h.isDirty(k)}
                      onRevert={() => h.revert(k)}
                    />
                  );
                })}
              </Subsection>
            </>
          );
        }}
      />
      <TrackerPoliciesPanel />
    </div>
  );
}
