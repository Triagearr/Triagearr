import { useRef, useState } from "react";
import type { VolumeViewT } from "@/api/schemas";
import { humanBytes, pct } from "@/lib/format";

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
    badge = "below threshold · run armed";
    badgeClass = "badge badge-danger";
  } else if (target > 0 && free < target) {
    badge = "above target · idle";
    badgeClass = "badge badge-warn";
  } else {
    badge = "healthy";
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
          {pct(100 - free)} used
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
              threshold · {threshold}% free
            </div>
          </div>
        )}
        {target > 0 && (
          <div className="gauge-mark target" style={{ left: `${targetUsed}%` }}>
            <div className="gauge-mark-label below">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--green)", marginRight: 2 }} />
              target · {target}% free
            </div>
          </div>
        )}
      </div>

      {/* Stats grid */}
      <div className="gauge-grid">
        <div>
          <div className="gauge-lbl">Total</div>
          <div className="gauge-val">{humanBytes(total)}</div>
        </div>
        <div>
          <div className="gauge-lbl">Used</div>
          <div className="gauge-val">{humanBytes(used)}</div>
        </div>
        <div>
          <div className="gauge-lbl">Free</div>
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
            currently used
            {totalBytes && usedBytes
              ? ` · ${Math.round(usedBytes / 1e12 * 10) / 10} of ${Math.round(totalBytes / 1e12 * 10) / 10} TiB`
              : ""}
          </div>
        </div>
        <div style={{ marginLeft: "auto", display: "flex", gap: 20 }}>
          {[
            { label: "Threshold (free%)", value: thresholdFree, color: "var(--red-2)" },
            { label: "Target (free%)",    value: targetFree,    color: "var(--green-2)" },
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
            <div className="disk-handle-label above">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--red)", flex: "none" }} />
              trigger when free &lt; <strong style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{thresholdFree}%</strong>
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
            <div className="disk-handle-pip" />
            <div className="disk-handle-line" />
            <div className="disk-handle-label below">
              <span style={{ display: "inline-block", width: 7, height: 7, borderRadius: 2, background: "var(--green)", flex: "none" }} />
              stop when free ≥ <strong style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{targetFree}%</strong>
            </div>
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
              {100 - usedT}% free
            </span>
          ))}
        </div>
      </div>

      {/* Legend */}
      <div className="disk-editor-legend">
        <span><span className="dot green" /> Safe — free ≥ target</span>
        <span><span className="dot amber" /> Idle — above target, below threshold</span>
        <span><span className="dot red" />  Armed — below threshold</span>
      </div>
    </div>
  );
}
