import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  deleteChatMutation,
  evictDeletedChatsFromCache,
} from "../api/chat-queries";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type ChatBatchDeleteDialogProps = {
  chatIds: string[];
  onChanged?: () => Promise<void> | void;
  onDeleted: (chatIds: string[]) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
};

export function ChatBatchDeleteDialog({
  chatIds,
  onChanged,
  onDeleted,
  onOpenChange,
  open,
}: ChatBatchDeleteDialogProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const remove = useMutation(withLocalErrorMutation(deleteChatMutation()));
  const [remainingIds, setRemainingIds] = useState(chatIds);
  const [failedCount, setFailedCount] = useState(0);
  const [isDeleting, setIsDeleting] = useState(false);

  const close = () => {
    if (isDeleting) return;
    remove.reset();
    onOpenChange(false);
  };
  const confirm = async () => {
    setIsDeleting(true);
    setFailedCount(0);
    const deletedIds: string[] = [];
    const failedIds: string[] = [];

    for (const chatId of remainingIds) {
      try {
        await remove.mutateAsync({ path: { chatId } });
        deletedIds.push(chatId);
      } catch {
        failedIds.push(chatId);
      }
    }

    if (deletedIds.length > 0) {
      evictDeletedChatsFromCache(queryClient, deletedIds);
      onDeleted(deletedIds);
    }
    await queryClient.invalidateQueries({
      queryKey: ["chats", "list"],
    });
    await onChanged?.();

    setIsDeleting(false);
    if (failedIds.length === 0) {
      onOpenChange(false);
      return;
    }
    setRemainingIds(failedIds);
    setFailedCount(failedIds.length);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
      }}
    >
      <DialogContent
        size="sm"
        title={t("taskPage.batchDeleteTitle")}
        description={t("taskPage.batchDeleteDescription")}
      >
        <div className="px-6 pb-5">
          <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
            <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
            <p>
              {t("taskPage.batchDeleteWarning", {
                count: remainingIds.length,
              })}
            </p>
          </div>
          {failedCount > 0 ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {t("taskPage.batchDeleteFailed", { count: failedCount })}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            disabled={isDeleting}
            onClick={close}
          >
            {t("cancelAction")}
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={isDeleting}
            onClick={() => void confirm()}
          >
            {isDeleting ? <LoaderCircle className="animate-spin" /> : null}
            {t("taskPage.deleteSelected")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
