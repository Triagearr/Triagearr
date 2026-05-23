import { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { useTriggerRun } from "@/api/hooks";

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
      title={isLive ? "Execute live run?" : "Plan dry-run?"}
      description={
        isLive
          ? `Live mode deletes media via *arr and removes torrents from qBit. Type "${liveConfirmPhrase}" to confirm.`
          : "Dry-run computes candidates and persists the plan without deleting anything."
      }
    >
      {isLive && (
        <Input
          autoFocus
          placeholder={`Type "${liveConfirmPhrase}" to confirm`}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          className="mb-3"
        />
      )}

      {trigger.isError && (
        <div className="mb-3 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
          {String(trigger.error)}
        </div>
      )}

      <div className="flex justify-end gap-2">
        <Button variant="outline" onClick={close}>
          Cancel
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
          {trigger.isPending ? "Running…" : isLive ? "Execute live" : "Plan dry-run"}
        </Button>
      </div>
    </Modal>
  );
}
