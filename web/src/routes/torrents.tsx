import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTorrents } from "@/api/hooks";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Table, TBody, TD, TH, THead, TR } from "@/components/ui/Table";
import { humanBytes, relativeTime, shortHash } from "@/lib/format";

function TorrentsPage() {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const [sort, setSort] = useState("name");
  const [privateOnly, setPrivateOnly] = useState(false);
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const list = useTorrents({ q, sort, privateOnly, limit, offset });

  return (
    <div className="p-6 space-y-4 max-w-7xl">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Torrents</h1>
          <p className="text-sm text-muted-foreground">
            {list.data ? `${list.data.total} torrents (showing ${list.data.torrents.length})` : "Loading…"}
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
        <select
          value={sort}
          onChange={(e) => setSort(e.target.value)}
          className="h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="name">sort: name</option>
          <option value="score">sort: score</option>
          <option value="size">sort: size</option>
          <option value="ratio">sort: ratio</option>
          <option value="seeders">sort: seeders</option>
          <option value="last_seen">sort: last seen</option>
        </select>
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

      <div className="rounded-lg border border-border bg-card">
        <Table>
          <THead>
            <TR>
              <TH>Name</TH>
              <TH>Category</TH>
              <TH className="text-right">Size</TH>
              <TH className="text-right">Ratio</TH>
              <TH className="text-right">Seeders</TH>
              <TH>State</TH>
              <TH>Last seen</TH>
            </TR>
          </THead>
          <TBody>
            {list.data?.torrents.map((t) => (
              <TR
                key={t.hash}
                className="cursor-pointer"
                onClick={() => navigate({ to: "/torrents/$hash", params: { hash: t.hash } })}
              >
                <TD>
                  <div className="font-medium truncate max-w-md">{t.name}</div>
                  <div className="text-xs text-muted-foreground font-mono">{shortHash(t.hash)}</div>
                </TD>
                <TD className="text-muted-foreground">{t.category || "—"}</TD>
                <TD className="font-mono text-right">{humanBytes(t.size)}</TD>
                <TD className="font-mono text-right">{t.ratio != null ? t.ratio.toFixed(3) : "—"}</TD>
                <TD className="font-mono text-right">{t.seeders ?? "—"}</TD>
                <TD className="text-muted-foreground">
                  {t.state ? <Badge variant="muted">{t.state}</Badge> : "—"}
                </TD>
                <TD className="text-muted-foreground">{relativeTime(t.last_seen)}</TD>
              </TR>
            ))}
            {list.data?.torrents.length === 0 && (
              <TR>
                <TD colSpan={7} className="text-center text-muted-foreground py-8">
                  No torrents match these filters.
                </TD>
              </TR>
            )}
          </TBody>
        </Table>
      </div>

    </div>
  );
}

export const Route = createFileRoute("/torrents")({ component: TorrentsPage });
