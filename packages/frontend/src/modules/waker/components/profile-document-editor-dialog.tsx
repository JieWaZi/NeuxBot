import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  getWakerDocumentQueryKey,
  updateWakerDocumentMutation,
} from "../api/waker-queries";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";

export function ProfileDocumentEditorDialog({
  content,
  onClose,
  wakerId,
}: {
  content: string;
  onClose: () => void;
  wakerId: string;
}) {
  const { t } = useTranslation(["waker", "common"]);
  const queryClient = useQueryClient();
  const update = useMutation(
    withLocalErrorMutation(updateWakerDocumentMutation()),
  );
  const [initialContent] = useState(content);
  const [draft, setDraft] = useState(initialContent);
  const [confirmClose, setConfirmClose] = useState(false);
  const dirty = draft !== initialContent;

  const requestClose = () => {
    if (update.isPending) return;
    if (dirty) setConfirmClose(true);
    else onClose();
  };

  const save = async () => {
    const path = { kind: "agents" as const, wakerId };
    try {
      await update.mutateAsync({ path, body: { content: draft } });
      await queryClient.invalidateQueries({
        queryKey: getWakerDocumentQueryKey({ path }),
      });
      onClose();
    } catch {
      // Mutation 保留错误，编辑内容留在当前弹窗。
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (open || update.isPending) return;
        if (confirmClose) setConfirmClose(false);
        else requestClose();
      }}
    >
      <DialogContent
        size={confirmClose ? "sm" : "lg"}
        title={confirmClose ? t("profile.discardTitle") : t("profile.edit")}
        description={confirmClose ? t("profile.discardDescription") : undefined}
        onPointerDownOutside={(event) => {
          if (!dirty || confirmClose) return;
          event.preventDefault();
          requestClose();
        }}
        onEscapeKeyDown={(event) => {
          if (!dirty || confirmClose) return;
          event.preventDefault();
          requestClose();
        }}
      >
        {confirmClose ? (
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              onClick={() => setConfirmClose(false)}
            >
              {t("profile.keepEditing")}
            </Button>
            <Button type="button" variant="danger" onClick={onClose}>
              {t("profile.discard")}
            </Button>
          </DialogFooter>
        ) : (
          <>
            <div className="min-h-0 flex-1 px-6 pb-5">
              <Textarea
                aria-label={t("profile.markdown")}
                placeholder={t("profile.markdownPlaceholder")}
                className="min-h-64 resize-y font-mono"
                value={draft}
                disabled={update.isPending}
                onChange={(event) => setDraft(event.target.value)}
              />
              {update.error ? (
                <p role="alert" className="text-destructive type-body mt-3">
                  {localApiErrorMessage(update.error, t("profile.saveError"))}
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button
                type="button"
                disabled={!dirty || update.isPending}
                onClick={() => void save()}
              >
                {update.isPending ? (
                  <LoaderCircle className="animate-spin" />
                ) : null}
                {t("common:save")}
              </Button>
              <Button
                type="button"
                variant="secondary"
                disabled={update.isPending}
                onClick={requestClose}
              >
                {t("common:cancel")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
