import { useEffect, useMemo, useState } from "react";
import { useScoringDefaults, useScoringSimulation, type ScoringSimInput } from "@/api/hooks";
import type { ScoringSimResultT, SimFactorT } from "@/api/schemas";
import type { SectionHelpers } from "./SettingsField";
import { Badge } from "@/components/ui/Badge";
import { m } from "@/paraglide/messages";
import { FACTOR_LABEL } from "@/lib/scoringFactors";

// ARCH_LABEL / ARCH_DESC map the backend's stable identifiers to localized
// strings. Paraglide generates one function per message key, so a static map is
// the idiomatic way to resolve one from a runtime string.
const ARCH_LABEL: Record<string, () => string> = {
  public_well_seeded: m.settings_scoring_arch_public_well_seeded,
  private_obligation_met: m.settings_scoring_arch_private_obligation_met,
  private_in_hnr_window: m.settings_scoring_arch_private_in_hnr_window,
  private_obligation_unmet: m.settings_scoring_arch_private_obligation_unmet,
  rare_content: m.settings_scoring_arch_rare_content,
  dead_tracker_library: m.settings_scoring_arch_dead_tracker_library,
};
const ARCH_DESC: Record<string, () => string> = {
  public_well_seeded: m.settings_scoring_archdesc_public_well_seeded,
  private_obligation_met: m.settings_scoring_archdesc_private_obligation_met,
  private_in_hnr_window: m.settings_scoring_archdesc_private_in_hnr_window,
  private_obligation_unmet: m.settings_scoring_archdesc_private_obligation_unmet,
  rare_content: m.settings_scoring_archdesc_rare_content,
  dead_tracker_library: m.settings_scoring_archdesc_dead_tracker_library,
};
// GATE_LABEL maps the scorer's raw gate strings (stable identifiers emitted by
// the Go factors) to localized labels.
const GATE_LABEL: Record<string, () => string> = {
  "public — inert": m.settings_scoring_gate_public,
  all_trackers_dead: m.settings_scoring_gate_all_trackers_dead,
  no_swarm_signal: m.settings_scoring_gate_no_swarm_signal,
};

// barCap is the contribution magnitude that fills a factor bar. Picked around a
// typical weight so ordinary factors span the bar while the hard vetoes
// (-1000 / -10000) simply max it out instead of squashing everything else.
const barCap = 50;

function useDebounced<T extends string>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

function fmt(n: number): string {
  if (Math.abs(n) >= 1000) return n.toFixed(0);
  if (Number.isInteger(n)) return String(n);
  return n.toFixed(2);
}

function FactorRow({ f }: { f: SimFactorT }) {
  const gated = !!f.gate;
  const c = f.contribution;
  const width = Math.min(100, (Math.abs(c) / barCap) * 100);
  const tone = gated
    ? "bg-muted-foreground/40"
    : c > 0
      ? "bg-emerald-500"
      : c < 0
        ? "bg-rose-500"
        : "bg-muted-foreground/40";
  const label = FACTOR_LABEL[f.name]?.() ?? f.name;
  const gateLabel = f.gate ? (GATE_LABEL[f.gate]?.() ?? f.gate) : "";
  return (
    <div className="grid grid-cols-[minmax(0,8rem)_1fr_3.5rem] items-center gap-2 text-xs leading-none">
      <span
        className={gated ? "text-muted-foreground line-through truncate" : "text-foreground truncate"}
        title={label}
      >
        {label}
      </span>
      {gated ? (
        <span
          className="col-span-2 text-[10px] italic text-muted-foreground whitespace-nowrap overflow-hidden text-ellipsis"
          title={gateLabel}
        >
          {gateLabel}
        </span>
      ) : (
        <>
          <div className="h-2 rounded-full bg-muted overflow-hidden">
            <div className={`h-full rounded-full ${tone}`} style={{ width: `${width}%` }} />
          </div>
          <span
            className={`justify-self-end font-mono font-medium ${c > 0 ? "text-emerald-700 dark:text-emerald-300" : c < 0 ? "text-rose-700 dark:text-rose-400" : "text-muted-foreground"}`}
          >
            {c > 0 ? "+" : ""}
            {fmt(c)}
          </span>
        </>
      )}
    </div>
  );
}

