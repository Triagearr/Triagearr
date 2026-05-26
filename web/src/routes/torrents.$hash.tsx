import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowUpRight, Lock, Shield, ShieldOff, Unlock } from "lucide-react";
import { useMemo } from "react";
import { useSetTorrentProtected, useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { Sparkline } from "@/components/Sparkline";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";
import { m } from "@/paraglide/messages";

function arrDeepLink(arrType: string, arrUrl: string, titleSlug: string): string {
  if (!arrUrl) return "";
  if (titleSlug) {
    if (arrType === "sonarr") return `${arrUrl}/series/${titleSlug}`;
    if (arrType === "radarr") return `${arrUrl}/movie/${titleSlug}`;
  }
  return arrUrl;
}

function scoreTier(score: number | null | undefined): "low" | "med" | "high" {
  if (score == null) return "low";
  if (score <= 1) return "low";
  if (score <= 5) return "med";
  return "high";
}

function TorrentDetailPage() {
  const { hash } = Route.useParams();
  const torrent = useTorrent(hash);
  const snaps = useSnapshots(hash);
  const setProtected = useSetTorrentProtected();

  const snapshots = snaps.data?.snapshots;
  const ratioSeries = useMemo(
    () => (snapshots ?? []).map((p) => ({ ts: p.ts, value: p.ratio })),
    [snapshots],
  );
  const seedSeries = useMemo(
    () => (snapshots ?? []).map((p) => ({ ts: p.ts, value: p.seeders })),
    [snapshots],
  );

  const t = torrent.data;
  const score = t?.score?.score;
  const tier = scoreTier(score);

  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className="topbar">
        <Link
          to="/torrents"
          className="btn btn-ghost btn-sm"
          style={{ textDecoration: "none", padding: "0 6px" }}
        >
          <ArrowLeft size={14} />
        </Link>
        <div className="topbar-title">{m.torrents_detail_title()}</div>
        <div className="topbar-sub" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
          {t ? shortHash(t.hash, 12) : "—"}
        </div>
      </div>

      <div className="page" style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 1100 }}>
        {torrent.isLoading && (
          <div style={{ color: "var(--fg-3)", fontSize: 13 }}>{m.common_loading()}</div>
        )}
        {torrent.isError && (
          <div className="card">
            <div className="card-body" style={{ color: "var(--red-2)", fontSize: 13 }}>
              {String(torrent.error)}
            </div>
          </div>
        )}

        {t && (
          <>
            {/* Header */}
            <div style={{ display: "flex", alignItems: "flex-start", gap: 16, flexWrap: "wrap" }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 18, fontWeight: 600, letterSpacing: "-0.01em", lineHeight: 1.3, wordBreak: "break-word" }}>
                  {t.name}
                </div>
                <div style={{ marginTop: 6, display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
                  {t.private
                    ? <span className="badge"><Lock size={9} /> {m.torrents_badge_private()}</span>
                    : <span className="badge"><Unlock size={9} /> {m.torrents_badge_public()}</span>}
                  {t.protected && <span className="badge badge-warn"><Shield size={9} /> {m.torrents_badge_protected()}</span>}
                  {t.score?.excluded && <span className="badge badge-warn">{m.torrents_badge_excluded()}</span>}
                  {t.score && !t.score.any_tracker_alive && (
                    <span className="badge badge-danger">{m.torrents_badge_tracker_dead()}</span>
                  )}
                  <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)", wordBreak: "break-all" }}>
                    {t.hash}
                  </span>
                </div>
              </div>
              <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
                <button
                  className={`btn btn-sm ${t.protected ? "btn-ghost" : "btn-primary"}`}
                  disabled={setProtected.isPending}
                  onClick={() => setProtected.mutate({ hash: t.hash, protected: !t.protected })}
                  title={
                    t.protected
                      ? m.torrents_unprotect_title()
                      : m.torrents_protect_title()
                  }
                >
                  {t.protected ? <><ShieldOff size={12} /> {m.torrents_unprotect()}</> : <><Shield size={12} /> {m.torrents_protect()}</>}
                </button>
              </div>
            </div>

            {/* Stats grid */}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
              {[
                ["size",    m.torrents_stat_size(),    humanBytes(t.size)],
                ["ratio",   m.torrents_stat_ratio(),   t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"],
                ["seeders", m.torrents_stat_seeders(), t.latest?.seeders != null ? String(t.latest.seeders) : "—"],
                ["reap",    m.torrents_stat_reap(),    score != null ? score.toFixed(2) : "—"],
              ].map(([id, k, v]) => (
                <div key={id} style={{ background: "var(--card)", padding: "12px 14px" }}>
                  <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{k}</div>
                  <div style={{
                    fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 22, fontWeight: 600,
                    letterSpacing: "-0.02em", marginTop: 4,
                    color: id === "reap" ? (tier === "high" ? "var(--red-2)" : tier === "med" ? "var(--amber-2)" : "var(--green-2)") : "inherit",
                  }}>{v}</div>
                </div>
              ))}
            </div>

            {/* Two-column: Metadata + Score */}
            <div style={{ display: "grid", gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: 16 }}>
              <div className="card">
                <div className="card-head"><div className="card-title">{m.torrents_metadata()}</div></div>
                <div className="card-body">
                  <dl className="kv-grid">
                    <dt>{m.torrents_meta_category()}</dt>      <dd>{t.category || "—"}</dd>
                    <dt>{m.torrents_meta_save_path()}</dt>     <dd style={{ wordBreak: "break-all" }}>{t.save_path || "—"}</dd>
                    <dt>{m.torrents_meta_added()}</dt>         <dd>{relativeTime(t.added_on)}</dd>
                    <dt>{m.torrents_meta_completed()}</dt>     <dd>{t.completion_on ? relativeTime(t.completion_on) : "—"}</dd>
                    <dt>{m.torrents_meta_last_seen()}</dt>     <dd>{relativeTime(t.last_seen)}</dd>
                    <dt>{m.torrents_meta_state()}</dt>         <dd>{t.latest?.state ?? "—"}</dd>
                    <dt>{m.torrents_meta_uploaded()}</dt>      <dd>{t.latest?.uploaded != null ? humanBytes(t.latest.uploaded) : "—"}</dd>
                    <dt>{m.torrents_meta_tags()}</dt>          <dd>{t.tags || "—"}</dd>
                    {t.protected && t.protected_at && (
                      <>
                        <dt>{m.torrents_meta_protected()}</dt> <dd>{relativeTime(t.protected_at)}</dd>
                      </>
                    )}
                  </dl>
                </div>
              </div>

              <div className="card">
                <div className="card-head">
                  <div className="card-title">{m.torrents_score_breakdown()}</div>
                  {t.score && (
                    <div className="card-sub">
                      <span className="badge">{t.score.private ? m.torrents_ratio_obligation() : m.torrents_swarm_only()}</span>
                    </div>
                  )}
                </div>
                <div className="card-body">
                  {t.score ? (
                    <>
                      {t.score.excluded && (
                        <div style={{ marginBottom: 10, fontSize: 12 }}>
                          <span className="badge badge-warn">{m.torrents_badge_excluded()}</span>{" "}
                          <span style={{ color: "var(--fg-3)" }}>{t.score.exclusion_reasons}</span>
                        </div>
                      )}
                      <ScoreBreakdown factors={t.score.factors} total={t.score.score} />
                      <div style={{ marginTop: 8, fontSize: 11, color: "var(--fg-3)" }}>
                        computed {relativeTime(t.score.computed_at)}
                      </div>
                    </>
                  ) : (
                    <div style={{ color: "var(--fg-3)", fontSize: 13 }}>No score persisted yet.</div>
                  )}
                </div>
              </div>
            </div>

            {/* Trackers */}
            <div className="card">
              <div className="card-head"><div className="card-title">{m.torrents_trackers()}</div></div>
              <div className="card-body tight">
                {t.trackers.length === 0 ? (
                  <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 13 }}>{m.torrents_no_trackers()}</div>
                ) : (
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>{m.torrents_tr_host()}</th>
                        <th>{m.torrents_tr_status()}</th>
                        <th style={{ textAlign: "right" }}>{m.torrents_tr_last_check()}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {t.trackers.map((tr) => (
                        <tr key={`${tr.host}-${tr.url}`}>
                          <td className="mono">{tr.host || "—"}</td>
                          <td>
                            <span className={`badge ${tr.status === "working" ? "badge-success" : "badge-danger"}`}>
                              {tr.status}
                            </span>
                          </td>
                          <td className="num" style={{ color: "var(--fg-3)" }}>{relativeTime(tr.last_checked)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </div>

            {/* Links */}
            <div className="card">
              <div className="card-head">
                <div className="card-title">{m.torrents_arr_links()}</div>
                <div className="card-sub">{t.links.length}</div>
              </div>
              <div className="card-body tight">
                {t.links.length === 0 ? (
                  <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 13 }}>
                    {m.torrents_orphan()}
                  </div>
                ) : (
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>{m.torrents_link_arr()}</th>
                        <th style={{ width: 90 }}>{m.torrents_link_file_id()}</th>
                        <th style={{ width: 100, textAlign: "right" }}>{m.torrents_link_size()}</th>
                        <th>{m.torrents_link_live_path()}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {t.links.map((l) => {
                        const href = arrDeepLink(l.arr_type, l.arr_url, l.title_slug);
                        return (
                          <tr key={`${l.arr_type}-${l.file_id}`}>
                            <td>
                              {href ? (
                                <a
                                  href={href}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12.5, color: "inherit", textDecoration: "none" }}
                                  title={m.torrents_open_in({ arr: l.arr_type })}
                                >
                                  <ArrLogo kind={l.arr_type} size={16} />
                                  {l.arr_type}
                                  <ArrowUpRight size={11} style={{ opacity: 0.6 }} />
                                </a>
                              ) : (
                                <span style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12.5 }}>
                                  <ArrLogo kind={l.arr_type} size={16} />
                                  {l.arr_type}
                                </span>
                              )}
                            </td>
                            <td className="mono">{l.file_id}</td>
                            <td className="num">{humanBytes(l.size)}</td>
                            <td className="mono" style={{ fontSize: 11, color: "var(--fg-3)", maxWidth: 380, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={l.live_path}>
                              {l.live_path}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                )}
              </div>
            </div>

            {/* History */}
            {ratioSeries.length > 1 && (
              <div style={{ display: "grid", gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: 16 }}>
                <div className="card">
                  <div className="card-head"><div className="card-title">{m.torrents_ratio_30d()}</div></div>
                  <div className="card-body">
                    <div style={{ color: tier === "high" ? "var(--red-2)" : "var(--primary)" }}>
                      <Sparkline data={ratioSeries} />
                    </div>
                  </div>
                </div>
                <div className="card">
                  <div className="card-head"><div className="card-title">{m.torrents_seeders_30d()}</div></div>
                  <div className="card-body">
                    <Sparkline data={seedSeries} />
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export const Route = createFileRoute("/torrents/$hash")({ component: TorrentDetailPage });
