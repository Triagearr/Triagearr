type Factor = {
  name?: string;
  factor?: string;
  weight?: number;
  raw?: number;
  contribution?: number;
  note?: string;
  reason?: string;
  [k: string]: unknown;
};

export function ScoreBreakdown({ factors, total }: { factors: unknown; total?: number }) {
  let rows: Factor[] = [];
  if (Array.isArray(factors)) rows = factors as Factor[];
  else if (factors && typeof factors === "object") rows = Object.entries(factors).map(([k, v]) => ({ name: k, ...(v as object) }));

  if (rows.length === 0) {
    return <div className="text-sm text-muted-foreground">No factor breakdown was persisted for this torrent.</div>;
  }
  return (
    <div className="flex flex-col gap-2">
      {typeof total === "number" && (
        <div className="text-sm">
          <span className="text-muted-foreground">total score:</span>{" "}
          <span className="font-mono">{total.toFixed(2)}</span>
        </div>
      )}
      <ul className="divide-y divide-border rounded-md border border-border bg-muted/30">
        {rows.map((r, i) => (
          <li key={i} className="px-3 py-2 text-sm flex flex-col gap-0.5">
            <div className="flex items-baseline justify-between">
              <span className="font-medium">{r.name ?? r.factor ?? `factor ${i + 1}`}</span>
              <span className="font-mono text-xs text-muted-foreground">
                {r.contribution !== undefined
                  ? r.contribution.toFixed(2)
                  : r.weight !== undefined
                  ? r.weight.toFixed(2)
                  : "—"}
              </span>
            </div>
            {(r.note || r.reason) && (
              <span className="text-xs text-muted-foreground">{r.note ?? r.reason}</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
