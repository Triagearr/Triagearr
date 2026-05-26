import { useVolume } from "@/api/hooks";
import { DiskGaugeEditor } from "@/components/PressureGauge";
import { Field, SectionShell, type SectionHelpers } from "./SettingsField";
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
  const mk = `volume.disk_pressure.max_run_size_gb`;
  const thresholdVal = Number(h.fieldValue(tk, v.disk_pressure.threshold_free_percent));
  const targetVal = Number(h.fieldValue(gk, v.disk_pressure.target_free_percent));

  return (
    <>
      {live ? (
        <DiskGaugeEditor
          thresholdFree={thresholdVal}
          targetFree={targetVal}
          onThreshold={(val) => h.setField(tk, String(val))}
          onTarget={(val) => h.setField(gk, String(val))}
          usedPct={100 - (live.free_percent ?? 0)}
          totalBytes={Number(live.total_bytes ?? 0)}
          usedBytes={Number(live.used_bytes ?? 0)}
        />
      ) : (
        <div className="text-xs text-muted-foreground">
          {usage.isLoading ? m.settings_disk_loading_usage() : m.settings_disk_no_sample()}
        </div>
      )}
      <Field
        label={m.settings_disk_threshold_label()}
        keyName={tk}
        type="number"
        description={m.settings_disk_threshold_desc()}
        value={h.fieldValue(tk, v.disk_pressure.threshold_free_percent)}
        onChange={(val) => h.setField(tk, val)}
        overridden={h.isOverridden(tk)}
        dirty={h.isDirty(tk)}
        onRevert={() => h.revert(tk)}
      />
      <Field
        label={m.settings_disk_target_label()}
        keyName={gk}
        type="number"
        description={m.settings_disk_target_desc()}
        value={h.fieldValue(gk, v.disk_pressure.target_free_percent)}
        onChange={(val) => h.setField(gk, val)}
        overridden={h.isOverridden(gk)}
        dirty={h.isDirty(gk)}
        onRevert={() => h.revert(gk)}
      />
      <Field
        label={m.settings_disk_max_run_label()}
        keyName={mk}
        type="number"
        description={m.settings_disk_max_run_desc()}
        value={h.fieldValue(mk, v.disk_pressure.max_run_size_gb)}
        onChange={(val) => h.setField(mk, val)}
        overridden={h.isOverridden(mk)}
        dirty={h.isDirty(mk)}
        onRevert={() => h.revert(mk)}
      />
    </>
  );
}
