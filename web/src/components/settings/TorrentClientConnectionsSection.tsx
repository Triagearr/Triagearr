import { useState } from "react";
import {
  useTorrentClientConnections,
  useCreateTorrentClientConnection,
  useUpdateTorrentClientConnection,
  useDeleteTorrentClientConnection,
  useTestTorrentClientConnection,
  type TorrentClientConnectionInput,
} from "@/api/hooks";
import type { TorrentClientConnectionT } from "@/api/schemas";
import { Drawer } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { TorrentClientLogo } from "@/components/TorrentClientLogo";
import { cn } from "@/lib/cn";
import {
  FieldRow,
  Toggle,
  splitList,
  useConnectionDrawer,
  DrawerActions,
  type ConnectionMutations,
} from "./ConnectionsCommon";

// ── Kind catalogue ─────────────────────────────────────────────────────────────

type KindMeta = {
  value: string;
  label: string;
  stub: boolean;
  urlPlaceholder: string;
};

const KINDS: KindMeta[] = [
  { value: "qbittorrent",  label: "qBittorrent",  stub: false, urlPlaceholder: "http://qbittorrent:8080" },
  { value: "transmission", label: "Transmission", stub: true,  urlPlaceholder: "http://transmission:9091" },
  { value: "deluge",       label: "Deluge",       stub: true,  urlPlaceholder: "http://deluge:8112" },
  { value: "rtorrent",     label: "rTorrent",     stub: true,  urlPlaceholder: "http://rtorrent:5000" },
];

// ── Tile ───────────────────────────────────────────────────────────────────────

type TileStatus = "unconfigured" | "disabled" | "configured";

function tileStatus(c: TorrentClientConnectionT | undefined): TileStatus {
  if (!c) return "unconfigured";
  if (!c.enabled) return "disabled";
  return "configured";
}

function KindTile({
  meta,
  connection,
  onClick,
}: {
  meta: KindMeta;
  connection: TorrentClientConnectionT | undefined;
  onClick: () => void;
}) {
  const status = tileStatus(connection);

  const stateClass: Record<TileStatus, string> = {
    unconfigured: "state-unconfigured",
    disabled:     "state-disabled",
    configured:   "state-healthy",
  };

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={meta.stub}
      className={cn("arr-tile", stateClass[status], meta.stub && "opacity-50 cursor-not-allowed")}
    >
      <div className="arr-tile-head">
        <TorrentClientLogo kind={meta.value} size={36} greyscale={meta.stub} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name">{meta.label}</div>
          {meta.stub && (
            <div className="arr-tile-tag" style={{ color: "var(--fg-4)", fontSize: 10 }}>coming soon</div>
          )}
        </div>
        <div
          className="arr-tile-state"
          title={
            status === "configured"   ? "Configured"
            : status === "disabled"   ? "Disabled"
            :                           "Not configured"
          }
        >
          {status === "configured"   && <><span className="dot green" /><span className="arr-tile-state-text" style={{ color: "var(--green-2)" }}>Configured</span></>}
          {status === "disabled"     && <><span className="dot" /><span className="arr-tile-state-text" style={{ color: "var(--fg-3)" }}>Disabled</span></>}
          {status === "unconfigured" && <><span className="dot" style={{ background: "transparent", border: "1px dashed var(--border-2)" }} /><span className="arr-tile-state-text" style={{ color: "var(--fg-3)" }}>Not configured</span></>}
        </div>
      </div>

      {connection && <div className="arr-tile-url">{connection.url}</div>}

      {connection && (
        <div className="arr-tile-toggles">
          <span className={cn("arr-chip", connection.enabled && "on")}>
            <span className="arr-chip-dot" /> Enabled
          </span>
          <span className={cn("arr-chip", connection.delete_with_files && "on danger")}>
            <span className="arr-chip-dot" /> Delete files
          </span>
        </div>
      )}

      {!connection && !meta.stub && (
        <div className="arr-tile-empty"><span>Click to configure</span></div>
      )}
    </button>
  );
}

// ── Form ───────────────────────────────────────────────────────────────────────

type Form = {
  url: string;
  username: string;
  password: string;
  enabled: boolean;
  category_exclude: string;
  tags_exclude: string;
  delete_with_files: boolean;
  timeout_seconds: number;
};

const emptyForm = (): Form => ({
  url: "",
  username: "",
  password: "",
  enabled: true,
  category_exclude: "",
  tags_exclude: "",
  delete_with_files: true,
  timeout_seconds: 30,
});

const connectionToForm = (c: TorrentClientConnectionT): Form => ({
  url: c.url,
  username: c.username,
  password: c.password,
  enabled: c.enabled,
  category_exclude: (c.category_exclude ?? []).join(", "),
  tags_exclude: (c.tags_exclude ?? []).join(", "),
  delete_with_files: c.delete_with_files,
  timeout_seconds: c.timeout_seconds,
});

