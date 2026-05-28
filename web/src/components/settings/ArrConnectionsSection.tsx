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
import { m } from "@/paraglide/messages";
import {
  FieldRow,
  Toggle,
  splitList,
  useConnectionDrawer,
  DrawerActions,
  ConnectionKindTile,
  ConnectionsSectionShell,
  validateConnUrl,
  validateTimeout,
  validatePublicUrl,
  noAutofillProps,
  type ConnectionMutations,
  type VisualTileStatus,
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

const STATUS_TEXT: Record<TileStatus, () => string> = {
  unconfigured: m.common_not_configured,
  disabled:     m.common_disabled,
  unhealthy:    m.settings_status_unreachable,
  healthy:      m.settings_status_connected,
};

const VISUAL: Record<TileStatus, VisualTileStatus> = {
  unconfigured: "unconfigured",
  disabled:     "disabled",
  unhealthy:    "unhealthy",
  healthy:      "healthy",
};

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
  return (
    <ConnectionKindTile
      label={meta.label}
      stub={meta.stub}
      connected={!!connection}
      status={VISUAL[status]}
      statusText={STATUS_TEXT[status]()}
      url={connection?.public_url || connection?.url}
      chips={connection ? [
        { label: m.settings_chip_enabled(), on: connection.enabled },
        { label: m.settings_chip_poll(),    on: connection.poll },
        { label: m.settings_chip_act(),     on: connection.act, danger: true, liveTag: true },
      ] : undefined}
      lastError={arrView?.last_error}
      footNote={status === "disabled" ? m.common_disabled() : undefined}
      renderLogo={(size) => <SharedArrLogo kind={meta.value} size={size} greyscale={meta.stub} />}
      onClick={onClick}
    />
  );
}

// ── Form ────────────────────────────────────────────────────────────────────────

type Form = {
  url: string;
  public_url: string;
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
  public_url: "",
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
  public_url: c.public_url,
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
  public_url: f.public_url.trim(),
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
    const urlErr = validateConnUrl(f.url);
    if (urlErr) return urlErr;
    if (!f.api_key) return m.settings_validate_api_key_required();
  }
  return validateTimeout(f.timeout_seconds) ?? validatePublicUrl(f.public_url);
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
    testSuccessMsg: m.settings_arr_test_success(),
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
            <span className="font-medium">{arrView.healthy ? m.settings_arr_reachable() : m.settings_status_unreachable()}</span>
            {arrView.last_error && (
              <span className="text-xs opacity-80 truncate">— {arrView.last_error}</span>
            )}
          </div>
        )}

        <div className="space-y-3">
          <FieldRow label={m.settings_field_url()}>
            <Input value={form.url} placeholder={meta.urlPlaceholder}
              onChange={(e) => set("url", e.target.value)} {...noAutofillProps} />
          </FieldRow>
          <FieldRow label={m.settings_field_public_url()} hint={m.settings_field_public_url_hint()}>
            <Input value={form.public_url} placeholder={`https://${meta.value}.example.com`}
              onChange={(e) => set("public_url", e.target.value)} {...noAutofillProps} />
          </FieldRow>
          <FieldRow label={m.settings_field_api_key()}>
            <Input type="password" value={form.api_key} placeholder="••••••••"
              onChange={(e) => set("api_key", e.target.value)} {...noAutofillProps} />
          </FieldRow>
          <FieldRow label={m.settings_field_timeout()} hint={m.settings_field_timeout_hint()}>
            <Input type="number" min={0} className="w-28" value={form.timeout_seconds}
              onChange={(e) => set("timeout_seconds", Number(e.target.value))} />
          </FieldRow>
          <FieldRow label={m.settings_field_tags_exclude()} hint={m.settings_field_comma_separated()}>
            <Input value={form.tags_exclude} placeholder="keep, archive"
              onChange={(e) => set("tags_exclude", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_categories()} hint={m.settings_field_categories_hint()}>
            <Input value={form.categories_only} placeholder={meta.categoryHint}
              onChange={(e) => set("categories_only", e.target.value)} />
          </FieldRow>
          <FieldRow label={m.settings_field_flags()}>
            <div className="flex flex-wrap gap-4">
              <Toggle label={m.settings_toggle_enabled()} checked={form.enabled} onChange={(v) => set("enabled", v)} />
              <Toggle label={m.settings_toggle_poll()} checked={form.poll} onChange={(v) => set("poll", v)} />
              <Toggle label={m.settings_toggle_act_allow_deletes()} checked={form.act} onChange={(v) => set("act", v)} />
            </div>
          </FieldRow>
          {form.act && (
            <p className="text-xs text-destructive/90 sm:pl-[9rem]">
              {m.settings_arr_act_warning()}
            </p>
          )}
        </div>

        <DrawerActions
          state={state}
          mutations={mutations}
          testDisabled={!form.url || !form.api_key}
          testHint={m.settings_arr_test_hint()}
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
    <ConnectionsSectionShell
      title={m.settings_arr_title()}
      description={m.settings_arr_description()}
      error={connections.isError ? (connections.error ?? m.settings_arr_load_failed()) : undefined}
      drawer={openMeta && (
        <ConnectionDrawer
          key={open}
          meta={openMeta}
          connection={connectionByKind[openMeta.value]}
          arrView={arrViewByKind[openMeta.value]}
          open={open === openMeta.value}
          onClose={() => setOpen(null)}
        />
      )}
    >
      {KINDS.map((meta) => (
        <KindTile
          key={meta.value}
          meta={meta}
          connection={connectionByKind[meta.value]}
          arrView={arrViewByKind[meta.value]}
          onClick={() => setOpen(meta.value)}
        />
      ))}
    </ConnectionsSectionShell>
  );
}
