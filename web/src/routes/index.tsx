import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { AlertTriangle, Lock, RefreshCw, Unlock, Zap } from "lucide-react";
import { memo } from "react";
import { useSummary, useArrs, useTriggerRun } from "@/api/hooks";
import { PressureGauge } from "@/components/PressureGauge";
import { ScoreCell } from "@/components/ScoreCell";
import { humanBytes, relativeTime } from "@/lib/format";
import type { ArrViewT } from "@/api/schemas";
import { ArrLogo } from "@/components/ArrLogo";
import { m } from "@/paraglide/messages";

function ModeBadge({ mode }: { mode: string }) {
  return mode === "live"
    ? <span className="badge badge-solid-danger">● {m.common_mode_live()}</span>
    : <span className="badge">{m.common_mode_dry_run()}</span>;
}

type StatCardProps = {
  label: string;
  value: React.ReactNode;
  foot: string;
  accent?: string;
};

const StatCard = memo(function StatCard({ label, value, foot, accent }: StatCardProps) {
  return (
    <div className="card" style={{ padding: 14 }}>
      <div className="stat-label">{label}</div>
      <div className="stat-value" style={accent ? { color: accent } : undefined}>{value}</div>
      <div className="stat-foot">{foot}</div>
    </div>
  );
});

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
            ? <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>{m.common_healthy()}</span></>
            : <><span className="dot red pulse" /><span style={{ color: "var(--red-2)" }}>{m.dash_arr_down()}</span></>
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
          ? <span>{m.dash_arr_checked()} {relativeTime(arr.last_health_check)}</span>
          : <span>{m.dash_arr_never_checked()}</span>
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
        <div className="topbar-title">{m.dash_title()}</div>
        {data && (
          <div className="topbar-sub">
            {topScore.length > 0 ? m.dash_scored_count({ count: topScore.length }) : m.dash_no_scores_yet()}
          </div>
        )}
        <div className="topbar-right">
          <button className="btn btn-sm" onClick={() => summary.refetch()}>
            <RefreshCw size={12} /> {m.common_refresh()}
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={handleTriggerDryRun}
            disabled={trigger.isPending}
          >
            <Zap size={12} /> {m.dash_trigger_dry_run()}
          </button>
        </div>
      </div>

      {/* Page content */}
      <div className="page">
        {summary.isLoading && (
          <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.common_loading()}</div>
        )}
        {summary.isError && (
          <div className="badge badge-danger" style={{ marginBottom: 12 }}>
            <AlertTriangle size={11} /> {String(summary.error)}
          </div>
        )}

        {data && (
          <>
            {/* Stat cards */}
            <div className="grid-resp" style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 12, marginBottom: 14 }}>
              <StatCard label={m.dash_stat_torrents_tracked()} value={data.counts.torrents} foot={m.dash_stat_in_qbittorrent()} />
              <StatCard label={m.dash_stat_scored()} value={data.counts.scored} foot={m.dash_stat_last_cycle()} />
              <StatCard label={m.dash_stat_actions_all_time()} value={data.counts.actions} foot={m.dash_stat_deletions_executed()} />
              <StatCard
                label={m.dash_stat_arrs_healthy()}
                value={totalArrs > 0 ? `${healthyArrs}/${totalArrs}` : "—"}
                foot={healthyArrs < totalArrs ? m.dash_stat_arrs_down({ count: totalArrs - healthyArrs }) : m.dash_stat_all_healthy()}
                accent={healthyArrs < totalArrs ? "var(--amber-2)" : undefined}
              />
            </div>

            {/* Disk + recent runs */}
            <div className="grid-resp" style={{ display: "grid", gridTemplateColumns: "1.2fr 1fr", gap: 12, marginBottom: 14 }}>
              {/* Disk pressure */}
              <div className="card">
                <div className="card-head">
                  <span className="card-title">{m.dash_disk_pressure()}</span>
                  {volume?.path && (
                    <span className="card-sub" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                      {volume.path}
                    </span>
                  )}
                </div>
                <div className="card-body">
                  {volume
                    ? <PressureGauge volume={volume} />
                    : <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.dash_no_volume_configured()}</div>
                  }
                </div>
              </div>

              {/* Recent runs */}
              <div className="card">
                <div className="card-head">
                  <span className="card-title">{m.dash_recent_runs()}</span>
                  <Link to="/actions" className="btn btn-ghost btn-sm" style={{ marginLeft: "auto" }}>
                    {m.dash_view_all()} →
                  </Link>
                </div>
                <div className="card-body tight">
                  {lastRuns.length === 0 ? (
                    <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 12 }}>{m.dash_no_runs_yet()}</div>
                  ) : (
                    <table className="tbl">
                      <tbody>
                        {lastRuns.slice(0, 5).map((r) => (
                          <tr key={r.run_id} className="clickable" onClick={() => navigate({ to: "/actions", search: { run: r.run_id } })}>
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
                  <span className="card-title">{m.dash_top_reap_candidates()}</span>
                  <span className="card-sub">{m.dash_top_reap_sub()}</span>
                </div>
                <div className="card-body tight">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th style={{ width: 28 }}>#</th>
                        <th>{m.dash_th_name()}</th>
                        <th style={{ textAlign: "right", width: 110 }}>{m.dash_th_reap_score()}</th>
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
                                ? <span className="badge"><Lock size={9} /> {m.dash_badge_private()}</span>
                                : <span className="badge"><Unlock size={9} /> {m.dash_badge_public()}</span>
                              }
                              {!t.any_tracker_alive && (
                                <span className="badge badge-danger">{m.dash_badge_tracker_dead()}</span>
                              )}
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
                  <span className="card-title">{m.dash_arr_instance_health()}</span>
                  <span className="card-sub">{m.dash_polled_30s()}</span>
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
