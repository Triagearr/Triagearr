import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useAction, useActions, useRuns } from "@/api/hooks";
import { Badge } from "@/components/ui/Badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Drawer } from "@/components/ui/Modal";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { RunTriggerDialog } from "@/components/RunTriggerDialog";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";
import type { ActionStatusT, AuditOutcomeT } from "@/api/schemas";

function RunsSection() {
  const [open, setOpen] = useState<"dry-run" | "live" | null>(null);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Trigger a run</CardTitle>
        <CardDescription>
          A dry-run plans deletions without touching anything. Live runs require the daemon to be
          started with <code className="font-mono">mode: live</code>; otherwise the request is
          forced back to dry-run server-side.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={() => setOpen("dry-run")}>Plan dry-run</Button>
          <Button variant="destructive" onClick={() => setOpen("live")}>
            Execute live…
          </Button>
        </div>
        {open && <RunTriggerDialog open onClose={() => setOpen(null)} mode={open} />}
      </CardContent>
    </Card>
  );
}

const statusTone: Record<ActionStatusT, "success" | "muted" | "destructive"> = {
  succeeded: "success",
  pending: "muted",
  running: "muted",
  failed_qbit: "destructive",
  aborted_arr_fail: "destructive",
};

const outcomeTone: Record<AuditOutcomeT, "success" | "muted" | "destructive"> = {
  ok: "success",
  failed: "destructive",
  skipped: "muted",
  not_attempted: "muted",
};

function ActionsPage() {
  const actions = useActions(50, 0);
  const runs = useRuns();
  const [openAction, setOpenAction] = useState<number | undefined>();

  return (
    <div className="p-4 sm:p-6 space-y-4">
      <header>
        <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">Actions</h1>
        <p className="text-sm text-muted-foreground">Per-candidate destructive operations, ordered newest first.</p>
      </header>

      <RunsSection />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-1">
          <CardHeader>
            <CardTitle>Recent runs</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {runs.data?.runs.length === 0 && (
              <div className="text-sm text-muted-foreground">No runs yet.</div>
            )}
            {(runs.data?.runs ?? []).slice(0, 10).map((run) => (
              <div
                key={run.run_id}
                className="rounded-md border border-border bg-card/50 p-2 text-sm"
              >
                <div className="flex items-baseline justify-between">
                  <span className="font-mono">#{run.run_id}</span>
                  <Badge variant={run.mode === "live" ? "destructive" : "muted"}>{run.mode}</Badge>
                </div>
                <div className="text-xs text-muted-foreground">
                  {humanBytes(run.estimated_freed_bytes)} · {relativeTime(run.triggered_at)}
                </div>
                <div className="text-xs text-muted-foreground">stop: {run.stop_reason}</div>
              </div>
            ))}
          </CardContent>
        </Card>

        <div className="lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Timeline</CardTitle>
            </CardHeader>
            <CardContent>
              {actions.data?.actions.length === 0 && (
                <div className="text-sm text-muted-foreground">No actions executed yet.</div>
              )}
              {(actions.data?.actions ?? []).length > 0 && (
                <>
                  {/* Desktop / tablet: data table */}
                  <div className="hidden sm:block">
                    <Table>
                      <THead>
                        <TR>
                          <TH>Run</TH>
                          <TH>Hash</TH>
                          <TH>Status</TH>
                          <TH className="text-right">Freed</TH>
                          <TH>Started</TH>
                          <TH></TH>
                        </TR>
                      </THead>
                      <TBody>
                        {(actions.data?.actions ?? []).map((a) => (
                          <TR key={a.id}>
                            <TD className="font-mono">#{a.run_id}</TD>
                            <TD className="font-mono">{shortHash(a.torrent_hash, 12)}</TD>
                            <TD>
                              <Badge
                                variant={statusTone[a.status]}
                              >
                                {a.status}
                              </Badge>
                            </TD>
                            <TD className="text-right font-mono">{humanBytes(a.freed_bytes)}</TD>
                            <TD className="text-muted-foreground">{relativeTime(a.started_at)}</TD>
                            <TD>
                              <Button size="sm" variant="ghost" onClick={() => setOpenAction(a.id)}>
                                audit
                              </Button>
                            </TD>
                          </TR>
                        ))}
                      </TBody>
                    </Table>
                  </div>

                  {/* Mobile: card stack */}
                  <div className="sm:hidden flex flex-col gap-2">
                    {(actions.data?.actions ?? []).map((a) => (
                      <button
                        key={a.id}
                        onClick={() => setOpenAction(a.id)}
                        className="text-left rounded-lg border border-border bg-card p-3 active:bg-muted/40"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-mono text-sm">
                            #{a.run_id} · {shortHash(a.torrent_hash, 10)}
                          </span>
                          <Badge variant={statusTone[a.status]}>
                            {a.status}
                          </Badge>
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
        </div>
      </div>

      <Drawer
        open={openAction != null}
        onClose={() => setOpenAction(undefined)}
        title={openAction ? `Action #${openAction} · audit trail` : undefined}
      >
        <ActionAudit id={openAction} />
      </Drawer>
    </div>
  );
}

function ActionAudit({ id }: { id: number | undefined }) {
  const detail = useAction(id);
  if (!id) return null;
  if (detail.isLoading) return <div className="text-sm text-muted-foreground">Loading…</div>;
  if (detail.isError)
    return <div className="text-sm text-destructive">{String(detail.error)}</div>;
  if (!detail.data) return null;

  return (
    <div className="space-y-4">
      <div className="text-sm grid grid-cols-2 gap-y-1">
        <div className="text-muted-foreground">Status</div>
        <div className="text-right">
          <Badge
            variant={statusTone[detail.data.action.status]}
          >
            {detail.data.action.status}
          </Badge>
        </div>
        <div className="text-muted-foreground">Hash</div>
        <div className="text-right font-mono">{shortHash(detail.data.action.torrent_hash, 14)}</div>
        <div className="text-muted-foreground">Freed</div>
        <div className="text-right font-mono">{humanBytes(detail.data.action.freed_bytes)}</div>
        <div className="text-muted-foreground">Started</div>
        <div className="text-right">{relativeTime(detail.data.action.started_at)}</div>
      </div>

      <div>
        <h3 className="text-sm font-medium mb-2">Audit entries</h3>
        {detail.data.audit.length === 0 ? (
          <div className="text-sm text-muted-foreground">No audit rows.</div>
        ) : (
          <ul className="space-y-2">
            {detail.data.audit.map((e) => (
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

export const Route = createFileRoute("/actions")({ component: ActionsPage });
