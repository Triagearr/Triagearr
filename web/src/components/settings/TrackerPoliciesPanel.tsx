import { useEffect, useMemo, useState } from "react";
import {
  useScoringDefaults,
  useUpdateScoringDefaults,
  useTrackerPolicies,
  useUpsertTrackerPolicy,
  useDeleteTrackerPolicy,
  type TrackerPolicyInput,
} from "@/api/hooks";
import type { ScoringDefaultsT, TrackerHostStatT } from "@/api/schemas";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/Table";

// DefaultsForm edits the singleton scoring_defaults row (ADR-0026). These
// values apply to every torrent whose tracker has no override.
function DefaultsForm() {
  const defaults = useScoringDefaults();
  const update = useUpdateScoringDefaults();
  const [draft, setDraft] = useState<ScoringDefaultsT | null>(null);

  useEffect(() => {
    if (defaults.data) setDraft(defaults.data);
  }, [defaults.data]);

  if (defaults.isLoading || !draft) {
    return <div className="text-sm text-muted-foreground">Loading defaults…</div>;
  }
  const dirty =
    defaults.data != null &&
    (draft.min_ratio !== defaults.data.min_ratio ||
      draft.min_seed_days !== defaults.data.min_seed_days ||
      draft.rare_threshold !== defaults.data.rare_threshold);

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-3 gap-3">
        <NumberField
          label="Min ratio"
          value={draft.min_ratio}
          step={0.1}
          onChange={(v) => setDraft({ ...draft, min_ratio: v })}
        />
        <NumberField
          label="Min seed days"
          value={draft.min_seed_days}
          onChange={(v) => setDraft({ ...draft, min_seed_days: v })}
        />
        <NumberField
          label="Rare threshold"
          value={draft.rare_threshold}
          onChange={(v) => setDraft({ ...draft, rare_threshold: v })}
        />
      </div>
      <div className="flex items-center gap-2">
        <Button
          disabled={!dirty || update.isPending}
          onClick={() => update.mutate(draft)}
        >
          {update.isPending ? "Saving…" : "Save defaults"}
        </Button>
        {dirty && (
          <Button variant="outline" onClick={() => defaults.data && setDraft(defaults.data)}>
            Discard
          </Button>
        )}
        <p className="text-xs text-muted-foreground">
          Applied to every tracker without an explicit override.
        </p>
      </div>
    </div>
  );
}

function NumberField({
  label,
  value,
  step,
  onChange,
}: {
  label: string;
  value: number;
  step?: number;
  onChange: (n: number) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-muted-foreground">{label}</label>
      <Input
        type="number"
        step={step ?? 1}
        value={value}
        onChange={(e) => {
          const n = Number(e.target.value);
          onChange(Number.isFinite(n) ? n : 0);
        }}
      />
    </div>
  );
}

