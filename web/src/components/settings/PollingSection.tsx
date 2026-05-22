import { Field, SectionShell } from "./SettingsField";

const INTERVALS = [
  "qbit_interval",
  "arr_interval",
  "arr_file_min_interval",
  "tracker_interval",
  "disk_interval",
] as const;

export function PollingSection() {
  return (
    <SectionShell
      title="Polling intervals"
      description="How often each poller runs. Accepts Go durations like 30s, 5m, 1h."
      render={(h) => {
        const p = h.settings.values.polling;
        return (
          <>
            {INTERVALS.map((name) => {
              const k = `polling.${name}`;
              return (
                <Field
                  key={k}
                  label={name}
                  keyName={k}
                  type="text"
                  placeholder="e.g. 30s, 5m, 1h"
                  value={h.fieldValue(k, p[name])}
                  onChange={(v) => h.setField(k, v)}
                  overridden={h.isOverridden(k)}
                  dirty={h.isDirty(k)}
                  onRevert={() => h.revert(k)}
                />
              );
            })}
          </>
        );
      }}
    />
  );
}
