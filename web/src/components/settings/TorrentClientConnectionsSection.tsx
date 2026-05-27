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
import {
  FieldRow,
  Toggle,
  splitList,
  useConnectionDrawer,
  DrawerActions,
  ConnectionKindTile,
  type ConnectionMutations,
  type VisualTileStatus,
} from "./ConnectionsCommon";
import { m } from "@/paraglide/messages";

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

const VISUAL: Record<TileStatus, VisualTileStatus> = {
  unconfigured: "unconfigured",
  disabled:     "disabled",
  configured:   "healthy",
};

const STATUS_TEXT: Record<TileStatus, () => string> = {
  configured:   m.settings_status_configured,
  disabled:     m.common_disabled,
  unconfigured: m.common_not_configured,
};

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
  return (
    <ConnectionKindTile
      label={meta.label}
      stub={meta.stub}
      connected={!!connection}
      status={VISUAL[status]}
      statusText={STATUS_TEXT[status]()}
      url={connection?.url}
      chips={connection ? [
        { label: m.settings_chip_enabled(),      on: connection.enabled },
        { label: m.settings_chip_delete_files(), on: connection.delete_with_files, danger: true },
      ] : undefined}
      renderLogo={(size) => <TorrentClientLogo kind={meta.value} size={size} greyscale={meta.stub} />}
      onClick={onClick}
    />
  );
}

// ── Form ───────────────────────────────────────────────────────────────────────

type Form = {
  url: string;
  public_url: string;
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
  public_url: "",
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
  public_url: c.public_url,
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
  public_url: f.public_url.trim(),
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
    if (!f.url.trim()) return m.settings_validate_url_required();
    try {
      const u = new URL(f.url);
      if (!u.host) return m.settings_validate_url_host();
    } catch {
      return m.settings_validate_url_invalid();
    }
  }
  if (f.timeout_seconds < 0) return m.settings_validate_timeout_positive();
  if (f.public_url.trim()) {
    try {
      const u = new URL(f.public_url.trim());
      if (!u.host || (u.protocol !== "http:" && u.protocol !== "https:")) {
        return m.settings_validate_public_url_absolute();
      }
    } catch {
      return m.settings_validate_public_url_invalid();
    }
  }
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
    testSuccessMsg: m.settings_torrent_test_success(),
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
          <FieldRow label={m.settings_field_url()}>
            <Input value={form.url} placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_public_url()} hint={m.settings_field_public_url_hint()}>
            <Input value={form.public_url} placeholder={`https://${meta.value}.example.com`}
              onChange={(e) => set("public_url", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_username()}>
            <Input value={form.username} placeholder="admin"
              onChange={(e) => set("username", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_password()}>
            <Input type="password" value={form.password} placeholder="••••••••"
              onChange={(e) => set("password", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_timeout()} hint={m.settings_field_timeout_hint()}>
            <Input type="number" min={0} className="w-28" value={form.timeout_seconds}
              onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
          </FieldRow>
          <FieldRow label={m.settings_field_categories()} hint={m.settings_field_categories_excluded_hint()}>
            <Input value={form.category_exclude} placeholder="keep, archive"
              onChange={(e) => set("category_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_tags_exclude()} hint={m.settings_field_comma_separated()}>
            <Input value={form.tags_exclude} placeholder="forever, triagearr-keep"
              onChange={(e) => set("tags_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_flags()}>
            <div className="flex flex-wrap gap-4">
              <Toggle label={m.settings_toggle_enabled()} checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle label={m.settings_toggle_delete_with_files()}
                checked={form.delete_with_files}
                onChange={(v) => set("delete_with_files", v)} />
            </div>
          </FieldRow>
        </div>

        <DrawerActions
          state={state}
          mutations={mutations}
          testDisabled={!form.url}
          testHint={m.settings_torrent_test_hint()}
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
          <h2 className="text-lg font-semibold">{m.settings_torrent_title()}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {m.settings_torrent_description()}
          </p>
        </div>

        {connections.isError && (
          <div className="text-sm text-destructive">
            {String(connections.error ?? m.settings_arr_load_failed())}
          </div>
        )}

        <div className="arr-tile-grid">
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
