import { createFileRoute, Link } from "@tanstack/react-router";
import { useSummary } from "@/api/hooks";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Callout } from "@/components/ui/Callout";
import { PressureGauge } from "@/components/PressureGauge";
import { Badge } from "@/components/ui/Badge";
import { humanBytes, relativeTime } from "@/lib/format";

function Dashboard() {
  const summary = useSummary();
  const data = summary.data;

  const volumes = data?.volumes ?? [];
  const arrs = data?.arrs ?? [];
  const lastRuns = data?.last_runs ?? [];
  const topScore = data?.top_score ?? [];

  return (
    <div className="p-4 sm:p-6 space-y-6">
      <div>
        <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">Dashboard</h1>
        <p className="text-sm text-muted-foreground">Overview of pressure, recent runs, and top candidates.</p>
      </div>

      {summary.isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
      {summary.isError && <Callout>{String(summary.error)}</Callout>}

      {data && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 sm:gap-4">
            <StatCard label="Torrents" value={String(data.counts.torrents)} />
            <StatCard label="Scored" value={String(data.counts.scored)} />
            <StatCard label="Total actions" value={String(data.counts.actions)} />
            <StatCard
              label="Healthy *arrs"
              value={`${arrs.filter((a) => a.healthy).length} / ${arrs.length}`}
            />
          </div>

          {volumes.length > 0 && (
            <section className="space-y-3">
              <h2 className="text-base font-semibold">Volumes</h2>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {volumes.map((v) => (
                  <PressureGauge key={v.name} volume={v} />
                ))}
              </div>
            </section>
          )}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <Card>
              <CardHeader>
                <CardTitle>Recent runs</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                {lastRuns.length === 0 && (
                  <div className="text-sm text-muted-foreground">No runs yet.</div>
                )}
                {lastRuns.slice(0, 5).map((run) => (
                  <Link
                    key={run.run_id}
                    to="/actions"
                    className="flex items-baseline justify-between rounded-md px-3 py-2 hover:bg-muted/50"
                  >
                    <div className="flex items-baseline gap-2 text-sm">
                      <span className="font-mono">#{run.run_id}</span>
                      <Badge variant={run.mode === "live" ? "destructive" : "muted"}>{run.mode}</Badge>
                      <span className="text-muted-foreground">{run.volume ?? "—"}</span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {humanBytes(run.estimated_freed_bytes)} · {relativeTime(run.triggered_at)}
                    </div>
                  </Link>
                ))}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Top candidates</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                {topScore.length === 0 && (
                  <div className="text-sm text-muted-foreground">No scored torrents yet.</div>
                )}
                {topScore.slice(0, 10).map((s) => (
                  <Link
                    key={s.hash}
                    to="/torrents/$hash"
                    params={{ hash: s.hash }}
                    className="flex items-baseline justify-between gap-3 rounded-md px-3 py-2 hover:bg-muted/50"
                  >
                    <div className="flex items-baseline gap-2 text-sm min-w-0">
                      <span className="truncate" title={s.name}>
                        {s.name}
                      </span>
                      {s.private && (
                        <Badge variant="muted" className="shrink-0">
                          private
                        </Badge>
                      )}
                      {!s.any_tracker_alive && (
                        <Badge variant="warning" className="shrink-0">
                          tracker dead
                        </Badge>
                      )}
                    </div>
                    <span className="font-mono text-sm shrink-0">{s.score.toFixed(2)}</span>
                  </Link>
                ))}
              </CardContent>
            </Card>
          </div>

          {arrs.length > 0 && (
            <section className="space-y-3">
              <h2 className="text-base font-semibold">*arr instances</h2>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                {arrs.map((a) => (
                  <Card key={`${a.type}/${a.name}`}>
                    <CardContent className="p-4 space-y-1">
                      <div className="flex items-center justify-between">
                        <div className="font-medium">{a.name}</div>
                        <Badge variant={a.healthy ? "success" : "destructive"}>
                          {a.healthy ? "healthy" : "down"}
                        </Badge>
                      </div>
                      <div className="text-xs text-muted-foreground font-mono">{a.type}</div>
                      <div className="text-xs text-muted-foreground truncate">{a.url}</div>
                      {a.last_health_check && (
                        <div className="text-xs text-muted-foreground">
                          checked {relativeTime(a.last_health_check)}
                        </div>
                      )}
                      {a.last_error && (
                        <div className="text-xs text-destructive truncate" title={a.last_error}>
                          {a.last_error}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </section>
          )}
        </>
      )}

    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
        <div className="mt-1 text-2xl font-semibold">{value}</div>
      </CardContent>
    </Card>
  );
}

export const Route = createFileRoute("/")({ component: Dashboard });
