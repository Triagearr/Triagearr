import { useEffect, useState } from "react";
import {
  useSettings,
  useUpdateSettings,
  useTestNotification,
  useNotificationCatalogue,
  useNotificationDeliveries,
} from "@/api/hooks";
import type { SettingsOverrideInput } from "@/api/hooks";
import { Drawer } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Badge } from "@/components/ui/Badge";
import { Switch } from "@/components/ui/Switch";
import { Bell, Mail, Webhook } from "lucide-react";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";
import {
  ConnectionKindTile,
  noAutofillProps,
  type VisualTileStatus,
} from "./ConnectionsCommon";
import { parseValueForKey } from "./SettingsField";

// ── Provider descriptors ────────────────────────────────────────────────────
//
// Each human channel (ADR-0033) is described declaratively: the drawer renders
// the field list, builds the dotted override keys (notifications.<id>.<field>),
// and the section decides "configured" from configuredField. The Apprise URL is
// assembled server-side from these fields — the UI never sees or sends a URL.

type FieldType = "text" | "password";

type ProviderField = {
  key: string; // field suffix, e.g. "bot_token"
  label: () => string;
  type: FieldType;
  placeholder?: string;
};

type ProviderMeta = {
  id: string;
  label: string;
  description: () => string;
  logo: (size: number) => React.ReactNode;
  fields: ProviderField[];
  configuredField: string; // presence marks the provider "configured"
};

// ── Logos ─────────────────────────────────────────────────────────────────
//
// Brand channels carry their official mark; ntfy/email/webhook (no iconic
// brand) use a lucide glyph on a tinted disc, in the same circular style.

// LogoBadge is a circular tinted disc that centres a logo/icon.
function LogoBadge({ bg, size, children }: { bg: string; size: number; children: React.ReactNode }) {
  return (
    <div
      className="flex items-center justify-center rounded-full"
      style={{ background: bg, width: size, height: size }}
    >
      {children}
    </div>
  );
}

function TelegramLogo({ size }: { size: number }) {
  return (
    <svg viewBox="0 0 240 240" fill="none" xmlns="http://www.w3.org/2000/svg" style={{ width: size, height: size }}>
      <circle cx="120" cy="120" r="120" fill="#229ED9" />
      <path
        d="M178.5 66.4L153.3 174.7c-1.8 8-6.6 10-13.4 6.2l-37-27.2-17.9 17.2c-2 2-3.6 3.6-7.3 3.6l2.6-37.3 67.9-61.4c2.9-2.6-.7-4.1-4.5-1.5L73.4 128.5l-35.7-11.2c-7.8-2.4-7.9-7.7 1.6-11.4l130.4-50.3c6.5-2.4 12.2 1.6 9.8 10.8z"
        fill="white"
      />
    </svg>
  );
}

function DiscordLogo({ size }: { size: number }) {
  return (
    <LogoBadge bg="#5865F2" size={size}>
      <svg viewBox="0 0 24 24" fill="white" style={{ width: size * 0.62, height: size * 0.62 }}>
        <path d="M20.317 4.3698a19.7913 19.7913 0 0 0-4.8851-1.5152.0741.0741 0 0 0-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 0 0-.0785-.037 19.7363 19.7363 0 0 0-4.8852 1.515.0699.0699 0 0 0-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 0 0 .0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 0 0 .0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 0 0-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 0 1-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 0 1 .0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 0 1 .0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 0 1-.0066.1276 12.2986 12.2986 0 0 1-1.873.8914.0766.0766 0 0 0-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 0 0 .0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 0 0 .0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 0 0-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189Z" />
      </svg>
    </LogoBadge>
  );
}

