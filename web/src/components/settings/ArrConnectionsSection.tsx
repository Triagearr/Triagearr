import { useState } from "react";
import {
  useArrConnections,
  useCreateArrConnection,
  useUpdateArrConnection,
  useDeleteArrConnection,
  useTestArrConnection,
  type ArrConnectionInput,
} from "@/api/hooks";
import type { ArrConnectionT } from "@/api/schemas";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";
import { cn } from "@/lib/cn";

const KINDS = [
  { value: "sonarr", label: "Sonarr" },
  { value: "radarr", label: "Radarr" },
  { value: "lidarr", label: "Lidarr (stub)" },
  { value: "readarr", label: "Readarr (stub)" },
  { value: "whisparr_v2", label: "Whisparr v2 (stub)" },
  { value: "whisparr_v3", label: "Whisparr v3 (stub)" },
];

// Form is the editable shape of one connection. tags_exclude / categories_only
// are kept comma-joined while editing and split on save.
type Form = {
  kind: string;
  name: string;
  url: string;
  api_key: string;
  enabled: boolean;
  poll: boolean;
  act: boolean;
  tags_exclude: string;
  categories_only: string;
  timeout_seconds: number;
};

const emptyForm: Form = {
  kind: "sonarr",
  name: "",
  url: "",
  api_key: "",
  enabled: true,
  poll: true,
  act: false,
  tags_exclude: "",
  categories_only: "",
  timeout_seconds: 30,
};

function connectionToForm(c: ArrConnectionT): Form {
  return {
    kind: c.kind,
    name: c.name,
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

function formToInput(f: Form): ArrConnectionInput {
  return {
    kind: f.kind,
    name: f.name.trim(),
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

// clientValidate mirrors the server's checks so the operator gets immediate
// feedback. Returns an error message, or null when the form is acceptable.
function clientValidate(f: Form): string | null {
  if (!f.name.trim()) return "Name is required.";
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
    <div className="grid grid-cols-1 sm:grid-cols-[10rem_1fr] sm:items-center gap-1 sm:gap-3 text-sm">
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

function ConnectionCard({
  connection,
  onDiscardDraft,
}: {
  connection?: ArrConnectionT;
  onDiscardDraft?: () => void;
}) {
  const isDraft = connection === undefined;
  const original = connection ? connectionToForm(connection) : emptyForm;
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
        await create.mutateAsync(formToInput(form));
        onDiscardDraft?.();
      } else {
        await update.mutateAsync({ id: connection.id, input: formToInput(form) });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const onTest = async () => {
    setTestResult(null);
    try {
      await test.mutateAsync({
        kind: form.kind,
        url: form.url.trim(),
        api_key: form.api_key,
        timeout_seconds: form.timeout_seconds,
      });
      setTestResult({ ok: true, msg: "Connection OK — the *arr instance responded." });
    } catch (e) {
      setTestResult({ ok: false, msg: e instanceof Error ? e.message : String(e) });
    }
  };

  const onDelete = async () => {
    if (isDraft) {
      onDiscardDraft?.();
      return;
    }
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setError(null);
    try {
      await del.mutateAsync(connection.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setConfirmDelete(false);
    }
  };

  return (
    <div className="rounded-lg border bg-background p-4 space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div className="font-medium text-sm">
          {isDraft ? "New connection" : `${connection.kind} / ${connection.name}`}
        </div>
        {!isDraft && !connection.enabled && (
          <span className="text-xs text-muted-foreground">disabled</span>
        )}
      </div>

      <div className="space-y-2">
        <FieldRow label="Type">
          <Select value={form.kind} onChange={(e) => set("kind", e.target.value)}>
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </Select>
        </FieldRow>
        <FieldRow label="Name">
          <Input
            value={form.name}
            placeholder="main"
            onChange={(e) => set("name", e.target.value)}
          />
        </FieldRow>
        <FieldRow label="URL">
          <Input
            value={form.url}
            placeholder="http://sonarr:8989"
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
        <FieldRow label="Categories only" hint="comma-separated, optional">
          <Input
            value={form.categories_only}
            placeholder="tv-sonarr"
            onChange={(e) => set("categories_only", e.target.value)}
          />
        </FieldRow>
        <FieldRow label="Flags">
          <div className="flex flex-wrap gap-4">
            <Toggle
              label="Enabled"
              checked={form.enabled}
              onChange={(v) => set("enabled", v)}
            />
            <Toggle label="Poll" checked={form.poll} onChange={(v) => set("poll", v)} />
            <Toggle label="Act (allow deletes)" checked={form.act} onChange={(v) => set("act", v)} />
          </div>
        </FieldRow>
        {form.act && (
          <p className="text-xs text-destructive/90 sm:pl-[10rem]">
            Act is on — Triagearr may delete media from this instance during live runs.
          </p>
        )}
      </div>

      {error && (
        <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
          {error}
        </div>
      )}
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

      <div className="flex flex-wrap items-center gap-2 pt-1">
        <Button onClick={onSave} disabled={!dirty || busy}>
          {create.isPending || update.isPending ? "Saving…" : isDraft ? "Create" : "Save"}
        </Button>
        <Button variant="outline" onClick={onTest} disabled={test.isPending}>
          {test.isPending ? "Testing…" : "Test connection"}
        </Button>
        <Button
          variant={confirmDelete ? "destructive" : "ghost"}
          onClick={onDelete}
          disabled={busy}
          className="ml-auto"
        >
          {isDraft ? "Discard" : confirmDelete ? "Confirm delete?" : "Delete"}
        </Button>
      </div>
    </div>
  );
}

export function ArrConnectionsSection() {
  const { data, isLoading, isError, error } = useArrConnections();
  const [drafts, setDrafts] = useState<number[]>([]);
  const [nextDraft, setNextDraft] = useState(1);

  const addDraft = () => {
    setDrafts((d) => [...d, nextDraft]);
    setNextDraft((n) => n + 1);
  };
  const removeDraft = (key: number) => setDrafts((d) => d.filter((k) => k !== key));

  const connections = data?.connections ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>*arr connections</CardTitle>
        <CardDescription>
          Sonarr / Radarr / etc. instances Triagearr talks to. These are stored in the
          database (ADR-0022) — the YAML <code>arrs:</code> block only seeds them on first
          boot. Saving a change reloads the daemon automatically.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
        {isError && (
          <div className="text-sm text-destructive">{String(error ?? "failed to load")}</div>
        )}

        {!isLoading && !isError && connections.length === 0 && drafts.length === 0 && (
          <div className="text-sm text-muted-foreground">
            No connections yet. Add one to let Triagearr poll an *arr instance.
          </div>
        )}

        {connections.map((c) => (
          <ConnectionCard key={c.id} connection={c} />
        ))}
        {drafts.map((key) => (
          <ConnectionCard
            key={`draft-${key}`}
            onDiscardDraft={() => removeDraft(key)}
          />
        ))}

        <div className="pt-1">
          <Button variant="outline" onClick={addDraft}>
            + Add connection
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
