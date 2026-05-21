import { useState, type ReactNode } from "react";
import { useSession, useLogin } from "@/api/hooks";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";

/**
 * LoginGate renders the SPA when authentication is disabled OR when the
 * user holds a valid session cookie. Otherwise it shows the login form.
 * Authentication is opt-in (ADR-0019); operators can enable / disable it
 * from Settings → Security.
 */
export function LoginGate({ children }: { children: ReactNode }) {
  const session = useSession();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  if (session.isLoading) {
    return <div className="p-10 text-sm text-muted-foreground">Connecting…</div>;
  }

  if (session.isError) {
    return (
      <div className="min-h-screen p-6 flex items-center justify-center">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle>Cannot reach the API</CardTitle>
            <CardDescription>
              {String(session.error)} — is the daemon running on this host?
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    );
  }

  const status = session.data;
  if (!status?.auth_enabled || status.authenticated) {
    return <>{children}</>;
  }

  return (
    <div className="min-h-screen p-4 sm:p-6 flex items-center justify-center bg-background">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>
            Authentication is enabled on this Triagearr instance.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              login.mutate({ username, password });
            }}
          >
            <Input
              autoComplete="username"
              placeholder="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
              required
              disabled={login.isPending}
            />
            <Input
              type="password"
              autoComplete="current-password"
              placeholder="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={login.isPending}
            />
            {login.isError && (
              <div className="text-sm text-destructive">{String(login.error)}</div>
            )}
            <Button
              type="submit"
              disabled={!username.trim() || !password || login.isPending}
            >
              {login.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
