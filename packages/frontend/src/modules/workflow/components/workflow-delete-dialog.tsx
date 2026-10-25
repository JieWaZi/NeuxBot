import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { deleteWorkflowMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { listWorkflowsQueryKey } from "../api/workflow-queries";

export function WorkflowDeleteDialog({
  id,
  revisionId,
  open,
  onOpenChange,
  onDeleted,
}: {
  id: string;
  revisionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
}) {
  const { t } = useTranslation("workflow");
  const client = useQueryClient();
  const remove = useMutation(withLocalErrorMutation(deleteWorkflowMutation()));
  const confirmDelete = async () => {
    try {
      await remove.mutateAsync({
        path: { id },
        body: { expectedRevisionId: revisionId },
      });
      onOpenChange(false);
      onDeleted?.();
      await client.invalidateQueries({ queryKey: listWorkflowsQueryKey() });
    } catch {
      /* Mutation owns error. */
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!remove.isPending) onOpenChange(value);
      }}
    >
      <DialogContent
        title={t("deleteWorkflow")}
        description={t("deleteWorkflowHelp")}
      >
        {remove.error ? (
          <p role="alert" className="text-destructive type-body px-6 pb-5">
            {localApiErrorMessage(remove.error, t("error"))}
          </p>
        ) : null}
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={remove.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => void confirmDelete()}
          >
            {t("deleteWorkflow")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
