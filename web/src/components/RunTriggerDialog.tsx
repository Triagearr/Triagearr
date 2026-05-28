import { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Callout } from "@/components/ui/Callout";
import { Input } from "@/components/ui/Input";
import { useTriggerRun } from "@/api/hooks";
import { m } from "@/paraglide/messages";

type Props = {
  open: boolean;
  onClose: () => void;
  onSuccess?: (runId: number) => void;
  mode: "dry-run" | "live";
};

const liveConfirmPhrase = "delete";

export function RunTriggerDialog({ open, onClose, onSuccess, mode }: Props) {
  const [typed, setTyped] = useState("");
  const trigger = useTriggerRun();
  const isLive = mode === "live";
  const armed = !isLive || typed === liveConfirmPhrase;

  const close = () => {
    setTyped("");
    trigger.reset();
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={close}
      title={isLive ? m.comp_run_execute_live_title() : m.comp_run_plan_dryrun_title()}
      description={
        isLive
          ? m.comp_run_live_description({ phrase: liveConfirmPhrase })
          : m.comp_run_dryrun_description()
      }
    >
      {isLive && (
        <Input
          autoFocus
          placeholder={m.comp_run_confirm_placeholder({ phrase: liveConfirmPhrase })}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          className="mb-3"
        />
      )}

      {trigger.isError && (
        <Callout className="mb-3">{String(trigger.error)}</Callout>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={close}>
          {m.common_cancel()}
        </Button>
        <Button
          variant={isLive ? "destructive" : "default"}
          disabled={!armed || trigger.isPending}
          onClick={() =>
            trigger.mutate(
              { mode },
              {
                onSuccess: (data) => {
                  close();
                  onSuccess?.(data.run_id);
                },
              },
            )
          }
        >
          {trigger.isPending ? m.comp_run_running() : isLive ? m.comp_run_execute_live() : m.comp_run_plan_dryrun()}
        </Button>
      </div>
    </Modal>
  );
}
