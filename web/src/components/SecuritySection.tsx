import { useState } from "react";
import {
  useChangePassword,
  useDisableAuth,
  useEnableAuth,
  useLogout,
  useSession,
} from "@/api/hooks";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";

/** SecuritySection drives the opt-in built-in auth from the Settings page. */
export function SecuritySection() {
  const session = useSession();

  if (session.isLoading) {
    return null;
  }

  const status = session.data;
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div>
            <CardTitle>Security</CardTitle>
            <CardDescription>
              Authentication is{" "}
              {status?.auth_enabled ? (
                <Badge variant="success">enabled</Badge>
              ) : (
                <Badge variant="muted">disabled</Badge>
              )}
              . Programmatic clients can always use{" "}
              <code className="font-mono">X-API-Key</code> in parallel.
            </CardDescription>
          </div>
          {status?.authenticated && <UserBadge username={status.username ?? ""} />}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {!status?.auth_enabled ? <EnablePanel /> : <ManagePanel />}
      </CardContent>
    </Card>
  );
}

function UserBadge({ username }: { username: string }) {
  const logout = useLogout();
  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">signed in as</span>
      <span className="font-mono">{username}</span>
      <Button
        size="sm"
        variant="outline"
        onClick={() => logout.mutate()}
      >
        Sign out
      </Button>
    </div>
  );
}

function EnablePanel() {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [generated, setGenerated] = useState<string | null>(null);
  const enable = useEnableAuth();

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        Enable a username + password gate. The password can be auto-generated
        (leave blank); it's shown <strong>once</strong> here and never stored
        in plain text.
      </p>
      <form
        className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto]"
        onSubmit={(e) => {
          e.preventDefault();
          enable.mutate(
            { username: username.trim(), password: password || undefined },
            {
              onSuccess: (data) => {
                if (data.password) setGenerated(data.password);
                setPassword("");
              },
            },
          );
        }}
      >
        <Input
          autoComplete="username"
          placeholder="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <Input
          type="password"
          autoComplete="new-password"
          placeholder="leave blank to auto-generate"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button type="submit" disabled={enable.isPending || !username.trim()}>
          Enable
        </Button>
      </form>
      {enable.isError && (
        <div className="text-sm text-destructive">{String(enable.error)}</div>
      )}
      {generated && <GeneratedPasswordCard password={generated} />}
    </div>
  );
}

function GeneratedPasswordCard({ password }: { password: string }) {
  return (
    <div className="rounded-md border border-primary/40 bg-primary/5 p-3 space-y-2">
      <div className="text-sm font-medium">Save this password now</div>
      <div className="text-xs text-muted-foreground">
        It won't be shown again. You're already signed in for this session.
      </div>
      <div className="flex items-center gap-2 flex-wrap">
        <code className="font-mono text-sm break-all bg-muted/40 px-2 py-1 rounded">
          {password}
        </code>
        <Button
          size="sm"
          variant="outline"
          onClick={() => navigator.clipboard?.writeText(password)}
        >
          Copy
        </Button>
      </div>
    </div>
  );
}

function ManagePanel() {
  return (
    <div className="space-y-4">
      <ChangePasswordForm />
      <hr className="border-border" />
      <DisableAuthForm />
    </div>
  );
}

function ChangePasswordForm() {
  const [current, setCurrent] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [generated, setGenerated] = useState<string | null>(null);
  const change = useChangePassword();
  return (
    <div className="space-y-3">
      <div className="font-medium text-sm">Change password</div>
      <form
        className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto]"
        onSubmit={(e) => {
          e.preventDefault();
          change.mutate(
            { current, newPassword: newPassword || undefined },
            {
              onSuccess: (data) => {
                if (data.password) setGenerated(data.password);
                setCurrent("");
                setNewPassword("");
              },
            },
          );
        }}
      >
        <Input
          type="password"
          autoComplete="current-password"
          placeholder="current password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          required
        />
        <Input
          type="password"
          autoComplete="new-password"
          placeholder="new (blank to auto-generate)"
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
        />
        <Button type="submit" disabled={change.isPending || !current}>
          Rotate
        </Button>
      </form>
      {change.isError && (
        <div className="text-sm text-destructive">{String(change.error)}</div>
      )}
      {generated && <GeneratedPasswordCard password={generated} />}
    </div>
  );
}

function DisableAuthForm() {
  const [password, setPassword] = useState("");
  const disable = useDisableAuth();
  return (
    <div className="space-y-3">
      <div className="font-medium text-sm text-destructive">Disable authentication</div>
      <p className="text-xs text-muted-foreground">
        Removes the user and forgets the session. The dashboard becomes open
        — only do this if upstream protection (TinyAuth/Authelia/private LAN)
        is in place.
      </p>
      <form
        className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_auto]"
        onSubmit={(e) => {
          e.preventDefault();
          disable.mutate({ password });
        }}
      >
        <Input
          type="password"
          autoComplete="current-password"
          placeholder="current password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        <Button type="submit" variant="destructive" disabled={disable.isPending || !password}>
          Disable
        </Button>
      </form>
      {disable.isError && (
        <div className="text-sm text-destructive">{String(disable.error)}</div>
      )}
    </div>
  );
}
