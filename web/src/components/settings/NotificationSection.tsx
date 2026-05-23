import { useState } from "react";
import { useSettings, useUpdateSettings, useTestNotification } from "@/api/hooks";
import type { SettingsOverrideInput } from "@/api/hooks";
import { Drawer } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/cn";

// ── Provider logos ────────────────────────────────────────────────────────────

function TelegramLogo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 240 240"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <circle cx="120" cy="120" r="120" fill="#229ED9" />
      <path
        d="M178.5 66.4L153.3 174.7c-1.8 8-6.6 10-13.4 6.2l-37-27.2-17.9 17.2c-2 2-3.6 3.6-7.3 3.6l2.6-37.3 67.9-61.4c2.9-2.6-.7-4.1-4.5-1.5L73.4 128.5l-35.7-11.2c-7.8-2.4-7.9-7.7 1.6-11.4l130.4-50.3c6.5-2.4 12.2 1.6 9.8 10.8z"
        fill="white"
      />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" className="w-4 h-4">
      <path
        d="M3 8l3.5 3.5L13 4.5"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// ── Provider card ─────────────────────────────────────────────────────────────

type ProviderCardProps = {
  name: string;
  description: string;
  logo: React.ReactNode;
  enabled: boolean;
  onClick: () => void;
};

function ProviderCard({ name, description, logo, enabled, onClick }: ProviderCardProps) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "group relative flex flex-col items-center gap-3 rounded-xl border p-6 text-center transition-colors",
        "hover:border-primary/60 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        enabled ? "border-primary/40 bg-accent/20" : "border-border bg-card",
      )}
    >
      {enabled && (
        <span className="absolute top-2.5 right-2.5 flex items-center gap-1 text-xs font-medium text-emerald-500">
          <CheckIcon />
          On
        </span>
      )}
      <div className="w-14 h-14 flex items-center justify-center">{logo}</div>
      <div>
        <div className="font-medium text-sm">{name}</div>
        <div className="text-xs text-muted-foreground mt-0.5">{description}</div>
      </div>
    </button>
  );
}

// ── Telegram drawer ───────────────────────────────────────────────────────────

type Pending = Record<string, string | null>;

function TelegramDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const settings = useSettings();
  const update = useUpdateSettings();
  const test = useTestNotification();

  const [pending, setPending] = useState<Pending>({});
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);

  const tg = settings.data?.values.notifications.telegram ?? {};
  const overridden = new Set(settings.data?.overridden_keys ?? []);

  function field(key: string, fallback: string | boolean | undefined): string {
    if (key in pending) return pending[key] ?? "";
    return fallback != null ? String(fallback) : "";
  }
  const dirty = (key: string) => key in pending;
  const overr = (key: string) => overridden.has(key);
  const set = (key: string, v: string | null) => setPending((p) => ({ ...p, [key]: v }));

  const enabledVal = field("notifications.telegram.enabled", tg.enabled ?? false) === "true";
  const dirtyCount = Object.keys(pending).length;

  const onSave = async () => {
    setError(null);
    const ops: SettingsOverrideInput[] = [];
    for (const [key, raw] of Object.entries(pending)) {
      if (raw === null) {
        ops.push({ key, value: null });
        continue;
      }
      if (key === "notifications.telegram.enabled") {
        ops.push({ key, value: raw === "true" });
      } else {
        if (raw.trim() === "") {
          setError(`${key}: empty value — use revert to clear`);
          return;
        }
        ops.push({ key, value: raw });
      }
    }
    if (ops.length === 0) return;
    try {
      await update.mutateAsync(ops);
      setPending({});
    } catch (e) {
      setError(String(e));
    }
  };

  const onTest = async () => {
    setTestResult(null);
    try {
      await test.mutateAsync();
      setTestResult({ ok: true, msg: "Test sent — check your Telegram chat." });
    } catch (e) {
      setTestResult({ ok: false, msg: e instanceof Error ? e.message : String(e) });
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={
        <div className="flex items-center gap-3">
          <TelegramLogo className="w-8 h-8" />
          Telegram
        </div>
      }
    >
      {settings.isLoading ? (
        <div className="text-sm text-muted-foreground">Loading…</div>
      ) : (
        <div className="space-y-6">
          {/* Enable toggle */}
          <div className="flex items-center justify-between rounded-lg border border-border p-4">
            <div>
              <div className="text-sm font-medium">Enable Telegram notifications</div>
              <div className="text-xs text-muted-foreground mt-0.5">
                Fires on disk-pressure deletes only — not on manual runs.
              </div>
            </div>
            <button
              role="switch"
              aria-checked={enabledVal}
              onClick={() => set("notifications.telegram.enabled", String(!enabledVal))}
              className={cn(
                "relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                enabledVal ? "bg-primary" : "bg-input",
              )}
            >
              <span
                className={cn(
                  "pointer-events-none inline-block h-5 w-5 rounded-full bg-background shadow-lg transition-transform",
                  enabledVal ? "translate-x-5" : "translate-x-0",
                )}
              />
            </button>
          </div>

          {/* Credentials */}
          <div className="space-y-4">
            <CredentialField
              label="Bot token"
              keyName="notifications.telegram.bot_token"
              type="password"
              placeholder="123456:ABC-DEF..."
              value={field("notifications.telegram.bot_token", tg.bot_token)}
              onChange={(v) => set("notifications.telegram.bot_token", v)}
              dirty={dirty("notifications.telegram.bot_token")}
              overridden={overr("notifications.telegram.bot_token")}
              onRevert={() => set("notifications.telegram.bot_token", null)}
            />
            <CredentialField
              label="Chat ID"
              keyName="notifications.telegram.chat_id"
              type="text"
              placeholder="-1001234567890"
              value={field("notifications.telegram.chat_id", tg.chat_id)}
              onChange={(v) => set("notifications.telegram.chat_id", v)}
              dirty={dirty("notifications.telegram.chat_id")}
              overridden={overr("notifications.telegram.chat_id")}
              onRevert={() => set("notifications.telegram.chat_id", null)}
            />
          </div>

          {/* Errors */}
          {error && (
            <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">
              {error}
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center gap-3 pt-2 border-t border-border">
            <Button onClick={onSave} disabled={dirtyCount === 0 || update.isPending}>
              {update.isPending
                ? "Saving…"
                : `Save ${dirtyCount} change${dirtyCount === 1 ? "" : "s"}`}
            </Button>
            {dirtyCount > 0 && (
              <Button variant="outline" onClick={() => setPending({})} disabled={update.isPending}>
                Discard
              </Button>
            )}
          </div>

          {/* Test */}
          <div className="space-y-2 pt-2 border-t border-border">
            <div className="text-xs text-muted-foreground">
              Sends a synthetic message through the saved config.
            </div>
            <div className="flex items-center gap-3">
              <Button
                variant="outline"
                onClick={onTest}
                disabled={test.isPending || dirtyCount > 0}
              >
                {test.isPending ? "Sending…" : "Send test message"}
              </Button>
              {dirtyCount > 0 && (
                <span className="text-xs text-muted-foreground">Save first to test.</span>
              )}
            </div>
            {testResult && (
              <div
                className={cn(
                  "text-sm rounded-md border p-2",
                  testResult.ok
                    ? "text-foreground border-border"
                    : "text-destructive border-destructive/50",
                )}
              >
                {testResult.msg}
              </div>
            )}
          </div>
        </div>
      )}
    </Drawer>
  );
}

// ── Credential field ──────────────────────────────────────────────────────────

type CredentialFieldProps = {
  label: string;
  keyName: string;
  type: "text" | "password";
  placeholder?: string;
  value: string;
  onChange: (v: string) => void;
  dirty: boolean;
  overridden: boolean;
  onRevert: () => void;
};

function CredentialField(p: CredentialFieldProps) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <label className="text-xs font-medium text-muted-foreground font-mono">{p.label}</label>
        <div className="flex items-center gap-1">
          {p.dirty && <Badge variant="warning">edited</Badge>}
          {!p.dirty && p.overridden && (
            <>
              <Badge>overridden</Badge>
              <Button size="sm" variant="ghost" onClick={p.onRevert} title="Revert to YAML default">
                ↺
              </Button>
            </>
          )}
        </div>
      </div>
      <Input
        type={p.type}
        value={p.value}
        placeholder={p.placeholder}
        onChange={(e) => p.onChange(e.target.value)}
      />
    </div>
  );
}

// ── Main section ──────────────────────────────────────────────────────────────

type OpenProvider = "telegram" | null;

export function NotificationSection() {
  const settings = useSettings();
  const [open, setOpen] = useState<OpenProvider>(null);

  const tg = settings.data?.values.notifications.telegram;
  const telegramEnabled = tg?.enabled ?? false;

  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">Notifications</h2>
          <p className="text-sm text-muted-foreground mt-1">
            Choose a provider to configure. Notifications fire only on disk-pressure deletes —
            manual runs stay silent. Credentials are stored as runtime overrides.
          </p>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          <ProviderCard
            name="Telegram"
            description="Bot API"
            logo={<TelegramLogo className="w-12 h-12" />}
            enabled={telegramEnabled}
            onClick={() => setOpen("telegram")}
          />
        </div>
      </div>

      <TelegramDrawer open={open === "telegram"} onClose={() => setOpen(null)} />
    </>
  );
}
