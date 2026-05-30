import { m } from "@/paraglide/messages";
import { Tooltip } from "@/components/ui/Tooltip";
import { FACTOR_LABEL, FACTOR_TIP } from "@/lib/scoringFactors";
import { relativeTime } from "@/lib/format";

type Factor = {
  name?: string;
  factor?: string;
  weight?: number;
  raw?: number;
  contribution?: number;
  note?: string;
  reason?: string;
  [k: string]: unknown;
};

export function ScoreBreakdown({
  factors,
  total,
  trackerDeadEligibleAt,
}: {
  factors: unknown;
  total?: number;
  // When set, every tracker is dead but the grace window has not elapsed yet:
  // shown as a countdown on the tracker_dead_bonus row so a 0.00 contribution
  // reads as "pending" rather than "broken".
  trackerDeadEligibleAt?: string | null;
}) {
  let rows: Factor[] = [];
  if (Array.isArray(factors)) rows = factors as Factor[];
  else if (factors && typeof factors === "object") rows = Object.entries(factors).map(([k, v]) => ({ name: k, ...(v as object) }));

  if (rows.length === 0) {
    return <div className="text-sm text-muted-foreground">{m.comp_score_no_breakdown()}</div>;
  }
  return (
    <div className="flex flex-col gap-2">
      {typeof total === "number" && (
        <div className="text-sm">
          <span className="text-muted-foreground">{m.comp_score_total()}</span>{" "}
          <span className="font-mono">{total.toFixed(2)}</span>
        </div>
      )}
      <ul className="divide-y divide-border rounded-md border border-border bg-muted/30">
        {rows.map((r, i) => {
          const key = r.name ?? r.factor;
          const tip = key ? FACTOR_TIP[key] : undefined;
          const label = key ? FACTOR_LABEL[key]?.() ?? key : m.comp_score_factor_n({ n: i + 1 });
          const nameEl = (
            <span
              className={`font-medium${tip ? " cursor-help underline decoration-dotted decoration-muted-foreground/60 underline-offset-2" : ""}`}
            >
              {label}
            </span>
          );
          return (
            <li key={key ?? `idx-${i}`} className="px-3 py-2 text-sm flex flex-col gap-0.5">
              <div className="flex items-baseline justify-between">
                {tip ? (
                  <Tooltip content={<span style={{ whiteSpace: "normal", display: "block", lineHeight: 1.35 }}>{tip()}</span>}>
                    {nameEl}
                  </Tooltip>
                ) : nameEl}
                <span className="font-mono text-xs text-muted-foreground">
                  {r.contribution !== undefined
                    ? r.contribution.toFixed(2)
                    : r.weight !== undefined
                    ? r.weight.toFixed(2)
                    : "—"}
                </span>
              </div>
              {(r.note || r.reason) && (
                <span className="text-xs text-muted-foreground">{r.note ?? r.reason}</span>
              )}
              {key === "tracker_dead_bonus" && trackerDeadEligibleAt && (
                <span className="text-xs text-amber-600 dark:text-amber-400">
                  {m.comp_score_tracker_dead_pending({ when: relativeTime(trackerDeadEligibleAt) })}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