function ResultRow({ r }: { r: ScoringSimResultT }) {
  const [open, setOpen] = useState(false);
  const score = r.breakdown.score;
  // A strongly negative score is a veto/guard hit — surface it as protected.
  const protectedHit = score <= -100;
  const title = ARCH_LABEL[r.name]?.() ?? r.name;
  const desc = ARCH_DESC[r.name]?.() ?? "";
  return (
    <div className="rounded-md border border-border bg-card">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-2.5 px-3 py-2 text-left hover:bg-muted/50 rounded-md"
      >
        <span className="text-muted-foreground text-xs w-3 shrink-0">{open ? "▾" : "▸"}</span>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-medium">{title}</div>
          <div className="text-[11px] leading-tight text-muted-foreground truncate">{desc}</div>
        </div>
        <Badge
          variant="outline"
          className={
            protectedHit
              ? "border-transparent bg-rose-600 text-white dark:bg-rose-600"
              : "border-transparent bg-emerald-600 text-white dark:bg-emerald-600"
          }
        >
          {protectedHit ? m.settings_scoring_sim_protected() : m.settings_scoring_sim_reapable()}
        </Badge>
        <span
          className={`font-mono text-sm w-16 text-right shrink-0 ${protectedHit ? "text-rose-700 dark:text-rose-400" : "text-emerald-700 dark:text-emerald-300"}`}
        >
          {fmt(score)}
        </span>
      </button>
      {open && (
        <div className="px-3 pb-3 pt-1.5 space-y-2 border-t border-border">
          {r.breakdown.factors.map((f) => (
            <FactorRow key={f.name} f={f} />
          ))}
        </div>
      )}
    </div>
  );
}

// ScoringSimulator scores a fixed set of example torrents against the config
// the operator is currently editing (pending overrides + saved values) and
// renders them ranked by score, each expandable into a factor-by-factor
// waterfall. It re-runs live (debounced) as fields change so the impact of a
// weight or threshold is visible before saving. Intended to sit beside the
// scoring fields so the knobs and their effect are on screen together.
export function ScoringSimulator({ h }: { h: SectionHelpers }) {
  const defaults = useScoringDefaults();
  const sc = h.settings.values.scoring;

  const num = (key: string, fallback: number | undefined): number => {
    const raw = h.fieldValue(key, fallback);
    const n = Number(raw);
    return Number.isFinite(n) ? n : 0;
  };

  const input: ScoringSimInput = {
    weights: {
      ratio_obligation_met: num("scoring.weights.ratio_obligation_met", sc.weights?.ratio_obligation_met),
      upload_velocity_inv: num("scoring.weights.upload_velocity_inv", sc.weights?.upload_velocity_inv),
      age_days: num("scoring.weights.age_days", sc.weights?.age_days),
      seeders_low_guard: num("scoring.weights.seeders_low_guard", sc.weights?.seeders_low_guard),
      swarm_health_bonus: num("scoring.weights.swarm_health_bonus", sc.weights?.swarm_health_bonus),
      tracker_dead_bonus: num("scoring.weights.tracker_dead_bonus", sc.weights?.tracker_dead_bonus),
    },
    hnr_window_days: num("scoring.hnr_window_days", sc.hnr_window_days),
    defaults: {
      min_ratio: defaults.data?.min_ratio ?? 1,
      min_seed_days: defaults.data?.min_seed_days ?? 30,
      rare_threshold: defaults.data?.rare_threshold ?? 3,
    },
  };

  const debouncedKey = useDebounced(JSON.stringify(input), 200);
  const debouncedInput = useMemo(() => JSON.parse(debouncedKey) as ScoringSimInput, [debouncedKey]);
  const sim = useScoringSimulation(debouncedInput);

  const ranked = useMemo(
    () => (sim.data ? [...sim.data].sort((a, b) => b.breakdown.score - a.breakdown.score) : []),
    [sim.data],
  );

  return (
    <div className="rounded-lg border border-border bg-muted/30 p-3 space-y-2.5">
      <div>
        <div className="text-sm font-medium">{m.settings_scoring_sim_title()}</div>
        <p className="text-[11px] leading-snug text-muted-foreground mt-0.5">
          {m.settings_scoring_sim_description()}
        </p>
      </div>
      {sim.isError ? (
        <div className="text-xs text-destructive">{String(sim.error)}</div>
      ) : ranked.length === 0 ? (
        <div className="text-xs text-muted-foreground">{m.settings_scoring_sim_loading()}</div>
      ) : (
        <div className="space-y-1.5">
          {ranked.map((r) => (
            <ResultRow key={r.name} r={r} />
          ))}
        </div>
      )}
    </div>
  );
}
