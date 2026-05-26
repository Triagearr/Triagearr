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
import { m } from "@/paraglide/messages";

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
            <CardTitle>{m.comp_security_title()}</CardTitle>
            <CardDescription>
              {m.comp_security_auth_is()}{" "}
              {status?.auth_enabled ? (
                <Badge variant="success">{m.common_enabled()}</Badge>
              ) : (
                <Badge variant="muted">{m.common_disabled()}</Badge>
              )}
              {m.comp_security_api_key_prefix()}{" "}
              <code className="font-mono">X-API-Key</code> {m.comp_security_api_key_suffix()}
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
      <span className="text-muted-foreground">{m.comp_security_signed_in_as()}</span>
      <span className="font-mono">{username}</span>
      <Button
        size="sm"
        variant="outline"
        onClick={() => logout.mutate()}
      >
        {m.comp_security_sign_out()}
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
        {m.comp_security_enable_intro_prefix()}<strong>{m.comp_security_enable_intro_once()}</strong>{m.comp_security_enable_intro_suffix()}
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
          placeholder={m.comp_username()}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
        <Input
          type="password"
          autoComplete="new-password"
          placeholder={m.comp_security_blank_autogenerate()}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button type="submit" disabled={enable.isPending || !username.trim()}>
          {m.comp_security_enable()}
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
      <div className="text-sm font-medium">{m.comp_security_save_password_now()}</div>
      <div className="text-xs text-muted-foreground">
        {m.comp_security_password_not_shown_again()}
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
          {m.comp_security_copy()}
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
      <div className="font-medium text-sm">{m.comp_security_change_password()}</div>
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
          placeholder={m.comp_security_current_password()}
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          required
        />
        <Input
          type="password"
          autoComplete="new-password"
          placeholder={m.comp_security_new_blank_autogenerate()}
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
        />
        <Button type="submit" disabled={change.isPending || !current}>
          {m.comp_security_rotate()}
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
      <div className="font-medium text-sm text-destructive">{m.comp_security_disable_auth()}</div>
      <p className="text-xs text-muted-foreground">
        {m.comp_security_disable_warning()}
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
          placeholder={m.comp_security_current_password()}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        <Button type="submit" variant="destructive" disabled={disable.isPending || !password}>
          {m.comp_security_disable()}
        </Button>
      </form>
      {disable.isError && (
        <div className="text-sm text-destructive">{String(disable.error)}</div>
      )}
    </div>
  );
}
