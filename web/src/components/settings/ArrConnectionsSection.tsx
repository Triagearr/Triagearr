import { useState } from "react";
import {
  useArrConnections,
  useArrs,
  useCreateArrConnection,
  useUpdateArrConnection,
  useDeleteArrConnection,
  useTestArrConnection,
  type ArrConnectionInput,
} from "@/api/hooks";
import type { ArrConnectionT, ArrViewT } from "@/api/schemas";
import { Drawer } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { cn } from "@/lib/cn";

// ── Kind catalogue ─────────────────────────────────────────────────────────────

type KindMeta = {
  value: string;
  label: string;
  logo: string; // path served from /public
  stub: boolean;
  urlPlaceholder: string;
  categoryHint: string;
};

const KINDS: KindMeta[] = [
  {
    value: "sonarr",
    label: "Sonarr",
    logo: "/logos/sonarr.svg",
    stub: false,
    urlPlaceholder: "http://sonarr:8989",
    categoryHint: "tv-sonarr",
  },
  {
    value: "radarr",
    label: "Radarr",
    logo: "/logos/radarr.svg",
    stub: false,
    urlPlaceholder: "http://radarr:7878",
    categoryHint: "radarr",
  },
  {
    value: "lidarr",
    label: "Lidarr",
    logo: "/logos/lidarr.svg",
    stub: true,
    urlPlaceholder: "http://lidarr:8686",
    categoryHint: "lidarr",
  },
  {
    value: "readarr",
    label: "Readarr",
    logo: "/logos/readarr.svg",
    stub: true,
    urlPlaceholder: "http://readarr:8787",
    categoryHint: "readarr",
  },
  {
    value: "whisparr_v2",
    label: "Whisparr v2",
    logo: "/logos/whisparr.svg",
    stub: true,
    urlPlaceholder: "http://whisparr:6969",
    categoryHint: "whisparr",
  },
  {
    value: "whisparr_v3",
    label: "Whisparr v3",
    logo: "/logos/whisparr.svg",
    stub: true,
    urlPlaceholder: "http://whisparr:6969",
    categoryHint: "whisparr",
  },
];

// ── Logo helper (wraps the shared ArrLogo component) ─────────────────────────

import { ArrLogo as SharedArrLogo } from "@/components/ArrLogo";

// ── Kind tile — same horizontal layout as the dashboard health cards ───────────

type TileStatus = "unconfigured" | "disabled" | "unhealthy" | "healthy";

function tileStatus(
  connection: ArrConnectionT | undefined,
  arrView: ArrViewT | undefined,
): TileStatus {
  if (!connection) return "unconfigured";
  if (!connection.enabled) return "disabled";
  if (arrView?.healthy) return "healthy";
  return "unhealthy";
}

function KindTile({
  meta,
  connection,
  arrView,
  onClick,
}: {
  meta: KindMeta;
  connection: ArrConnectionT | undefined;
  arrView: ArrViewT | undefined;
  onClick: () => void;
}) {
  const status = tileStatus(connection, arrView);

  const stateClass: Record<TileStatus, string> = {
    unconfigured: "state-unconfigured",
    disabled:     "state-disabled",
    unhealthy:    "state-down",
    healthy:      "state-healthy",
  };

  const statusLabel: Record<TileStatus, string> = {
    unconfigured: "Not configured",
    disabled:     "Disabled",
    unhealthy:    "Unreachable",
    healthy:      "Connected",
  };

  const statusEl = (
    <div className="arr-tile-state">
      {status === "healthy"      && <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>Connected</span></>}
      {status === "unhealthy"    && <><span className="dot red pulse" /><span style={{ color: "var(--red-2)" }}>Unreachable</span></>}
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
      {/* Tile header: logo + label + status */}
      <div className="arr-tile-head">
        <SharedArrLogo kind={meta.value} size={36} greyscale={meta.stub} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name">{meta.label}</div>
          {meta.stub && (
            <div className="arr-tile-tag" style={{ color: "var(--fg-4)", fontSize: 10 }}>coming soon</div>
          )}
        </div>
        {statusEl}
      </div>

      {/* URL */}
      {connection && (
        <div className="arr-tile-url">{connection.url}</div>
      )}

      {/* Chips: Enabled / Poll / Act */}
      {connection && (
        <div className="arr-tile-toggles">
          <span className={cn("arr-chip", connection.enabled && "on")}>
            <span className="arr-chip-dot" /> Enabled
          </span>
          <span className={cn("arr-chip", connection.poll && "on")}>
            <span className="arr-chip-dot" /> Poll
          </span>
          <span className={cn("arr-chip", connection.act && "on danger")}>
            <span className="arr-chip-dot" /> Act
            {connection.act && (
              <span style={{ marginLeft: 3, fontSize: 9.5, fontFamily: "'Geist Mono',ui-monospace,monospace", color: "var(--red-2)" }}>LIVE</span>
            )}
          </span>
        </div>
      )}

      {/* Last error */}
      {arrView?.last_error && (
        <div className="arr-tile-error">{arrView.last_error}</div>
      )}

      {/* Footer: not configured prompt */}
      {!connection && !meta.stub && (
        <div className="arr-tile-empty">
          <span>Click to configure</span>
        </div>
      )}

      {/* Status label for disabled/unconfigured when connection exists */}
      {connection && status === "disabled" && (
        <div className="arr-tile-foot" style={{ color: "var(--fg-4)", fontSize: 11 }}>
          {statusLabel[status]}
        </div>
      )}
    </button>
  );
}

// ── Connection form (inside Drawer) ────────────────────────────────────────────

