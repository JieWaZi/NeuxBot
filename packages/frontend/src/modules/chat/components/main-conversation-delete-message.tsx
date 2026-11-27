import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { deleteMainConversationMessage } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import type { ConversationSubject } from "../api/main-conversation";

export function MainConversationDeleteMessage({
  subject,
  messageId,
  trigger,
  close,
  onDeleted,
}: {
  subject: ConversationSubject;
  messageId: string;
  trigger: HTMLButtonElement;
  close: () => void;
  onDeleted: (messageId: string) => Promise<void>;
}) {
  const { t } = useTranslation("chat");
  const remove = useMutation({
    mutationFn: async () => {
      await deleteMainConversationMessage(
        withLocalErrorSurface({
          path: { ...subject, messageId },
          throwOnError: true,
        }),
      );
    },
    onSuccess: async () => {
      await onDeleted(messageId);
      close();
    },
  });
  return (
    <Dialog open onOpenChange={(open) => !open && !remove.isPending && close()}>
      <DialogContent
        size="sm"
        title={t("deletePublicMessage")}
        description={t("deletePublicMessageDescription")}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (trigger.isConnected) trigger.focus({ preventScroll: true });
        }}
      >
        {remove.isError && (
          <p role="alert" className="text-destructive type-body px-6">
            {localApiErrorMessage(remove.error, t("deletePublicMessageFailed"))}
          </p>
        )}
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            disabled={remove.isPending}
            onClick={close}
          >
            {t("common:cancel")}
          </Button>
          <Button
            type="button"
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate()}
          >
            {t("common:delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
