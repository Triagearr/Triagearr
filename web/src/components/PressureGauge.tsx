import { useRef, useState } from "react";
import type { VolumeViewT } from "@/api/schemas";
import { humanBytes, pct } from "@/lib/format";
import { m } from "@/paraglide/messages";

export function PressureGauge({
  volume,
  threshold: thresholdProp,
  target: targetProp,
}: {
  volume: VolumeViewT;
  threshold?: number;
  target?: number;
}) {
  const free    = volume.free_percent ?? 0;
  const threshold = thresholdProp ?? volume.threshold_free_percent ?? 0;
  const target    = targetProp    ?? volume.target_free_percent    ?? 0;
  const total = Number(volume.total_bytes ?? 0);
  const used  = Number(volume.used_bytes  ?? 0);
  const fillPct = total > 0 ? (used / total) * 100 : 0;

  let badge: string;
  let badgeClass: string;
  if (threshold > 0 && free <= threshold) {
    badge = m.comp_gauge_below_threshold();
    badgeClass = "badge badge-danger";
  } else if (target > 0 && free < target) {
    badge = m.comp_gauge_above_target();
    badgeClass = "badge badge-warn";
  } else {
    badge = m.comp_gauge_healthy();
    badgeClass = "badge badge-success";
  }

  // used % positions for the marks
  const thresholdUsed = 100 - threshold;
  const targetUsed    = 100 - target;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {/* Header row */}
      <div style={{ display: "flex", alignItems: "baseline", gap: 10 }}>
        <div style={{
          fontSize: 28, fontWeight: 600, letterSpacing: "-0.03em", lineHeight: 1,
          fontFamily: "'Geist Mono',ui-monospace,monospace",
          color: threshold > 0 && free <= threshold ? "var(--red-2)" : "var(--amber-2)",
        }}>
          {m.comp_gauge_pct_used({ pct: pct(100 - free) })}
        </div>
        <div style={{ marginLeft: "auto" }}>
          <span className={badgeClass}>{badge}</span>
        </div>
      </div>

      {/* Gauge bar */}
      <div style={{ position: "relative", marginTop: 18, marginBottom: 18 }}>
        <div className="gauge-bar-wrap">
          <div className="gauge-fill" style={{ width: `${Math.min(100, fillPct).toFixed(2)}%` }} />
        </div>
        {threshold > 0 && (
          <div className="gauge-mark threshold" style={{ left: `${thresholdUsed}%` }}>
            <div className="gauge-mark-label above">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--red)", marginRight: 2 }} />
              {m.comp_gauge_threshold_mark({ pct: threshold })}
            </div>
          </div>
        )}
        {target > 0 && (
          <div className="gauge-mark target" style={{ left: `${targetUsed}%` }}>
            <div className="gauge-mark-label below">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--green)", marginRight: 2 }} />
              {m.comp_gauge_target_mark({ pct: target })}
            </div>
          </div>
        )}
      </div>

      {/* Stats grid */}
      <div className="gauge-grid">
        <div>
          <div className="gauge-lbl">{m.comp_gauge_total()}</div>
          <div className="gauge-val">{humanBytes(total)}</div>
        </div>
        <div>
          <div className="gauge-lbl">{m.comp_gauge_used()}</div>
          <div className="gauge-val">{humanBytes(used)}</div>
        </div>
        <div>
          <div className="gauge-lbl">{m.comp_gauge_free()}</div>
          <div className="gauge-val" style={{ color: threshold > 0 && free <= threshold ? "var(--red-2)" : "var(--green-2)" }}>
            {humanBytes(Number(volume.free_bytes ?? 0))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Full interactive editor (Settings page) ────────────────────────────────
export function DiskGaugeEditor({
  thresholdFree,
  targetFree,
  onThreshold,
  onTarget,
  usedPct = 0,
  totalBytes,
  usedBytes,
}: {
  thresholdFree: number;
  targetFree: number;
  onThreshold: (v: number) => void;
  onTarget: (v: number) => void;
  usedPct?: number;
  totalBytes?: number;
  usedBytes?: number;
}) {
  const barRef = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState<"threshold" | "target" | null>(null);
  const thresholdUsed = 100 - thresholdFree;
  const targetUsed    = 100 - targetFree;

  const xToPctUsed = (clientX: number) => {
    const r = barRef.current?.getBoundingClientRect();
    if (!r) return 0;
    return Math.max(0, Math.min(100, ((clientX - r.left) / r.width) * 100));
  };

  function startDrag(which: "threshold" | "target") {
    setDragging(which);
    const onMove = (e: MouseEvent) => {
      const used = xToPctUsed(e.clientX);
      const free = Math.round(100 - used);
      if (which === "threshold") {
        onThreshold(Math.max(1, Math.min(targetFree - 1, free)));
      } else {
        onTarget(Math.max(thresholdFree + 1, Math.min(80, free)));
      }
    };
    const onUp = () => {
      setDragging(null);
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }

  const isCritical = usedPct >= thresholdUsed;

  return (
    <div className="disk-editor">
      {/* Header */}
      <div className="disk-editor-head">
        <div>
          <div className="disk-editor-current" style={{ color: isCritical ? "var(--red-2)" : "var(--amber-2)" }}>
            {usedPct.toFixed(1)}%
          </div>
          <div style={{ fontSize: 11, color: "var(--fg-3)", marginTop: 2 }}>
            {m.comp_gauge_currently_used()}
            {totalBytes && usedBytes
              ? m.comp_gauge_used_of_total({
                  used: Math.round(usedBytes / 1e12 * 10) / 10,
                  total: Math.round(totalBytes / 1e12 * 10) / 10,
                })
              : ""}
          </div>
        </div>
        <div style={{ marginLeft: "auto", display: "flex", gap: 20 }}>
          {[
            { label: m.comp_gauge_threshold_label(), value: thresholdFree, color: "var(--red-2)" },
            { label: m.comp_gauge_target_label(),    value: targetFree,    color: "var(--green-2)" },
          ].map(({ label, value, color }) => (
            <div key={label} style={{ textAlign: "right" }}>
              <div style={{ fontSize: 11, color: "var(--fg-3)" }}>{label}</div>
              <div style={{ fontSize: 22, fontWeight: 600, letterSpacing: "-0.02em", color, fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                {value}%
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Interactive bar */}
      <div className="disk-editor-bar-region">
        <div className="disk-editor-track" ref={barRef}>
          <div className="disk-editor-fill" style={{ width: `${usedPct}%` }} />
          {/* Idle zone: between target and threshold */}
          <div className="disk-editor-zone idle" style={{ left: `${targetUsed}%`, width: `${thresholdUsed - targetUsed}%` }} />
          {/* Armed zone: past threshold */}
          <div className="disk-editor-zone armed" style={{ left: `${thresholdUsed}%`, right: 0 }} />
        </div>

        {/* Handle layer — overlays track exactly via margin-top: -trackHeight */}
        <div className="disk-editor-handle-layer">
          {/* Threshold handle */}
          <button
            type="button"
            className={`disk-handle threshold${dragging === "threshold" ? " active" : ""}`}
            style={{ left: `${thresholdUsed}%` }}
            onMouseDown={() => startDrag("threshold")}
          >
            <div
              className="disk-handle-label above"
              style={Math.abs(thresholdUsed - targetUsed) < 18 ? { bottom: "calc(100% + 28px)" } : undefined}
            >
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--red)", flex: "none" }} />
              {m.comp_gauge_trigger_when()} <strong style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{thresholdFree}%</strong>
            </div>
            <div className="disk-handle-line" />
            <div className="disk-handle-pip" />
          </button>

          {/* Target handle */}
          <button
            type="button"
            className={`disk-handle target${dragging === "target" ? " active" : ""}`}
            style={{ left: `${targetUsed}%` }}
            onMouseDown={() => startDrag("target")}
          >
            <div className="disk-handle-label above">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--green)", flex: "none" }} />
              {m.comp_gauge_stop_when()} <strong style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{targetFree}%</strong>
            </div>
            <div className="disk-handle-line" />
            <div className="disk-handle-pip" />
          </button>
        </div>

        {/* Tick labels — expressed as % free (right = empty = most free) */}
        <div style={{ position: "relative", marginTop: 6, height: 14 }}>
          {[0, 25, 50, 75, 100].map((usedT) => (
            <span key={usedT} style={{
              position: "absolute",
              transform: usedT === 0 ? "none" : usedT === 100 ? "translateX(-100%)" : "translateX(-50%)",
              fontSize: 10.5, color: "var(--fg-4)",
              left: `${usedT}%`, fontFamily: "'Geist Mono',ui-monospace,monospace",
            }}>
              {m.comp_gauge_pct_free({ pct: 100 - usedT })}
            </span>
          ))}
        </div>
      </div>

    </div>
  );
}
