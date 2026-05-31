import { useEffect, useState } from "react";
import { useSettings, useUpdateSettings, useTestNotification } from "@/api/hooks";
import type { SettingsOverrideInput } from "@/api/hooks";
import { Drawer } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
import { Switch } from "@/components/ui/Switch";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";
import {
  ConnectionKindTile,
  noAutofillProps,
  type VisualTileStatus,
} from "./ConnectionsCommon";
import { parseValueForKey } from "./SettingsField";

// ── Provider catalogue ────────────────────────────────────────────────────────

type ProviderMeta = {
  id: string;
  label: string;
  description: string;
  logo: React.ReactNode;
  stub?: boolean;
};

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

// ── Tile ──────────────────────────────────────────────────────────────────────

type TileStatus = "unconfigured" | "disabled" | "enabled";

const VISUAL: Record<TileStatus, VisualTileStatus> = {
  unconfigured: "unconfigured",
  disabled:     "disabled",
  enabled:      "healthy",
};

const STATUS_TEXT: Record<TileStatus, () => string> = {
  enabled:      m.settings_notif_telegram_enabled_status,
  disabled:     m.common_disabled,
  unconfigured: m.common_not_configured,
};

function ProviderTile({
  meta,
  enabled,
  configured,
  onClick,
}: {
  meta: ProviderMeta;
  enabled: boolean;
  configured: boolean;
  onClick: () => void;
}) {
  const status: TileStatus = !configured ? "unconfigured" : !enabled ? "disabled" : "enabled";
  return (
    <ConnectionKindTile
      label={meta.label}
      subtitle={meta.description}
      stub={meta.stub}
      connected={configured}
      status={VISUAL[status]}
      statusText={STATUS_TEXT[status]()}
      chips={configured ? [{ label: m.settings_chip_enabled(), on: enabled }] : undefined}
      renderLogo={(size) => (
        <div style={{ width: size, height: size, flexShrink: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
          {meta.logo}
        </div>
      )}
      onClick={onClick}
    />
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
          {!p.dirty && p.overridden && (
            <>
              <Badge>{m.settings_field_badge_overridden()}</Badge>
              <Button size="sm" variant="ghost" onClick={p.onRevert} title={m.settings_field_revert_title()}>
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
        className={p.dirty ? "ring-1 ring-amber-500/70 border-amber-500/70" : undefined}
        {...noAutofillProps}
      />
    </div>
  );
}

// ── Toggle row ────────────────────────────────────────────────────────────────

function ToggleRow({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-border p-4">
      <div>
        <div className="text-sm font-medium">{label}</div>
        {hint && <div className="text-xs text-muted-foreground mt-0.5">{hint}</div>}
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
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
  // Hold "applying" across the PUT + daemon reload + refetch so the save state
  // stays legible instead of flipping back early. See SectionShell.
  const [applying, setApplying] = useState(false);

  useEffect(() => {
    if (settings.data) {
      setPending({});
      setApplying(false);
    }
  }, [settings.dataUpdatedAt]);

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
      const parsed = parseValueForKey(key, raw);
      if (parsed instanceof Error) {
        setError(`${key}: ${parsed.message}`);
        return;
      }
      ops.push({ key, value: parsed });
    }
    if (ops.length === 0) return;
    try {
      setApplying(true);
      await update.mutateAsync(ops);
      // pending clears when the refreshed snapshot arrives (effect above).
    } catch (e) {
      setError(String(e));
      setApplying(false);
    }
  };

  const busy = update.isPending || applying;

  const onTest = async () => {
    setTestResult(null);
    try {
      await test.mutateAsync();
      setTestResult({ ok: true, msg: m.settings_notif_test_success() });
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
        <div className="text-sm text-muted-foreground">{m.common_loading()}</div>
      ) : (
        <div className="space-y-6">
          {/* Enable toggle */}
          <ToggleRow
            label={m.settings_notif_enable_telegram()}
            hint={m.settings_notif_enable_telegram_hint()}
            checked={enabledVal}
            onChange={(v) => set("notifications.telegram.enabled", String(v))}
          />

          {/* Credentials */}
          <div className="space-y-4">
            <CredentialField
              label={m.settings_notif_bot_token()}
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
              label={m.settings_notif_chat_id()}
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
            <Button onClick={onSave} disabled={dirtyCount === 0 || busy}>
              {busy
                ? m.settings_shell_saving_reloading()
                : dirtyCount === 0
                  ? m.settings_shell_no_changes()
                  : dirtyCount === 1
                    ? m.settings_notif_save_changes({ count: dirtyCount })
                    : m.settings_notif_save_changes_plural({ count: dirtyCount })}
            </Button>
            {dirtyCount > 0 && !busy && (
              <Button variant="outline" onClick={() => setPending({})}>
                {m.settings_notif_discard()}
              </Button>
            )}
            {busy && (
              <span className="text-xs text-muted-foreground">
                {m.settings_shell_daemon_restarting()}
              </span>
            )}
          </div>

          {/* Test */}
          <div className="space-y-2 pt-2 border-t border-border">
            <div className="text-xs text-muted-foreground">
              {m.settings_notif_test_desc()}
            </div>
            <div className="flex items-center gap-3">
              <Button
                variant="outline"
                onClick={onTest}
                disabled={test.isPending || dirtyCount > 0}
              >
                {test.isPending ? m.settings_notif_sending() : m.settings_notif_send_test()}
              </Button>
              {dirtyCount > 0 && (
                <span className="text-xs text-muted-foreground">{m.settings_notif_save_first()}</span>
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

// ── Main section ──────────────────────────────────────────────────────────────

type OpenProvider = "telegram" | null;

export function NotificationSection() {
  const settings = useSettings();
  const [open, setOpen] = useState<OpenProvider>(null);

  const tg = settings.data?.values.notifications.telegram;
  const telegramEnabled = tg?.enabled ?? false;
  // Considered configured once bot_token is set (minimum required credential).
  const telegramConfigured = Boolean(tg?.bot_token);

  const PROVIDERS: ProviderMeta[] = [
    {
      id: "telegram",
      label: "Telegram",
      description: m.settings_notif_telegram_bot_api(),
      logo: <TelegramLogo className="w-9 h-9" />,
    },
  ];

  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{m.settings_notif_title()}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {m.settings_notif_description()}
          </p>
        </div>

        <div className="arr-tile-grid">
          {PROVIDERS.map((meta) => (
            <ProviderTile
              key={meta.id}
              meta={meta}
              enabled={meta.id === "telegram" ? telegramEnabled : false}
              configured={meta.id === "telegram" ? telegramConfigured : false}
              onClick={() => setOpen(meta.id as OpenProvider)}
            />
          ))}
        </div>
      </div>

      <TelegramDrawer open={open === "telegram"} onClose={() => setOpen(null)} />
    </>
  );
}
