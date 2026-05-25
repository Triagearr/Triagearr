import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Lock, Unlock, X } from "lucide-react";
import { useMemo } from "react";
import { useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { Sparkline } from "@/components/Sparkline";
import { humanBytes, relativeTime } from "@/lib/format";

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
              Torrent
            </span>
            <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto" }} onClick={onClose}>
              <X size={12} /> Close
            </button>
          </div>
          {t ? (
            <>
              <div style={{ fontSize: 13, fontWeight: 600, lineHeight: 1.35, wordBreak: "break-word" }}>
                {t.name}
              </div>
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
                {t.private
                  ? <span className="badge"><Lock size={9} /> private</span>
                  : <span className="badge"><Unlock size={9} /> public</span>
                }
                {t.score?.excluded && <span className="badge badge-warn">excluded</span>}
                {t.score && !t.score.any_tracker_alive && (
                  <span className="badge badge-danger">tracker dead</span>
                )}
                <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5, color: "var(--fg-3)" }}>
                  {t.hash}
                </span>
              </div>
            </>
          ) : (
            <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
              {torrent.isLoading ? "Loading…" : "Failed to load."}
            </div>
          )}
        </div>

        {t && (
          <div className="drawer-body">
            {/* Stats grid */}
            <div className="drawer-section" style={{ marginTop: 0 }}>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
                {[
                  ["Size",    humanBytes(t.size)],
                  ["Ratio",   t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"],
                  ["Seeders", t.latest?.seeders != null ? String(t.latest.seeders) : "—"],
                  ["Reap",    score != null ? score.toFixed(2) : "—"],
                ].map(([k, v]) => (
                  <div key={k} style={{ background: "var(--card)", padding: "8px 10px" }}>
                    <div style={{ fontSize: 10, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{k}</div>
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
              <div className="drawer-section-title">Score breakdown</div>
              {t.score
                ? <ScoreBreakdown factors={t.score.factors} total={t.score.score} />
                : <div style={{ color: "var(--fg-3)", fontSize: 12 }}>No score persisted yet.</div>
              }
            </div>

            {/* Trackers */}
            <div className="drawer-section">
              <div className="drawer-section-title">Trackers</div>
              {t.trackers.length === 0 ? (
                <div style={{ color: "var(--fg-3)", fontSize: 12 }}>No trackers stored yet.</div>
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
              <div className="drawer-section-title">*arr links ({t.links.length})</div>
              {t.links.length === 0 ? (
                <div style={{ color: "var(--fg-3)", fontSize: 12 }}>
                  Orphan — no *arr instance imported this torrent.
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
                <div className="drawer-section-title">Ratio · last 30 days</div>
                <div style={{ background: "var(--card-2)", border: "1px solid var(--border)", borderRadius: 6, padding: "10px 12px" }}>
                  <div style={{ color: tier === "high" ? "var(--red-2)" : "var(--primary)" }}>
                    <Sparkline data={ratioSeries}  />
                  </div>
                  <div style={{ display: "flex", justifyContent: "space-between", marginTop: 4, fontSize: 11, color: "var(--fg-3)", fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
                    <span>30d ago</span>
                    <span>today · {t.latest?.ratio?.toFixed(3) ?? "—"}</span>
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
                Open full page <ArrowUpRight size={11} />
              </Link>
            </div>
          </div>
        )}
      </aside>
    </>
  );
}
