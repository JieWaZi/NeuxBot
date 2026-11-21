import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { cancelChatExecutionMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogTrigger,
} from "@/shared/ui/dialog";

export type WorkflowInvocationStop = {
  executionId: string;
  beforeStart: boolean;
};

export function CancelWorkflowInvocation({
  chatId,
  executionId,
  beforeStart,
}: {
  chatId: string;
} & WorkflowInvocationStop) {
  const { t } = useTranslation("chat");
  const cancel = useMutation(
    withLocalErrorMutation(cancelChatExecutionMutation()),
  );
  const action = t(
    beforeStart ? "workflow.cancelInvocation" : "workflow.stopTurn",
  );
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Dialog>
        <DialogTrigger asChild>
          <Button
            type="button"
            variant="danger-quiet"
            size="sm"
            loading={cancel.isPending}
            disabled={cancel.isSuccess}
          >
            {action}
          </Button>
        </DialogTrigger>
        <DialogContent
          title={action}
          description={t(
            beforeStart
              ? "workflow.cancelInvocationHelp"
              : "workflow.stopTurnHelp",
          )}
          size="sm"
        >
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="secondary">
                {t("common:cancel")}
              </Button>
            </DialogClose>
            <DialogClose asChild>
              <Button
                type="button"
                variant="danger"
                onClick={() =>
                  cancel.mutate({ path: { chatId }, query: { executionId } })
                }
              >
                {action}
              </Button>
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {cancel.error ? (
        <p role="alert" className="text-destructive type-ui">
          {localApiErrorMessage(cancel.error, t("workflow.cancelFailed"))}
        </p>
      ) : null}
    </div>
  );
}
