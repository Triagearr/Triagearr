import { useEffect, useMemo, useState } from "react";
import { useSettings, useUpdateSettings, type SettingsOverrideInput } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";

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

  // Reset pending whenever a fresh server snapshot arrives (we just saved,
  // or another tab/process changed the config).
  useEffect(() => {
    if (settings.data) setPending({});
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
        <CardContent className="text-sm text-muted-foreground">Loading…</CardContent>
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
          {String(settings.error ?? "no data")}
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
      await update.mutateAsync(ops);
    } catch (e) {
      setError(String(e));
    }
  };

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
          <Button onClick={onSave} disabled={dirtyCount === 0 || update.isPending}>
            {update.isPending
              ? "Saving + reloading…"
              : `Save ${dirtyCount} change${dirtyCount === 1 ? "" : "s"}`}
          </Button>
          {dirtyCount > 0 && (
            <Button variant="outline" onClick={() => setPending({})} disabled={update.isPending}>
              Discard
            </Button>
          )}
          {update.isPending && (
            <span className="text-xs text-muted-foreground">
              Daemon restarting — values refresh shortly.
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
  onChange: (v: string) => void;
  overridden: boolean;
  dirty: boolean;
  onRevert: () => void;
};

export function Field(p: FieldProps) {
  return (
    <div className="grid grid-cols-[12rem_1fr_auto] items-center gap-2 text-sm">
      <label className="text-muted-foreground font-mono text-xs" title={p.keyName}>
        {p.label}
      </label>
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
        />
      )}
      <div className="flex items-center gap-1">
        {p.dirty && <Badge variant="warning">edited</Badge>}
        {!p.dirty && p.overridden && (
          <>
            <Badge>overridden</Badge>
            <Button size="sm" variant="ghost" onClick={p.onRevert} title="Revert to YAML default">
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
function parseValueForKey(key: string, raw: string): unknown | Error {
  // Boolean toggle — value is always "true"/"false", never empty.
  if (key === "notifications.telegram.enabled") return raw === "true";
  if (raw.trim() === "") {
    return new Error("empty value (use the revert button to remove the override)");
  }
  // Notification credentials and polling/cron fields go as JSON strings.
  if (key.startsWith("notifications.")) return raw;
  if (key.startsWith("polling.")) return raw;
  const n = Number(raw);
  if (Number.isNaN(n)) return new Error(`expected a number, got ${JSON.stringify(raw)}`);
  return n;
}
