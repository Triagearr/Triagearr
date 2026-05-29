// MetricGrid renders the repeated key/value summary cells used by the run and
// action panels. cols sets the column count; pass plain on an item whose value
// is a non-mono element (e.g. a badge).
export type MetricItem = { k: React.ReactNode; v: React.ReactNode; plain?: boolean };

export function MetricGrid({ cols, items }: { cols: number; items: MetricItem[] }) {
  return (
    <div className="metric-grid" style={{ "--metric-cols": cols } as React.CSSProperties}>
      {items.map((it, i) => (
        <div className="metric-cell" key={i}>
          <div className="metric-k">{it.k}</div>
          <div className={`metric-v${it.plain ? " plain" : ""}`}>{it.v}</div>
        </div>
      ))}
    </div>
  );
}
