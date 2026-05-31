import { useState } from "react";
import { useSettings, useUpdateSettings } from "@/api/hooks";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Callout } from "@/components/ui/Callout";
import { Modal } from "@/components/ui/Modal";
import { cn } from "@/lib/cn";
import { m } from "@/paraglide/messages";

// ModeSection is intentionally standalone (not built on SectionShell): mode is
// a single high-stakes switch, so arming it (dry-run → live) is committed
// atomically behind a confirmation dialog rather than the usual edit-then-save
// flow. Disarming (→ dry-run) applies immediately, without friction.
export function ModeSection() {
  const settings = useSettings();
  const update = useUpdateSettings();
  const [confirmOpen, setConfirmOpen] = useState(false);

  if (settings.isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{m.settings_mode_title()}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">{m.common_loading()}</CardContent>
      </Card>
    );
  }
  if (settings.isError || !settings.data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{m.settings_mode_title()}</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-destructive">
          {String(settings.error ?? m.settings_shell_no_data())}
        </CardContent>
      </Card>
    );
  }

  const isLive = settings.data.values.mode === "live";
  const isOverridden = (settings.data.overridden_keys ?? []).includes("mode");
  const busy = update.isPending;

  const apply = (value: "live" | "dry-run" | null) =>
    update.mutateAsync([{ key: "mode", value }]);

  // Disarming is frictionless; arming routes through the confirmation dialog.
  const onToggle = (next: boolean) => {
    if (next) setConfirmOpen(true);
    else void apply("dry-run");
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.settings_mode_title()}</CardTitle>
        <CardDescription>{m.settings_mode_description()}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex items-center justify-between rounded-lg border border-border p-4">
          <div>
            <div className="flex items-center gap-2 text-sm font-medium">
              {m.settings_mode_toggle_label()}
              {isLive ? (
                <Badge variant="destructive">● {m.common_mode_live()}</Badge>
              ) : (
                <Badge variant="muted">{m.common_mode_dry_run()}</Badge>
              )}
              {isOverridden && <Badge variant="outline">{m.settings_mode_overridden()}</Badge>}
            </div>
            <div className="text-xs text-muted-foreground mt-0.5">
              {m.settings_mode_toggle_hint()}
            </div>
          </div>
          <button
            role="switch"
            aria-checked={isLive}
            disabled={busy}
            onClick={() => onToggle(!isLive)}
            className={cn(
              "relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50",
              isLive ? "bg-rose-600" : "bg-input",
            )}
          >
            <span
              className={cn(
                "pointer-events-none inline-block h-5 w-5 rounded-full bg-background shadow-lg transition-transform",
                isLive ? "translate-x-5" : "translate-x-0",
              )}
            />
          </button>
        </div>

        {isOverridden && !busy && (
          <Button variant="outline" onClick={() => void apply(null)}>
            {m.settings_mode_revert_yaml()}
          </Button>
        )}

        {update.isError && <Callout>{String(update.error)}</Callout>}
      </CardContent>

      <Modal
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        title={m.settings_mode_confirm_title()}
        description={m.settings_mode_confirm_body()}
      >
        {update.isError && <Callout className="mb-3">{String(update.error)}</Callout>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirmOpen(false)} disabled={busy}>
            {m.settings_mode_confirm_cancel()}
          </Button>
          <Button
            variant="destructive"
            disabled={busy}
            onClick={() =>
              void apply("live").then(() => setConfirmOpen(false))
            }
          >
            {busy ? m.settings_shell_saving_reloading() : m.settings_mode_confirm_cta()}
          </Button>
        </div>
      </Modal>
    </Card>
  );
}
