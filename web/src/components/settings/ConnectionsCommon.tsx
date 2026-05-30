import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Check, X } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";

// ConnectionsSectionShell wraps the outer scaffold (title, description, error
// banner, tile grid) shared by ArrConnectionsSection and
// TorrentClientConnectionsSection. Body holds the tiles; drawer holds the
// editor portal so the open state stays adjacent to the grid.
export function ConnectionsSectionShell({
  title,
  description,
  error,
  children,
  drawer,
}: {
  title: string;
  description: string;
  error?: unknown;
  children: ReactNode;
  drawer?: ReactNode;
}) {
  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{title}</h2>
          <p className="text-sm text-muted-foreground mt-1">{description}</p>
        </div>

        {error != null && error !== false && (
          <div className="text-sm text-destructive">{String(error)}</div>
        )}

        <div className="arr-tile-grid">{children}</div>
      </div>

      {drawer}
    </>
  );
}

// Spread on credential/URL fields in connection drawers to keep password
// managers (LastPass, 1Password, Bitwarden) from clobbering them on save —
// they detect the *arr API-key field as a password and overwrite both it and
// the public URL when refilling the page form.
export const noAutofillProps = {
  autoComplete: "off",
  "data-lpignore": "true",
  "data-1p-ignore": "true",
  "data-bwignore": "true",
  "data-form-type": "other",
} as const;

// ── Shared connection tile ──────────────────────────────────────────────────────

export type ChipDef = {
  label: string;
  on: boolean;
  danger?: boolean;
  liveTag?: boolean;
};

export type VisualTileStatus = "unconfigured" | "disabled" | "healthy" | "unhealthy";

const TILE_STATE_CLASS: Record<VisualTileStatus, string> = {
  unconfigured: "state-unconfigured",
  disabled:     "state-disabled",
  unhealthy:    "state-down",
  healthy:      "state-healthy",
};

const STATUS_COLOR: Record<VisualTileStatus, string> = {
  healthy:      "var(--green-2)",
  unhealthy:    "var(--red-2)",
  disabled:     "var(--fg-3)",
  unconfigured: "var(--fg-3)",
};

export function ConnectionKindTile({
  label,
  subtitle,
  stub,
  connected,
  status,
  statusText,
  url,
  chips,
  lastError,
  footNote,
  renderLogo,
  onClick,
}: {
  label: string;
  subtitle?: string;
  stub?: boolean;
  connected: boolean;
  status: VisualTileStatus;
  statusText: string;
  url?: string;
  chips?: ChipDef[];
  lastError?: string;
  footNote?: string;
  renderLogo: (size: number) => ReactNode;
  onClick: () => void;
}) {
  const color = STATUS_COLOR[status];
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={stub}
      className={cn("arr-tile", TILE_STATE_CLASS[status], stub && "opacity-50 cursor-not-allowed")}
    >
      <div className="arr-tile-head">
        {renderLogo(36)}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="arr-tile-name">{label}</div>
          {stub
            ? <div className="arr-tile-tag" style={{ color: "var(--fg-4)", fontSize: 10 }}>{m.settings_coming_soon()}</div>
            : subtitle
              ? <div className="arr-tile-tag">{subtitle}</div>
              : null
          }
        </div>
        <div className="arr-tile-state" title={statusText}>
          {status === "unconfigured" ? (
            <><span className="dot" style={{ background: "transparent", border: "1px dashed var(--border-2)" }} /><span className="arr-tile-state-text" style={{ color }}>{statusText}</span></>
          ) : status === "unhealthy" ? (
            <><span className="dot red pulse" /><span className="arr-tile-state-text" style={{ color }}>{statusText}</span></>
          ) : status === "healthy" ? (
            <><span className="dot green" /><span className="arr-tile-state-text" style={{ color }}>{statusText}</span></>
          ) : (
            <><span className="dot" /><span className="arr-tile-state-text" style={{ color }}>{statusText}</span></>
          )}
        </div>
      </div>

      {url && <div className="arr-tile-url">{url}</div>}

      {chips && chips.length > 0 && (
        <div className="arr-tile-toggles">
          {chips.map((chip) => (
            <span key={chip.label} className={cn("arr-chip", chip.on && "on", chip.on && chip.danger && "danger")}>
              <span className="arr-chip-dot" /> {chip.label}
              {chip.on && chip.liveTag && (
                <span style={{ marginLeft: 3, fontSize: 9.5, fontFamily: "'Geist Mono',ui-monospace,monospace", color: "var(--red-2)" }}>LIVE</span>
              )}
            </span>
          ))}
        </div>
      )}

      {lastError && <div className="arr-tile-error">{lastError}</div>}

      {!connected && !stub && (
        <div className="arr-tile-empty"><span>{m.settings_click_to_configure()}</span></div>
      )}

      {footNote && (
        <div className="arr-tile-foot" style={{ color: "var(--fg-4)", fontSize: 11 }}>
          {footNote}
        </div>
      )}
    </button>
  );
}

