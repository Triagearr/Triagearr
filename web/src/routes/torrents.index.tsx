import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Download, Lock, RefreshCw, Search, Unlock } from "lucide-react";
import { useDeferredValue, useState } from "react";
import { useTorrentCategories, useTorrents } from "@/api/hooks";
import { TorrentDrawer } from "@/components/TorrentDrawer";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";

type TorrentsSearch = { detail?: string };
type Order = "asc" | "desc";

function defaultOrder(field: string): Order {
  return field === "name" ? "asc" : "desc";
}

function scoreTier(score: number | null | undefined): "low" | "med" | "high" {
  if (score == null) return "low";
  if (score <= 1) return "low";
  if (score <= 5) return "med";
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

function TorrentsPage() {
  const navigate = useNavigate();
  const { detail } = Route.useSearch();
  const [q, setQ] = useState("");
  const deferredQ = useDeferredValue(q);
  const [sort, setSort] = useState("score");
  const [order, setOrder] = useState<Order>("desc");
  const [category, setCategory] = useState("");
  const [privateOnly, setPrivateOnly] = useState(false);
  const [excludedOnly, setExcludedOnly] = useState(false);
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const list = useTorrents({ q: deferredQ, sort, order, category, privateOnly, excludedOnly, limit, offset });
  const cats = useTorrentCategories();

  function onSort(field: string) {
    if (sort === field) setOrder((o) => (o === "asc" ? "desc" : "asc"));
    else { setSort(field); setOrder(defaultOrder(field)); }
    setOffset(0);
  }

  function openDetail(hash: string) { navigate({ to: "/torrents", search: { detail: hash } }); }
  function closeDetail() { navigate({ to: "/torrents", search: {} }); }

  const total = list.data?.total ?? 0;
  const shown = list.data?.torrents.length ?? 0;
  const page = Math.floor(offset / limit) + 1;
  const totalPages = Math.max(1, Math.ceil(total / limit));

  function SortIcon({ field }: { field: string }) {
    if (sort !== field) return null;
    return order === "asc" ? <ArrowUp size={10} /> : <ArrowDown size={10} />;
  }

  function sortTh(field: string, label: string, style?: React.CSSProperties) {
    return (
      <th
        className={`sortable`}
        style={style}
        onClick={() => onSort(field)}
      >
        <span style={{ display: "inline-flex", alignItems: "center", gap: 3 }}>
          {label} <SortIcon field={field} />
        </span>
      </th>
    );
  }

  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className="topbar">
        <div className="topbar-title">Torrents</div>
        <div className="topbar-sub">
          {list.data ? `${total.toLocaleString()} total · showing ${shown}` : "Loading…"}
        </div>
        <div className="topbar-right">
          <button className="btn btn-sm" onClick={() => list.refetch()}>
            <RefreshCw size={12} /> Re-score
          </button>
          <button className="btn btn-sm">
            <Download size={12} /> Export CSV
          </button>
        </div>
      </div>

      {/* Page (no padding — table goes edge-to-edge) */}
      <div className="page no-pad" style={{ display: "flex", flexDirection: "column" }}>
        {/* Filters bar */}
        <div className="filters-bar">
          <div className="input-wrap" style={{ flex: "0 1 280px" }}>
            <Search size={13} />
            <input
              className="ds-input"
              style={{ paddingLeft: 28, fontSize: 12 }}
              placeholder="Search name or hash…"
              value={q}
              onChange={(e) => { setQ(e.target.value); setOffset(0); }}
            />
          </div>
          <select
            className="ds-select"
            style={{ width: 170 }}
            value={category}
            onChange={(e) => { setCategory(e.target.value); setOffset(0); }}
          >
            <option value="">all categories</option>
            {cats.data?.categories.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
          <label className="filter-checkbox">
            <input type="checkbox" checked={privateOnly} onChange={(e) => { setPrivateOnly(e.target.checked); setOffset(0); }} />
            private only
          </label>
          <label className="filter-checkbox">
            <input type="checkbox" checked={excludedOnly} onChange={(e) => { setExcludedOnly(e.target.checked); setOffset(0); }} />
            excluded only
          </label>
          <span style={{ marginLeft: "auto", fontSize: 11.5, color: "var(--fg-3)" }}>
            <kbd>/</kbd> search · <kbd>↵</kbd> open
          </span>
        </div>

        {/* Scrollable table */}
        <div style={{ flex: 1, overflow: "auto" }}>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ minWidth: 260 }}>Name</th>
                {sortTh("category", "Category", { width: 130 })}
                {sortTh("size", "Size", { width: 90, textAlign: "right" })}
                {sortTh("ratio", "Ratio", { width: 70, textAlign: "right" })}
                {sortTh("seeders", "Seeders", { width: 70, textAlign: "right" })}
                {sortTh("score", "Reap score", { width: 120, textAlign: "right" })}
                <th style={{ width: 100 }}>State</th>
                {sortTh("last_seen", "Last seen", { width: 90 })}
              </tr>
            </thead>
            <tbody>
              {list.data?.torrents.map((t) => (
                <tr
                  key={t.hash}
                  className={`clickable ${detail === t.hash ? "row-selected" : ""}`}
                  onClick={() => openDetail(t.hash)}
                >
                  <td className="name-cell">
                    <div className="name-text">{t.name}</div>
                    <div className="name-meta">
                      {t.private
                        ? <span className="badge"><Lock size={9} /> private</span>
                        : <span className="badge"><Unlock size={9} /> public</span>
                      }
                      {t.excluded && <span className="badge badge-warn">excluded</span>}
                      {t.any_tracker_alive === false && (
                        <span className="badge badge-danger">tracker dead</span>
                      )}
                      <span style={{ opacity: 0.6 }}>{shortHash(t.hash, 10)}</span>
                    </div>
                  </td>
                  <td style={{ fontSize: 12, color: "var(--fg-2)" }}>
                    <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 11.5 }}>
                      {t.category || "—"}
                    </span>
                  </td>
                  <td className="num">{humanBytes(t.size)}</td>
                  <td className="num">{t.ratio != null ? t.ratio.toFixed(3) : "—"}</td>
                  <td className="num">{t.seeders ?? "—"}</td>
                  <td style={{ textAlign: "right", paddingRight: 12 }}>
                    <ScoreCell score={t.score} />
                  </td>
                  <td>
                    {t.state
                      ? <span className="badge" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", fontSize: 10.5 }}>{t.state}</span>
                      : <span style={{ color: "var(--fg-4)" }}>—</span>
                    }
                  </td>
                  <td style={{ color: "var(--fg-3)", fontSize: 11.5, fontVariantNumeric: "tabular-nums" }}>
                    {relativeTime(t.last_seen)}
                  </td>
                </tr>
              ))}
              {list.data?.torrents.length === 0 && (
                <tr>
                  <td colSpan={8} style={{ textAlign: "center", padding: 32, color: "var(--fg-3)" }}>
                    No torrents match these filters.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        <div className="pagination">
          <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
            {(offset + 1).toLocaleString()}–{Math.min(offset + limit, total).toLocaleString()}
          </span>
          <span style={{ color: "var(--fg-4)" }}>/</span>
          <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>{total.toLocaleString()}</span>
          <div style={{ flex: 1 }} />
          <button className="btn btn-sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>
            ← Prev
          </button>
          <span style={{ fontFamily: "'Geist Mono',ui-monospace,monospace", minWidth: 60, textAlign: "center", fontSize: 11.5 }}>
            {page} / {totalPages}
          </span>
          <button className="btn btn-sm" disabled={!list.data || offset + limit >= total} onClick={() => setOffset(offset + limit)}>
            Next →
          </button>
        </div>
      </div>

      <TorrentDrawer hash={detail ?? null} onClose={closeDetail} />
    </div>
  );
}

export const Route = createFileRoute("/torrents/")({
  component: TorrentsPage,
  validateSearch: (search: Record<string, unknown>): TorrentsSearch => ({
    detail: typeof search.detail === "string" ? search.detail : undefined,
  }),
});