type Form = {
  url: string;
  api_key: string;
  enabled: boolean;
  poll: boolean;
  act: boolean;
  tags_exclude: string;
  categories_only: string;
  timeout_seconds: number;
};

function emptyForm(meta: KindMeta): Form {
  return {
    url: "",
    api_key: "",
    enabled: true,
    poll: true,
    act: false,
    tags_exclude: "",
    categories_only: meta.categoryHint,
    timeout_seconds: 30,
  };
}

function connectionToForm(c: ArrConnectionT): Form {
  return {
    url: c.url,
    api_key: c.api_key,
    enabled: c.enabled,
    poll: c.poll,
    act: c.act,
    tags_exclude: (c.tags_exclude ?? []).join(", "),
    categories_only: (c.categories_only ?? []).join(", "),
    timeout_seconds: c.timeout_seconds,
  };
}

function splitList(s: string): string[] {
  return s
    .split(",")
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
}

function formToInput(kind: string, f: Form): ArrConnectionInput {
  return {
    kind,
    url: f.url.trim(),
    api_key: f.api_key,
    enabled: f.enabled,
    poll: f.poll,
    act: f.act,
    tags_exclude: splitList(f.tags_exclude),
    categories_only: splitList(f.categories_only),
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
    if (!f.api_key) return "API key is required when the connection is enabled.";
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
  arrView,
  open,
  onClose,
}: {
  meta: KindMeta;
  connection: ArrConnectionT | undefined;
  arrView: ArrViewT | undefined;
  open: boolean;
  onClose: () => void;
}) {
  const isDraft = connection === undefined;
  const original = connection ? connectionToForm(connection) : emptyForm(meta);
  const [form, setForm] = useState<Form>(original);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const create = useCreateArrConnection();
  const update = useUpdateArrConnection();
  const del = useDeleteArrConnection();
  const test = useTestArrConnection();

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
        api_key: form.api_key,
        timeout_seconds: form.timeout_seconds,
      });
      setTestResult({ ok: true, msg: "Connection OK — the instance responded." });
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
          <SharedArrLogo kind={meta.value} size={32} />
          {meta.label}
        </div>
      }
    >
      <div className="space-y-5" key={open ? "open" : "closed"} onFocus={undefined}>
        {/* Health status banner (only when connection is saved and the registry has seen it) */}
        {!isDraft && arrView && (
          <div
            className={cn(
              "flex items-center gap-2 rounded-lg border px-4 py-2.5 text-sm",
              arrView.healthy
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                : "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400",
            )}
          >
            <span className="font-medium">{arrView.healthy ? "Reachable" : "Unreachable"}</span>
            {arrView.last_error && (
              <span className="text-xs opacity-80 truncate">— {arrView.last_error}</span>
            )}
          </div>
        )}

        {/* Form fields */}
        <div className="space-y-3">
          <FieldRow label="URL">
            <Input
              value={form.url}
              placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="API key">
            <Input
              type="password"
              value={form.api_key}
              placeholder="••••••••"
              onChange={(e) => set("api_key", e.target.value)}
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
          <FieldRow label="Tags exclude" hint="comma-separated">
            <Input
              value={form.tags_exclude}
              placeholder="keep, archive"
              onChange={(e) => set("tags_exclude", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Categories" hint="comma-separated, optional">
            <Input
              value={form.categories_only}
              placeholder={meta.categoryHint}
              onChange={(e) => set("categories_only", e.target.value)}
            />
          </FieldRow>
          <FieldRow label="Flags">
            <div className="flex flex-wrap gap-4">
              <Toggle label="Enabled" checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle label="Poll" checked={form.poll} onChange={(v) => set("poll", v)} />
              <Toggle
                label="Act (allow deletes)"
                checked={form.act}
                onChange={(v) => set("act", v)}
              />
            </div>
          </FieldRow>
          {form.act && (
            <p className="text-xs text-destructive/90 sm:pl-[9rem]">
              Act is on — Triagearr may delete media from this instance during live runs.
            </p>
          )}
        </div>

        {/* Errors */}
        {error && (
          <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
            {error}
          </div>
        )}

        {/* Save / Delete */}
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

        {/* Test connection */}
        <div className="space-y-2 pt-2 border-t border-border">
          <div className="text-xs text-muted-foreground">
            Pings the instance with the current credentials without saving.
          </div>
          <Button
            variant="outline"
            onClick={onTest}
            disabled={test.isPending || !form.url || !form.api_key}
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

export function ArrConnectionsSection() {
  const connections = useArrConnections();
  const arrs = useArrs();
  const [open, setOpen] = useState<OpenKind>(null);

  // Index connections by kind (one per kind post-refactor).
  const connectionByKind = Object.fromEntries(
    (connections.data?.connections ?? []).map((c) => [c.kind, c]),
  );

  // Index arr health views by type (= kind).
  const arrViewByKind = Object.fromEntries((arrs.data?.arrs ?? []).map((a) => [a.type, a]));

  const openMeta = KINDS.find((k) => k.value === open) ?? null;

  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">*arr connections</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Click a tile to configure. Connections are stored in the database (ADR-0022) — the YAML{" "}
            <code>arrs:</code> block only seeds them on first boot. Changes reload the daemon
            automatically.
          </p>
        </div>

        {connections.isError && (
          <div className="text-sm text-destructive">
            {String(connections.error ?? "Failed to load connections.")}
          </div>
        )}

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
          {KINDS.map((meta) => (
            <KindTile
              key={meta.value}
              meta={meta}
              connection={connectionByKind[meta.value]}
              arrView={arrViewByKind[meta.value]}
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
          arrView={arrViewByKind[openMeta.value]}
          open={open === openMeta.value}
          onClose={() => setOpen(null)}
        />
      )}
    </>
  );
}
