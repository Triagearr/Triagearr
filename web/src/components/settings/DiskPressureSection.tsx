import { useVolume } from "@/api/hooks";
import { DiskGaugeEditor } from "@/components/PressureGauge";
import { SectionShell, type SectionHelpers } from "./SettingsField";
import { m } from "@/paraglide/messages";

export function DiskPressureSection() {
  return (
    <SectionShell
      title={m.settings_disk_title()}
      description={m.settings_disk_description()}
      render={(h) => <DiskPressureFields h={h} />}
    />
  );
}

function DiskPressureFields({ h }: { h: SectionHelpers }) {
  const v = h.settings.values.volume;
  const usage = useVolume();
  const live = usage.data?.volume;

  const tk = `volume.disk_pressure.threshold_free_percent`;
  const gk = `volume.disk_pressure.target_free_percent`;
  const thresholdVal = Number(h.fieldValue(tk, v.disk_pressure.threshold_free_percent));
  const targetVal = Number(h.fieldValue(gk, v.disk_pressure.target_free_percent));

  if (!live) {
    return (
      <div className="text-xs text-muted-foreground">
        {usage.isLoading ? m.settings_disk_loading_usage() : m.settings_disk_no_sample()}
      </div>
    );
  }

  return (
    <DiskGaugeEditor
      thresholdFree={thresholdVal}
      targetFree={targetVal}
      onThreshold={(val) => h.setField(tk, String(val))}
      onTarget={(val) => h.setField(gk, String(val))}
      usedPct={100 - (live.free_percent ?? 0)}
      totalBytes={Number(live.total_bytes ?? 0)}
      usedBytes={Number(live.used_bytes ?? 0)}
    />
  );
}
