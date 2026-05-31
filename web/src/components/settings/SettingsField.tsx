import { useEffect, useMemo, useState } from "react";
import { useSettings, useUpdateSettings, type SettingsOverrideInput } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Tooltip } from "@/components/ui/Tooltip";
import { m } from "@/paraglide/messages";

// Pending holds the user's in-flight edits for the current section keyed by
// dotted koanf path. Null means "delete this override" (revert to YAML).
export type Pending = Record<string, string | null>;

// SectionShell is the common layout used by every settings section. It owns
// the pending state, the Save / Discard buttons, and the loading + reload
// transitions. Pass `children` a function that renders the section's fields
// given the helpers.
export function SectionShell({
  title,
  description,
  render,
}: {
  title: string;
  description?: React.ReactNode;
  render: (helpers: SectionHelpers) => React.ReactNode;
}) {
  const settings = useSettings();
  const update = useUpdateSettings();
  const [pending, setPending] = useState<Pending>({});
  const [error, setError] = useState<string | null>(null);
  // The PUT resolves fast, but the values only settle once the daemon has
  // reloaded and we've refetched (~1s later). Hold an "applying" state across
  // that whole window so the button doesn't flip back to a clickable "Save N
  // changes" mid-flight — that gap reads as "nothing happened".
  const [applying, setApplying] = useState(false);

  // Reset pending whenever a fresh server snapshot arrives (we just saved,
  // or another tab/process changed the config) — that snapshot is also the
  // signal that an in-flight save has fully landed.
  useEffect(() => {
    if (settings.data) {
      setPending({});
      setApplying(false);
    }
  }, [settings.dataUpdatedAt]);

  const overridden = useMemo(
    () => new Set(settings.data?.overridden_keys ?? []),
    [settings.data?.overridden_keys],
  );

  if (settings.isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">{m.common_loading()}</CardContent>
      </Card>
    );
  }
  if (settings.isError || !settings.data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-destructive">
          {String(settings.error ?? m.settings_shell_no_data())}
        </CardContent>
      </Card>
    );
  }

  const helpers: SectionHelpers = {
    settings: settings.data,
    pending,
    setField: (key, value) => setPending((p) => ({ ...p, [key]: value })),
    fieldValue: (key, fallback) => {
      if (key in pending) return pending[key] ?? "";
      return fallback != null ? String(fallback) : "";
    },
    isDirty: (key) => key in pending,
    isOverridden: (key) => overridden.has(key),
    revert: (key) => setPending((p) => ({ ...p, [key]: null })),
  };

  const dirtyCount = Object.keys(pending).length;

  const onSave = async () => {
    setError(null);
    const ops: SettingsOverrideInput[] = [];
    for (const [key, raw] of Object.entries(pending)) {
      if (raw === null) {
        ops.push({ key, value: null });
        continue;
      }
      const parsed = parseValueForKey(key, raw);
      if (parsed instanceof Error) {
        setError(`${key}: ${parsed.message}`);
        return;
      }
      ops.push({ key, value: parsed });
    }
    if (ops.length === 0) return;
    try {
      setApplying(true);
      await update.mutateAsync(ops);
      // Stay "applying" until the refreshed snapshot lands (cleared by the
      // effect above); the PUT resolving early isn't the same as "done".
    } catch (e) {
      setError(String(e));
      setApplying(false);
    }
  };

  const busy = update.isPending || applying;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">{render(helpers)}</div>

        {error && (
          <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
            {error}
          </div>
        )}

        <div className="flex items-center gap-3 pt-2 border-t">
          <Button onClick={onSave} disabled={dirtyCount === 0 || busy}>
            {busy
              ? m.settings_shell_saving_reloading()
              : dirtyCount === 0
                ? m.settings_shell_no_changes()
                : dirtyCount === 1
                  ? m.settings_shell_save_changes({ count: dirtyCount })
                  : m.settings_shell_save_changes_plural({ count: dirtyCount })}
          </Button>
          {dirtyCount > 0 && !busy && (
            <Button variant="outline" onClick={() => setPending({})}>
              {m.settings_shell_discard()}
            </Button>
          )}
          {busy && (
            <span className="text-xs text-muted-foreground">
              {m.settings_shell_daemon_restarting()}
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

// SectionHelpers are the small set of pure helpers a section needs to wire
// its Field rows. Kept narrow so adding a new section doesn't require new
// shell knowledge.
export type SectionHelpers = {
  settings: import("@/api/schemas").SettingsViewT;
  pending: Pending;
  setField: (key: string, value: string | null) => void;
  fieldValue: (key: string, fallback: string | number | boolean | undefined) => string;
  isDirty: (key: string) => boolean;
  isOverridden: (key: string) => boolean;
  revert: (key: string) => void;
};

// Subsection visually groups a few fields under a small label (e.g. "Weights"
// inside Scoring). Pure layout.
export function Subsection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="pl-3 border-l border-border space-y-2">
      <div className="text-xs uppercase tracking-wider text-muted-foreground">{title}</div>
      <div className="space-y-2">{children}</div>
    </div>
  );
}

