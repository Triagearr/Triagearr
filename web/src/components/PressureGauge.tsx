import { useRef, useState } from "react";
import type { VolumeViewT } from "@/api/schemas";
import { humanBytes, pct, relativeTime } from "@/lib/format";
import { Badge } from "@/components/ui/Badge";

export function PressureGauge({
  volume,
  threshold: thresholdProp,
  target: targetProp,
  onThresholdChange,
  onTargetChange,
}: {
  volume: VolumeViewT;
  threshold?: number;
  target?: number;
  onThresholdChange?: (value: number) => void;
  onTargetChange?: (value: number) => void;
}) {
  const free = volume.free_percent ?? 0;
  const threshold = thresholdProp ?? volume.threshold_free_percent ?? 0;
  const target = targetProp ?? volume.target_free_percent ?? 0;
  const barRef = useRef<HTMLDivElement>(null);
  const interactive = !!(onThresholdChange || onTargetChange);

  let tone: "destructive" | "warning" | "success" = "success";
  if (threshold > 0 && free <= threshold) tone = "destructive";
  else if (target > 0 && free < target) tone = "warning";

  const total = Number(volume.total_bytes ?? 0);
  const used = Number(volume.used_bytes ?? 0);
  const fillPct = total > 0 ? (used / total) * 100 : 0;

  return (
    <div className="rounded-lg border bg-card text-card-foreground p-4 flex flex-col gap-3">
      <div className="flex items-start justify-between">
        <div>
          <div className="font-medium">{volume.name}</div>
          <div className="text-xs text-muted-foreground font-mono">{volume.path}</div>
        </div>
        <Badge variant={tone}>{pct(free)} free</Badge>
      </div>

      <div className={interactive ? "pt-7 pb-1" : ""}>
        <div className="relative" ref={barRef}>
          <div className="h-2.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className={
                "h-full transition-all " +
                (tone === "destructive"
                  ? "bg-destructive"
                  : tone === "warning"
                  ? "bg-amber-500"
                  : "bg-emerald-500")
              }
              style={{ width: `${Math.min(100, fillPct).toFixed(2)}%` }}
            />
          </div>

          {threshold > 0 && (
            <DragHandle
              positionPct={100 - threshold}
              value={threshold}
              color="red"
              barRef={barRef}
              interactive={!!onThresholdChange}
              onChange={onThresholdChange}
            />
          )}
          {target > 0 && (
            <DragHandle
              positionPct={100 - target}
              value={target}
              color="amber"
              barRef={barRef}
              interactive={!!onTargetChange}
              onChange={onTargetChange}
            />
          )}
        </div>
      </div>

      <div className="grid grid-cols-3 gap-2 text-xs text-muted-foreground">
        <div>
          <div className="font-mono text-foreground">{humanBytes(used)}</div>
          <div>used</div>
        </div>
        <div>
          <div className="font-mono text-foreground">{humanBytes(Number(volume.free_bytes ?? 0))}</div>
          <div>free</div>
        </div>
        <div>
          <div className="font-mono text-foreground">{humanBytes(total)}</div>
          <div>total</div>
        </div>
      </div>

      {(threshold > 0 || target > 0) && (
        <div className="flex gap-4 text-xs text-muted-foreground">
          {threshold > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="inline-block h-2 w-2 rounded-sm bg-red-400/90" />
              trigger below {pct(threshold)} free
            </span>
          )}
          {target > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="inline-block h-2 w-2 rounded-sm bg-amber-400/90" />
              restore to {pct(target)} free
            </span>
          )}
        </div>
      )}

      {volume.measured_at && (
        <div className="text-xs text-muted-foreground">last sample {relativeTime(volume.measured_at)}</div>
      )}
    </div>
  );
}

function DragHandle({
  positionPct,
  value,
  color,
  barRef,
  interactive,
  onChange,
}: {
  positionPct: number;
  value: number;
  color: "red" | "amber";
  barRef: React.RefObject<HTMLDivElement | null>;
  interactive: boolean;
  onChange?: (freePct: number) => void;
}) {
  const [active, setActive] = useState(false);
  const [hovered, setHovered] = useState(false);

  const startDrag = (e: React.MouseEvent) => {
    if (!onChange) return;
    e.preventDefault();
    setActive(true);

    const bar = barRef.current;
    if (!bar) return;

    const move = (ev: MouseEvent) => {
      const { left, width } = bar.getBoundingClientRect();
      const posPct = Math.min(100, Math.max(0, ((ev.clientX - left) / width) * 100));
      onChange(Math.min(99, Math.max(1, Math.round(100 - posPct))));
    };

    const up = () => {
      setActive(false);
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", up);
    };

    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
  };

  const lineColor = color === "red" ? "bg-red-400/90" : "bg-amber-400/90";
  const gripColor = color === "red" ? "bg-red-400" : "bg-amber-400";
  const labelStyle =
    color === "red"
      ? "bg-red-950/90 text-red-300 border-red-500/40"
      : "bg-amber-950/90 text-amber-300 border-amber-500/40";

  return (
    <div
      className="absolute top-0 bottom-0"
      style={{
        left: `${positionPct}%`,
        transform: "translateX(-50%)",
        width: interactive ? "20px" : "2px",
        cursor: interactive ? "col-resize" : "default",
      }}
      onMouseDown={startDrag}
      onMouseEnter={() => interactive && setHovered(true)}
      onMouseLeave={() => !active && setHovered(false)}
    >
      {/* vertical line */}
      <div className={`absolute inset-y-0 left-1/2 -translate-x-1/2 w-px ${lineColor}`} />

      {/* grip circle */}
      {interactive && (
        <div
          className={`absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 h-3.5 w-3.5 rounded-full ring-2 ring-background shadow-md ${gripColor} transition-transform ${active ? "scale-125" : "scale-100"}`}
        />
      )}

      {/* floating label above bar */}
      {interactive && (active || hovered) && (
        <div
          className={`absolute bottom-full mb-2 left-1/2 -translate-x-1/2 text-[11px] font-medium px-2 py-0.5 rounded border whitespace-nowrap pointer-events-none ${labelStyle}`}
        >
          {value}%
        </div>
      )}
    </div>
  );
}
