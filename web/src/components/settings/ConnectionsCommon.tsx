import { useState } from "react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";

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

export function splitList(s: string): string[] {
  return s
    .split(",")
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
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

  const set = <K extends keyof Form>(key: K, value: Form[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const dirty = isDraft || JSON.stringify(form) !== JSON.stringify(original);
  const busy = mutations.create.isPending || mutations.update.isPending || mutations.del.isPending;

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
        await mutations.update.mutateAsync({ kind, input: formToInput(kind, form) });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
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
          {mutations.create.isPending || mutations.update.isPending ? m.common_saving() : isDraft ? m.settings_conn_create() : m.common_save()}
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
        <Button variant="outline" onClick={onTest} disabled={mutations.test.isPending || testDisabled}>
          {mutations.test.isPending ? m.settings_conn_testing() : m.settings_conn_test_connection()}
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
    </>
  );
}
