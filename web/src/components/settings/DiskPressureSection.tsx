import { Field, SectionShell, Subsection } from "./SettingsField";

export function DiskPressureSection() {
  return (
    <SectionShell
      title="Disk pressure"
      description="Per-volume thresholds that drive the auto-deletion trigger. Threshold is when a run fires; target is how much free space the run aims to restore."
      render={(h) => {
        const volumes = h.settings.values.volumes ?? [];
        if (volumes.length === 0) {
          return <div className="text-sm text-muted-foreground">No volumes configured.</div>;
        }
        return (
          <>
            {volumes.map((v, idx) => {
              const tk = `volumes.${idx}.disk_pressure.threshold_free_percent`;
              const gk = `volumes.${idx}.disk_pressure.target_free_percent`;
              const mk = `volumes.${idx}.disk_pressure.max_run_size_gb`;
              return (
                <Subsection key={v.name} title={v.name}>
                  <Field
                    label="Threshold free %"
                    keyName={tk}
                    type="number"
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
                    value={h.fieldValue(mk, v.disk_pressure.max_run_size_gb)}
                    onChange={(val) => h.setField(mk, val)}
                    overridden={h.isOverridden(mk)}
                    dirty={h.isDirty(mk)}
                    onRevert={() => h.revert(mk)}
                  />
                </Subsection>
              );
            })}
          </>
        );
      }}
    />
  );
}
