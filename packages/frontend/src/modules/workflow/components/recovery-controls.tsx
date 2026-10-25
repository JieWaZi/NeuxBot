import { useMutation } from "@tanstack/react-query";
import { SkipForward } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { skipWorkflowWorkerMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

export function SkipWorkerControl({
  id,
  runId,
  instanceId,
  generation,
  skipRequested,
  onChanged,
}: {
  id: string;
  runId: string;
  instanceId: string;
  generation: number;
  skipRequested: boolean;
  onChanged: () => void;
}) {
  const { t } = useTranslation("workflow");
  const [requestId, setRequestId] = useState<string>();
  const skip = useMutation(
    withLocalErrorMutation(skipWorkflowWorkerMutation()),
  );
  const submit = async () => {
    if (!requestId) return;
    try {
      await skip.mutateAsync({
        path: { id, runId, instanceId },
        body: { requestId, attemptGeneration: generation },
      });
      setRequestId(undefined);
      onChanged();
    } catch {
      /* Mutation owns the local error. */
    }
  };
  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        disabled={skipRequested}
        onClick={() => {
          skip.reset();
          setRequestId(crypto.randomUUID());
        }}
      >
        <SkipForward />
        {t(skipRequested ? "skipStopping" : "skipWorker")}
      </Button>
      <Dialog
        open={Boolean(requestId)}
        onOpenChange={(open) => {
          if (!open && !skip.isPending) setRequestId(undefined);
        }}
      >
        <DialogContent
          title={t("skipWorker")}
          description={t("skipWorkerHelp")}
        >
          {skip.error ? (
            <p role="alert" className="text-destructive type-body px-6 pb-5">
              {localApiErrorMessage(skip.error, t("error"))}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={skip.isPending}
              onClick={() => setRequestId(undefined)}
            >
              {t("close")}
            </Button>
            <Button disabled={skip.isPending} onClick={() => void submit()}>
              {t("skipWorker")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