function SlackLogo({ size }: { size: number }) {
  return (
    <LogoBadge bg="#ffffff" size={size}>
      <svg viewBox="0 0 122.8 122.8" style={{ width: size * 0.56, height: size * 0.56 }}>
        <path d="M25.8 77.6c0 7.1-5.8 12.9-12.9 12.9S0 84.7 0 77.6s5.8-12.9 12.9-12.9h12.9v12.9z" fill="#E01E5A" />
        <path d="M32.3 77.6c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9v32.3c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V77.6z" fill="#E01E5A" />
        <path d="M45.2 25.8c-7.1 0-12.9-5.8-12.9-12.9S38.1 0 45.2 0s12.9 5.8 12.9 12.9v12.9H45.2z" fill="#36C5F0" />
        <path d="M45.2 32.3c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H12.9C5.8 58.1 0 52.3 0 45.2s5.8-12.9 12.9-12.9h32.3z" fill="#36C5F0" />
        <path d="M97 45.2c0-7.1 5.8-12.9 12.9-12.9s12.9 5.8 12.9 12.9-5.8 12.9-12.9 12.9H97V45.2z" fill="#2EB67D" />
        <path d="M90.5 45.2c0 7.1-5.8 12.9-12.9 12.9s-12.9-5.8-12.9-12.9V12.9C64.7 5.8 70.5 0 77.6 0s12.9 5.8 12.9 12.9v32.3z" fill="#2EB67D" />
        <path d="M77.6 97c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9-12.9-5.8-12.9-12.9V97h12.9z" fill="#ECB22E" />
        <path d="M77.6 90.5c-7.1 0-12.9-5.8-12.9-12.9s5.8-12.9 12.9-12.9h32.3c7.1 0 12.9 5.8 12.9 12.9s-5.8 12.9-12.9 12.9H77.6z" fill="#ECB22E" />
      </svg>
    </LogoBadge>
  );
}

function NtfyLogo({ size }: { size: number }) {
  return (
    <LogoBadge bg="#56a3df" size={size}>
      <Bell size={size * 0.52} color="white" strokeWidth={2.2} />
    </LogoBadge>
  );
}

function EmailLogo({ size }: { size: number }) {
  return (
    <LogoBadge bg="#64748b" size={size}>
      <Mail size={size * 0.52} color="white" strokeWidth={2.2} />
    </LogoBadge>
  );
}

function WebhookLogo({ size }: { size: number }) {
  return (
    <LogoBadge bg="#0ea5e9" size={size}>
      <Webhook size={size * 0.52} color="white" strokeWidth={2.2} />
    </LogoBadge>
  );
}

const PROVIDERS: ProviderMeta[] = [
  {
    id: "telegram",
    label: "Telegram",
    description: m.settings_notif_telegram_bot_api,
    logo: (s) => <TelegramLogo size={s} />,
    configuredField: "bot_token",
    fields: [
      { key: "bot_token", label: m.settings_notif_bot_token, type: "password", placeholder: "123456:ABC-DEF..." },
      { key: "chat_id", label: m.settings_notif_chat_id, type: "text", placeholder: "-1001234567890" },
    ],
  },
  {
    id: "discord",
    label: "Discord",
    description: m.settings_notif_discord_desc,
    logo: (s) => <DiscordLogo size={s} />,
    configuredField: "webhook_url",
    fields: [
      { key: "webhook_url", label: m.settings_notif_webhook_url, type: "password", placeholder: "https://discord.com/api/webhooks/…" },
    ],
  },
  {
    id: "ntfy",
    label: "ntfy",
    description: m.settings_notif_ntfy_desc,
    logo: (s) => <NtfyLogo size={s} />,
    configuredField: "topic",
    fields: [
      { key: "server", label: m.settings_notif_ntfy_server, type: "text", placeholder: "https://ntfy.sh" },
      { key: "topic", label: m.settings_notif_ntfy_topic, type: "text", placeholder: "my-topic" },
      { key: "username", label: m.settings_notif_username, type: "text" },
      { key: "password", label: m.settings_notif_password, type: "password" },
    ],
  },
  {
    id: "email",
    label: "Email",
    description: m.settings_notif_email_desc,
    logo: (s) => <EmailLogo size={s} />,
    configuredField: "host",
    fields: [
      { key: "host", label: m.settings_notif_email_host, type: "text", placeholder: "smtp.example.com" },
      { key: "port", label: m.settings_notif_email_port, type: "text", placeholder: "587" },
      { key: "username", label: m.settings_notif_username, type: "text" },
      { key: "password", label: m.settings_notif_password, type: "password" },
      { key: "from", label: m.settings_notif_email_from, type: "text", placeholder: "triagearr@example.com" },
    ],
  },
  {
    id: "slack",
    label: "Slack",
    description: m.settings_notif_slack_desc,
    logo: (s) => <SlackLogo size={s} />,
    configuredField: "webhook_url",
    fields: [
      { key: "webhook_url", label: m.settings_notif_webhook_url, type: "password", placeholder: "https://hooks.slack.com/services/…" },
    ],
  },
  {
    id: "webhook",
    label: "Webhook",
    description: m.settings_notif_webhook_native_desc,
    logo: (s) => <WebhookLogo size={s} />,
    configuredField: "url",
    fields: [
      { key: "url", label: m.settings_notif_webhook_target_url, type: "text", placeholder: "https://…" },
      { key: "secret", label: m.settings_notif_webhook_secret, type: "password" },
    ],
  },
];