const formToInput = (kind: string, f: Form): TorrentClientConnectionInput => ({
  kind,
  url: f.url.trim(),
  username: f.username,
  password: f.password,
  enabled: f.enabled,
  category_exclude: splitList(f.category_exclude),
  tags_exclude: splitList(f.tags_exclude),
  delete_with_files: f.delete_with_files,
  timeout_seconds: f.timeout_seconds,
});

type TorrentTestInput = {
  kind: string;
  url: string;
  username: string;
  password: string;
  timeout_seconds: number;
};

const formToTest = (kind: string, f: Form): TorrentTestInput => ({
  kind,
  url: f.url.trim(),
  username: f.username,
  password: f.password,
  timeout_seconds: f.timeout_seconds,
});

function clientValidate(f: Form): string | null {
  if (f.enabled) {
    if (!f.url.trim()) return "URL is required when the connection is enabled.";
    try {
      const u = new URL(f.url);
      if (!u.host) return "URL must include a host.";
    } catch {
      return "URL is not valid.";
    }
  }
  if (f.timeout_seconds < 0) return "Timeout must be zero or positive.";
  return null;
}

// ── Drawer ─────────────────────────────────────────────────────────────────────

function ConnectionDrawer({
  meta,
  connection,
  open,
  onClose,
}: {
  meta: KindMeta;
  connection: TorrentClientConnectionT | undefined;
  open: boolean;
  onClose: () => void;
}) {
  const mutations: ConnectionMutations<TorrentClientConnectionInput, TorrentTestInput> = {
    create: useCreateTorrentClientConnection(),
    update: useUpdateTorrentClientConnection(),
    del:    useDeleteTorrentClientConnection(),
    test:   useTestTorrentClientConnection(),
  };
  const state = useConnectionDrawer<TorrentClientConnectionT, Form, TorrentClientConnectionInput, TorrentTestInput>({
    kind: meta.value,
    connection,
    emptyForm,
    connectionToForm,
    formToInput,
    formToTest,
    clientValidate,
    testSuccessMsg: "Connection OK — the client responded.",
    mutations,
    onClose,
  });
  const { form, set } = state;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={
        <div className="flex items-center gap-3">
          <TorrentClientLogo kind={meta.value} size={32} />
          {meta.label}
        </div>
      }
    >
      <div className="space-y-5" key={open ? "open" : "closed"} onFocus={undefined}>
        <div className="space-y-3">
          <FieldRow label="URL">
            <Input value={form.url} placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)} />
          </FieldRow>
          <FieldRow label="Username">
            <Input value={form.username} placeholder="admin"
              onChange={(e) => set("username", e.target.value)} />
          </FieldRow>
          <FieldRow label="Password">
            <Input type="password" value={form.password} placeholder="••••••••"
              onChange={(e) => set("password", e.target.value)} />
          </FieldRow>
          <FieldRow label="Timeout" hint="seconds">
            <Input type="number" min={0} className="w-28" value={form.timeout_seconds}
              onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
          </FieldRow>
          <FieldRow label="Categories" hint="comma-separated, excluded">
            <Input value={form.category_exclude} placeholder="keep, archive"
              onChange={(e) => set("category_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label="Tags exclude" hint="comma-separated">
            <Input value={form.tags_exclude} placeholder="forever, triagearr-keep"
              onChange={(e) => set("tags_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label="Flags">
            <div className="flex flex-wrap gap-4">
              <Toggle label="Enabled" checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle label="Delete files (delete_with_files)"
                checked={form.delete_with_files}
                onChange={(v) => set("delete_with_files", v)} />
            </div>
          </FieldRow>
        </div>

        <DrawerActions
          state={state}
          mutations={mutations}
          testDisabled={!form.url}
          testHint="Pings the client with the current credentials without saving."
        />
      </div>
    </Drawer>
  );
}

// ── Main section ───────────────────────────────────────────────────────────────

export function TorrentClientConnectionsSection() {
  const connections = useTorrentClientConnections();
  const [open, setOpen] = useState<string | null>(null);

  const connectionByKind = Object.fromEntries(
    (connections.data?.connections ?? []).map((c) => [c.kind, c]),
  );
  const openMeta = KINDS.find((k) => k.value === open) ?? null;

  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">Torrent connections</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Click a tile to configure. Connections are stored in the database (ADR-0025) — the YAML{" "}
            <code>torrent_clients:</code> block only seeds them on first boot. Changes reload the
            daemon automatically. Only qBittorrent has a backend today; the other tiles are
            placeholders for upcoming clients.
          </p>
        </div>

        {connections.isError && (
          <div className="text-sm text-destructive">
            {String(connections.error ?? "Failed to load connections.")}
          </div>
        )}

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          {KINDS.map((meta) => (
            <KindTile
              key={meta.value}
              meta={meta}
              connection={connectionByKind[meta.value]}
              onClick={() => setOpen(meta.value)}
            />
          ))}
        </div>
      </div>

      {openMeta && (
        <ConnectionDrawer
          key={open}
          meta={openMeta}
          connection={connectionByKind[openMeta.value]}
          open={open === openMeta.value}
          onClose={() => setOpen(null)}
        />
      )}
    </>
  );
}
