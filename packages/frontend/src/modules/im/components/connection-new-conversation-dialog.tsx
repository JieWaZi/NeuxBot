import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { selectImConversation } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { Skeleton } from "@/shared/ui/skeleton";
import { useIMBindings } from "../queries";
import { ConnectionChatPicker } from "./connection-chat-picker";
import { useDialogFocusRestore } from "./use-dialog-focus-restore";

export function ConnectionNewConversationDialog({
  connectionId,
  onClose,
  onSaved,
}: {
  connectionId: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore();
  const query = useIMBindings(connectionId);
  const [bindingId, setBindingId] = useState<string>();
  const bindings = (query.data || []).filter(
    (item) => item.id && item.state === "approved",
  );
  const binding = bindings.find((item) => item.id === bindingId) || bindings[0];
  const create = useMutation({
    mutationFn: () => {
      if (!binding?.id) throw new Error(t("operationFailed"));
      return selectImConversation(
        withLocalErrorSurface({
          path: { bindingId: binding.id },
          body: { linkId: "" },
          throwOnError: true,
        }),
      );
    },
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !create.isPending) onClose();
      }}
    >
      <DialogContent
        {...dialogFocus}
        size="lg"
        title={t("newConversation")}
        description={t("newConversationHint")}
      >
        <div className="grid min-h-0 gap-4 overflow-y-auto px-6 pb-6">
          {query.isError ? (
            <QueryState
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          ) : query.isPending ? (
            <div role="status" aria-busy="true" aria-label={t("loading")}>
              <Skeleton className="h-24 w-full" />
            </div>
          ) : binding ? (
            <ConnectionChatPicker
              bindings={bindings}
              binding={binding}
              disabled={create.isPending}
              onSelect={(id) => {
                setBindingId(id);
                create.reset();
              }}
            />
          ) : (
            <p className="type-body text-content-secondary">
              {t("noAuthorizedChats")}
            </p>
          )}
          {create.error ? (
            <p role="alert" className="type-body text-destructive">
              {localApiErrorMessage(create.error, t("operationFailed"))}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={create.isPending}
            onClick={onClose}
          >
            {t("cancel")}
          </Button>
          <Button
            disabled={
              !binding || query.isPending || query.isError || create.isPending
            }
            onClick={() => create.mutate()}
          >
            {t("newConversation")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
