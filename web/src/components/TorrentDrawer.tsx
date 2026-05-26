import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Lock, Shield, ShieldOff, Unlock, X } from "lucide-react";
import { useMemo } from "react";
import { useSetTorrentProtected, useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { Sparkline } from "@/components/Sparkline";
import { humanBytes, relativeTime } from "@/lib/format";
import { m } from "@/paraglide/messages";

function arrDeepLink(arrType: string, arrUrl: string, titleSlug: string): string {
  if (!arrUrl) return "";
  if (titleSlug) {
    if (arrType === "sonarr") return `${arrUrl}/series/${titleSlug}`;
    if (arrType === "radarr") return `${arrUrl}/movie/${titleSlug}`;
  }
  return arrUrl;
}

type Props = { hash: string | null; onClose: () => void };

export function TorrentDrawer({ hash, onClose }: Props) {
  const open = Boolean(hash);
  const torrent = useTorrent(hash ?? "");
  const snaps = useSnapshots(hash ?? "");
  const setProtected = useSetTorrentProtected();

  const ratioSeries = useMemo(
    () => (snaps.data?.snapshots ?? []).map((p) => ({ ts: p.ts, value: p.ratio })),
    [snaps.data],
  );

  const t = torrent.data;
  const score = t?.score?.score;
  const tier = score == null ? "low" : score <= 1 ? "low" : score <= 5 ? "med" : "high";

  if (!open) return null;
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <aside className="ds-drawer">
        {/* Header */}
        <div className="drawer-head" style={{ flexDirection: "column", alignItems: "stretch", gap: 8 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>
              {m.comp_drawer_torrent()}
            </span>
            <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto" }} onClick={onClose}>
              <X size={12} /> {m.common_close()}
            </button>
          </div>
          {t ? (
            <>
              <div style={{ fontSize: 13, fontWeight: 600, lineHeight: 1.35, wordBreak: "break-word" }}>
                {t.name}
              </div>
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
                {t.private
                  ? <span className="badge"><Lock size={9} /> {m.comp_drawer_private()}</span>
                  : <span className="badge"><Unlock size={9} /> {m.comp_drawer_public()}</span>
                }
                {t.protected && <span className="badge badge-warn"><Shield size={9} /> {m.comp_drawer_protected()}</span>}
                {t.score?.excluded && <span className="badge badge-warn">{m.comp_drawer_excluded()}</span>}
                {t.score && !t.score.any_tracker_alive && (
                  <span className="badge badge-danger">{m.comp_drawer_tracker_dead()}</span>
                )}
                <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5, color: "var(--fg-3)" }}>
                  {t.hash}
                </span>
              </div>
            </>
          ) : (
            <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
              {torrent.isLoading ? m.common_loading() : m.comp_drawer_failed_to_load()}
            </div>
          )}
        </div>

        {t && (
          <div className="drawer-body">
            {/* Stats grid */}
            <div className="drawer-section" style={{ marginTop: 0 }}>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
                {[
                  ["Size",    m.comp_drawer_stat_size(),    humanBytes(t.size)],
                  ["Ratio",   m.comp_drawer_stat_ratio(),   t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"],
                  ["Seeders", m.comp_drawer_stat_seeders(), t.latest?.seeders != null ? String(t.latest.seeders) : "—"],
                  ["Reap",    m.comp_drawer_stat_reap(),    score != null ? score.toFixed(2) : "—"],
                ].map(([k, label, v]) => (
                  <div key={k} style={{ background: "var(--card)", padding: "8px 10px" }}>
                    <div style={{ fontSize: 10, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{label}</div>
                    <div style={{
                      fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 14, marginTop: 2,
                      color: k === "Reap" ? (tier === "high" ? "var(--red-2)" : tier === "med" ? "var(--amber-2)" : "var(--green-2)") : "inherit",
                    }}>{v}</div>
                  </div>
                ))}
              </div>
            </div>

            {/* Score breakdown */}
            <div className="drawer-section">
              <div className="drawer-section-title">{m.comp_drawer_score_breakdown()}</div>
              {t.score
                ? <ScoreBreakdown factors={t.score.factors} total={t.score.score} />
                : <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.comp_drawer_no_score()}</div>
              }
            </div>

            {/* Trackers */}
            <div className="drawer-section">
              <div className="drawer-section-title">{m.comp_drawer_trackers()}</div>
              {t.trackers.length === 0 ? (
                <div style={{ color: "var(--fg-3)", fontSize: 12 }}>{m.comp_drawer_no_trackers()}</div>
              ) : (
                <table className="tbl" style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
                  <tbody>
                    {t.trackers.map((tr) => (
                      <tr key={`${tr.host}-${tr.url}`}>
                        <td className="mono" style={{ fontSize: 11.5 }}>{tr.host || "—"}</td>
                        <td>
                          <span className={`badge ${tr.status === "working" ? "badge-success" : "badge-danger"}`}>
                            {tr.status}
                          </span>
                        </td>
                        <td style={{ color: "var(--fg-3)", fontSize: 11, textAlign: "right", fontVariantNumeric: "tabular-nums" }}>
                          {relativeTime(tr.last_checked)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>

            {/* *arr links */}
            <div className="drawer-section">
              <div className="drawer-section-title">{m.comp_drawer_arr_links({ count: t.links.length })}</div>
              {t.links.length === 0 ? (
                <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
                  {m.comp_drawer_orphan()}
                </div>
              ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                  {t.links.map((l) => {
                    const href = arrDeepLink(l.arr_type, l.arr_url, l.title_slug);
                    return (
                      <a
                        key={`${l.arr_type}-${l.file_id}`}
                        href={href || undefined}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{
                          display: "flex", alignItems: "center", gap: 8,
                          padding: "8px 10px",
                          background: "var(--card-2)",
                          border: "1px solid var(--border)",
                          borderRadius: 6,
                          textDecoration: "none",
                          color: "inherit",
                          cursor: href ? "pointer" : "default",
                        }}
                      >
                        <ArrLogo kind={l.arr_type} size={16} />
                        <span style={{ fontSize: 12, flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
                          {l.arr_type}
                        </span>
                        <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)" }}>
                          {humanBytes(l.size)}
                        </span>
                        {href && <ArrowUpRight size={11} style={{ color: "var(--fg-3)", flexShrink: 0 }} />}
                      </a>
                    );
                  })}
                </div>
              )}
            </div>

            {/* Sparkline */}
            {ratioSeries.length > 1 && (
              <div className="drawer-section">
                <div className="drawer-section-title">{m.comp_drawer_ratio_30d()}</div>
                <div style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, padding: "10px 12px" }}>
                  <div style={{ color: tier === "high" ? "var(--red-2)" : "var(--primary)" }}>
                    <Sparkline data={ratioSeries}  />
                  </div>
                  <div style={{ display: "flex", justifyContent: "space-between", marginTop: 4, fontSize: 11, color: "var(--fg-3)", fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                    <span>{m.comp_drawer_30d_ago()}</span>
                    <span>{m.comp_drawer_today({ ratio: t.latest?.ratio?.toFixed(3) ?? "—" })}</span>
                  </div>
                </div>
              </div>
            )}

            {/* Footer actions */}
            <div className="drawer-section" style={{ display: "flex", gap: 8, paddingTop: 4 }}>
              <Link
                to="/torrents/$hash"
                params={{ hash: t.hash }}
                className="btn btn-sm"
                style={{ textDecoration: "none" }}
              >
                {m.comp_drawer_open_full_page()} <ArrowUpRight size={11} />
              </Link>
              <button
                className={`btn btn-sm ${t.protected ? "btn-ghost" : "btn-primary"}`}
                disabled={setProtected.isPending}
                onClick={() => setProtected.mutate({ hash: t.hash, protected: !t.protected })}
                title={
                  t.protected
                    ? m.comp_drawer_unprotect_title()
                    : m.comp_drawer_protect_title()
                }
              >
                {t.protected ? <><ShieldOff size={11} /> {m.comp_drawer_unprotect()}</> : <><Shield size={11} /> {m.comp_drawer_protect()}</>}
              </button>
            </div>
          </div>
        )}
      </aside>
    </>
  );
}