// Shared form primitives + drawer-state hook for the *arr-connections and
// torrent-client-connections settings sections. The two sections only diverge
// on their kind catalog, tile visuals and form field set; everything below is
// identical, so it lives here.

export function FieldRow({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
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

export function Toggle({
  checked,
  onChange,
  label,
  danger = false,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  danger?: boolean;
}) {
  // Only flag the destructive tint while the flag is actually on — an unchecked
  // destructive toggle is harmless and shouldn't read as a warning.
  const hot = danger && checked;
  return (
    <label className={cn("inline-flex items-center gap-2 text-sm cursor-pointer", hot && "text-rose-700 dark:text-rose-300 font-medium")}>
      <input
        type="checkbox"
        className={cn("h-4 w-4", hot ? "accent-rose-600" : "accent-primary")}
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      {label}
    </label>
  );
}

// ReachabilityBanner shows the live health of a configured connection at the top
// of its drawer. Shared by the *arr and torrent-client sections so the status
// styling (and its light-mode contrast) stays in one place.
export function ReachabilityBanner({ healthy, lastError }: { healthy: boolean; lastError?: string }) {
  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-lg border px-4 py-2.5 text-sm",
        healthy
          ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
          : "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
      )}
    >
      <span className="font-medium">{healthy ? m.settings_arr_reachable() : m.settings_status_unreachable()}</span>
      {lastError && <span className="text-xs opacity-80 truncate">— {lastError}</span>}
    </div>
  );
}

export function splitList(s: string): string[] {
  return s
    .split(",")
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
}

// Shared field validators for the two connection sections. They diverge only
// on the credential field (api_key vs username/password), so the URL, timeout
// and public_url checks live here to stay in lockstep.
export function validateConnUrl(url: string): string | null {
  if (!url.trim()) return m.settings_validate_url_required();
  try {
    const u = new URL(url);
    if (!u.host) return m.settings_validate_url_host();
  } catch {
    return m.settings_validate_url_invalid();
  }
  return null;
}

export function validateTimeout(seconds: number): string | null {
  if (seconds < 0) return m.settings_validate_timeout_positive();
  return null;
}

export function validatePublicUrl(publicUrl: string): string | null {
  const trimmed = publicUrl.trim();
  if (!trimmed) return null;
  try {
    const u = new URL(trimmed);
    if (!u.host || (u.protocol !== "http:" && u.protocol !== "https:")) {
      return m.settings_validate_public_url_absolute();
    }
  } catch {
    return m.settings_validate_public_url_invalid();
  }
  return null;
}

// Minimal mutation surface the hook needs from the createConnectionHooks
// outputs. Each mutation exposes mutateAsync + isPending — that's enough.
type Mutation<TArgs> = {
  mutateAsync: (args: TArgs) => Promise<unknown>;
  isPending: boolean;
};

export type ConnectionMutations<TInput extends { kind: string }, TTest> = {
  create: Mutation<TInput>;
  update: Mutation<{ kind: string; input: TInput }>;
  del: Mutation<string>;
  test: Mutation<TTest>;
};

export type DrawerState<Form> = {
  form: Form;
  set: <K extends keyof Form>(key: K, value: Form[K]) => void;
  dirty: boolean;
  busy: boolean;
  error: string | null;
  testResult: { ok: boolean; msg: string } | null;
  confirmDelete: boolean;
  isDraft: boolean;
  onSave: () => Promise<void>;
  onTest: () => Promise<void>;
  onDelete: () => Promise<void>;
};

// useConnectionDrawer encapsulates the drawer state machine shared by the two
// sections: form / dirty tracking / save / test / two-step confirm delete.
export function useConnectionDrawer<
  Conn,
  Form,
  TInput extends { kind: string },
  TTest,
