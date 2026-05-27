// Score tier + cell shared by the dashboard and the torrents list. The tier
// thresholds drive both the bar fill and the color band; keep them in sync
// with the scorer's documented ranges (docs/SCORING.md).
export function scoreTier(score: number | null | undefined): "low" | "med" | "high" {
  if (score == null) return "low";
  if (score <= 1) return "low";
  if (score <= 5) return "med";
  return "high";
}

export function ScoreCell({ score }: { score: number | null | undefined }) {
  if (score == null) return <span style={{ color: "var(--fg-4)" }}>—</span>;
  const tier = scoreTier(score);
  const pct = Math.min(100, Math.max(0, (score / 10) * 100));
  return (
    <span className={`score-cell ${tier}`}>
      <span className="score-bar"><i style={{ width: `${pct}%` }} /></span>
      {score.toFixed(2)}
    </span>
  );
}
