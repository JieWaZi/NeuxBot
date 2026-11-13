import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, Sparkles } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@/shared/api/errors";
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
  revision,
  maxTokens,
  onClose,
  onCloseAutoFocus,
  onOptimize,
  wakerId,
}: {
  content: string;
  revision?: string;
  maxTokens?: number;
  onClose: () => void;
  onCloseAutoFocus: (event: Event) => void;
  onOptimize: () => void;
  wakerId: string;
}) {
  const { t } = useTranslation(["waker", "common"]);
  const queryClient = useQueryClient();
  const update = useMutation(
    withLocalErrorMutation(updateWakerDocumentMutation()),
  );
  const [initial] = useState({ content, revision });
  const [draft, setDraft] = useState(initial.content);
  const [discardAction, setDiscardAction] = useState<"close" | "optimize">();
  const dirty = draft !== initial.content;

  const requestClose = () => {
    if (update.isPending) return;
    if (dirty) setDiscardAction("close");
    else onClose();
  };
  const optimize = () => {
    if (dirty) setDiscardAction("optimize");
    else onOptimize();
  };

  const save = async () => {
    const path = { kind: "agents" as const, wakerId };
    try {
      await update.mutateAsync({
        path,
        body: { content: draft, expectedRevision: initial.revision },
      });
      await queryClient.invalidateQueries({
        queryKey: getWakerDocumentQueryKey({ path }),
      });
      onClose();
    } catch (error) {
      if (error instanceof ApiError && error.code === "WakerProfileChanged") {
        void queryClient.invalidateQueries({
          queryKey: getWakerDocumentQueryKey({ path }),
        });
      }
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (open || update.isPending) return;
        if (discardAction) setDiscardAction(undefined);
        else requestClose();
      }}
    >
      <DialogContent
        layout={discardAction ? "content" : "form"}
        size={discardAction ? "sm" : "lg"}
        title={discardAction ? t("profile.discardTitle") : t("profile.edit")}
        description={
          discardAction ? t("profile.discardDescription") : undefined
        }
        onCloseAutoFocus={onCloseAutoFocus}
        onPointerDownOutside={(event) => {
          if (!dirty || discardAction) return;
          event.preventDefault();
          requestClose();
        }}
        onEscapeKeyDown={(event) => {
          if (!dirty || discardAction) return;
          event.preventDefault();
          requestClose();
        }}
      >
        {discardAction ? (
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              onClick={() => setDiscardAction(undefined)}
            >
              {t("profile.keepEditing")}
            </Button>
            <Button
              type="button"
              variant="danger"
              onClick={discardAction === "optimize" ? onOptimize : onClose}
            >
              {t("profile.discard")}
            </Button>
          </DialogFooter>
        ) : (
          <>
            <div className="flex min-h-0 flex-1 flex-col gap-3 px-6 pb-5">
              <Textarea
                aria-label={t("profile.markdown")}
                placeholder={t("profile.markdownPlaceholder")}
                className="min-h-0 flex-1 resize-none overflow-y-auto font-mono"
                value={draft}
                disabled={update.isPending}
                onChange={(event) => setDraft(event.target.value)}
              />
              {update.error ? (
                <p role="alert" className="text-destructive type-body shrink-0">
                  {localApiErrorMessage(update.error, t("profile.saveError"))}
                </p>
              ) : null}
            </div>
            <DialogFooter className="flex-wrap">
              <Button
                type="button"
                variant="ghost"
                className="mr-auto"
                disabled={update.isPending}
                onClick={optimize}
              >
                <Sparkles />
                {t("profile.optimize")}
              </Button>
              {maxTokens ? (
                <span className="text-muted-foreground type-caption mr-2">
                  {t("profile.tokenLimit", { count: maxTokens })}
                </span>
              ) : null}
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