export type FieldProps = {
  label: string;
  keyName: string;
  type: "number" | "text" | "password" | "checkbox";
  value: string;
  placeholder?: string;
  description?: string;
  onChange: (v: string) => void;
  overridden: boolean;
  dirty: boolean;
  onRevert: () => void;
  // compact narrows the input to a few characters' worth of width — for short
  // numeric fields (weights, thresholds) that don't need a full-width box.
  compact?: boolean;
  // tooltip, when set, shows a short hover explanation on the label and hints
  // at it with a dotted underline + help cursor.
  tooltip?: string;
};

export function Field(p: FieldProps) {
  const cols = p.compact
    ? "grid-cols-1 sm:grid-cols-[10rem_6rem_auto]"
    : "grid-cols-1 sm:grid-cols-[12rem_1fr_auto]";
  const labelEl = (
    <label
      className={`text-muted-foreground font-mono text-xs ${p.tooltip ? "cursor-help underline decoration-dotted decoration-muted-foreground/60 underline-offset-2" : ""}`}
      title={p.tooltip ? undefined : p.keyName}
    >
      {p.label}
    </label>
  );
  return (
    <div className={`grid ${cols} items-center gap-2 text-sm`}>
      <div className="flex flex-col gap-0.5">
        {p.tooltip ? (
          <Tooltip
            content={
              <span style={{ whiteSpace: "normal", display: "block", lineHeight: 1.35 }}>
                {p.tooltip}
              </span>
            }
          >
            {labelEl}
          </Tooltip>
        ) : (
          labelEl
        )}
        {p.description && (
          <span className="text-[10px] leading-tight text-muted-foreground/60">{p.description}</span>
        )}
      </div>
      {p.type === "checkbox" ? (
        <input
          type="checkbox"
          className="h-4 w-4 justify-self-start accent-primary"
          checked={p.value === "true"}
          onChange={(e) => p.onChange(String(e.target.checked))}
        />
      ) : (
        <Input
          type={p.type}
          value={p.value}
          placeholder={p.placeholder}
          onChange={(e) => p.onChange(e.target.value)}
          className={[
            p.compact && p.type === "number" ? "[appearance:textfield] [&::-webkit-inner-spin-button]:hidden [&::-webkit-outer-spin-button]:hidden" : "",
            p.dirty ? "ring-1 ring-amber-500/70 border-amber-500/70" : "",
          ].filter(Boolean).join(" ") || undefined}
        />
      )}
      <div className="flex items-center gap-1">
        {p.overridden && !p.dirty && (
          <>
            <Badge>{m.settings_field_badge_overridden()}</Badge>
            <Button size="sm" variant="ghost" onClick={p.onRevert} title={m.settings_field_revert_title()}>
              ↺
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

// parseValueForKey decides how to encode the raw input string for the API:
// polling/cron fields go as JSON strings, everything else as numbers. The
// backend re-validates via config.LoadWithOverrides — so this is just a
// best-effort serialization, not a security boundary.
export function parseValueForKey(key: string, raw: string): unknown | Error {
  // Boolean toggle — value is always "true"/"false", never empty.
  if (key === "notifications.telegram.enabled") return raw === "true";
  // Mode is the dry-run/live enum string ("live"/"dry-run").
  if (key === "mode") return raw;
  if (raw.trim() === "") {
    return new Error(m.settings_field_error_empty());
  }
  // Notification credentials and polling/cron fields go as JSON strings.
  if (key.startsWith("notifications.")) return raw;
  if (key.startsWith("polling.")) return raw;
  const n = Number(raw);
  if (Number.isNaN(n)) return new Error(m.settings_field_error_nan({ value: JSON.stringify(raw) }));
  return n;
}
