import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowUpRight, Lock, Shield, ShieldOff, Unlock } from "lucide-react";
import { useMemo } from "react";
import { useSetTorrentProtected, useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { Sparkline } from "@/components/Sparkline";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";

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
        <div className="topbar-title">Torrent</div>
        <div className="topbar-sub" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
          {t ? shortHash(t.hash, 12) : "—"}
        </div>
      </div>

      <div className="page" style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 1100 }}>
        {torrent.isLoading && (
          <div style={{ color: "var(--fg-3)", fontSize: 13 }}>Loading…</div>
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
                    ? <span className="badge"><Lock size={9} /> private</span>
                    : <span className="badge"><Unlock size={9} /> public</span>}
                  {t.protected && <span className="badge badge-warn"><Shield size={9} /> protected</span>}
                  {t.score?.excluded && <span className="badge badge-warn">excluded</span>}
                  {t.score && !t.score.any_tracker_alive && (
                    <span className="badge badge-danger">tracker dead</span>
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
                      ? "Remove the protection: this torrent becomes a normal cleanup candidate again."
                      : "Protect this torrent from cleanup. Reversible at any time."
                  }
                >
                  {t.protected ? <><ShieldOff size={12} /> Unprotect</> : <><Shield size={12} /> Protect</>}
                </button>
              </div>
            </div>

            {/* Stats grid */}
            <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 1, background: "var(--border)", border: "1px solid var(--border)", borderRadius: 6, overflow: "hidden" }}>
              {[
                ["Size",    humanBytes(t.size)],
                ["Ratio",   t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"],
                ["Seeders", t.latest?.seeders != null ? String(t.latest.seeders) : "—"],
                ["Reap",    score != null ? score.toFixed(2) : "—"],
              ].map(([k, v]) => (
                <div key={k} style={{ background: "var(--card)", padding: "12px 14px" }}>
                  <div style={{ fontSize: 11, color: "var(--fg-3)", textTransform: "uppercase", letterSpacing: ".05em" }}>{k}</div>
                  <div style={{
                    fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 22, fontWeight: 600,
                    letterSpacing: "-0.02em", marginTop: 4,
                    color: k === "Reap" ? (tier === "high" ? "var(--red-2)" : tier === "med" ? "var(--amber-2)" : "var(--green-2)") : "inherit",
                  }}>{v}</div>
                </div>
              ))}
            </div>

            {/* Two-column: Metadata + Score */}
            <div style={{ display: "grid", gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: 16 }}>
              <div className="card">
                <div className="card-head"><div className="card-title">Metadata</div></div>
                <div className="card-body">
                  <dl className="kv-grid">
                    <dt>Category</dt>      <dd>{t.category || "—"}</dd>
                    <dt>Save path</dt>     <dd style={{ wordBreak: "break-all" }}>{t.save_path || "—"}</dd>
                    <dt>Added</dt>         <dd>{relativeTime(t.added_on)}</dd>
                    <dt>Completed</dt>     <dd>{t.completion_on ? relativeTime(t.completion_on) : "—"}</dd>
                    <dt>Last seen</dt>     <dd>{relativeTime(t.last_seen)}</dd>
                    <dt>State</dt>         <dd>{t.latest?.state ?? "—"}</dd>
                    <dt>Uploaded</dt>      <dd>{t.latest?.uploaded != null ? humanBytes(t.latest.uploaded) : "—"}</dd>
                    <dt>Tags</dt>          <dd>{t.tags || "—"}</dd>
                    {t.protected && t.protected_at && (
                      <>
                        <dt>Protected</dt> <dd>{relativeTime(t.protected_at)}</dd>
                      </>
                    )}
                  </dl>
                </div>
              </div>

              <div className="card">
                <div className="card-head">
                  <div className="card-title">Score breakdown</div>
                  {t.score && (
                    <div className="card-sub">
                      <span className="badge">{t.score.private ? "ratio-obligation" : "swarm-only"}</span>
                    </div>
                  )}
                </div>
                <div className="card-body">
                  {t.score ? (
                    <>
                      {t.score.excluded && (
                        <div style={{ marginBottom: 10, fontSize: 12 }}>
                          <span className="badge badge-warn">excluded</span>{" "}
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
              <div className="card-head"><div className="card-title">Trackers</div></div>
              <div className="card-body tight">
                {t.trackers.length === 0 ? (
                  <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 13 }}>No trackers stored yet.</div>
                ) : (
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>Host</th>
                        <th>Status</th>
                        <th style={{ textAlign: "right" }}>Last check</th>
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
                <div className="card-title">*arr links</div>
                <div className="card-sub">{t.links.length}</div>
              </div>
              <div className="card-body tight">
                {t.links.length === 0 ? (
                  <div style={{ padding: 14, color: "var(--fg-3)", fontSize: 13 }}>
                    Orphan — no *arr instance imported this torrent (or import history not synced yet).
                  </div>
                ) : (
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>*arr</th>
                        <th style={{ width: 90 }}>File ID</th>
                        <th style={{ width: 100, textAlign: "right" }}>Size</th>
                        <th>Live path</th>
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
                                  title={`Open in ${l.arr_type}`}
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
                  <div className="card-head"><div className="card-title">Ratio · last 30 days</div></div>
                  <div className="card-body">
                    <div style={{ color: tier === "high" ? "var(--red-2)" : "var(--primary)" }}>
                      <Sparkline data={ratioSeries} />
                    </div>
                  </div>
                </div>
                <div className="card">
                  <div className="card-head"><div className="card-title">Seeders · last 30 days</div></div>
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
