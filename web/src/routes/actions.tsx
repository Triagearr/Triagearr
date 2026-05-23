import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useAction, useActions, useRun, useRunActions, useRuns } from "@/api/hooks";
import { Badge } from "@/components/ui/Badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Drawer } from "@/components/ui/Modal";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { RunTriggerDialog } from "@/components/RunTriggerDialog";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";
import { cn } from "@/lib/cn";
import type { ActionStatusT, AuditOutcomeT, RunResponseT } from "@/api/schemas";

// ── Tone maps ─────────────────────────────────────────────────────────────────

const statusTone: Record<ActionStatusT, "success" | "muted" | "destructive"> = {
  succeeded: "success",
  pending: "muted",
  running: "muted",
  failed_qbit: "destructive",
  aborted_arr_fail: "destructive",
  // FS namespace probe failed after *arr deletes succeeded: aborted, not failed.
  aborted_nlink_check: "destructive",
  // Cross-seed peer detected at T3.5: safe abort, no destructive intent.
  skipped_cross_seed: "muted",
};

const outcomeTone: Record<AuditOutcomeT, "success" | "muted" | "destructive"> = {
  ok: "success",
  failed: "destructive",
  skipped: "muted",
  not_attempted: "muted",
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function isInFlight(run: RunResponseT) {
  return run.status === "pending" || run.status === "running";
}

// ── Run list item ─────────────────────────────────────────────────────────────

function RunItem({
  run,
  selected,
  onClick,
}: {
  run: RunResponseT;
  selected: boolean;
  onClick: () => void;
}) {
  const inFlight = isInFlight(run);
  return (
    <button
      onClick={onClick}
      className={cn(
        "w-full text-left rounded-lg border px-3 py-2.5 text-sm transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        selected
          ? "border-primary/60 bg-primary/5"
          : "border-border bg-card hover:border-border/80 hover:bg-accent/30",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono font-medium">#{run.run_id}</span>
        <div className="flex items-center gap-1.5">
          {inFlight && (
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-amber-400 animate-pulse" />
          )}
          <Badge variant={run.mode === "live" ? "destructive" : "muted"}>{run.mode}</Badge>
        </div>
      </div>
      <div className="text-xs text-muted-foreground mt-0.5 truncate">
        {relativeTime(run.triggered_at)} · {run.stop_reason}
      </div>
      {run.estimated_freed_bytes > 0 && (
        <div className="text-xs text-muted-foreground">
          {humanBytes(run.estimated_freed_bytes)} freed
        </div>
      )}
    </button>
  );
}

// ── Run detail ────────────────────────────────────────────────────────────────

function RunDetail({
  run,
  onAudit,
}: {
  run: RunResponseT;
  onAudit: (actionId: number) => void;
}) {
  const inFlight = isInFlight(run);
  const actions = useRunActions(run.run_id, inFlight ? 2_000 : undefined);
  const actionList = actions.data?.actions ?? [];

  return (
    <div className="space-y-5">
      {/* Metadata */}
      <div className="rounded-lg border border-border bg-card/60 p-4 grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        <span className="text-muted-foreground">Mode</span>
        <span className="text-right">
          <Badge variant={run.mode === "live" ? "destructive" : "muted"}>{run.mode}</Badge>
        </span>
        <span className="text-muted-foreground">Status</span>
        <span className="text-right flex items-center justify-end gap-1.5">
          {inFlight && (
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-amber-400 animate-pulse" />
          )}
          <span className="font-mono">{run.status}</span>
        </span>
        <span className="text-muted-foreground">Triggered</span>
        <span className="text-right">{relativeTime(run.triggered_at)}</span>
        <span className="text-muted-foreground">By</span>
        <span className="text-right font-mono text-xs">{run.triggered_by}</span>
        <span className="text-muted-foreground">Stop reason</span>
        <span className="text-right font-mono text-xs">{run.stop_reason}</span>
        <span className="text-muted-foreground">Freed (est.)</span>
        <span className="text-right font-mono">{humanBytes(run.estimated_freed_bytes)}</span>
      </div>

      {/* Candidates (scored plan) */}
      {run.candidates && run.candidates.length > 0 && (
        <div>
          <h3 className="text-sm font-medium mb-2">
            Candidates{" "}
            <span className="text-muted-foreground font-normal">({run.candidates.length})</span>
          </h3>
          <div className="hidden sm:block">
            <Table>
              <THead>
                <TR>
                  <TH>Rank</TH>
                  <TH>Hash</TH>
                  <TH className="text-right">Score</TH>
                  <TH className="text-right">Size</TH>
                  <TH className="text-right">Would free</TH>
                </TR>
              </THead>
              <TBody>
                {run.candidates.map((c) => (
                  <TR key={c.torrent_hash}>
                    <TD className="font-mono text-muted-foreground">#{c.rank}</TD>
                    <TD className="font-mono">{shortHash(c.torrent_hash, 12)}</TD>
                    <TD className="text-right font-mono">{c.score.toFixed(1)}</TD>
                    <TD className="text-right font-mono">{humanBytes(c.size_bytes)}</TD>
                    <TD className="text-right font-mono">{humanBytes(c.would_free_bytes)}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          </div>
          {/* Mobile */}
          <div className="sm:hidden flex flex-col gap-1.5">
            {run.candidates.map((c) => (
              <div key={c.torrent_hash} className="rounded border border-border p-2 text-xs font-mono">
                <div className="flex justify-between">
                  <span>#{c.rank} {shortHash(c.torrent_hash, 10)}</span>
                  <span>score {c.score.toFixed(1)}</span>
                </div>
                <div className="text-muted-foreground">{humanBytes(c.size_bytes)} · frees {humanBytes(c.would_free_bytes)}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Actions (execution results) */}
      <div>
        <h3 className="text-sm font-medium mb-2">
          Actions
          {inFlight && (
            <span className="ml-2 text-xs text-muted-foreground font-normal animate-pulse">
              live…
            </span>
          )}
          {actions.isLoading && (
            <span className="ml-2 text-xs text-muted-foreground font-normal">Loading…</span>
          )}
        </h3>

        {!actions.isLoading && actionList.length === 0 && (
          <div className="text-sm text-muted-foreground">
            {inFlight ? "Waiting for first action…" : "No actions recorded for this run."}
          </div>
        )}

        {actionList.length > 0 && (
          <>
            <div className="hidden sm:block">
              <Table>
                <THead>
                  <TR>
                    <TH>Rank</TH>
                    <TH>Hash</TH>
                    <TH>Status</TH>
                    <TH className="text-right">Freed</TH>
                    <TH>Started</TH>
                    <TH></TH>
                  </TR>
                </THead>
                <TBody>
                  {actionList.map((a) => (
                    <TR key={a.id}>
                      <TD className="font-mono text-muted-foreground">#{a.rank}</TD>
                      <TD className="font-mono">{shortHash(a.torrent_hash, 12)}</TD>
                      <TD>
                        <Badge variant={statusTone[a.status]}>{a.status}</Badge>
                      </TD>
                      <TD className="text-right font-mono">{humanBytes(a.freed_bytes)}</TD>
                      <TD className="text-muted-foreground">{relativeTime(a.started_at)}</TD>
                      <TD>
                        <Button size="sm" variant="ghost" onClick={() => onAudit(a.id)}>
                          audit
                        </Button>
                      </TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            </div>
            <div className="sm:hidden flex flex-col gap-2">
              {actionList.map((a) => (
                <button
                  key={a.id}
                  onClick={() => onAudit(a.id)}
                  className="text-left rounded-lg border border-border bg-card p-3 active:bg-muted/40"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-sm">#{a.rank} · {shortHash(a.torrent_hash, 10)}</span>
                    <Badge variant={statusTone[a.status]}>{a.status}</Badge>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    freed {humanBytes(a.freed_bytes)} · {relativeTime(a.started_at)}
                  </div>
                </button>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ── Audit drawer ──────────────────────────────────────────────────────────────

function ActionAudit({ id }: { id: number | undefined }) {
  const detail = useAction(id);
  if (!id) return null;
  if (detail.isLoading) return <div className="text-sm text-muted-foreground">Loading…</div>;
  if (detail.isError) return <div className="text-sm text-destructive">{String(detail.error)}</div>;
  if (!detail.data) return null;

  const { action, audit } = detail.data;

  return (
    <div className="space-y-4">
      <div className="text-sm grid grid-cols-2 gap-y-1">
        <div className="text-muted-foreground">Status</div>
        <div className="text-right">
          <Badge variant={statusTone[action.status]}>{action.status}</Badge>
        </div>
        <div className="text-muted-foreground">Hash</div>
        <div className="text-right font-mono">{shortHash(action.torrent_hash, 14)}</div>
        <div className="text-muted-foreground">Freed</div>
        <div className="text-right font-mono">{humanBytes(action.freed_bytes)}</div>
        <div className="text-muted-foreground">Started</div>
        <div className="text-right">{relativeTime(action.started_at)}</div>
      </div>

      <div>
        <h3 className="text-sm font-medium mb-2">Audit trail</h3>
        {audit.length === 0 ? (
          <div className="text-sm text-muted-foreground">No audit rows.</div>
        ) : (
          <ul className="space-y-2">
            {audit.map((e) => (
              <li key={e.id} className="rounded-md border border-border bg-muted/30 p-2 text-sm">
                <div className="flex items-baseline justify-between">
                  <div className="flex items-center gap-2">
                    <Badge variant="muted">{e.step}</Badge>
                    <Badge variant={outcomeTone[e.outcome]}>{e.outcome}</Badge>
                  </div>
                  <span className="text-xs text-muted-foreground">{relativeTime(e.ts)}</span>
                </div>
                {(e.arr_name || e.arr_file_id) && (
                  <div className="text-xs text-muted-foreground mt-1">
                    {e.arr_name && <>arr: {e.arr_name}</>}{" "}
                    {e.arr_file_id ? `· file ${e.arr_file_id}` : ""}
                  </div>
                )}
                {e.detail && <div className="text-xs mt-1 font-mono break-all">{e.detail}</div>}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

// ── Timeline (cross-run action log) ──────────────────────────────────────────

function Timeline({
  onAudit,
  onSelectRun,
}: {
  onAudit: (id: number) => void;
  onSelectRun: (runId: number) => void;
}) {
  const actions = useActions(50, 0);
  const list = actions.data?.actions ?? [];

  return (
    <Card className="flex flex-col min-h-0 shrink-0">
      <CardHeader className="pb-2 shrink-0">
        <CardTitle className="text-base">
          Live deletions
          {list.length > 0 && (
            <span className="ml-2 text-muted-foreground font-normal text-sm">
              {list.length} actions
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="overflow-y-auto">
        {actions.isLoading && (
          <div className="text-sm text-muted-foreground">Loading…</div>
        )}
        {!actions.isLoading && list.length === 0 && (
          <div className="text-sm text-muted-foreground">
            No live deletions yet. Actions are only recorded during live runs — dry-runs show their candidates in the detail panel.
          </div>
        )}
        {list.length > 0 && (
          <>
            <div className="hidden sm:block">
              <Table>
                <THead>
                  <TR>
                    <TH>Run</TH>
                    <TH>Hash</TH>
                    <TH>Status</TH>
                    <TH className="text-right">Freed</TH>
                    <TH>When</TH>
                    <TH></TH>
                  </TR>
                </THead>
                <TBody>
                  {list.map((a) => (
                    <TR key={a.id}>
                      <TD>
                        <button onClick={() => onSelectRun(a.run_id)} className="font-mono text-primary hover:underline">
                          #{a.run_id}
                        </button>
                      </TD>
                      <TD className="font-mono">{shortHash(a.torrent_hash, 12)}</TD>
                      <TD><Badge variant={statusTone[a.status]}>{a.status}</Badge></TD>
                      <TD className="text-right font-mono">{humanBytes(a.freed_bytes)}</TD>
                      <TD className="text-muted-foreground">{relativeTime(a.started_at)}</TD>
                      <TD>
                        <Button size="sm" variant="ghost" onClick={() => onAudit(a.id)}>
                          audit
                        </Button>
                      </TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            </div>
            <div className="sm:hidden flex flex-col gap-2">
              {list.map((a) => (
                <button
                  key={a.id}
                  onClick={() => onAudit(a.id)}
                  className="text-left rounded-lg border border-border bg-card p-3 active:bg-muted/40"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-sm">#{a.run_id} · {shortHash(a.torrent_hash, 10)}</span>
                    <Badge variant={statusTone[a.status]}>{a.status}</Badge>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    freed {humanBytes(a.freed_bytes)} · {relativeTime(a.started_at)}
                  </div>
                </button>
              ))}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────

function ActionsPage() {
  const runs = useRuns();
  const [triggerMode, setTriggerMode] = useState<"dry-run" | "live" | null>(null);
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);
  const [auditActionId, setAuditActionId] = useState<number | undefined>();

  const runList = runs.data?.runs ?? [];
  // The /runs list strips candidates (handlers_runs.go). Fetch the selected
  // run individually so the detail panel can render the dry-run plan; fall
  // back to the list row while the single-run query is in flight.
  const selectedRunDetail = useRun(selectedRunId ?? undefined, 5_000);
  const selectedRun =
    selectedRunDetail.data ?? runList.find((r) => r.run_id === selectedRunId) ?? null;

  // h-[calc(100dvh-3.5rem)] accounts for the mobile top bar (h-14).
  // On md+ the sidebar is on the side so the full viewport height is available.
  return (
    <div className="flex flex-col h-[calc(100dvh-3.5rem)] md:h-dvh p-4 sm:p-6 gap-3 overflow-hidden">
      {/* Header + trigger — fixed height, never shrink */}
      <div className="shrink-0 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">Actions</h1>
            <p className="text-sm text-muted-foreground">
              Trigger a run, then select it to track candidates and actions in real time.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button onClick={() => setTriggerMode("dry-run")}>Plan dry-run</Button>
            <Button variant="destructive" onClick={() => setTriggerMode("live")}>
              Execute live…
            </Button>
            {runs.isFetching && (
              <span className="text-xs text-muted-foreground">Refreshing…</span>
            )}
          </div>
        </div>
      </div>

      {/* Master-detail — flex-1 takes all remaining space */}
      <div className="flex-1 min-h-0 grid grid-cols-1 lg:grid-cols-[260px_1fr] gap-3">
        {/* Runs list — scrolls internally */}
        <Card className="flex flex-col min-h-0 overflow-hidden">
          <CardHeader className="pb-2 shrink-0">
            <CardTitle className="text-base">
              Runs{" "}
              {runList.length > 0 && (
                <span className="text-muted-foreground font-normal text-sm">
                  ({runList.length})
                </span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="overflow-y-auto space-y-1.5">
            {runs.isLoading && (
              <div className="text-sm text-muted-foreground">Loading…</div>
            )}
            {!runs.isLoading && runList.length === 0 && (
              <div className="text-sm text-muted-foreground">
                No runs yet. Trigger one above.
              </div>
            )}
            {runList.map((run) => (
              <RunItem
                key={run.run_id}
                run={run}
                selected={run.run_id === selectedRunId}
                onClick={() => setSelectedRunId(run.run_id)}
              />
            ))}
          </CardContent>
        </Card>

        {/* Run detail — scrolls internally */}
        <Card className="flex flex-col min-h-0 overflow-hidden">
          <CardHeader className="pb-2 shrink-0">
            <CardTitle className="text-base">
              {selectedRun ? (
                <>
                  Run #{selectedRun.run_id}
                  {isInFlight(selectedRun) && (
                    <span className="ml-2 inline-block h-2 w-2 rounded-full bg-amber-400 animate-pulse" />
                  )}
                </>
              ) : (
                "Run detail"
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="overflow-y-auto">
            {!selectedRun ? (
              <div className="text-sm text-muted-foreground py-8 text-center">
                Select a run on the left to see its candidates and actions.
              </div>
            ) : (
              <RunDetail run={selectedRun} onAudit={setAuditActionId} />
            )}
          </CardContent>
        </Card>
      </div>

      {/* Timeline — fixed compact height, scrolls internally */}
      <div className="shrink-0 max-h-52">
        <Timeline onAudit={setAuditActionId} onSelectRun={setSelectedRunId} />
      </div>

      {/* Trigger dialog */}
      {triggerMode && (
        <RunTriggerDialog
          open
          mode={triggerMode}
          onClose={() => setTriggerMode(null)}
          onSuccess={(runId) => {
            setSelectedRunId(runId);
            setTriggerMode(null);
          }}
        />
      )}

      {/* Audit drawer */}
      <Drawer
        open={auditActionId != null}
        onClose={() => setAuditActionId(undefined)}
        title={auditActionId ? `Action #${auditActionId} · audit trail` : undefined}
      >
        <ActionAudit id={auditActionId} />
      </Drawer>
    </div>
  );
}

export const Route = createFileRoute("/actions")({ component: ActionsPage });
