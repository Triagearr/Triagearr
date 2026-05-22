import { useState } from "react";
import { useTestNotification } from "@/api/hooks";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";
import { Field, SectionShell, Subsection } from "./SettingsField";

// TelegramTestButton sends a synthetic notification through the saved config.
// It is disabled while there are unsaved edits, since the test exercises the
// daemon's currently-loaded config — not the pending form values.
function TelegramTestButton({ hasUnsaved }: { hasUnsaved: boolean }) {
  const test = useTestNotification();
  const [result, setResult] = useState<{ ok: boolean; msg: string } | null>(null);

  const onTest = async () => {
    setResult(null);
    try {
      await test.mutateAsync();
      setResult({ ok: true, msg: "Test notification sent — check your Telegram chat." });
    } catch (e) {
      setResult({ ok: false, msg: e instanceof Error ? e.message : String(e) });
    }
  };

  return (
    <div className="space-y-2 pt-2">
      <div className="flex items-center gap-3">
        <Button variant="outline" onClick={onTest} disabled={test.isPending || hasUnsaved}>
          {test.isPending ? "Sending…" : "Send test notification"}
        </Button>
        {hasUnsaved && (
          <span className="text-xs text-muted-foreground">Save changes first to test them.</span>
        )}
      </div>
      {result && (
        <div
          className={cn(
            "text-sm rounded-md border p-2",
            result.ok ? "text-foreground border-border" : "text-destructive border-destructive/50",
          )}
        >
          {result.msg}
        </div>
      )}
    </div>
  );
}

export function NotificationSection() {
  return (
    <SectionShell
      title="Notifications"
      description="A notification is sent only when a disk-pressure run actually deletes (or attempts to delete) media — manual runs stay silent. Credentials are stored as runtime overrides."
      render={(h) => {
        const tg = h.settings.values.notifications.telegram;
        const hasUnsaved = Object.keys(h.pending).length > 0;
        return (
          <>
            <Subsection title="Telegram">
              <Field
                label="Enabled"
                keyName="notifications.telegram.enabled"
                type="checkbox"
                value={h.fieldValue("notifications.telegram.enabled", tg.enabled ?? false)}
                onChange={(v) => h.setField("notifications.telegram.enabled", v)}
                overridden={h.isOverridden("notifications.telegram.enabled")}
                dirty={h.isDirty("notifications.telegram.enabled")}
                onRevert={() => h.revert("notifications.telegram.enabled")}
              />
              <Field
                label="Bot token"
                keyName="notifications.telegram.bot_token"
                type="password"
                placeholder="123456:ABC-DEF..."
                value={h.fieldValue("notifications.telegram.bot_token", tg.bot_token)}
                onChange={(v) => h.setField("notifications.telegram.bot_token", v)}
                overridden={h.isOverridden("notifications.telegram.bot_token")}
                dirty={h.isDirty("notifications.telegram.bot_token")}
                onRevert={() => h.revert("notifications.telegram.bot_token")}
              />
              <Field
                label="Chat ID"
                keyName="notifications.telegram.chat_id"
                type="text"
                placeholder="-1001234567890"
                value={h.fieldValue("notifications.telegram.chat_id", tg.chat_id)}
                onChange={(v) => h.setField("notifications.telegram.chat_id", v)}
                overridden={h.isOverridden("notifications.telegram.chat_id")}
                dirty={h.isDirty("notifications.telegram.chat_id")}
                onRevert={() => h.revert("notifications.telegram.chat_id")}
              />
            </Subsection>
            <TelegramTestButton hasUnsaved={hasUnsaved} />
          </>
        );
      }}
    />
  );
}
