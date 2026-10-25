import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import {
  getWorkBoardSummaryQueryKey,
  listWorkBoardQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  deleteWorkflowRunMutation,
  refreshWorkflow,
} from "../api/workflow-queries";
import { runTime } from "../model/run-presentation";
import type { WorkflowRun } from "../model/types";

export function RunDeleteDialog({
  id,
  run,
  number,
  open,
  onClose,
  onDeleted,
}: {
  id: string;
  run: WorkflowRun;
  number: number;
  open: boolean;
  onClose: () => void;
  onDeleted: (runId: string) => void;
}) {
  const { t, i18n } = useTranslation("workflow");
  const client = useQueryClient();
  const remove = useMutation(
    withLocalErrorMutation(deleteWorkflowRunMutation()),
  );
  const confirmDelete = async () => {
    if (!run.id) return;
    try {
      await remove.mutateAsync({ path: { id, runId: run.id } });
      onDeleted(run.id);
      onClose();
      await Promise.all([
        refreshWorkflow(client, id),
        client.invalidateQueries({ queryKey: listWorkBoardQueryKey() }),
        client.invalidateQueries({ queryKey: getWorkBoardSummaryQueryKey() }),
      ]);
    } catch {
      /* 删除失败时保留确认与所选记录，错误由 Mutation 展示。 */
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value && !remove.isPending) onClose();
      }}
    >
      <DialogContent
        title={t("deleteRunTitle")}
        description={t("deleteRunHelp")}
      >
        <div className="grid gap-2 px-6 pb-5">
          <p className="type-body">{t("runNumber", { number })}</p>
          {run.createdAt ? (
            <p className="text-content-caption type-caption">
              {runTime(run.createdAt, i18n.language)}
            </p>
          ) : null}
          {remove.error ? (
            <p role="alert" className="text-destructive type-body">
              {localApiErrorMessage(remove.error, t("deleteRunError"))}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={remove.isPending}
            onClick={onClose}
          >
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => void confirmDelete()}
          >
            {t("deleteRunConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
