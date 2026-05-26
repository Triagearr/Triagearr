import { createFileRoute } from "@tanstack/react-router";
import { useSettings } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Tooltip } from "@/components/ui/Tooltip";
import { m } from "@/paraglide/messages";

// ── Recursive JSON renderer with override highlighting + hover tooltip ─────
//
// Uses settings.values whose JSON keys are snake_case and align 1-to-1 with
// the koanf dotted paths in overridden_keys / baseline_values.

type JsonValueProps = {
  value: unknown;
  overridden: Set<string>;
  baseline: Record<string, unknown> | null;
  path?: string;
  indent?: number;
};

// Walks a nested object along a dotted path and returns the leaf value,
// or undefined if the path doesn't exist.
function getAtPath(obj: unknown, path: string): unknown {
  if (!path || obj == null) return undefined;
  const parts = path.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

function JsonValue({ value, overridden, baseline, path = "", indent = 0 }: JsonValueProps) {
  const pad = "  ".repeat(indent);
  const padInner = "  ".repeat(indent + 1);

  if (value === null)
    return <span style={{ color: "var(--fg-3)" }}>null</span>;
  if (typeof value === "boolean")
    return <span style={{ color: "var(--amber)" }}>{String(value)}</span>;
  if (typeof value === "number")
    return <span style={{ color: "var(--primary)" }}>{value}</span>;
  if (typeof value === "string")
    return <span style={{ color: "var(--green-2)" }}>"{value}"</span>;
  if (Array.isArray(value)) {
    if (value.length === 0) return <span>{"[]"}</span>;
    return (
      <>
        {"[\n"}
        {value.map((item, i) => (
          <span key={i}>
            {padInner}
            <JsonValue value={item} overridden={overridden} baseline={baseline} path={`${path}[${i}]`} indent={indent + 1} />
            {i < value.length - 1 ? "," : ""}
            {"\n"}
          </span>
        ))}
        {pad}{"]"}
      </>
    );
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return <span>{"{}"}</span>;
    return (
      <>
        {"{\n"}
        {entries.map(([key, val], i) => {
          const childPath = path ? `${path}.${key}` : key;
          const isOverridden = overridden.has(childPath);
          const comma = i < entries.length - 1 ? "," : "";
          const baselineVal = isOverridden && baseline != null
            ? getAtPath(baseline, childPath)
            : undefined;

          return isOverridden ? (
            // display:block stretches across the pre width (amber background)
            // and creates an implicit line break — no \n needed inside.
            <span
              key={key}
              style={{
                display: "block",
                background: "var(--amber-bg)",
                borderLeft: "2px solid var(--amber)",
                paddingLeft: "4px",
              }}
            >
              {padInner}
              <span style={{ color: "var(--fg)" }}>"{key}"</span>
              {": "}
              <Tooltip
                content={
                  baselineVal !== undefined
                    ? <>
                        <span style={{ color: "oklch(0.55 0.17 75)", fontSize: "0.65rem" }}>{m.settings_debug_yaml_baseline()}</span>
                        {"\n"}
                        {JSON.stringify(baselineVal)}
                      </>
                    : <span style={{ opacity: 0.6 }}>{m.settings_debug_yaml_baseline_unavailable()}</span>
                }
              >
                <span style={{ cursor: "help" }}>
                  <JsonValue value={val} overridden={overridden} baseline={baseline} path={childPath} indent={indent + 1} />
                </span>
              </Tooltip>
              {comma}
              {"  "}
              <span
                style={{
                  fontSize: "0.6rem",
                  fontFamily: "inherit",
                  background: "var(--amber)",
                  color: "oklch(0.25 0.04 75)",
                  borderRadius: "3px",
                  padding: "1px 5px",
                  verticalAlign: "middle",
                  letterSpacing: "0.03em",
                  fontWeight: 600,
                }}
              >
                {m.settings_debug_override_tag()}
              </span>
            </span>
          ) : (
            <span key={key}>
              {padInner}
              <span style={{ color: "var(--fg-3)" }}>"{key}"</span>
              {": "}
              <JsonValue value={val} overridden={overridden} baseline={baseline} path={childPath} indent={indent + 1} />
              {comma}
              {"\n"}
            </span>
          );
        })}
        {pad}{"}"}
      </>
    );
  }
  return <span>{JSON.stringify(value)}</span>;
}

// ── Page ───────────────────────────────────────────────────────────────────

function DebugSection() {
  const settings = useSettings();

  const overridden = new Set<string>(settings.data?.overridden_keys ?? []);
  const overrideCount = overridden.size;
  const baseline = (settings.data?.baseline_values ?? null) as Record<string, unknown> | null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {m.settings_debug_effective_config_title()}
          {overrideCount > 0 && (
            <span
              style={{
                background: "var(--amber-bg)",
                color: "var(--amber-2)",
                border: "1px solid var(--amber)",
                borderRadius: "4px",
                padding: "1px 7px",
                fontSize: "0.72rem",
                fontWeight: 600,
              }}
            >
              {overrideCount > 1
                ? m.settings_debug_override_count_plural({ count: overrideCount })
                : m.settings_debug_override_count({ count: overrideCount })}
            </span>
          )}
        </CardTitle>
        <CardDescription>
          {m.settings_debug_description_intro()}{" "}
          {overrideCount > 0
            ? m.settings_debug_description_with_overrides()
            : m.settings_debug_description_no_overrides()}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {settings.isLoading && (
          <div className="text-sm" style={{ color: "var(--fg-3)" }}>{m.common_loading()}</div>
        )}
        {settings.isError && (
          <div className="text-sm" style={{ color: "var(--red)" }}>{String(settings.error)}</div>
        )}
        {settings.data ? (
          <pre
            className="text-xs font-mono p-3 rounded-md border overflow-x-auto max-h-[70vh]"
            style={{
              background: "var(--card-2)",
              borderColor: "var(--border)",
              lineHeight: "1.65",
            }}
          >
            <JsonValue
              value={settings.data.values}
              overridden={overridden}
              baseline={baseline}
            />
          </pre>
        ) : null}
      </CardContent>
    </Card>
  );
}

export const Route = createFileRoute("/settings/debug")({ component: DebugSection });
