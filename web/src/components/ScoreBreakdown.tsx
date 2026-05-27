import { m } from "@/paraglide/messages";
import { Tooltip } from "@/components/ui/Tooltip";

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

const FACTOR_TIP: Record<string, () => string> = {
  ratio_obligation_met: m.settings_scoring_tip_ratio_obligation_met,
  upload_velocity_inv:  m.settings_scoring_tip_upload_velocity_inv,
  age_days:             m.settings_scoring_tip_age_days,
  seeders_low_guard:    m.settings_scoring_tip_seeders_low_guard,
  swarm_health_bonus:   m.settings_scoring_tip_swarm_health_bonus,
  tracker_dead_bonus:   m.settings_scoring_tip_tracker_dead_bonus,
};

export function ScoreBreakdown({ factors, total }: { factors: unknown; total?: number }) {
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
          const nameEl = (
            <span
              className={`font-medium${tip ? " cursor-help underline decoration-dotted decoration-muted-foreground/60 underline-offset-2" : ""}`}
            >
              {key ?? m.comp_score_factor_n({ n: i + 1 })}
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
            </li>
          );
        })}
      </ul>
    </div>
  );
}
