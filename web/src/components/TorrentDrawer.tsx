import { Link } from "@tanstack/react-router";
import { ArrowUpRight } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { useSnapshots, useTorrent } from "@/api/hooks";
import { ScoreBreakdown } from "@/components/ScoreBreakdown";
import { Sparkline } from "@/components/Sparkline";
import { Badge } from "@/components/ui/Badge";
import { Drawer } from "@/components/ui/Modal";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { humanBytes, relativeTime } from "@/lib/format";

type Props = {
  hash: string | null;
  onClose: () => void;
};

export function TorrentDrawer({ hash, onClose }: Props) {
  const open = Boolean(hash);
  // Both hooks guard internally with `enabled: Boolean(hash)`, so the empty
  // string passed while closed never triggers a request.
  const torrent = useTorrent(hash ?? "");
  const snaps = useSnapshots(hash ?? "");

  const ratioSeries = useMemo(
    () => (snaps.data?.snapshots ?? []).map((p) => ({ ts: p.ts, value: p.ratio })),
    [snaps.data],
  );

  const t = torrent.data;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={t ? <span className="block truncate">{t.name}</span> : "Torrent"}
    >
      {torrent.isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
      {torrent.isError && (
        <div className="text-sm text-destructive">Failed to load this torrent.</div>
      )}
      {t && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground font-mono break-all">{t.hash}</span>
            {t.private ? <Badge>private</Badge> : <Badge variant="muted">public</Badge>}
            {t.score?.excluded && <Badge variant="warning">excluded</Badge>}
            {t.score && !t.score.any_tracker_alive && (
              <Badge variant="destructive">tracker dead</Badge>
            )}
          </div>

          <div className="grid grid-cols-2 gap-2">
            <Stat label="Size" value={humanBytes(t.size)} />
            <Stat label="Ratio" value={t.latest?.ratio != null ? t.latest.ratio.toFixed(3) : "—"} />
            <Stat
              label="Seeders"
              value={t.latest?.seeders != null ? String(t.latest.seeders) : "—"}
            />
            <Stat label="Score" value={t.score ? t.score.score.toFixed(2) : "—"} />
          </div>

          <Section title="Score breakdown">
            {t.score ? (
              <ScoreBreakdown factors={t.score.factors} total={t.score.score} />
            ) : (
              <div className="text-sm text-muted-foreground">No score persisted yet.</div>
            )}
          </Section>

          <Section title="Trackers">
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
          </Section>

          <Section title={`*arr links (${t.links.length})`}>
            {t.links.length === 0 ? (
              <div className="text-sm text-muted-foreground">
                Orphan — no *arr instance imported this torrent.
              </div>
            ) : (
              <ul className="text-sm space-y-1">
                {t.links.map((l) => (
                  <li
                    key={`${l.arr_type}-${l.arr_name}-${l.file_id}`}
                    className="flex items-center gap-2"
                  >
                    <Badge variant="muted">{l.arr_type}</Badge>
                    <span className="truncate">{l.arr_name}</span>
                    <span className="ml-auto font-mono text-xs text-muted-foreground">
                      {humanBytes(l.size)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Section>

          <Section title="Ratio over 30 days">
            <Sparkline data={ratioSeries} />
          </Section>

          <Link
            to="/torrents/$hash"
            params={{ hash: t.hash }}
            className="flex items-center justify-center gap-1 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm font-medium hover:bg-muted"
          >
            Open full page <ArrowUpRight className="h-4 w-4" />
          </Link>
        </div>
      )}
    </Drawer>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-muted/30 p-3">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold font-mono">{value}</div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </h3>
      {children}
    </div>
  );
}
