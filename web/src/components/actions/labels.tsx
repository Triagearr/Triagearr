import type { ActionStatusT, AuditOutcomeT } from "@/api/schemas";
import { Tooltip } from "@/components/ui/Tooltip";
import { m } from "@/paraglide/messages";

// ── Tone maps ─────────────────────────────────────────────────────────────────
export const statusClass: Record<ActionStatusT, string> = {
  succeeded:           "badge-success",
  pending:             "",
  running:             "badge-info",
  failed_qbit:         "badge-danger",
  aborted_arr_fail:    "badge-danger",
  aborted_nlink_check: "badge-danger",
  skipped_cross_seed:  "",
};
export const outcomeTone: Record<AuditOutcomeT, string> = {
  ok: "badge-success", failed: "badge-danger", skipped: "", not_attempted: "",
};

// ── Enum → human label/explanation maps ─────────────────────────────────────
// Backend enums (internal/triagearr/types.go) are rendered through these so the
// UI shows a readable label instead of e.g. "aborted_arr_fail". Lookups fall
// back to the raw value so a newly added backend variant still displays.
const actionStatusLabel: Record<ActionStatusT, () => string> = {
  succeeded:           m.as_label_succeeded,
  pending:             m.as_label_pending,
  running:             m.as_label_running,
  failed_qbit:         m.as_label_failed_qbit,
  aborted_arr_fail:    m.as_label_aborted_arr_fail,
  aborted_nlink_check: m.as_label_aborted_nlink_check,
  skipped_cross_seed:  m.as_label_skipped_cross_seed,
};
const actionStatusDesc: Partial<Record<ActionStatusT, () => string>> = {
  failed_qbit:         m.as_desc_failed_qbit,
  aborted_arr_fail:    m.as_desc_aborted_arr_fail,
  aborted_nlink_check: m.as_desc_aborted_nlink_check,
  skipped_cross_seed:  m.as_desc_skipped_cross_seed,
};
const outcomeLabel: Record<AuditOutcomeT, () => string> = {
  ok: m.ao_label_ok, failed: m.ao_label_failed, skipped: m.ao_label_skipped, not_attempted: m.ao_label_not_attempted,
};
const outcomeDesc: Partial<Record<AuditOutcomeT, () => string>> = {
  skipped: m.ao_desc_skipped, not_attempted: m.ao_desc_not_attempted,
};
export const runStatusLabel: Record<string, () => string> = {
  pending: m.rs_label_pending, running: m.rs_label_running, completed: m.rs_label_completed, aborted: m.rs_label_aborted, stopped: m.rs_label_stopped,
};
export const runTriggerLabel: Record<string, () => string> = {
  disk_pressure: m.rt_label_disk_pressure, http: m.rt_label_http, cli: m.rt_label_cli,
};
export const stopReasonLabel: Record<string, () => string> = {
  target_reached: m.sr_label_target_reached, no_more_candidates: m.sr_label_no_more_candidates, user_stopped: m.sr_label_user_stopped,
};
export const auditStepLabel: Record<string, () => string> = {
  arr_delete: m.ast_label_arr_delete, nlink_check: m.ast_label_nlink_check, qbit_delete: m.ast_label_qbit_delete,
};
export function labelOf(map: Record<string, () => string>, value: string): string {
  return (map[value] ?? (() => value || "—"))();
}

function tipWrap(text: string, child: React.ReactNode) {
  return (
    <Tooltip content={<span style={{ whiteSpace: "normal", display: "block", lineHeight: 1.35 }}>{text}</span>}>
      {child}
    </Tooltip>
  );
}

export function ActionStatusBadge({ status }: { status: ActionStatusT }) {
  const badge = <span className={`badge ${statusClass[status]}`}>{(actionStatusLabel[status] ?? (() => status))()}</span>;
  const desc = actionStatusDesc[status]?.();
  return desc ? tipWrap(desc, badge) : badge;
}

export function OutcomeBadge({ outcome }: { outcome: AuditOutcomeT }) {
  const badge = <span className={`badge ${outcomeTone[outcome]}`}>{(outcomeLabel[outcome] ?? (() => outcome))()}</span>;
  const desc = outcomeDesc[outcome]?.();
  return desc ? tipWrap(desc, badge) : badge;
}

export function ModeBadge({ mode }: { mode: string }) {
  // Live runs are signalled by the status dots/badges; only dry-runs need an
  // explicit tag so they aren't mistaken for the destructive default.
  return mode === "live" ? null : <span className="badge">{m.common_mode_dry_run()}</span>;
}
