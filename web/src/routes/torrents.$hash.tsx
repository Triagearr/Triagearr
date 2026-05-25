import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowUpRight } from "lucide-react";
import { useMemo } from "react";
import { useSnapshots, useTorrent } from "@/api/hooks";
import { ArrLogo } from "@/components/ArrLogo";
import { Badge } from "@/components/ui/Badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { Tabs } from "@/components/ui/Tabs";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { Callout } from "@/components/ui/Callout";
import { Sparkline } from "@/components/Sparkline";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";

function arrDeepLink(arrType: string, arrUrl: string, titleSlug: string): string {
  if (!arrUrl) return "";
  if (titleSlug) {
    if (arrType === "sonarr") return `${arrUrl}/series/${titleSlug}`;
    if (arrType === "radarr") return `${arrUrl}/movie/${titleSlug}`;
  }
  return arrUrl;
}

function TorrentDetailPage() {
  const { hash } = Route.useParams();
  const torrent = useTorrent(hash);
  const snaps = useSnapshots(hash);

  const snapshots = snaps.data?.snapshots;
  const series = useMemo(
    () => (snapshots ?? []).map((p) => ({ ts: p.ts, value: p.ratio })),
    [snapshots],
  );
  const seedSeries = useMemo(
    () => (snapshots ?? []).map((p) => ({ ts: p.ts, value: p.seeders })),
    [snapshots],
  );

  if (torrent.isLoading) return <div className="p-4 sm:p-6 text-sm text-muted-foreground">Loading…</div>;
  if (torrent.isError)
    return (
      <div className="p-4 sm:p-6">
        <Link to="/torrents" className="text-sm text-muted-foreground flex items-center gap-1">
          <ArrowLeft className="h-4 w-4" /> back to torrents
        </Link>
        <div className="mt-4">
          <Callout>{String(torrent.error)}</Callout>
        </div>
      </div>
    );
  if (!torrent.data) return null;
  const t = torrent.data;

  return (
    <div className="p-4 sm:p-6 space-y-4 max-w-5xl">
      <Link to="/torrents" className="text-sm text-muted-foreground flex items-center gap-1">
        <ArrowLeft className="h-4 w-4" /> back to torrents
      </Link>

      <header className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold truncate">{t.name}</h1>
          <div className="text-xs text-muted-foreground font-mono break-all">{t.hash}</div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {t.private ? <Badge>private</Badge> : <Badge variant="muted">public</Badge>}
          {t.score?.excluded && <Badge variant="warning">excluded</Badge>}
          {t.score && !t.score.any_tracker_alive && <Badge variant="warning">tracker dead</Badge>}
        </div>
      </header>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Stat label="Size" value={humanBytes(t.size)} />
        <Stat label="Ratio" value={t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"} />
        <Stat label="Seeders" value={t.latest?.seeders != null ? String(t.latest.seeders) : "—"} />
        <Stat label="Score" value={t.score ? t.score.score.toFixed(2) : "—"} />
      </div>

      <Tabs
        tabs={[
          {
            id: "overview",
            label: "Overview",
            content: (
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                <Card>
                  <CardHeader>
                    <CardTitle>Metadata</CardTitle>
                  </CardHeader>
                  <CardContent className="text-sm space-y-1">
                    <Row k="Category" v={t.category || "—"} />
                    <Row k="Save path" v={<code className="font-mono text-xs">{t.save_path}</code>} />
                    <Row k="Added" v={relativeTime(t.added_on)} />
                    <Row k="Completed" v={t.completion_on ? relativeTime(t.completion_on) : "—"} />
                    <Row k="Last seen" v={relativeTime(t.last_seen)} />
                    <Row k="State" v={t.latest?.state ?? "—"} />
                    <Row k="Uploaded" v={t.latest?.uploaded != null ? humanBytes(t.latest.uploaded) : "—"} />
                    <Row k="Tags" v={t.tags || "—"} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle>Trackers</CardTitle>
                  </CardHeader>
                  <CardContent>
                    {t.trackers.length === 0 ? (
                      <div className="text-sm text-muted-foreground">No trackers stored yet.</div>
                    ) : (
                      <Table>
                        <THead>
                          <TR>
                            <TH>Host</TH>
                            <TH>Status</TH>
                            <TH>Last check</TH>
                          </TR>
                        </THead>
                        <TBody>
                          {t.trackers.map((tr) => (
                            <TR key={`${tr.host}-${tr.url}`}>
                              <TD>{tr.host || "—"}</TD>
                              <TD>
                                <Badge variant={tr.status === "working" ? "success" : "warning"}>
                                  {tr.status}
                                </Badge>
                              </TD>
                              <TD className="text-muted-foreground">{relativeTime(tr.last_checked)}</TD>
                            </TR>
                          ))}
                        </TBody>
                      </Table>
                    )}
                  </CardContent>
                </Card>
              </div>
            ),
          },
          {
            id: "score",
            label: "Score",
            content: (
              <Card>
                <CardHeader>
                  <CardTitle>Score breakdown</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {t.score ? (
                    <>
                      <div className="text-sm text-muted-foreground">
                        computed {relativeTime(t.score.computed_at)} · regime{" "}
                        <Badge variant="muted">{t.score.private ? "ratio-obligation" : "swarm-only"}</Badge>
                        {t.score.excluded && (
                          <>
                            {" "}· <Badge variant="warning">excluded</Badge>{" "}
                            <span className="text-xs">{t.score.exclusion_reasons}</span>
                          </>
                        )}
                      </div>
                      <ScoreBreakdown factors={t.score.factors} total={t.score.score} />
                    </>
                  ) : (
                    <div className="text-sm text-muted-foreground">No score persisted yet.</div>
                  )}
                </CardContent>
              </Card>
            ),
          },
          {
            id: "links",
            label: `Links (${t.links.length})`,
            content: (
              <Card>
                <CardHeader>
                  <CardTitle>*arr-side imports</CardTitle>
                </CardHeader>
                <CardContent>
                  {t.links.length === 0 ? (
                    <div className="text-sm text-muted-foreground">
                      Orphan — no *arr instance imported this torrent (or import history not synced yet).
                    </div>
                  ) : (
                    <Table>
                      <THead>
                        <TR>
                          <TH>*arr</TH>
                          <TH>File ID</TH>
                          <TH className="text-right">Size</TH>
                          <TH>Live path</TH>
                        </TR>
                      </THead>
                      <TBody>
                        {t.links.map((l) => {
                          const href = arrDeepLink(l.arr_type, l.arr_url, l.title_slug);
                          return (
                            <TR key={`${l.arr_type}-${l.file_id}`}>
                              <TD>
                                {href ? (
                                  <a
                                    href={href}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
                                    title={`Open in ${l.arr_type}`}
                                  >
                                    <ArrLogo kind={l.arr_type} size={16} />
                                    {l.arr_type}
                                    <ArrowUpRight className="h-3 w-3 opacity-60" />
                                  </a>
                                ) : (
                                  <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
                                    <ArrLogo kind={l.arr_type} size={16} />
                                    {l.arr_type}
                                  </span>
                                )}
                              </TD>
                              <TD className="font-mono">{l.file_id}</TD>
                              <TD className="text-right font-mono">{humanBytes(l.size)}</TD>
                              <TD className="font-mono text-xs truncate max-w-md" title={l.live_path}>
                                {l.live_path}
                              </TD>
                            </TR>
                          );
                        })}
                      </TBody>
                    </Table>
                  )}
                </CardContent>
              </Card>
            ),
          },
          {
            id: "history",
            label: "History",
            content: (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <Card>
                  <CardHeader>
                    <CardTitle>Ratio over time</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <Sparkline data={series} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle>Seeders over time</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <Sparkline data={seedSeries} color="var(--accent-foreground)" />
                  </CardContent>
                </Card>
              </div>
            ),
          },
        ]}
      />

      <div className="text-xs text-muted-foreground">torrent {shortHash(t.hash)}</div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="p-3">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
        <div className="mt-1 text-xl font-semibold font-mono">{value}</div>
      </CardContent>
    </Card>
  );
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 border-b border-border/40 pb-1 last:border-0">
      <span className="text-xs uppercase tracking-wide text-muted-foreground">{k}</span>
      <span className="text-sm text-right truncate">{v}</span>
    </div>
  );
}

export const Route = createFileRoute("/torrents/$hash")({ component: TorrentDetailPage });
