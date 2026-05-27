import { createFileRoute } from "@tanstack/react-router";
import { AlertTriangle, Play, X, Zap } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import {
  useAction, useActions, usePreviewRun, useRun, useRunActions, useRuns, useTriggerRun,
} from "@/api/hooks";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";
import { Tooltip } from "@/components/ui/Tooltip";
import type { ActionStatusT, ActionViewT, AuditOutcomeT, RunResponseT } from "@/api/schemas";
import { m } from "@/paraglide/messages";

function torrentLabel(name: string | undefined, hash: string, maxLen = 40): string {
  if (name) return name.length > maxLen ? name.slice(0, maxLen - 1) + "…" : name;
  return shortHash(hash, 12);
}

// ── Tone maps ─────────────────────────────────────────────────────────────────
const statusClass: Record<ActionStatusT, string> = {
  succeeded:           "badge-success",
  pending:             "",
  running:             "badge-info",
  failed_qbit:         "badge-danger",
  aborted_arr_fail:    "badge-danger",
  aborted_nlink_check: "badge-danger",
  skipped_cross_seed:  "",
};
const outcomeTone: Record<AuditOutcomeT, string> = {
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
const runStatusLabel: Record<string, () => string> = {
  pending: m.rs_label_pending, running: m.rs_label_running, completed: m.rs_label_completed, aborted: m.rs_label_aborted,
};
const runTriggerLabel: Record<string, () => string> = {
  disk_pressure: m.rt_label_disk_pressure, http: m.rt_label_http, cli: m.rt_label_cli,
};
const stopReasonLabel: Record<string, () => string> = {
  target_reached: m.sr_label_target_reached, no_more_candidates: m.sr_label_no_more_candidates,
};
const auditStepLabel: Record<string, () => string> = {
  arr_delete: m.ast_label_arr_delete, nlink_check: m.ast_label_nlink_check, qbit_delete: m.ast_label_qbit_delete,
};
function labelOf(map: Record<string, () => string>, value: string): string {
  return (map[value] ?? (() => value || "—"))();
}

function tipWrap(text: string, child: React.ReactNode) {
  return (
    <Tooltip content={<span style={{ whiteSpace: "normal", display: "block", lineHeight: 1.35 }}>{text}</span>}>
      {child}
    </Tooltip>
  );
}

function ActionStatusBadge({ status }: { status: ActionStatusT }) {
  const badge = <span className={`badge ${statusClass[status]}`}>{(actionStatusLabel[status] ?? (() => status))()}</span>;
  const desc = actionStatusDesc[status]?.();
  return desc ? tipWrap(desc, badge) : badge;
}

function OutcomeBadge({ outcome }: { outcome: AuditOutcomeT }) {
  const badge = <span className={`badge ${outcomeTone[outcome]}`}>{(outcomeLabel[outcome] ?? (() => outcome))()}</span>;
  const desc = outcomeDesc[outcome]?.();
  return desc ? tipWrap(desc, badge) : badge;
}

function isInFlight(run: RunResponseT) {
  return run.status === "pending" || run.status === "running";
}

function ModeBadge({ mode }: { mode: string }) {
  // Live runs are signalled by the status dots/badges; only dry-runs need an
  // explicit tag so they aren't mistaken for the destructive default.
  return mode === "live" ? null : <span className="badge">{m.common_mode_dry_run()}</span>;
}

// MetricGrid renders the repeated key/value summary cells used by the run and
// action panels. cols sets the column count; pass plain on an item whose value
// is a non-mono element (e.g. a badge).
type MetricItem = { k: React.ReactNode; v: React.ReactNode; plain?: boolean };
function MetricGrid({ cols, items }: { cols: number; items: MetricItem[] }) {
  return (
    <div className="metric-grid" style={{ "--metric-cols": cols } as React.CSSProperties}>
      {items.map((it, i) => (
        <div className="metric-cell" key={i}>
          <div className="metric-k">{it.k}</div>
          <div className={`metric-v${it.plain ? " plain" : ""}`}>{it.v}</div>
        </div>
      ))}
    </div>
  );
}

// ── Confirm live modal ────────────────────────────────────────────────────────
function ConfirmLiveModal({ open, onClose, onConfirm }: {
  open: boolean; onClose: () => void; onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [armed, setArmed] = useState(false);
  const [count, setCount] = useState(5);
  const expected = "LIVE";
  const cardRef = useRef<HTMLDivElement>(null);
  const restoreFocus = useRef<HTMLElement | null>(null);

  // Fetch the plan a live run would execute right now, only while open.
  const preview = usePreviewRun(open);
  const candidates = preview.data?.candidates ?? [];
  const hasCandidates = candidates.length > 0;

  useEffect(() => {
    if (!open) { setTyped(""); setArmed(false); setCount(5); return; }
    const tick = setInterval(() => {
      setCount((c) => {
        if (c <= 1) { clearInterval(tick); setArmed(true); return 0; }
        return c - 1;
      });
    }, 1000);
    return () => clearInterval(tick);
  }, [open]);

  // Escape to close + a minimal focus trap: remember the previously focused
  // element, keep Tab within the card, restore focus on close.
  useEffect(() => {
    if (!open) return;
    restoreFocus.current = document.activeElement as HTMLElement | null;
    const fn = (e: KeyboardEvent) => {
      if (e.key === "Escape") { onClose(); return; }
      if (e.key !== "Tab" || !cardRef.current) return;
      const focusable = cardRef.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input, [href], [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", fn);
    return () => {
      document.removeEventListener("keydown", fn);
      restoreFocus.current?.focus?.();
    };
  }, [open, onClose]);

  if (!open) return null;
  const matches = typed === expected;
  const canExecute = matches && armed && hasCandidates;

  return (
    <div className="modal-scrim" onClick={onClose}>
      <div
        className="modal-card"
        ref={cardRef}
        role="dialog"
        aria-modal="true"
        aria-label={m.actions_trigger_live_run()}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head" style={{ background: "var(--red-bg)", borderBottomColor: "color-mix(in oklch, var(--red) 30%, transparent)" }}>
          <AlertTriangle size={16} style={{ color: "var(--red-2)", flex: "none" }} />
          <h3 style={{ color: "var(--red-2)", fontSize: 14, fontWeight: 600, margin: 0 }}>
            {m.actions_trigger_live_run()}
          </h3>
          <span className="badge badge-solid-danger" style={{ marginLeft: "auto" }}>{m.actions_destructive()}</span>
        </div>
        <div className="modal-body">
          <p style={{ margin: 0, fontSize: 13 }}>
            {m.actions_warning_prefix()} <strong style={{ color: "var(--red-2)" }}>{m.actions_warning_permanently_delete()}</strong> {m.actions_warning_suffix()}
          </p>

          {/* Plan preview — what a live run would delete right now */}
          <div style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            {preview.isLoading ? (
              <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--fg-3)" }}>
                {m.actions_preview_loading()}
              </div>
            ) : !hasCandidates ? (
              <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--fg-3)" }}>
                {m.actions_preview_empty()}
              </div>
            ) : (
              <>
                <div style={{ padding: "8px 12px", fontSize: 12.5, fontWeight: 600, borderBottom: "1px solid var(--border)" }}>
                  {m.actions_preview_headline({
                    count: candidates.length,
                    freed: humanBytes(preview.data?.estimated_freed_bytes ?? 0),
                  })}
                </div>
                <div style={{ maxHeight: 160, overflow: "auto" }}>
                  <table className="tbl">
                    <tbody>
                      {candidates.map((c) => (
                        <tr key={c.torrent_hash}>
                          <td className="mono" style={{ color: "var(--fg-3)", width: 40 }}>#{c.rank}</td>
                          <td>{torrentLabel(c.torrent_name, c.torrent_hash)}</td>
                          <td className="num" style={{ width: 70 }}>{c.score.toFixed(1)}</td>
                          <td className="num" style={{ width: 80 }}>{humanBytes(c.would_free_bytes)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </div>

          <div className="field">
            <label className="field-label">
              {m.actions_type_label_before()} <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", color: "var(--red-2)" }}>{expected}</span> {m.actions_type_label_after()}
            </label>
            <input
              className="ds-input mono-val"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={expected}
              autoFocus
            />
          </div>
        </div>
        <div className="modal-foot">
          <button className="btn" onClick={onClose}>{m.common_cancel()}</button>
          <button
            className="btn btn-destructive"
            disabled={!canExecute}
            onClick={() => { onConfirm(); onClose(); }}
          >
            <Zap size={12} />
            {armed ? m.actions_execute_live_run() : m.actions_hold_seconds({ count })}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Audit drawer ──────────────────────────────────────────────────────────────
function AuditDrawer({ id, onClose }: { id: number | undefined; onClose: () => void }) {
  const detail = useAction(id);

  useEffect(() => {
    if (!id) return;
    const fn = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", fn);
    return () => document.removeEventListener("keydown", fn);
  }, [id, onClose]);

  if (!id) return null;
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <aside className="ds-drawer" style={{ width: 500 }} role="dialog" aria-modal="true">
        <div className="drawer-head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>
              {m.actions_audit_action({ id })}
            </div>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose}><X size={12} /> {m.common_close()}</button>
        </div>
        <div className="drawer-body">
          {detail.isLoading && <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.common_loading()}</div>}
          {detail.data && (() => {
            const { action, audit } = detail.data;
            return (
              <>
                <div className="drawer-section" style={{ marginTop: 0 }}>
                  <MetricGrid
                    cols={3}
                    items={[
                      { k: m.actions_col_status(), v: <ActionStatusBadge status={action.status} />, plain: true },
                      { k: m.actions_col_freed(), v: humanBytes(action.freed_bytes) },
                      { k: m.actions_col_started(), v: relativeTime(action.started_at) },
                    ]}
                  />
                  {action.torrent_name && (
                    <div style={{ marginTop: 8, fontSize: 12.5, fontWeight: 600 }}>
                      {action.torrent_name}
                    </div>
                  )}
                  <div style={{ marginTop: 4, fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5, color: "var(--fg-3)", wordBreak: "break-all" }}>
                    {action.torrent_hash}
                  </div>
                </div>

                <div className="drawer-section">
                  <div className="drawer-section-title">{m.actions_audit_trail()}</div>
                  <div style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, padding: "4px 12px" }}>
                    {audit.length === 0
                      ? <div style={{ color: "var(--fg-3)", fontSize: 12, padding: "8px 0" }}>{m.actions_no_audit_rows()}</div>
                      : audit.map((e) => (
                        <div key={e.id} className="audit-step">
                          <span className="audit-step-time">{relativeTime(e.ts)}</span>
                          <span className="audit-step-name">{labelOf(auditStepLabel, e.step)}</span>
                          <OutcomeBadge outcome={e.outcome} />
                          {e.detail && <span className="audit-step-detail">{e.detail}</span>}
                        </div>
                      ))
                    }
                  </div>
                </div>
              </>
            );
          })()}
        </div>
      </aside>
    </>
  );
}

// ── Run detail panel ──────────────────────────────────────────────────────────
function RunDetail({ run, onAudit }: { run: RunResponseT; onAudit: (id: number) => void }) {
  const inFlight = isInFlight(run);
  const actions = useRunActions(run.run_id, inFlight ? 2_000 : undefined);
  const actionList = actions.data?.actions ?? [];

  return (
    <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 18 }}>
      {/* Run header */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
          <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontWeight: 600, fontSize: 15 }}>
            #{run.run_id}
          </span>
          <ModeBadge mode={run.mode} />
          {inFlight && <span className="badge badge-warn"><span className="dot amber pulse" style={{ marginRight: 3 }} />{m.actions_status_running()}</span>}
          {run.status === "aborted" && <span className="badge badge-danger">{m.actions_status_aborted()}</span>}
          {run.status === "completed" && <span className="badge badge-success">{m.actions_status_completed()}</span>}
        </div>
        <MetricGrid
          cols={5}
          items={[
            { k: m.actions_col_status(),    v: labelOf(runStatusLabel, run.status), plain: true },
            { k: m.actions_col_triggered(), v: labelOf(runTriggerLabel, run.triggered_by), plain: true },
            { k: m.actions_col_stop(),      v: labelOf(stopReasonLabel, run.stop_reason), plain: true },
            { k: m.actions_col_freed(),     v: humanBytes(run.estimated_freed_bytes) },
            { k: m.actions_col_started(),   v: relativeTime(run.triggered_at) },
          ]}
        />
      </div>

      {/* Candidates */}
      {run.candidates && run.candidates.length > 0 && (
        <div>
          <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em", fontWeight: 600, marginBottom: 8 }}>
            {m.actions_candidates_count({ count: run.candidates.length })}
          </div>
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>{m.actions_th_rank()}</th>
                <th>{m.actions_th_hash()}</th>
                <th style={{ textAlign: "right", width: 80 }}>{m.actions_th_score()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_size()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_would_free()}</th>
              </tr>
            </thead>
            <tbody>
              {run.candidates.map((c) => (
                <tr key={c.torrent_hash}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{c.rank}</td>
                  <td>{torrentLabel(c.torrent_name, c.torrent_hash)}</td>
                  <td className="num">{c.score.toFixed(1)}</td>
                  <td className="num">{humanBytes(c.size_bytes)}</td>
                  <td className="num">{humanBytes(c.would_free_bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Actions */}
      <div>
        <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em", fontWeight: 600, marginBottom: 8 }}>
          {m.actions_section_actions()} {inFlight && <span className="badge badge-warn" style={{ marginLeft: 6, textTransform: "none" }}>{m.actions_live_ellipsis()}</span>}
        </div>
        {actionList.length === 0 ? (
          <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
            {inFlight ? m.actions_waiting_first_action() : m.actions_none_recorded()}
          </div>
        ) : (
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>{m.actions_th_rank()}</th>
                <th>{m.actions_th_hash()}</th>
                <th>{m.actions_th_status()}</th>
                <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_freed()}</th>
                <th style={{ width: 90 }}>{m.actions_th_started()}</th>
                <th style={{ width: 50 }}></th>
              </tr>
            </thead>
            <tbody>
              {actionList.map((a) => (
                <tr key={a.id}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{a.rank}</td>
                  <td>{torrentLabel(a.torrent_name, a.torrent_hash)}</td>
                  <td><ActionStatusBadge status={a.status} /></td>
                  <td className="num">{humanBytes(a.freed_bytes)}</td>
                  <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
                  <td>
                    <button className="btn btn-ghost btn-sm" onClick={() => onAudit(a.id)}>{m.actions_audit_btn()}</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

// AllDeletionsTable lists every recorded action across all runs. It is the
// landing view shown when no run is selected (only live runs record actions).
function AllDeletionsTable({ actions, onSelectRun, onAudit }: {
  actions: ActionViewT[]; onSelectRun: (id: number) => void; onAudit: (id: number) => void;
}) {
  return (
    <table className="tbl">
      <thead>
        <tr>
          <th style={{ width: 60 }}>{m.actions_th_run()}</th>
          <th>{m.actions_th_hash()}</th>
          <th>{m.actions_th_status()}</th>
          <th style={{ textAlign: "right", width: 90 }}>{m.actions_th_freed()}</th>
          <th style={{ width: 90 }}>{m.actions_th_when()}</th>
          <th style={{ width: 50 }}></th>
        </tr>
      </thead>
      <tbody>
        {actions.map((a) => (
          <tr key={a.id}>
            <td>
              <button
                className="btn btn-ghost btn-sm"
                style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", padding: "0 4px" }}
                onClick={() => onSelectRun(a.run_id)}
              >
                #{a.run_id}
              </button>
            </td>
            <td>{torrentLabel(a.torrent_name, a.torrent_hash)}</td>
            <td><ActionStatusBadge status={a.status} /></td>
            <td className="num">{humanBytes(a.freed_bytes)}</td>
            <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
            <td>
              <button className="btn btn-ghost btn-sm" onClick={() => onAudit(a.id)}>{m.actions_audit_btn()}</button>
            </td>
          </tr>
        ))}
        {actions.length === 0 && (
          <tr>
            <td colSpan={6} style={{ textAlign: "center", color: "var(--fg-3)", padding: "10px 14px", fontSize: 12 }}>
              {m.actions_no_live_deletions()}
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────
function ActionsPage() {
  const runs = useRuns();
  const allActions = useActions(50, 0);
  const trigger = useTriggerRun();
  const { run: runParam } = Route.useSearch();
  const [confirmLive, setConfirmLive] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(runParam ?? null);
  const [auditId, setAuditId] = useState<number | undefined>();

  // Sync selection when the ?run= param changes (e.g. navigating in from the
  // dashboard's recent-runs list while this page is already mounted).
  useEffect(() => {
    if (runParam != null) setSelectedRunId(runParam);
  }, [runParam]);

  const runList = runs.data?.runs ?? [];
  const runListEntry = runList.find((r) => r.run_id === selectedRunId) ?? null;
  // Poll the detail only while the selected run is still in flight; terminal
  // runs are fetched once and left alone.
  const selectedRunDetail = useRun(
    selectedRunId ?? undefined,
    runListEntry && isInFlight(runListEntry) ? 3_000 : undefined,
  );
  const selectedRun = selectedRunDetail.data ?? runListEntry;
  const hasLive = runList.some(isInFlight);
  const liveBusy = trigger.isPending || hasLive;

  function triggerDryRun() {
    trigger.mutate({ mode: "dry-run" }, {
      onSuccess: (r) => setSelectedRunId(r.run_id),
    });
  }
  function triggerLive() {
    trigger.mutate({ mode: "live" }, {
      onSuccess: (r) => setSelectedRunId(r.run_id),
    });
  }

  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className={`topbar ${hasLive ? "live" : ""}`}>
        <div className="topbar-title">{m.actions_title()}</div>
        <div className="topbar-sub">
          {hasLive
            ? <><span className="dot red pulse" style={{ marginRight: 6 }} />{m.actions_live_in_progress()}</>
            : m.actions_no_run_in_flight()
          }
        </div>
        <div className="topbar-right">
          <button className="btn" onClick={triggerDryRun} disabled={trigger.isPending}>
            <Play size={12} /> {m.actions_plan_dry_run()}
          </button>
          <button className="btn btn-destructive" onClick={() => setConfirmLive(true)} disabled={liveBusy}>
            <Zap size={12} /> {m.actions_execute_live()}
          </button>
        </div>
      </div>

      {/* Trigger error banner */}
      {trigger.isError && (
        <div className="banner banner-danger" role="alert">
          <AlertTriangle size={13} />
          <span>{m.actions_trigger_failed()}{trigger.error instanceof Error ? ` — ${trigger.error.message}` : ""}</span>
        </div>
      )}

      {/* Master-detail split */}
      <div className="split">
        {/* Runs list */}
        <div className="split-list">
          <div style={{ padding: "8px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 10.5, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em" }}>{m.actions_runs()}</span>
            <span style={{ marginLeft: "auto", fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)" }}>
              {runList.length}
            </span>
          </div>
          {runs.isLoading && (
            <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>{m.common_loading()}</div>
          )}
          {runs.isError && (
            <div style={{ padding: 14, color: "var(--red-2)", fontSize: 12 }}>{m.actions_load_failed()}</div>
          )}
          {!runs.isLoading && !runs.isError && runList.length === 0 && (
            <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>
              {m.actions_no_runs_trigger()}
            </div>
          )}
          {runList.map((r) => {
            const inf = isInFlight(r);
            return (
              <button
                key={r.run_id}
                className={`runrow ${r.run_id === selectedRunId ? "active" : ""}`}
                onClick={() => setSelectedRunId(r.run_id)}
              >
                <div className="runrow-top">
                  {inf && <span className="dot amber pulse" />}
                  {r.status === "aborted"   && <span className="dot red" />}
                  {r.status === "completed" && <span className="dot green" />}
                  <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>#{r.run_id}</span>
                  <ModeBadge mode={r.mode} />
                </div>
                <div className="runrow-bot">
                  <span>{r.triggered_by}</span>
                  <span>·</span>
                  <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{humanBytes(r.estimated_freed_bytes)}</span>
                  <span>·</span>
                  <span>{relativeTime(r.triggered_at)}</span>
                </div>
              </button>
            );
          })}
        </div>

        {/* Detail — selected run, or the cross-run deletions landing view */}
        <div className="split-detail">
          {selectedRun ? (
            <RunDetail run={selectedRun} onAudit={setAuditId} />
          ) : (
            <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
              <div className="live-strip-head" style={{ display: "flex", alignItems: "center" }}>
                {m.actions_live_deletions_all_runs()}
                {allActions.data && (
                  <span style={{ marginLeft: 8, color: "var(--fg-3)" }}>
                    · {m.actions_actions_count({ count: allActions.data.actions.length })}
                  </span>
                )}
              </div>
              <div style={{ padding: "10px 14px", fontSize: 12, color: "var(--fg-3)" }}>
                {m.actions_select_run_hint()}
              </div>
              <div style={{ flex: 1, overflow: "auto" }}>
                {allActions.isError ? (
                  <div style={{ padding: 14, color: "var(--red-2)", fontSize: 12 }}>{m.actions_load_failed()}</div>
                ) : (
                  <AllDeletionsTable
                    actions={allActions.data?.actions ?? []}
                    onSelectRun={setSelectedRunId}
                    onAudit={setAuditId}
                  />
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      <ConfirmLiveModal open={confirmLive} onClose={() => setConfirmLive(false)} onConfirm={triggerLive} />
      <AuditDrawer id={auditId} onClose={() => setAuditId(undefined)} />
    </div>
  );
}

type ActionsSearch = { run?: number };

export const Route = createFileRoute("/actions")({
  component: ActionsPage,
  validateSearch: (search: Record<string, unknown>): ActionsSearch => {
    const run = Number(search.run);
    return Number.isInteger(run) && run > 0 ? { run } : {};
  },
});
