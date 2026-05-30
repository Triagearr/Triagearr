import { ArrowUpRight, Lock, Shield, ShieldOff, Unlock, X } from "lucide-react";
import { memo, useMemo } from "react";
import { useSetTorrentProtected, useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { scoreTier } from "@/components/ScoreCell";
import { Sparkline } from "@/components/Sparkline";
import { humanBytes, relativeTime } from "@/lib/format";
import { m } from "@/paraglide/messages";

// Maps each *arr kind to its detail-page route prefix. whisparr_v2 is a
// Sonarr fork (series), v3 a Radarr fork (movies); Lidarr keys on artist,
// Readarr on author. Falls back to the instance root when the slug is absent
// (today only Sonarr/Radarr populate title_slug; the rest are stubs).
const arrRoutePrefix: Record<string, string> = {
  sonarr: "series",
  whisparr_v2: "series",
  radarr: "movie",
  whisparr_v3: "movie",
  lidarr: "artist",
  readarr: "author",
};

// Localized labels for the qBittorrent tracker status enum (schemas.ts).
function trackerStatusLabel(status: string): string {
  switch (status) {
    case "working": return m.tracker_status_working();
    case "not_contacted": return m.tracker_status_not_contacted();
    case "updating": return m.tracker_status_updating();
    case "not_working": return m.tracker_status_not_working();
    case "disabled": return m.tracker_status_disabled();
    default: return m.tracker_status_unknown();
  }
}

function arrDeepLink(arrType: string, arrUrl: string, titleSlug: string): string {
  if (!arrUrl) return "";
  const prefix = arrRoutePrefix[arrType];
  if (titleSlug && prefix) return `${arrUrl}/${prefix}/${titleSlug}`;
  return arrUrl;
}

type ArrLinkProps = {
  arrType: string;
  arrUrl: string;
  titleSlug: string;
  size: number;
};

const ArrLink = memo(function ArrLink({ arrType, arrUrl, titleSlug, size }: ArrLinkProps) {
  const href = arrDeepLink(arrType, arrUrl, titleSlug);
  return (
    <a
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
      <ArrLogo kind={arrType} size={16} />
      <span style={{ fontSize: 12, flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
        {arrType}
      </span>
      <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)" }}>
        {humanBytes(size)}
      </span>
      {href && <ArrowUpRight size={11} style={{ color: "var(--fg-3)", flexShrink: 0 }} />}
    </a>
  );
});

type Props = { hash: string | null; onClose: () => void };

export function TorrentDrawer({ hash, onClose }: Props) {
  const open = Boolean(hash);
  const torrent = useTorrent(hash ?? "");
  const snaps = useSnapshots(hash ?? "");
  const setProtected = useSetTorrentProtected();

  const ratioSeries = useMemo(
    () => (snaps.data?.snapshots ?? []).map((p) => ({ ts: p.ts, value: p.ratio })),
    [snaps.data?.snapshots],
  );

  const t = torrent.data;
  const score = t?.score?.score;
  const tier = scoreTier(score);

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
                ? <ScoreBreakdown factors={t.score.factors} total={t.score.score} trackerDeadEligibleAt={t.score.tracker_dead_eligible_at} />
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
                            {trackerStatusLabel(tr.status)}
                          </span>
                          {tr.first_seen_dead && (
                            <div style={{ color: "var(--fg-3)", fontSize: 10, marginTop: 2 }}>
                              {m.comp_drawer_tracker_dead_since({ when: relativeTime(tr.first_seen_dead) })}
                            </div>
                          )}
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
                  {t.links.map((l) => (
                    <ArrLink
                      key={`${l.arr_type}-${l.file_id}`}
                      arrType={l.arr_type}
                      arrUrl={l.arr_url}
                      titleSlug={l.title_slug}
                      size={l.size}
                    />
                  ))}
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

            {/* Full hash */}
            <div className="drawer-section">
              <div className="drawer-section-title">{m.comp_drawer_hash()}</div>
              <div style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11, color: "var(--fg-3)", wordBreak: "break-all" }}>
                {t.hash}
              </div>
            </div>

            {/* Footer actions */}
            <div className="drawer-section" style={{ display: "flex", gap: 8, paddingTop: 4 }}>
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
