import { useCallback, useId, useMemo } from "react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { m } from "@/paraglide/messages";

type Point = { ts: string; value: number };

const TOOLTIP_CONTENT_STYLE = {
  background: "var(--card)",
  border: "1px solid var(--border)",
  borderRadius: 6,
  fontSize: 12,
} as const;

const Y_DOMAIN = ["auto", "auto"] as const;

const labelFormatter = (label: unknown) => new Date(label as string).toLocaleString();

export function Sparkline({ data, color = "var(--primary)", yUnit = "" }: { data: Point[]; color?: string; yUnit?: string }) {
  // Multiple Sparklines on one page collide on a hard-coded gradient id.
  const gradientId = `sparkFill-${useId()}`;
  const valueFormatter = useCallback(
    (v: unknown) => [`${Number(v).toFixed(2)}${yUnit}`, ""] as [string, string],
    [yUnit],
  );
  const gradientStops = useMemo(
    () => (
      <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" stopColor={color} stopOpacity={0.35} />
        <stop offset="100%" stopColor={color} stopOpacity={0} />
      </linearGradient>
    ),
    [gradientId, color],
  );
  if (data.length === 0) {
    return <div className="text-xs text-muted-foreground">{m.comp_sparkline_no_data()}</div>;
  }
  return (
    <div className="h-32 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <defs>{gradientStops}</defs>
          <XAxis dataKey="ts" hide />
          <YAxis hide domain={Y_DOMAIN as unknown as [string, string]} />
          <Tooltip
            contentStyle={TOOLTIP_CONTENT_STYLE}
            labelFormatter={labelFormatter}
            formatter={valueFormatter}
          />
          <Area type="monotone" dataKey="value" stroke={color} fill={`url(#${gradientId})`} strokeWidth={2} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
