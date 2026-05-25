import { createFileRoute } from "@tanstack/react-router";
import { AlertTriangle, Play, X, Zap } from "lucide-react";
import { useEffect, useState } from "react";
import {
  useAction, useActions, useRun, useRunActions, useRuns, useTriggerRun,
} from "@/api/hooks";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";
import type { ActionStatusT, AuditOutcomeT, RunResponseT } from "@/api/schemas";

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

function isInFlight(run: RunResponseT) {
  return run.status === "pending" || run.status === "running";
}

function ModeBadge({ mode }: { mode: string }) {
  return mode === "live"
    ? <span className="badge badge-solid-danger">● live</span>
    : <span className="badge">dry-run</span>;
}

// ── Confirm live modal ────────────────────────────────────────────────────────
function ConfirmLiveModal({ open, onClose, onConfirm }: {
  open: boolean; onClose: () => void; onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [armed, setArmed] = useState(false);
  const [count, setCount] = useState(5);
  const expected = "LIVE";

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

  useEffect(() => {
    if (!open) return;
    const fn = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    document.addEventListener("keydown", fn);
    return () => document.removeEventListener("keydown", fn);
  }, [open, onClose]);

  if (!open) return null;
  const matches = typed === expected;

  return (
    <div className="modal-scrim" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head" style={{ background: "var(--red-bg)", borderBottomColor: "color-mix(in oklch, var(--red) 30%, transparent)" }}>
          <AlertTriangle size={16} style={{ color: "var(--red-2)", flex: "none" }} />
          <h3 style={{ color: "var(--red-2)", fontSize: 14, fontWeight: 600, margin: 0 }}>
            Trigger live run
          </h3>
          <span className="badge badge-solid-danger" style={{ marginLeft: "auto" }}>destructive</span>
        </div>
        <div className="modal-body">
          <p style={{ margin: 0, fontSize: 13 }}>
            This will <strong style={{ color: "var(--red-2)" }}>permanently delete</strong> files
            from disk and unmonitor matching items in connected *arrs.
          </p>
          <div className="field">
            <label className="field-label">
              Type <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", color: "var(--red-2)" }}>{expected}</span> to confirm
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
          <button className="btn" onClick={onClose}>Cancel</button>
          <button
            className="btn btn-destructive"
            disabled={!matches || !armed}
            onClick={() => { onConfirm(); onClose(); }}
          >
            <Zap size={12} />
            {armed ? "Execute live run" : `Hold ${count}s…`}
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
      <aside className="ds-drawer" style={{ width: 500 }}>
        <div className="drawer-head">
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>
              Audit · action #{id}
            </div>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose}><X size={12} /> Close</button>
        </div>
        <div className="drawer-body">
          {detail.isLoading && <div style={{ color: "var(--fg-3)", fontSize: 12 }}>Loading…</div>}
          {detail.data && (() => {
            const { action, audit } = detail.data;
            return (
              <>
                <div className="drawer-section" style={{ marginTop: 0 }}>
                  <div style={{ display: "grid", gridTemplateColumns: "repeat(3,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
                    {[
                      ["Status", <span className={`badge ${statusClass[action.status]}`}>{action.status}</span>],
                      ["Freed", humanBytes(action.freed_bytes)],
                      ["Started", relativeTime(action.started_at)],
                    ].map(([k, v]) => (
                      <div key={String(k)} style={{ background: "var(--card)", padding: "8px 10px" }}>
                        <div style={{ fontSize: 10, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{k}</div>
                        <div style={{ fontFamily: typeof v === "string" ? "'Geist Mono',ui-monospace,monospace" : undefined, fontSize: 12.5, marginTop: 2 }}>{v}</div>
                      </div>
                    ))}
                  </div>
                  <div style={{ marginTop: 8, fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5, color: "var(--fg-3)", wordBreak: "break-all" }}>
                    {action.torrent_hash}
                  </div>
                </div>

                <div className="drawer-section">
                  <div className="drawer-section-title">Audit trail</div>
                  <div style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, padding: "4px 12px" }}>
                    {audit.length === 0
                      ? <div style={{ color: "var(--fg-3)", fontSize: 12, padding: "8px 0" }}>No audit rows.</div>
                      : audit.map((e) => (
                        <div key={e.id} className="audit-step">
                          <span className="audit-step-time">{relativeTime(e.ts)}</span>
                          <span className="audit-step-name">{e.step}</span>
                          <span className={`badge ${outcomeTone[e.outcome]}`}>{e.outcome}</span>
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
          {inFlight && <span className="badge badge-warn"><span className="dot amber pulse" style={{ marginRight: 3 }} />running</span>}
          {run.status === "aborted" && <span className="badge badge-danger">aborted</span>}
          {run.status === "completed" && <span className="badge badge-success">completed</span>}
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(5,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
          {[
            ["Status",     run.status],
            ["Triggered",  run.triggered_by],
            ["Stop",       run.stop_reason],
            ["Freed",      humanBytes(run.estimated_freed_bytes)],
            ["Started",    relativeTime(run.triggered_at)],
          ].map(([k, v]) => (
            <div key={k} style={{ background: "var(--card)", padding: "9px 11px" }}>
              <div style={{ fontSize: 10, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{k}</div>
              <div style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11.5, marginTop: 2 }}>{v}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Candidates */}
      {run.candidates && run.candidates.length > 0 && (
        <div>
          <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em", fontWeight: 600, marginBottom: 8 }}>
            Candidates · {run.candidates.length}
          </div>
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>Rank</th>
                <th>Hash</th>
                <th style={{ textAlign: "right", width: 80 }}>Score</th>
                <th style={{ textAlign: "right", width: 90 }}>Size</th>
                <th style={{ textAlign: "right", width: 90 }}>Would free</th>
              </tr>
            </thead>
            <tbody>
              {run.candidates.map((c) => (
                <tr key={c.torrent_hash}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{c.rank}</td>
                  <td className="mono">{shortHash(c.torrent_hash, 12)}</td>
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
          Actions {inFlight && <span className="badge badge-warn" style={{ marginLeft: 6, textTransform: "none" }}>live…</span>}
        </div>
        {actionList.length === 0 ? (
          <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
            {inFlight ? "Waiting for first action…" : "No actions recorded for this run."}
          </div>
        ) : (
          <table className="tbl" style={{ border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
            <thead>
              <tr>
                <th style={{ width: 50 }}>Rank</th>
                <th>Hash</th>
                <th>Status</th>
                <th style={{ textAlign: "right", width: 90 }}>Freed</th>
                <th style={{ width: 90 }}>Started</th>
                <th style={{ width: 50 }}></th>
              </tr>
            </thead>
            <tbody>
              {actionList.map((a) => (
                <tr key={a.id}>
                  <td className="mono" style={{ color: "var(--fg-3)" }}>#{a.rank}</td>
                  <td className="mono">{shortHash(a.torrent_hash, 12)}</td>
                  <td><span className={`badge ${statusClass[a.status]}`}>{a.status}</span></td>
                  <td className="num">{humanBytes(a.freed_bytes)}</td>
                  <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
                  <td>
                    <button className="btn btn-ghost btn-sm" onClick={() => onAudit(a.id)}>audit</button>
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

// ── Page ──────────────────────────────────────────────────────────────────────
function ActionsPage() {
  const runs = useRuns();
  const allActions = useActions(50, 0);
  const trigger = useTriggerRun();
  const [confirmLive, setConfirmLive] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);
  const [auditId, setAuditId] = useState<number | undefined>();

  const runList = runs.data?.runs ?? [];
  const selectedRunDetail = useRun(selectedRunId ?? undefined, 5_000);
  const selectedRun = selectedRunDetail.data ?? runList.find((r) => r.run_id === selectedRunId) ?? null;
  const hasLive = runList.some(isInFlight);

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
        <div className="topbar-title">Actions</div>
        <div className="topbar-sub">
          {hasLive
            ? <><span className="dot red pulse" style={{ marginRight: 6 }} />Live run in progress</>
            : "No run in flight"
          }
        </div>
        <div className="topbar-right">
          <button className="btn" onClick={triggerDryRun} disabled={trigger.isPending}>
            <Play size={12} /> Plan dry-run
          </button>
          <button className="btn btn-destructive" onClick={() => setConfirmLive(true)}>
            <Zap size={12} /> Execute live…
          </button>
        </div>
      </div>

      {/* Master-detail split */}
      <div className="split">
        {/* Runs list */}
        <div className="split-list">
          <div style={{ padding: "8px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 10.5, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".06em" }}>Runs</span>
            <span style={{ marginLeft: "auto", fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)" }}>
              {runList.length}
            </span>
          </div>
          {runs.isLoading && (
            <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>Loading…</div>
          )}
          {!runs.isLoading && runList.length === 0 && (
            <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>
              No runs yet. Trigger one above.
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

        {/* Detail */}
        <div className="split-detail">
          {!selectedRun ? (
            <div style={{ padding: 32, color: "var(--fg-3)", fontSize: 13, textAlign: "center" }}>
              Select a run on the left to see its candidates and actions.
            </div>
          ) : (
            <RunDetail run={selectedRun} onAudit={setAuditId} />
          )}
        </div>
      </div>

      {/* Live deletions strip */}
      <div className="live-strip">
        <div className="live-strip-head">
          Live deletions — all runs
          {allActions.data && (
            <span style={{ marginLeft: 8, color: "var(--fg-3)" }}>
              · {allActions.data.actions.length} actions
            </span>
          )}
        </div>
        <div className="live-strip-body">
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 60 }}>Run</th>
                <th>Hash</th>
                <th>Status</th>
                <th style={{ textAlign: "right", width: 90 }}>Freed</th>
                <th style={{ width: 90 }}>When</th>
                <th style={{ width: 50 }}></th>
              </tr>
            </thead>
            <tbody>
              {(allActions.data?.actions ?? []).map((a) => (
                <tr key={a.id}>
                  <td>
                    <button
                      className="btn btn-ghost btn-sm"
                      style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", padding: "0 4px" }}
                      onClick={() => setSelectedRunId(a.run_id)}
                    >
                      #{a.run_id}
                    </button>
                  </td>
                  <td className="mono">{shortHash(a.torrent_hash, 12)}</td>
                  <td><span className={`badge ${statusClass[a.status]}`}>{a.status}</span></td>
                  <td className="num">{humanBytes(a.freed_bytes)}</td>
                  <td style={{ fontSize: 11.5, color: "var(--fg-3)" }}>{relativeTime(a.started_at)}</td>
                  <td>
                    <button className="btn btn-ghost btn-sm" onClick={() => setAuditId(a.id)}>audit</button>
                  </td>
                </tr>
              ))}
              {allActions.data?.actions.length === 0 && (
                <tr>
                  <td colSpan={6} style={{ textAlign: "center", color: "var(--fg-3)", padding: "10px 14px", fontSize: 12 }}>
                    No live deletions yet — only live runs record actions.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <ConfirmLiveModal open={confirmLive} onClose={() => setConfirmLive(false)} onConfirm={triggerLive} />
      <AuditDrawer id={auditId} onClose={() => setAuditId(undefined)} />
    </div>
  );
}

export const Route = createFileRoute("/actions")({ component: ActionsPage });
