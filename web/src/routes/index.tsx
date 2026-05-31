import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { AlertTriangle, Flame, Lock, RefreshCw, Skull, Unlock, Zap } from "lucide-react";
import { memo } from "react";
import {
  useSummary,
  useArrs,
  useTriggerRun,
  useSettings,
  useArrConnections,
  useTorrentClientConnections,
} from "@/api/hooks";
import { PressureGauge } from "@/components/PressureGauge";
import { ScoreCell } from "@/components/ScoreCell";
import { humanBytes, relativeTime } from "@/lib/format";
import type { ArrViewT, ClientViewT } from "@/api/schemas";
import { ArrLogo } from "@/components/ArrLogo";
import { TorrentClientLogo } from "@/components/TorrentClientLogo";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";

function ModeBadge({ mode }: { mode: string }) {
  return mode === "live"
    ? <span className="badge badge-solid-danger">● {m.common_mode_live()}</span>
    : <span className="badge">{m.common_mode_dry_run()}</span>;
}

// Health-tile chips — same visual as the Settings connection tiles
// (.arr-tile-toggles / .arr-chip), so the dashboard reads the per-instance
// act/enabled/delete state at a glance.
function TileChips({ chips }: { chips: { label: string; on: boolean; danger?: boolean }[] }) {
  return (
    <div className="arr-tile-toggles">
      {chips.map((c) => (
        <span key={c.label} className={cn("arr-chip", c.on && "on", c.on && c.danger && "danger")}>
          <span className="arr-chip-dot" /> {c.label}
        </span>
      ))}
    </div>
  );
}

// ModeStatCard — 5th stat card. Mirrors StatCard but is a coloured link to the
// Mode settings: red/danger when armed (live), neutral in dry-run.
function ModeStatCard({ mode }: { mode: string }) {
  const live = mode === "live";
  return (
    <Link
      to="/settings/mode"
      className="card"
      style={{
        padding: 14,
        textDecoration: "none",
        ...(live ? { borderColor: "var(--red)", background: "var(--red-bg)" } : {}),
      }}
    >
      <div className="stat-label">{m.settings_mode_title()}</div>
      <div className="stat-value" style={{ color: live ? "var(--red-2)" : undefined }}>
        {live ? m.common_mode_live() : m.common_mode_dry_run()}
      </div>
      <div className="stat-foot">{live ? m.dash_mode_foot_live() : m.dash_mode_foot_dry()}</div>
    </Link>
  );
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
function ArrHealthCard({ arr, conn }: { arr: ArrViewT; conn?: { enabled: boolean; act: boolean } }) {
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
      {conn && (
        <TileChips chips={[
          { label: m.settings_chip_enabled(), on: conn.enabled },
          { label: m.settings_chip_act(), on: conn.act, danger: true },
        ]} />
      )}
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

// ── Torrent-client health tile — mirrors ArrHealthCard; data already lives in
// the /summary response (torrent_clients[]). No per-client act flag exists, so
// the chips surface enabled + delete-files (what governs participation).
function ClientHealthCard({ client, conn }: {
  client: ClientViewT;
  conn?: { enabled: boolean; delete_with_files: boolean };
}) {
  const state = !client.healthy ? "down" : "healthy";
  return (
    <div className={`arr-tile state-${state}`} style={{ cursor: "default" }}>
      <div className="arr-tile-head">
        <TorrentClientLogo kind={client.kind} size={34} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name" style={{ textTransform: "capitalize" }}>{client.kind}</div>
        </div>
        <div className="arr-tile-state">
          {client.healthy
            ? <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>{m.common_healthy()}</span></>
            : <><span className="dot red pulse" /><span style={{ color: "var(--red-2)" }}>{m.dash_arr_down()}</span></>
          }
        </div>
      </div>
      <div className="arr-tile-url">{client.url}</div>
      {conn && (
        <TileChips chips={[
          { label: m.settings_chip_enabled(), on: conn.enabled },
          { label: m.settings_chip_delete_files(), on: conn.delete_with_files, danger: true },
        ]} />
      )}
      {client.last_error && (
        <div className="arr-tile-error">
          {client.last_error}
        </div>
      )}
      <div className="arr-tile-foot">
        {client.last_health_check
          ? <span>{m.dash_arr_checked()} {relativeTime(client.last_health_check)}</span>
          : <span>{m.dash_arr_never_checked()}</span>
        }
      </div>
    </div>
  );
}

function Dashboard() {
  const summary = useSummary();
  const arrsQuery = useArrs();
  const settings = useSettings();
  const arrConns = useArrConnections();
  const clientConns = useTorrentClientConnections();
  const trigger = useTriggerRun();
  const navigate = useNavigate();

  const data = summary.data;
  const arrs = data?.arrs ?? arrsQuery.data?.arrs ?? [];
  const torrentClients = data?.torrent_clients ?? [];
  const lastRuns = data?.last_runs ?? [];
  const topScore = data?.top_score ?? [];
  // Only positive scores are genuine reap candidates — negative scores are
  // vetoed/guard-protected and would never be deleted, so showing them under
  // "would be deleted first in a live run" misrepresents what a run does.
  const reapCandidates = topScore.filter((t) => t.score > 0).slice(0, 10);
  const volume = data?.volume;

  const mode = settings.data?.values.mode ?? "dry-run";
  const arrConnByKind = new Map((arrConns.data?.connections ?? []).map((c) => [c.kind, c]));
  const clientConnByKind = new Map((clientConns.data?.connections ?? []).map((c) => [c.kind, c]));

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
            <div className="grid-resp" style={{ display: "grid", gridTemplateColumns: "repeat(5,1fr)", gap: 12, marginBottom: 14 }}>
              <ModeStatCard mode={mode} />
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
            {reapCandidates.length > 0 && (
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
                      {reapCandidates.map((t, i) => (
                        <tr key={t.hash} className="clickable" onClick={() => navigate({ to: "/torrents", search: { detail: t.hash } })}>
                          <td className="mono" style={{ color: "var(--fg-3)" }}>{i + 1}</td>
                          <td className="name-cell">
                            <div className="name-text">{t.name}</div>
                            <div className="name-meta">
                              {t.private
                                ? <span className="badge"><Lock size={9} /> <span className="badge-label">{m.dash_badge_private()}</span></span>
                                : <span className="badge"><Unlock size={9} /> <span className="badge-label">{m.dash_badge_public()}</span></span>
                              }
                              {!t.any_tracker_alive && (
                                <span className="badge badge-danger"><Skull size={9} /> <span className="badge-label">{m.dash_badge_tracker_dead()}</span></span>
                              )}
                              {t.candidate_boost && (
                                <span className="badge badge-danger"><Flame size={9} /> <span className="badge-label">{m.dash_badge_prioritized()}</span></span>
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

            {/* Connection health — *arr instances + torrent clients in one grid */}
            {(arrs.length > 0 || torrentClients.length > 0) && (
              <div className="card">
                <div className="card-head">
                  <span className="card-title">{m.dash_connection_health()}</span>
                  <span className="card-sub">{m.dash_polled_30s()}</span>
                </div>
                <div className="card-body arr-grid">
                  {arrs.map((a) => (
                    <ArrHealthCard key={`arr-${a.name}`} arr={a} conn={arrConnByKind.get(a.type.toLowerCase())} />
                  ))}
                  {torrentClients.map((c) => (
                    <ClientHealthCard key={`client-${c.kind}`} client={c} conn={clientConnByKind.get(c.kind)} />
                  ))}
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
