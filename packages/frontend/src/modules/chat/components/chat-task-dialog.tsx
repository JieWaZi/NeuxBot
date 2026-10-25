import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  deleteChatMutation,
  evictDeletedChatsFromCache,
  updateChatMutation,
} from "../api/chat-queries";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";

type ChatTaskDialogProps = {
  chatId: string;
  mode: "rename" | "delete";
  open: boolean;
  title: string;
  onChanged?: () => Promise<void> | void;
  onOpenChange: (open: boolean) => void;
  onDeleted: (deletedChatId: string) => void;
};

export function ChatTaskDialog({
  chatId,
  mode,
  onChanged,
  onDeleted,
  onOpenChange,
  open,
  title,
}: ChatTaskDialogProps) {
  const { t } = useTranslation("chat");
  const queryClient = useQueryClient();
  const [nextTitle, setNextTitle] = useState(title);
  const rename = useMutation(withLocalErrorMutation(updateChatMutation()));
  const remove = useMutation(withLocalErrorMutation(deleteChatMutation()));

  const close = () => {
    rename.reset();
    remove.reset();
    onOpenChange(false);
  };
  const confirm = async () => {
    try {
      if (mode === "rename") {
        await rename.mutateAsync({
          path: { chatId },
          body: { title: nextTitle.trim() },
        });
      } else {
        await remove.mutateAsync({ path: { chatId } });
        evictDeletedChatsFromCache(queryClient, [chatId]);
        onDeleted(chatId);
      }
      await queryClient.invalidateQueries({
        queryKey: ["chats", "list"],
      });
      await onChanged?.();
      close();
    } catch {
      // React Query 保存错误，弹窗就近展示。
    }
  };
  const pending = rename.isPending || remove.isPending;
  const error = rename.error || remove.error;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
      }}
    >
      <DialogContent
        size="sm"
        title={mode === "rename" ? t("renameTask") : t("deleteTask")}
        description={mode === "delete" ? t("deleteTaskDescription") : undefined}
      >
        <div className="px-6 pb-5">
          {mode === "rename" ? (
            <Input
              autoFocus
              aria-label={t("taskTitle")}
              maxLength={120}
              value={nextTitle}
              onChange={(event) => setNextTitle(event.target.value)}
            />
          ) : (
            <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
              <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
              <p>{t("deleteTaskWarning", { title })}</p>
            </div>
          )}
          {error && (
            <p role="alert" className="text-destructive type-body mt-3">
              {localApiErrorMessage(
                rename.isError ? rename.error : remove.error,
                t("taskActionFailed"),
              )}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close}>
            {t("cancelAction")}
          </Button>
          <Button
            type="button"
            variant={mode === "delete" ? "danger" : "primary"}
            disabled={pending || (mode === "rename" && !nextTitle.trim())}
            onClick={() => void confirm()}
          >
            {pending && <LoaderCircle className="animate-spin" />}
            {mode === "rename" ? t("saveTask") : t("deleteTask")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
