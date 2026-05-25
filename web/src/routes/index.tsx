import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { AlertTriangle, Lock, RefreshCw, Unlock, Zap } from "lucide-react";
import { useSummary, useArrs, useTriggerRun } from "@/api/hooks";
import { PressureGauge } from "@/components/PressureGauge";
import { humanBytes, relativeTime } from "@/lib/format";
import type { ArrViewT } from "@/api/schemas";
import { ArrLogo } from "@/components/ArrLogo";

// ── Score tier (raw float from API) ───────────────────────────────────────
function scoreTier(score: number | null | undefined): "low" | "med" | "high" {
  if (score == null) return "low";
  if (score <= 1)  return "low";
  if (score <= 5)  return "med";
  return "high";
}

function ScoreCell({ score }: { score: number | null | undefined }) {
  if (score == null) return <span style={{ color: "var(--fg-4)" }}>—</span>;
  const tier = scoreTier(score);
  const pct = Math.min(100, Math.max(0, (score / 10) * 100));
  return (
    <span className={`score-cell ${tier}`}>
      <span className="score-bar"><i style={{ width: `${pct}%` }} /></span>
      {score.toFixed(2)}
    </span>
  );
}

function ModeBadge({ mode }: { mode: string }) {
  return mode === "live"
    ? <span className="badge badge-solid-danger">● live</span>
    : <span className="badge">dry-run</span>;
}

// ── *arr health tile — same visual as Settings tiles, uses real SVG logos ─────
function ArrHealthCard({ arr }: { arr: ArrViewT }) {
  const kind = arr.type.toLowerCase();
  const state = !arr.healthy ? "down" : "healthy";
  return (
    <div className={`arr-tile state-${state}`} style={{ cursor: "default" }}>
      <div className="arr-tile-head">
        <ArrLogo kind={kind} size={34} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name" style={{ textTransform: "capitalize" }}>{arr.name}</div>
          <div className="arr-tile-tag">{kind}</div>
        </div>
        <div className="arr-tile-state">
          {arr.healthy
            ? <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>Healthy</span></>
            : <><span className="dot red pulse" /><span style={{ color: "var(--red-2)" }}>Down</span></>
          }
        </div>
      </div>
      <div className="arr-tile-url">{arr.url}</div>
      {arr.last_error && (
        <div className="arr-tile-error">
          {arr.last_error}
        </div>
      )}
      <div className="arr-tile-foot">
        {arr.last_health_check
          ? <span>checked {relativeTime(arr.last_health_check)}</span>
          : <span>never checked</span>
        }
      </div>
    </div>
  );
}

