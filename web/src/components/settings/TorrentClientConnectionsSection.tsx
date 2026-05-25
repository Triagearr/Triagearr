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
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { TorrentClientLogo } from "@/components/TorrentClientLogo";
import { cn } from "@/lib/cn";

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

  const statusEl = (
    <div className="arr-tile-state">
      {status === "configured"   && <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>Configured</span></>}
      {status === "disabled"     && <><span className="dot" /><span style={{ color: "var(--fg-3)" }}>Disabled</span></>}
      {status === "unconfigured" && <span style={{ color: "var(--fg-3)" }}>Not configured</span>}
    </div>
  );

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
        {statusEl}
      </div>

      {connection && (
        <div className="arr-tile-url">{connection.url}</div>
      )}

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
        <div className="arr-tile-empty">
          <span>Click to configure</span>
        </div>
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

function emptyForm(): Form {
  return {
    url: "",
    username: "",
    password: "",
    enabled: true,
    category_exclude: "",
    tags_exclude: "",
    delete_with_files: true,
    timeout_seconds: 30,
  };
}

function connectionToForm(c: TorrentClientConnectionT): Form {
  return {
    url: c.url,
    username: c.username,
    password: c.password,
    enabled: c.enabled,
    category_exclude: (c.category_exclude ?? []).join(", "),
    tags_exclude: (c.tags_exclude ?? []).join(", "),
    delete_with_files: c.delete_with_files,
    timeout_seconds: c.timeout_seconds,
  };
}

function splitList(s: string): string[] {
  return s
    .split(",")
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
}

function formToInput(kind: string, f: Form): TorrentClientConnectionInput {
  return {
    kind,
    url: f.url.trim(),
    username: f.username,
    password: f.password,
    enabled: f.enabled,
    category_exclude: splitList(f.category_exclude),
    tags_exclude: splitList(f.tags_exclude),
    delete_with_files: f.delete_with_files,
    timeout_seconds: f.timeout_seconds,
  };
}

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

function FieldRow({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-[9rem_1fr] sm:items-center gap-1 sm:gap-3 text-sm">
      <label className="text-muted-foreground">
        {label}
        {hint && <span className="block text-xs text-muted-foreground/70">{hint}</span>}
      </label>
      <div>{children}</div>
    </div>
  );
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <label className="inline-flex items-center gap-2 text-sm cursor-pointer">
      <input
        type="checkbox"
        className="h-4 w-4 accent-primary"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      {label}
    </label>
  );
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
  const isDraft = connection === undefined;
  const original = connection ? connectionToForm(connection) : emptyForm();
  const [form, setForm] = useState<Form>(original);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const create = useCreateTorrentClientConnection();
  const update = useUpdateTorrentClientConnection();
  const del = useDeleteTorrentClientConnection();
  const test = useTestTorrentClientConnection();

  const set = <K extends keyof Form>(key: K, value: Form[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const dirty = isDraft || JSON.stringify(form) !== JSON.stringify(original);
  const busy = create.isPending || update.isPending || del.isPending;

  const onSave = async () => {
    setError(null);
    const msg = clientValidate(form);
    if (msg) {
      setError(msg);
      return;
    }
    try {
      if (isDraft) {
        await create.mutateAsync(formToInput(meta.value, form));
        onClose();
      } else {
        await update.mutateAsync({ kind: meta.value, input: formToInput(meta.value, form) });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const onTest = async () => {
    setTestResult(null);
    try {
      await test.mutateAsync({
        kind: meta.value,
        url: form.url.trim(),
        username: form.username,
        password: form.password,
        timeout_seconds: form.timeout_seconds,
      });
      setTestResult({ ok: true, msg: "Connection OK — the client responded." });
    } catch (e) {
      setTestResult({ ok: false, msg: e instanceof Error ? e.message : String(e) });
    }
  };

  const onDelete = async () => {
    if (isDraft) {
      onClose();
      return;
    }
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setError(null);
    try {
      await del.mutateAsync(connection.kind);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setConfirmDelete(false);
    }
  };

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
            <Input
              value={form.url}
              placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Username">
            <Input
              value={form.username}
              placeholder="admin"
              onChange={(e) => set("username", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Password">
            <Input
              type="password"
              value={form.password}
              placeholder="••••••••"
              onChange={(e) => set("password", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Timeout" hint="seconds">
            <Input
              type="number"
              min={0}
              className="w-28"
              value={form.timeout_seconds}
              onChange={(e) => set("timeout_seconds", Number(e.target.value))}
            />
          </FieldRow>
          <FieldRow label="Categories" hint="comma-separated, excluded">
            <Input
              value={form.category_exclude}
              placeholder="keep, archive"
              onChange={(e) => set("category_exclude", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Tags exclude" hint="comma-separated">
            <Input
              value={form.tags_exclude}
              placeholder="forever, triagearr-keep"
              onChange={(e) => set("tags_exclude", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Flags">
            <div className="flex flex-wrap gap-4">
              <Toggle label="Enabled" checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle
                label="Delete files (delete_with_files)"
                checked={form.delete_with_files}
                onChange={(v) => set("delete_with_files", v)}
              />
            </div>
          </FieldRow>
        </div>

        {error && (
          <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
            {error}
          </div>
        )}

        <div className="flex items-center gap-2 pt-2 border-t border-border">
          <Button onClick={onSave} disabled={!dirty || busy}>
            {create.isPending || update.isPending ? "Saving…" : isDraft ? "Create" : "Save"}
          </Button>
          <Button
            variant={confirmDelete ? "destructive" : "ghost"}
            onClick={onDelete}
            disabled={busy}
            className="ml-auto"
          >
            {isDraft ? "Cancel" : confirmDelete ? "Confirm delete?" : "Delete"}
          </Button>
        </div>

        <div className="space-y-2 pt-2 border-t border-border">
          <div className="text-xs text-muted-foreground">
            Pings the client with the current credentials without saving.
          </div>
          <Button
            variant="outline"
            onClick={onTest}
            disabled={test.isPending || !form.url}
          >
            {test.isPending ? "Testing…" : "Test connection"}
          </Button>
          {testResult && (
            <div
              className={cn(
                "text-sm rounded-md border p-2",
                testResult.ok
                  ? "text-foreground border-border"
                  : "text-destructive border-destructive/50",
              )}
            >
              {testResult.msg}
            </div>
          )}
        </div>
      </div>
    </Drawer>
  );
}

// ── Main section ───────────────────────────────────────────────────────────────

type OpenKind = string | null;

export function TorrentClientConnectionsSection() {
  const connections = useTorrentClientConnections();
  const [open, setOpen] = useState<OpenKind>(null);

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
