import { useState, type ReactNode } from "react";
import { useAuthMode } from "@/api/hooks";
import { getStoredApiKey, setStoredApiKey } from "@/api/client";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";

export function ApiKeyGate({ children }: { children: ReactNode }) {
  const mode = useAuthMode();
  const [key, setKey] = useState(getStoredApiKey());
  const [showForm, setShowForm] = useState(!getStoredApiKey());

  if (mode.isLoading) {
    return <div className="p-10 text-sm text-muted-foreground">Connecting…</div>;
  }

  if (mode.isError) {
    return (
      <div className="p-10">
        <Card className="max-w-md mx-auto">
          <CardHeader>
            <CardTitle>Cannot reach the API</CardTitle>
            <CardDescription>
              {String(mode.error)} — is the daemon running on this host?
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  if (mode.data?.auth === "none") {
    return <>{children}</>;
  }

  if (!showForm && getStoredApiKey()) {
    return <>{children}</>;
  }

  return (
    <div className="p-10 flex items-center justify-center min-h-screen">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>API key required</CardTitle>
          <CardDescription>
            The daemon is configured with <code className="font-mono">auth: apikey</code>. Paste the
            contents of <code className="font-mono">{"<data_dir>/api_key"}</code> to continue.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              setStoredApiKey(key.trim());
              setShowForm(false);
              location.reload();
            }}
          >
            <Input
              type="password"
              placeholder="x-api-key…"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              autoFocus
            />
            <Button type="submit" disabled={!key.trim()}>
              Save and continue
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
