import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useConfig, useVersion, useVolumes } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { RunTriggerDialog } from "@/components/RunTriggerDialog";
import { getStoredApiKey, setStoredApiKey } from "@/api/client";

function SettingsPage() {
  const cfg = useConfig();
  const version = useVersion();
  const volumes = useVolumes();
  const [volume, setVolume] = useState("");
  const [open, setOpen] = useState<"dry-run" | "live" | null>(null);

  const list = volumes.data?.volumes ?? [];
  const selectedVolume = volume || list[0]?.name || "";

  return (
    <div className="p-6 space-y-6 max-w-4xl">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">
          Effective configuration (secrets redacted), manual run trigger, and build info.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Trigger a run</CardTitle>
          <CardDescription>
            A dry-run plans deletions without touching anything. Live runs require the daemon to be
            started with <code className="font-mono">mode: live</code>; otherwise the request is
            forced back to dry-run server-side.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <label className="text-sm text-muted-foreground">Volume</label>
            <select
              value={selectedVolume}
              onChange={(e) => setVolume(e.target.value)}
              className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            >
              {list.map((v) => (
                <option key={v.name} value={v.name}>
                  {v.name} ({v.path})
                </option>
              ))}
            </select>
            <Button onClick={() => setOpen("dry-run")} disabled={!selectedVolume}>
              Plan dry-run
            </Button>
            <Button
              variant="destructive"
              onClick={() => setOpen("live")}
              disabled={!selectedVolume}
            >
              Execute live…
            </Button>
          </div>
          {selectedVolume && open && (
            <RunTriggerDialog
              open={open !== null}
              onClose={() => setOpen(null)}
              volume={selectedVolume}
              mode={open}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>API key</CardTitle>
          <CardDescription>
            Stored locally in <code className="font-mono">localStorage</code>. Only used when the
            daemon is configured with <code className="font-mono">auth: apikey</code>.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex items-center gap-2">
          <Badge variant={getStoredApiKey() ? "success" : "muted"}>
            {getStoredApiKey() ? "configured" : "not set"}
          </Badge>
          {getStoredApiKey() && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setStoredApiKey("");
                location.reload();
              }}
            >
              Forget key
            </Button>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Effective configuration</CardTitle>
          <CardDescription>
            Secret-bearing fields (qbit password, *arr API keys) are replaced with{" "}
            <code className="font-mono">***</code>.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {cfg.isLoading && <div className="text-sm text-muted-foreground">Loading…</div>}
          {cfg.isError && (
            <div className="text-sm text-destructive">{String(cfg.error)}</div>
          )}
          {cfg.data ? (
            <pre className="text-xs font-mono bg-muted/30 p-3 rounded-md border border-border overflow-x-auto max-h-[60vh]">
              {JSON.stringify(cfg.data, null, 2)}
            </pre>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>About</CardTitle>
        </CardHeader>
        <CardContent className="text-sm space-y-1">
          <div>
            version:{" "}
            <span className="font-mono">{version.data?.version ?? "unknown"}</span>
          </div>
          <div>
            commit: <span className="font-mono">{version.data?.commit ?? "unknown"}</span>
          </div>
          <div>
            built: <span className="font-mono">{version.data?.date ?? "unknown"}</span>
          </div>
          <div className="pt-2">
            <a
              className="text-primary underline"
              href="https://github.com/Triagearr/Triagearr"
              target="_blank"
              rel="noreferrer"
            >
              GitHub
            </a>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export const Route = createFileRoute("/settings")({ component: SettingsPage });
