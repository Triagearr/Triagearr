import type { VolumeViewT } from "@/api/schemas";
import { humanBytes, pct, relativeTime } from "@/lib/format";
import { Badge } from "@/components/ui/Badge";

export function PressureGauge({ volume }: { volume: VolumeViewT }) {
  const free = volume.free_percent ?? 0;
  const threshold = volume.threshold_free_percent ?? 0;
  const target = volume.target_free_percent ?? 0;

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
        <div className="text-xs text-muted-foreground flex gap-4">
          {threshold > 0 && <span>threshold {pct(threshold)}</span>}
          {target > 0 && <span>target {pct(target)}</span>}
        </div>
      )}

      {volume.measured_at && (
        <div className="text-xs text-muted-foreground">last sample {relativeTime(volume.measured_at)}</div>
      )}
    </div>
  );
}
