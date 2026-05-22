import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowDown, ArrowUp } from "lucide-react";
import { useDeferredValue, useState } from "react";
import { useTorrentCategories, useTorrents } from "@/api/hooks";
import { TorrentDrawer } from "@/components/TorrentDrawer";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { cn } from "@/lib/cn";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";

type TorrentsSearch = { detail?: string };
type Order = "asc" | "desc";

function defaultOrder(field: string): Order {
  return field === "name" ? "asc" : "desc";
}

function scoreTone(score: number | null | undefined): string {
  if (score == null) return "text-muted-foreground";
  if (score <= 0) return "text-emerald-600 dark:text-emerald-300";
  if (score < 5) return "text-foreground";
  return "text-amber-600 dark:text-amber-300";
}

function TorrentsPage() {
  const navigate = useNavigate();
  const { detail } = Route.useSearch();
  const [q, setQ] = useState("");
  const deferredQ = useDeferredValue(q);
  const [sort, setSort] = useState("name");
  const [order, setOrder] = useState<Order>("asc");
  const [category, setCategory] = useState("");
  const [privateOnly, setPrivateOnly] = useState(false);
  const [excludedOnly, setExcludedOnly] = useState(false);
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const list = useTorrents({
    q: deferredQ,
    sort,
    order,
    category,
    privateOnly,
    excludedOnly,
    limit,
    offset,
  });
  const cats = useTorrentCategories();

  function onSort(field: string) {
    if (sort === field) {
      setOrder((o) => (o === "asc" ? "desc" : "asc"));
    } else {
      setSort(field);
      setOrder(defaultOrder(field));
    }
    setOffset(0);
  }

  function openDetail(hash: string) {
    navigate({ to: "/torrents", search: { detail: hash } });
  }
  function closeDetail() {
    navigate({ to: "/torrents", search: {} });
  }

  const sortProps = { sort, order, onSort };

  return (
    <div className="p-4 sm:p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">Torrents</h1>
          <p className="text-sm text-muted-foreground">
            {list.data
              ? `${list.data.total} torrents (showing ${list.data.torrents.length})`
              : "Loading…"}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap gap-2 items-center">
        <Input
          placeholder="search by name…"
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setOffset(0);
          }}
          className="max-w-xs"
        />
        <Select
          value={category}
          onChange={(e) => {
            setCategory(e.target.value);
            setOffset(0);
          }}
        >
          <option value="">all categories</option>
          {cats.data?.categories.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </Select>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            checked={privateOnly}
            onChange={(e) => {
              setPrivateOnly(e.target.checked);
              setOffset(0);
            }}
          />
          private only
        </label>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            checked={excludedOnly}
            onChange={(e) => {
              setExcludedOnly(e.target.checked);
              setOffset(0);
            }}
          />
          excluded only
        </label>
        <div className="ml-auto flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - limit))}
          >
            ← prev
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!list.data || offset + limit >= list.data.total}
            onClick={() => setOffset(offset + limit)}
          >
            next →
          </Button>
        </div>
      </div>

      {/* Desktop / tablet: data table */}
      <div className="hidden md:block rounded-lg border border-border bg-card">
        <Table wrapperClassName="max-h-[calc(100vh-16rem)]">
          <THead className="sticky top-0 z-10 bg-card">
            <TR>
              <SortableTH field="name" label="Name" {...sortProps} />
              <TH>Category</TH>
              <SortableTH field="size" label="Size" className="text-right" {...sortProps} />
              <SortableTH field="ratio" label="Ratio" className="text-right" {...sortProps} />
              <SortableTH field="seeders" label="Seeders" className="text-right" {...sortProps} />
              <SortableTH field="score" label="Score" className="text-right" {...sortProps} />
              <TH>State</TH>
              <SortableTH field="last_seen" label="Last seen" {...sortProps} />
            </TR>
          </THead>
          <TBody>
            {list.data?.torrents.map((t) => (
              <TR
                key={t.hash}
                className="cursor-pointer"
                onClick={() => openDetail(t.hash)}
              >
                <TD>
                  <div className="font-medium truncate max-w-md">{t.name}</div>
                  <div className="mt-0.5 flex items-center gap-1.5">
                    <span className="text-xs text-muted-foreground font-mono">
                      {shortHash(t.hash)}
                    </span>
                    {!t.private && <Badge variant="muted">public</Badge>}
                    {t.excluded && <Badge variant="warning">excluded</Badge>}
                    {t.any_tracker_alive === false && (
                      <Badge variant="destructive">tracker dead</Badge>
                    )}
                  </div>
                </TD>
                <TD className="text-muted-foreground">{t.category || "—"}</TD>
                <TD className="font-mono text-right">{humanBytes(t.size)}</TD>
                <TD className="font-mono text-right">
                  {t.ratio != null ? t.ratio.toFixed(3) : "—"}
                </TD>
                <TD className="font-mono text-right">{t.seeders ?? "—"}</TD>
                <TD className={cn("font-mono text-right", scoreTone(t.score))}>
                  {t.score != null ? t.score.toFixed(2) : "—"}
                </TD>
                <TD className="text-muted-foreground">
                  {t.state ? <Badge variant="muted">{t.state}</Badge> : "—"}
                </TD>
                <TD className="text-muted-foreground">{relativeTime(t.last_seen)}</TD>
              </TR>
            ))}
            {list.data?.torrents.length === 0 && (
              <TR>
                <TD colSpan={8} className="text-center text-muted-foreground py-8">
                  No torrents match these filters.
                </TD>
              </TR>
            )}
          </TBody>
        </Table>
      </div>

      {/* Mobile: card stack */}
      <div className="md:hidden flex flex-col gap-2">
        {list.data?.torrents.map((t) => (
          <button
            key={t.hash}
            onClick={() => openDetail(t.hash)}
            className="text-left rounded-lg border border-border bg-card p-3 active:bg-muted/40"
          >
            <div className="font-medium leading-snug break-words">{t.name}</div>
            <div className="mt-1 flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground font-mono">{shortHash(t.hash)}</span>
              {!t.private && <Badge variant="muted">public</Badge>}
              {t.excluded && <Badge variant="warning">excluded</Badge>}
              {t.any_tracker_alive === false && <Badge variant="destructive">tracker dead</Badge>}
            </div>
            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs">
              <span className="text-muted-foreground">{humanBytes(t.size)}</span>
              {t.category && <span className="text-muted-foreground">· {t.category}</span>}
              {t.ratio != null && <span className="font-mono">ratio {t.ratio.toFixed(3)}</span>}
              {t.seeders != null && <span className="font-mono">seeders {t.seeders}</span>}
              {t.score != null && (
                <span className={cn("font-mono", scoreTone(t.score))}>
                  score {t.score.toFixed(2)}
                </span>
              )}
              {t.state && <Badge variant="muted">{t.state}</Badge>}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">{relativeTime(t.last_seen)}</div>
          </button>
        ))}
        {list.data?.torrents.length === 0 && (
          <div className="text-center text-muted-foreground py-8 text-sm">
            No torrents match these filters.
          </div>
        )}
      </div>

      <TorrentDrawer hash={detail ?? null} onClose={closeDetail} />
    </div>
  );
}

function SortableTH({
  field,
  label,
  sort,
  order,
  onSort,
  className,
}: {
  field: string;
  label: string;
  sort: string;
  order: Order;
  onSort: (field: string) => void;
  className?: string;
}) {
  const active = sort === field;
  return (
    <TH
      className={cn("cursor-pointer select-none hover:text-foreground", className)}
      onClick={() => onSort(field)}
      aria-sort={active ? (order === "asc" ? "ascending" : "descending") : "none"}
    >
      <span
        className={cn(
          "inline-flex items-center gap-1",
          className?.includes("text-right") && "flex-row-reverse",
          active && "text-foreground",
        )}
      >
        {label}
        {active &&
          (order === "asc" ? (
            <ArrowUp className="h-3 w-3" />
          ) : (
            <ArrowDown className="h-3 w-3" />
          ))}
      </span>
    </TH>
  );
}

export const Route = createFileRoute("/torrents/")({
  component: TorrentsPage,
  validateSearch: (search: Record<string, unknown>): TorrentsSearch => ({
    detail: typeof search.detail === "string" ? search.detail : undefined,
  }),
});
