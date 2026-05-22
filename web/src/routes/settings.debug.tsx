import { createFileRoute } from "@tanstack/react-router";
import { useConfig } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";

function DebugSection() {
  const cfg = useConfig();
  return (
    <Card>
      <CardHeader>
        <CardTitle>Effective configuration</CardTitle>
        <CardDescription>
          The full config the daemon is running with (YAML + persisted overrides). Secret-bearing
          fields (qbit password, *arr API keys) are replaced with <code className="font-mono">***</code>.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {cfg.isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
        {cfg.isError && <div className="text-sm text-destructive">{String(cfg.error)}</div>}
        {cfg.data ? (
          <pre className="text-xs font-mono bg-muted/30 p-3 rounded-md border border-border overflow-x-auto max-h-[60vh]">
            {JSON.stringify(cfg.data, null, 2)}
          </pre>
        ) : null}
      </CardContent>
    </Card>
  );
}

export const Route = createFileRoute("/settings/debug")({ component: DebugSection });
