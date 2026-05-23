import { useVolume } from "@/api/hooks";
import { PressureGauge } from "@/components/PressureGauge";
import { Field, SectionShell, Subsection, type SectionHelpers } from "./SettingsField";

export function DiskPressureSection() {
  return (
    <SectionShell
      title="Disk pressure"
      description="Thresholds that drive the auto-deletion trigger. Threshold is when a run fires; target is how much free space the run aims to restore."
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
    <Subsection title={v.name}>
      {live ? (
        <PressureGauge
          volume={live}
          threshold={thresholdVal}
          target={targetVal}
          onThresholdChange={(val) => h.setField(tk, String(val))}
          onTargetChange={(val) => h.setField(gk, String(val))}
        />
      ) : (
        <div className="text-xs text-muted-foreground">
          {usage.isLoading ? "Loading disk usage…" : "No disk sample yet."}
        </div>
      )}
      <Field
        label="Threshold free %"
        keyName={tk}
        type="number"
        description="Run fires when free space drops below this"
        value={h.fieldValue(tk, v.disk_pressure.threshold_free_percent)}
        onChange={(val) => h.setField(tk, val)}
        overridden={h.isOverridden(tk)}
        dirty={h.isDirty(tk)}
        onRevert={() => h.revert(tk)}
      />
      <Field
        label="Target free %"
        keyName={gk}
        type="number"
        description="Free space the run aims to restore"
        value={h.fieldValue(gk, v.disk_pressure.target_free_percent)}
        onChange={(val) => h.setField(gk, val)}
        overridden={h.isOverridden(gk)}
        dirty={h.isDirty(gk)}
        onRevert={() => h.revert(gk)}
      />
      <Field
        label="Max run size (GB)"
        keyName={mk}
        type="number"
        description="Cap on data deleted per run"
        value={h.fieldValue(mk, v.disk_pressure.max_run_size_gb)}
        onChange={(val) => h.setField(mk, val)}
        overridden={h.isOverridden(mk)}
        dirty={h.isDirty(mk)}
        onRevert={() => h.revert(mk)}
      />
    </Subsection>
  );
}