>(opts: {
  kind: string;
  connection: Conn | undefined;
  emptyForm: () => Form;
  connectionToForm: (c: Conn) => Form;
  formToInput: (kind: string, f: Form) => TInput;
  formToTest: (kind: string, f: Form) => TTest;
  clientValidate: (f: Form) => string | null;
  testSuccessMsg: string;
  mutations: ConnectionMutations<TInput, TTest>;
  onClose: () => void;
}): DrawerState<Form> {
  const {
    kind,
    connection,
    emptyForm,
    connectionToForm,
    formToInput,
    formToTest,
    clientValidate,
    testSuccessMsg,
    mutations,
    onClose,
  } = opts;

  const isDraft = connection === undefined;
  const original = connection ? connectionToForm(connection) : emptyForm();
  const [form, setForm] = useState<Form>(original);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  // Hold "applying" from save until the refreshed connection lands (~1s after
  // the PUT, once the daemon has reloaded), so the Save button doesn't sit
  // there looking unsaved in the meantime. Key the reset on the connection's
  // serialized value, not its reference — the parent rebuilds it every render.
  const [applying, setApplying] = useState(false);
  const originalKey = JSON.stringify(original);
  useEffect(() => {
    setApplying(false);
  }, [originalKey]);

  const set = <K extends keyof Form>(key: K, value: Form[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const dirty = isDraft || JSON.stringify(form) !== JSON.stringify(original);
  const busy = mutations.create.isPending || mutations.update.isPending || mutations.del.isPending || applying;

  const onSave = async () => {
    setError(null);
    const msg = clientValidate(form);
    if (msg) {
      setError(msg);
      return;
    }
    try {
      if (isDraft) {
        await mutations.create.mutateAsync(formToInput(kind, form));
        onClose();
      } else {
        setApplying(true);
        await mutations.update.mutateAsync({ kind, input: formToInput(kind, form) });
        // applying clears when the refreshed connection lands (effect above).
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setApplying(false);
    }
  };

  const onTest = async () => {
    setTestResult(null);
    try {
      await mutations.test.mutateAsync(formToTest(kind, form));
      setTestResult({ ok: true, msg: testSuccessMsg });
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
      await mutations.del.mutateAsync(kind);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setConfirmDelete(false);
    }
  };

  return {
    form, set, dirty, busy, error, testResult, confirmDelete, isDraft,
    onSave, onTest, onDelete,
  };
}

// DrawerActions renders the shared bottom of a connection drawer: error pane,
// Save/Delete row, and Test row. The two sections diverge only on which form
// fields enable the Test button — passed via `testDisabled`.
export function DrawerActions<Form, TInput extends { kind: string }, TTest>({
  state,
  mutations,
  testDisabled,
  testHint,
}: {
  state: DrawerState<Form>;
  mutations: ConnectionMutations<TInput, TTest>;
  testDisabled: boolean;
  testHint: string;
}) {
  const { error, isDraft, dirty, busy, confirmDelete, testResult, onSave, onDelete, onTest } = state;
  return (
    <>
      {error && (
        <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
          {error}
        </div>
      )}

      <div className="flex items-center gap-2 pt-2 border-t border-border">
        <Button onClick={onSave} disabled={!dirty || busy}>
          {busy ? m.common_saving() : isDraft ? m.settings_conn_create() : m.common_save()}
        </Button>
        <Button
          variant={confirmDelete ? "destructive" : "ghost"}
          onClick={onDelete}
          disabled={busy}
          className="ml-auto"
        >
          {isDraft ? m.common_cancel() : confirmDelete ? m.settings_conn_confirm_delete() : m.common_delete()}
        </Button>
      </div>

      <div className="space-y-2 pt-2 border-t border-border">
        <div className="text-xs text-muted-foreground">{testHint}</div>
        <div className="flex items-center gap-3">
          <Button variant="outline" className="shrink-0" onClick={onTest} disabled={mutations.test.isPending || testDisabled}>
            {mutations.test.isPending ? m.settings_conn_testing() : m.settings_conn_test_connection()}
          </Button>
          {testResult && (
            <div
              className={cn(
                "flex items-center gap-2 text-sm rounded-md border px-2.5 py-1.5 flex-1 min-w-0",
                testResult.ok
                  ? "text-emerald-700 dark:text-emerald-300 border-emerald-500/30 bg-emerald-500/10"
                  : "text-destructive border-destructive/50 bg-destructive/10",
              )}
            >
              {testResult.ok ? <Check size={14} className="shrink-0" /> : <X size={14} className="shrink-0" />}
              <span>{testResult.msg}</span>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
