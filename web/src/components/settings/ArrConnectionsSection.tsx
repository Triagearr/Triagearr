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
import { Input } from "@/components/ui/Input";
import { ArrLogo as SharedArrLogo } from "@/components/ArrLogo";
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
  logo: string;
  stub: boolean;
  urlPlaceholder: string;
  categoryHint: string;
};

const KINDS: KindMeta[] = [
  { value: "sonarr",      label: "Sonarr",      logo: "/logos/sonarr.svg",   stub: false, urlPlaceholder: "http://sonarr:8989",   categoryHint: "tv-sonarr" },
  { value: "radarr",      label: "Radarr",      logo: "/logos/radarr.svg",   stub: false, urlPlaceholder: "http://radarr:7878",   categoryHint: "radarr" },
  { value: "lidarr",      label: "Lidarr",      logo: "/logos/lidarr.svg",   stub: true,  urlPlaceholder: "http://lidarr:8686",   categoryHint: "lidarr" },
  { value: "readarr",     label: "Readarr",     logo: "/logos/readarr.svg",  stub: true,  urlPlaceholder: "http://readarr:8787",  categoryHint: "readarr" },
  { value: "whisparr_v2", label: "Whisparr v2", logo: "/logos/whisparr.svg", stub: true,  urlPlaceholder: "http://whisparr:6969", categoryHint: "whisparr" },
  { value: "whisparr_v3", label: "Whisparr v3", logo: "/logos/whisparr.svg", stub: true,  urlPlaceholder: "http://whisparr:6969", categoryHint: "whisparr" },
];

// ── Kind tile ──────────────────────────────────────────────────────────────────

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

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={meta.stub}
      className={cn("arr-tile", stateClass[status], meta.stub && "opacity-50 cursor-not-allowed")}
    >
      <div className="arr-tile-head">
        <SharedArrLogo kind={meta.value} size={36} greyscale={meta.stub} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name">{meta.label}</div>
          {meta.stub && (
            <div className="arr-tile-tag" style={{ color: "var(--fg-4)", fontSize: 10 }}>coming soon</div>
          )}
        </div>
        <div className="arr-tile-state">
          {status === "healthy"      && <><span className="dot green" /><span style={{ color: "var(--green-2)" }}>Connected</span></>}
          {status === "unhealthy"    && <><span className="dot red pulse" /><span style={{ color: "var(--red-2)" }}>Unreachable</span></>}
          {status === "disabled"     && <><span className="dot" /><span style={{ color: "var(--fg-3)" }}>Disabled</span></>}
          {status === "unconfigured" && <span style={{ color: "var(--fg-3)" }}>Not configured</span>}
        </div>
      </div>

      {connection && <div className="arr-tile-url">{connection.url}</div>}

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

      {arrView?.last_error && <div className="arr-tile-error">{arrView.last_error}</div>}

      {!connection && !meta.stub && (
        <div className="arr-tile-empty"><span>Click to configure</span></div>
      )}

      {connection && status === "disabled" && (
        <div className="arr-tile-foot" style={{ color: "var(--fg-4)", fontSize: 11 }}>
          {statusLabel[status]}
        </div>
      )}
    </button>
  );
}

// ── Form ────────────────────────────────────────────────────────────────────────

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

const emptyForm = (meta: KindMeta): Form => ({
  url: "",
  api_key: "",
  enabled: true,
  poll: true,
  act: false,
  tags_exclude: "",
  categories_only: meta.categoryHint,
  timeout_seconds: 30,
});

const connectionToForm = (c: ArrConnectionT): Form => ({
  url: c.url,
  api_key: c.api_key,
  enabled: c.enabled,
  poll: c.poll,
  act: c.act,
  tags_exclude: (c.tags_exclude ?? []).join(", "),
  categories_only: (c.categories_only ?? []).join(", "),
  timeout_seconds: c.timeout_seconds,
});

const formToInput = (kind: string, f: Form): ArrConnectionInput => ({
  kind,
  url: f.url.trim(),
  api_key: f.api_key,
  enabled: f.enabled,
  poll: f.poll,
  act: f.act,
  tags_exclude: splitList(f.tags_exclude),
  categories_only: splitList(f.categories_only),
  timeout_seconds: f.timeout_seconds,
});

type ArrTestInput = { kind: string; url: string; api_key: string; timeout_seconds: number };

const formToTest = (kind: string, f: Form): ArrTestInput => ({
  kind,
  url: f.url.trim(),
  api_key: f.api_key,
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
    if (!f.api_key) return "API key is required when the connection is enabled.";
  }
  if (f.timeout_seconds < 0) return "Timeout must be zero or positive.";
  return null;
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
  const mutations: ConnectionMutations<ArrConnectionInput, ArrTestInput> = {
    create: useCreateArrConnection(),
    update: useUpdateArrConnection(),
    del:    useDeleteArrConnection(),
    test:   useTestArrConnection(),
  };
  const state = useConnectionDrawer<ArrConnectionT, Form, ArrConnectionInput, ArrTestInput>({
    kind: meta.value,
    connection,
    emptyForm: () => emptyForm(meta),
    connectionToForm,
    formToInput,
    formToTest,
    clientValidate,
    testSuccessMsg: "Connection OK — the instance responded.",
    mutations,
    onClose,
  });
  const { form, set, isDraft } = state;

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

        <div className="space-y-3">
          <FieldRow label="URL">
            <Input value={form.url} placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)} />
          </FieldRow>
          <FieldRow label="API key">
            <Input type="password" value={form.api_key} placeholder="••••••••"
              onChange={(e) => set("api_key", e.target.value)} />
          </FieldRow>
          <FieldRow label="Timeout" hint="seconds">
            <Input type="number" min={0} className="w-28" value={form.timeout_seconds}
              onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
          </FieldRow>
          <FieldRow label="Tags exclude" hint="comma-separated">
            <Input value={form.tags_exclude} placeholder="keep, archive"
              onChange={(e) => set("tags_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label="Categories" hint="comma-separated, optional">
            <Input value={form.categories_only} placeholder={meta.categoryHint}
              onChange={(e) => set("categories_only", e.target.value)} />
          </FieldRow>
          <FieldRow label="Flags">
            <div className="flex flex-wrap gap-4">
              <Toggle label="Enabled" checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle label="Poll" checked={form.poll} onChange={(v) => set("poll", v)} />
              <Toggle label="Act (allow deletes)" checked={form.act} onChange={(v) => set("act", v)} />
            </div>
          </FieldRow>
          {form.act && (
            <p className="text-xs text-destructive/90 sm:pl-[9rem]">
              Act is on — Triagearr may delete media from this instance during live runs.
            </p>
          )}
        </div>

        <DrawerActions
          state={state}
          mutations={mutations}
          testDisabled={!form.url || !form.api_key}
          testHint="Pings the instance with the current credentials without saving."
        />
      </div>
    </Drawer>
  );
}

// ── Main section ───────────────────────────────────────────────────────────────

export function ArrConnectionsSection() {
  const connections = useArrConnections();
  const arrs = useArrs();
  const [open, setOpen] = useState<string | null>(null);

  const connectionByKind = Object.fromEntries(
    (connections.data?.connections ?? []).map((c) => [c.kind, c]),
  );
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
