import { Field, SectionShell } from "./SettingsField";
import { m } from "@/paraglide/messages";

const INTERVALS = [
  "torrent_client_interval",
  "arr_interval",
  "arr_file_min_interval",
  "tracker_interval",
  "disk_interval",
] as const;

// INTERVAL_TIP maps each poll interval to its short hover explanation.
const INTERVAL_TIP: Record<(typeof INTERVALS)[number], () => string> = {
  torrent_client_interval: m.settings_polling_tip_torrent_client_interval,
  arr_interval: m.settings_polling_tip_arr_interval,
  arr_file_min_interval: m.settings_polling_tip_arr_file_min_interval,
  tracker_interval: m.settings_polling_tip_tracker_interval,
  disk_interval: m.settings_polling_tip_disk_interval,
};

export function PollingSection() {
  return (
    <SectionShell
      title={m.settings_polling_title()}
      description={m.settings_polling_description()}
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
                  tooltip={INTERVAL_TIP[name]()}
                  placeholder={m.settings_polling_placeholder()}
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