function Dashboard() {
  const summary = useSummary();
  const arrsQuery = useArrs();
  const trigger = useTriggerRun();
  const navigate = useNavigate();

  const data = summary.data;
  const arrs = data?.arrs ?? arrsQuery.data?.arrs ?? [];
  const lastRuns = data?.last_runs ?? [];
  const topScore = data?.top_score ?? [];
  const volume = data?.volume;

  const healthyArrs = arrs.filter((a) => a.healthy).length;
  const totalArrs = arrs.length;

  function handleTriggerDryRun() {
    trigger.mutate({ mode: "dry-run" }, { onSuccess: () => navigate({ to: "/actions" }) });
  }

  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className="topbar">
        <div className="topbar-title">Dashboard</div>
        {data && (
          <div className="topbar-sub">
            {topScore.length > 0 ? `${topScore.length} scored` : "no scores yet"}
          </div>
        )}
        <div className="topbar-right">
          <button className="btn btn-sm" onClick={() => summary.refetch()}>
            <RefreshCw size={12} /> Refresh
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={handleTriggerDryRun}
            disabled={trigger.isPending}
          >
            <Zap size={12} /> Trigger dry-run
          </button>
        </div>
      </div>

      {/* Page content */}
      <div className="page">
        {summary.isLoading && (
          <div style={{ color: "var(--fg-3)", fontSize: 12 }}>Loading…</div>
        )}
        {summary.isError && (
          <div className="badge badge-danger" style={{ marginBottom: 12 }}>
            <AlertTriangle size={11} /> {String(summary.error)}
          </div>
        )}

        {data && (
          <>
            {/* Stat cards */}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 12, marginBottom: 14 }}>
              {[
                { label: "Torrents tracked", value: data.counts.torrents, foot: "in qBittorrent" },
                { label: "Scored", value: data.counts.scored, foot: "last cycle" },
                { label: "Actions (all time)", value: data.counts.actions, foot: "deletions executed" },
                {
                  label: "*arrs healthy",
                  value: totalArrs > 0 ? `${healthyArrs}/${totalArrs}` : "—",
                  foot: healthyArrs < totalArrs ? `${totalArrs - healthyArrs} down` : "all healthy",
                  accent: healthyArrs < totalArrs ? "var(--amber-2)" : undefined,
                },
              ].map(({ label, value, foot, accent }) => (
                <div key={label} className="card" style={{ padding: 14 }}>
                  <div className="stat-label">{label}</div>
                  <div className="stat-value" style={accent ? { color: accent } : undefined}>{value}</div>
                  <div className="stat-foot">{foot}</div>
                </div>
              ))}
            </div>

            {/* Disk + recent runs */}
            <div style={{ display: "grid", gridTemplateColumns: "1.2fr 1fr", gap: 12, marginBottom: 14 }}>
              {/* Disk pressure */}
              <div className="card">
                <div className="card-head">
                  <span className="card-title">Disk pressure</span>
                  {volume?.path && (
                    <span className="card-sub" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                      {volume.path}
                    </span>
                  )}
                </div>
                <div className="card-body">
                  {volume
                    ? <PressureGauge volume={volume} />
                    : <div style={{ color: "var(--fg-3)", fontSize: 12 }}>No volume configured.</div>
                  }
                </div>
              </div>

              {/* Recent runs */}
              <div className="card">
                <div className="card-head">
                  <span className="card-title">Recent runs</span>
                  <Link to="/actions" className="btn btn-ghost btn-sm" style={{ marginLeft: "auto" }}>
                    View all →
                  </Link>
                </div>
                <div className="card-body tight">
                  {lastRuns.length === 0 ? (
                    <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>No runs yet.</div>
                  ) : (
                    <table className="tbl">
                      <tbody>
                        {lastRuns.slice(0, 5).map((r) => (
                          <tr key={r.run_id} className="clickable" onClick={() => navigate({ to: "/actions" })}>
                            <td style={{ width: 1, paddingRight: 6 }}>
                              <ModeBadge mode={r.mode} />
                            </td>
                            <td className="mono" style={{ fontSize: 11.5 }}>#{r.run_id}</td>
                            <td className="num">{humanBytes(r.estimated_freed_bytes)}</td>
                            <td className="num" style={{ color: "var(--fg-3)", fontSize: 11.5 }}>
                              {relativeTime(r.triggered_at)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            </div>

            {/* Top reap candidates */}
            {topScore.length > 0 && (
              <div className="card" style={{ marginBottom: 14 }}>
                <div className="card-head">
                  <span className="card-title">Top reap candidates</span>
                  <span className="card-sub">highest score · would be deleted first in a live run</span>
                </div>
                <div className="card-body tight">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th style={{ width: 28 }}>#</th>
                        <th>Name</th>
                        <th style={{ textAlign: "right", width: 110 }}>Reap score</th>
                      </tr>
                    </thead>
                    <tbody>
                      {topScore.slice(0, 10).map((t, i) => (
                        <tr key={t.hash} className="clickable" onClick={() => navigate({ to: "/torrents", search: { detail: t.hash } })}>
                          <td className="mono" style={{ color: "var(--fg-3)" }}>{i + 1}</td>
                          <td className="name-cell">
                            <div className="name-text">{t.name}</div>
                            <div className="name-meta">
                              {t.private
                                ? <span className="badge"><Lock size={9} /> private</span>
                                : <span className="badge"><Unlock size={9} /> public</span>
                              }
                              {!t.any_tracker_alive && (
                                <span className="badge badge-danger">tracker dead</span>
                              )}
                              <span style={{ opacity: 0.6, fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                                {t.hash.slice(0, 10)}
                              </span>
                            </div>
                          </td>
                          <td style={{ textAlign: "right" }}><ScoreCell score={t.score} /></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {/* *arr health grid */}
            {arrs.length > 0 && (
              <div className="card">
                <div className="card-head">
                  <span className="card-title">*arr instance health</span>
                  <span className="card-sub">polled every 30s</span>
                </div>
                <div className="card-body arr-grid">
                  {arrs.map((a) => <ArrHealthCard key={a.name} arr={a} />)}
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export const Route = createFileRoute("/")({ component: Dashboard });