// ── Tile ──────────────────────────────────────────────────────────────────

type TileStatus = "unconfigured" | "disabled" | "enabled";

const VISUAL: Record<TileStatus, VisualTileStatus> = {
  unconfigured: "unconfigured",
  disabled: "disabled",
  enabled: "healthy",
};

const STATUS_TEXT: Record<TileStatus, () => string> = {
  enabled: m.settings_notif_telegram_enabled_status,
  disabled: m.common_disabled,
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
      subtitle={meta.description()}
      connected={configured}
      status={VISUAL[status]}
      statusText={STATUS_TEXT[status]()}
      chips={configured ? [{ label: m.settings_chip_enabled(), on: enabled }] : undefined}
      renderLogo={(size) => (
        <div style={{ width: size, height: size, flexShrink: 0, display: "flex", alignItems: "center", justifyContent: "center" }}>
          {meta.logo(size)}
        </div>
      )}
      onClick={onClick}
    />
  );
}

// ── Credential field ────────────────────────────────────────────────────────

function CredentialField(p: {
  label: string;
  type: FieldType;
  placeholder?: string;
  value: string;
  onChange: (v: string) => void;
  dirty: boolean;
  overridden: boolean;
  onRevert: () => void;
}) {
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

// ── Routing controls ──────────────────────────────────────────────────────

const SEVERITIES = ["info", "warning", "error"] as const;
function severityLabel(s: string): string {
  if (s === "warning") return m.settings_notif_severity_warning();
  if (s === "error") return m.settings_notif_severity_error();
  return m.settings_notif_severity_info();
}

// eventKindLabel maps a backend event kind to its translated display name.
function eventKindLabel(kind: string): string {
  switch (kind) {
    case "run.executed":
      return m.settings_notif_event_run_executed();
    case "run.partial":
      return m.settings_notif_event_run_partial();
    case "run.failed":
      return m.settings_notif_event_run_failed();
    case "disk.target_unreachable":
      return m.settings_notif_event_disk_target_unreachable();
    case "health.degraded":
      return m.settings_notif_event_health_degraded();
    case "health.recovered":
      return m.settings_notif_event_health_recovered();
    case "test":
      return m.settings_notif_event_test();
    default:
      return kind;
  }
}

function RoutingControls({
  minSeverity,
  mute,
  catalogue,
  onMinSeverity,
  onToggleMute,
}: {
  minSeverity: string;
  mute: string[];
  catalogue: { kind: string; severity: string }[];
  onMinSeverity: (v: string) => void;
  onToggleMute: (kind: string, on: boolean) => void;
}) {
  return (
    <div className="space-y-3 pt-2 border-t border-border">
      <div>
        <div className="text-sm font-medium">{m.settings_notif_routing_title()}</div>
        <div className="text-xs text-muted-foreground mt-0.5">{m.settings_notif_min_severity_hint()}</div>
      </div>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground font-mono">{m.settings_notif_min_severity()}</label>
        <Select value={minSeverity || "info"} onChange={(e) => onMinSeverity(e.target.value)} className="w-full">
          {SEVERITIES.map((s) => (
            <option key={s} value={s}>
              {severityLabel(s)}
            </option>
          ))}
        </Select>
      </div>
      <div className="space-y-1.5">
        <label className="text-xs font-medium text-muted-foreground font-mono">{m.settings_notif_mute()}</label>
        <div className="text-xs text-muted-foreground">{m.settings_notif_mute_hint()}</div>
        <div className="grid gap-1.5">
          {catalogue
            .filter((e) => e.kind !== "test")
            .map((e) => {
              const muted = mute.includes(e.kind);
              return (
                <label key={e.kind} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={muted}
                    onChange={(ev) => onToggleMute(e.kind, ev.target.checked)}
                  />
                  <span>{eventKindLabel(e.kind)}</span>
                  <span className="text-[10px] uppercase text-muted-foreground">{severityLabel(e.severity)}</span>
                </label>
              );
            })}
        </div>
      </div>
    </div>
  );
}

// ── Provider drawer ─────────────────────────────────────────────────────────

type Pending = Record<string, string | null>;

function ProviderDrawer({ meta, open, onClose }: { meta: ProviderMeta | null; open: boolean; onClose: () => void }) {
  const settings = useSettings();
  const update = useUpdateSettings();
  const test = useTestNotification();
  const catalogue = useNotificationCatalogue();

  const [pending, setPending] = useState<Pending>({});
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const [applying, setApplying] = useState(false);

  useEffect(() => {
    if (settings.data) {
      setPending({});
      setApplying(false);
      setTestResult(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.dataUpdatedAt, meta?.id]);

  if (!meta) return null;

  const prefix = `notifications.${meta.id}`;
  const node = (settings.data?.values.notifications as Record<string, Record<string, unknown>> | undefined)?.[meta.id] ?? {};
  const tu = settings.data?.values.notifications.target_unreachable ?? {};
  const overridden = new Set(settings.data?.overridden_keys ?? []);

  const field = (suffix: string): string => {
    const key = `${prefix}.${suffix}`;
    if (key in pending) return pending[key] ?? "";
    const v = node[suffix];
    return v != null ? String(v) : "";
  };
  const dirty = (key: string) => key in pending;
  const overr = (key: string) => overridden.has(key);
  const set = (key: string, v: string | null) => setPending((p) => ({ ...p, [key]: v }));

  const enabledVal = field("enabled") === "true" || (!(`${prefix}.enabled` in pending) && node.enabled === true);

  // Mute list: pending JSON wins, else the persisted array.
  const muteCurrent: string[] = (() => {
    const key = `${prefix}.mute`;
    if (key in pending) {
      try {
        return JSON.parse(pending[key] ?? "[]");
      } catch {
        return [];
      }
    }
    return (node.mute as string[] | null) ?? [];
  })();
  const minSeverityCurrent = field("min_severity") || (node.min_severity as string) || "info";

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
    } catch (e) {
      setError(String(e));
      setApplying(false);
    }
  };

  const busy = update.isPending || applying;

  const onTest = async () => {
    setTestResult(null);
    try {
      await test.mutateAsync({ provider: meta.id });
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
          <div style={{ width: 32, height: 32 }}>{meta.logo(32)}</div>
          {meta.label}
        </div>
      }
    >
      {settings.isLoading ? (
        <div className="text-sm text-muted-foreground">{m.common_loading()}</div>
      ) : (
        <div className="space-y-6">
          <ToggleRow
            label={m.settings_notif_enable_provider({ provider: meta.label })}
            hint={m.settings_notif_enable_hint()}
            checked={enabledVal}
            onChange={(v) => set(`${prefix}.enabled`, String(v))}
          />

          <div className="space-y-4">
            {meta.fields.map((f) => {
              const key = `${prefix}.${f.key}`;
              return (
                <CredentialField
                  key={key}
                  label={f.label()}
                  type={f.type}
                  placeholder={f.placeholder}
                  value={field(f.key)}
                  onChange={(v) => set(key, v)}
                  dirty={dirty(key)}
                  overridden={overr(key)}
                  onRevert={() => set(key, null)}
                />
              );
            })}
            {meta.id === "email" && (
              <CredentialField
                label={m.settings_notif_email_to()}
                type="text"
                placeholder="a@example.com, b@example.com"
                value={(() => {
                  const key = `${prefix}.to`;
                  if (key in pending) {
                    try {
                      return (JSON.parse(pending[key] ?? "[]") as string[]).join(", ");
                    } catch {
                      return "";
                    }
                  }
                  return ((node.to as string[] | null) ?? []).join(", ");
                })()}
                onChange={(v) =>
                  set(
                    `${prefix}.to`,
                    JSON.stringify(
                      v
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    ),
                  )
                }
                dirty={dirty(`${prefix}.to`)}
                overridden={overr(`${prefix}.to`)}
                onRevert={() => set(`${prefix}.to`, null)}
              />
            )}
          </div>

          <RoutingControls
            minSeverity={minSeverityCurrent}
            mute={muteCurrent}
            catalogue={catalogue.data ?? []}
            onMinSeverity={(v) => set(`${prefix}.min_severity`, v)}
            onToggleMute={(kind, on) => {
              const next = on ? [...muteCurrent, kind] : muteCurrent.filter((k) => k !== kind);
              set(`${prefix}.mute`, JSON.stringify(next));
            }}
          />

          {/* Global target-unreachable reminder cadence (ADR-0032). */}
          <div className="space-y-3 pt-2 border-t border-border">
            <div>
              <div className="text-sm font-medium">{m.settings_notif_alerts_title()}</div>
              <div className="text-xs text-muted-foreground mt-0.5">{m.settings_notif_reminder_interval_hint()}</div>
            </div>
            <CredentialField
              label={m.settings_notif_reminder_interval()}
              type="text"
              placeholder="24h"
              value={
                "notifications.target_unreachable.reminder_interval" in pending
                  ? (pending["notifications.target_unreachable.reminder_interval"] ?? "")
                  : (tu.reminder_interval ?? "")
              }
              onChange={(v) => set("notifications.target_unreachable.reminder_interval", v)}
              dirty={dirty("notifications.target_unreachable.reminder_interval")}
              overridden={overr("notifications.target_unreachable.reminder_interval")}
              onRevert={() => set("notifications.target_unreachable.reminder_interval", null)}
            />
          </div>

          {error && (
            <div className="text-sm text-destructive border border-destructive/50 rounded-md p-2">{error}</div>
          )}

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
            {busy && <span className="text-xs text-muted-foreground">{m.settings_shell_daemon_restarting()}</span>}
          </div>

          <div className="space-y-2 pt-2 border-t border-border">
            <div className="text-xs text-muted-foreground">{m.settings_notif_test_desc()}</div>
            <div className="flex items-center gap-3">
              <Button variant="outline" onClick={onTest} disabled={test.isPending || dirtyCount > 0}>
                {test.isPending ? m.settings_notif_sending() : m.settings_notif_send_test()}
              </Button>
              {dirtyCount > 0 && <span className="text-xs text-muted-foreground">{m.settings_notif_save_first()}</span>}
            </div>
            {testResult && (
              <div
                className={cn(
                  "text-sm rounded-md border p-2",
                  testResult.ok ? "text-foreground border-border" : "text-destructive border-destructive/50",
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

// ── Event catalogue + deliveries panels ─────────────────────────────────────

function CataloguePanel() {
  const catalogue = useNotificationCatalogue();
  if (!catalogue.data?.length) return null;
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-semibold">{m.settings_notif_catalogue_title()}</h3>
      <div className="grid gap-1 rounded-lg border border-border p-3">
        {catalogue.data.map((e) => (
          <div key={e.kind} className="flex items-center gap-2 text-sm">
            <span>{eventKindLabel(e.kind)}</span>
            <span className="text-[10px] uppercase text-muted-foreground">{severityLabel(e.severity)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function DeliveriesPanel() {
  const deliveries = useNotificationDeliveries();
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-semibold">{m.settings_notif_deliveries_title()}</h3>
      {!deliveries.data?.length ? (
        <div className="text-sm text-muted-foreground rounded-lg border border-border p-3">
          {m.settings_notif_deliveries_empty()}
        </div>
      ) : (
        <div className="grid gap-1 rounded-lg border border-border p-3">
          {deliveries.data.map((d, i) => (
            <div key={i} className="flex items-center gap-2 text-sm">
              <span className={cn("text-xs", d.ok ? "text-emerald-500" : "text-destructive")}>
                {d.ok ? m.settings_notif_delivery_ok() : m.settings_notif_delivery_failed()}
              </span>
              <span className="font-medium">{d.provider}</span>
              <span className="text-xs text-muted-foreground">{eventKindLabel(d.kind)}</span>
              {d.error && <span className="text-xs text-destructive truncate">{d.error}</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Main section ──────────────────────────────────────────────────────────

export function NotificationSection() {
  const settings = useSettings();
  const [open, setOpen] = useState<string | null>(null);

  const notif = settings.data?.values.notifications as Record<string, Record<string, unknown>> | undefined;
  const openMeta = PROVIDERS.find((p) => p.id === open) ?? null;

  return (
    <>
      <div className="space-y-6">
        <div>
          <h2 className="text-lg font-semibold">{m.settings_notif_title()}</h2>
          <p className="text-sm text-muted-foreground mt-1">{m.settings_notif_description()}</p>
        </div>

        <div className="arr-tile-grid">
          {PROVIDERS.map((meta) => {
            const node = notif?.[meta.id] ?? {};
            return (
              <ProviderTile
                key={meta.id}
                meta={meta}
                enabled={node.enabled === true}
                configured={Boolean(node[meta.configuredField])}
                onClick={() => setOpen(meta.id)}
              />
            );
          })}
        </div>

        <CataloguePanel />
        <DeliveriesPanel />
      </div>

      <ProviderDrawer meta={openMeta} open={open !== null} onClose={() => setOpen(null)} />
    </>
  );
}
