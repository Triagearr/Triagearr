import { createFileRoute } from "@tanstack/react-router";
import { AlertTriangle, ChevronLeft, Play, Zap } from "lucide-react";
import { useEffect, useState } from "react";
import { useActions, useRun, useRuns, useTriggerRun } from "@/api/hooks";
import { ApiError } from "@/api/client";
import { humanBytes, relativeTime } from "@/lib/format";
import { useIsPhone } from "@/lib/useMediaQuery";
import { AllDeletionsTable } from "@/components/actions/AllDeletionsTable";
import { AuditDrawer } from "@/components/actions/AuditDrawer";
import { ConfirmLiveModal } from "@/components/actions/ConfirmLiveModal";
import { ModeBadge } from "@/components/actions/labels";
import { RunDetail } from "@/components/actions/RunDetail";
import { isInFlight } from "@/components/actions/shared";
import { m } from "@/paraglide/messages";

// Localizes the run-trigger failure banner. Backend error strings are English
// and bypass i18n, so map the known HTTP statuses to translated reasons rather
// than rendering the raw message.
function triggerErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return `${m.actions_trigger_failed()} — ${m.actions_live_in_progress()}`;
    if (err.status === 400) return `${m.actions_trigger_failed()} — ${m.dash_no_volume_configured()}`;
  }
  return m.actions_trigger_failed();
}

function ActionsPage() {
  const runs = useRuns();
  const allActions = useActions(50, 0);
  const trigger = useTriggerRun();
  const { run: runParam } = Route.useSearch();
  const [confirmLive, setConfirmLive] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(runParam ?? null);
  const [auditId, setAuditId] = useState<number | undefined>();
  const isPhone = useIsPhone();

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
          <span>{triggerErrorMessage(trigger.error)}</span>
        </div>
      )}

      {/* Master-detail split */}
      <div className="split" data-mobile-view={isPhone && selectedRun ? "detail" : "list"}>
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
          {isPhone && selectedRun && (
            <div style={{ padding: "8px 12px", borderBottom: "1px solid var(--border)", flex: "none" }}>
              <button className="btn btn-ghost btn-sm" onClick={() => setSelectedRunId(null)}>
                <ChevronLeft size={13} /> {m.common_back()}
              </button>
            </div>
          )}
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