// PoliciesTable lists every tracker host the library has seen (joined from
// torrent_trackers) and lets the user edit its policy. A row with no override
// inherits the defaults; the inherited values are pre-filled so editing a
// blank row produces a complete override without having to re-type defaults.
function PoliciesTable() {
  const policies = useTrackerPolicies();
  const defaults = useScoringDefaults();
  const upsert = useUpsertTrackerPolicy();
  const remove = useDeleteTrackerPolicy();
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState<TrackerPolicyInput>({
    min_ratio: 1.0,
    min_seed_days: 30,
    rare_threshold: null,
    enabled: true,
  });

  const rows = useMemo(() => policies.data ?? [], [policies.data]);

  if (policies.isLoading) {
    return <div className="text-sm text-muted-foreground">Loading trackers…</div>;
  }
  if (rows.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No trackers observed yet. Configure qBittorrent and wait for the tracker poller to
        run.
      </p>
    );
  }

  const startEdit = (row: TrackerHostStatT) => {
    setEditing(row.tracker_host);
    setDraft({
      min_ratio: row.policy?.min_ratio ?? defaults.data?.min_ratio ?? 1.0,
      min_seed_days: row.policy?.min_seed_days ?? defaults.data?.min_seed_days ?? 30,
      rare_threshold: row.policy?.rare_threshold ?? null,
      enabled: row.policy?.enabled ?? true,
    });
  };

  return (
    <Table>
      <THead>
        <TR>
          <TH>Host</TH>
          <TH className="w-24 text-right">Torrents</TH>
          <TH className="w-28">Status</TH>
          <TH>Policy</TH>
          <TH className="w-40 text-right">Actions</TH>
        </TR>
      </THead>
      <TBody>
        {rows.map((row) => {
          const isEditing = editing === row.tracker_host;
          const hasOverride = row.policy != null;
          return (
            <TR key={row.tracker_host}>
              <TD className="font-mono text-xs">{row.tracker_host}</TD>
              <TD className="text-right tabular-nums">{row.torrent_count}</TD>
              <TD>
                {row.all_dead ? (
                  <Badge variant="destructive">dead</Badge>
                ) : row.any_alive ? (
                  <Badge variant="success">alive</Badge>
                ) : (
                  <Badge>—</Badge>
                )}
              </TD>
              <TD>
                {isEditing ? (
                  <PolicyEditor draft={draft} setDraft={setDraft} />
                ) : hasOverride ? (
                  <PolicySummary row={row} />
                ) : (
                  <span className="text-xs text-muted-foreground italic">
                    inherits defaults
                  </span>
                )}
              </TD>
              <TD className="text-right">
                {isEditing ? (
                  <div className="flex justify-end gap-1">
                    <Button
                      size="sm"
                      disabled={upsert.isPending}
                      onClick={async () => {
                        await upsert.mutateAsync({
                          host: row.tracker_host,
                          input: draft,
                        });
                        setEditing(null);
                      }}
                    >
                      Save
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => setEditing(null)}>
                      Cancel
                    </Button>
                  </div>
                ) : (
                  <div className="flex justify-end gap-1">
                    <Button size="sm" variant="outline" onClick={() => startEdit(row)}>
                      {hasOverride ? "Edit" : "Configure"}
                    </Button>
                    {hasOverride && (
                      <Button
                        size="sm"
                        variant="ghost"
                        title="Reset to defaults"
                        disabled={remove.isPending}
                        onClick={() => remove.mutate(row.tracker_host)}
                      >
                        Reset
                      </Button>
                    )}
                  </div>
                )}
              </TD>
            </TR>
          );
        })}
      </TBody>
    </Table>
  );
}

function PolicySummary({ row }: { row: TrackerHostStatT }) {
  const p = row.policy!;
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <Badge variant={p.enabled ? "default" : "muted"}>
        {p.enabled ? "active" : "disabled"}
      </Badge>
      <span className="font-mono">
        ratio≥{p.min_ratio} · seed≥{p.min_seed_days}d
        {p.rare_threshold != null && ` · rare≤${p.rare_threshold}`}
      </span>
    </div>
  );
}

function PolicyEditor({
  draft,
  setDraft,
}: {
  draft: TrackerPolicyInput;
  setDraft: (d: TrackerPolicyInput) => void;
}) {
  return (
    <div className="grid grid-cols-[auto_5rem_auto_5rem_auto_5rem_auto] items-center gap-1 text-xs">
      <label className="text-muted-foreground">ratio≥</label>
      <Input
        type="number"
        step={0.1}
        value={draft.min_ratio}
        onChange={(e) => setDraft({ ...draft, min_ratio: Number(e.target.value) || 0 })}
      />
      <label className="text-muted-foreground">seed≥</label>
      <Input
        type="number"
        value={draft.min_seed_days}
        onChange={(e) => setDraft({ ...draft, min_seed_days: Number(e.target.value) || 0 })}
      />
      <label className="text-muted-foreground">rare≤</label>
      <Input
        type="number"
        placeholder="default"
        value={draft.rare_threshold ?? ""}
        onChange={(e) =>
          setDraft({
            ...draft,
            rare_threshold: e.target.value === "" ? null : Number(e.target.value) || 0,
          })
        }
      />
      <label className="flex items-center gap-1">
        <input
          type="checkbox"
          checked={draft.enabled}
          onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
        />
        <span className="text-muted-foreground">enabled</span>
      </label>
    </div>
  );
}

export function TrackerPoliciesPanel() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Tracker policies</CardTitle>
        <CardDescription>
          Per-tracker overrides for Factor 1 (ratio obligation) and Factor 4 (rare-content
          guard). Trackers without an override inherit the conservative defaults. When all
          trackers attached to a torrent are dead, Factor 1 degrades silently — no policy
          enforcement against a counterparty that no longer exists.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <section className="space-y-2">
          <h3 className="text-sm font-semibold">Defaults</h3>
          <DefaultsForm />
        </section>
        <section className="space-y-2">
          <h3 className="text-sm font-semibold">Per-tracker overrides</h3>
          <PoliciesTable />
        </section>
      </CardContent>
    </Card>
  );
}
