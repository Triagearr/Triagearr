import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

type Point = { ts: string; value: number };

export function Sparkline({ data, color = "var(--primary)", yUnit = "" }: { data: Point[]; color?: string; yUnit?: string }) {
  if (data.length === 0) {
    return <div className="text-xs text-muted-foreground">No data yet.</div>;
  }
  return (
    <div className="h-32 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="sparkFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.35} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <XAxis dataKey="ts" hide />
          <YAxis hide domain={["auto", "auto"]} />
          <Tooltip
            contentStyle={{
              background: "var(--card)",
              border: "1px solid var(--border)",
              borderRadius: 6,
              fontSize: 12,
            }}
            labelFormatter={(label) => new Date(label as string).toLocaleString()}
            formatter={(v) => [`${Number(v).toFixed(2)}${yUnit}`, ""]}
          />
          <Area type="monotone" dataKey="value" stroke={color} fill="url(#sparkFill)" strokeWidth={2} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
