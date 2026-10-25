import { useDialogFocusRestore } from "./use-dialog-focus-restore";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { deleteImConnection } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import type { IMConnection } from "../types";
import { imConnectionDependencyOptions } from "../queries";
import {
  ConnectionDependencies,
  connectionDeletionBlocked,
} from "./connection-dependencies";

export function ConnectionDeleteDialog({
  connection,
  onClose,
  onDeleted,
}: {
  connection: IMConnection;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const { t } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore();
  const dependencies = useQuery(
    imConnectionDependencyOptions(connection.id || ""),
  );
  const remove = useMutation({
    mutationFn: () =>
      deleteImConnection(
        withLocalErrorSurface({
          path: { connectionId: connection.id || "" },
          query: {
            revision: connection.revision || 0,
            dependencyDigest: dependencies.data?.digest,
          },
          throwOnError: true,
        }),
      ),
    onSuccess: () => {
      onClose();
      onDeleted();
    },
    onError: () => {
      void dependencies.refetch();
    },
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !remove.isPending) onClose();
      }}
    >
      <DialogContent
        {...dialogFocus}
        title={t("deleteConnection")}
        description={t("deleteConnectionHint", { name: connection.name })}
      >
        <div className="min-h-0 overflow-y-auto px-6 pb-4">
          <ConnectionDependencies
            data={dependencies.data}
            loading={dependencies.isPending}
            error={dependencies.error}
            onRetry={() => void dependencies.refetch()}
          />
        </div>
        {remove.error ? (
          <p role="alert" className="text-destructive type-body px-6 pb-4">
            {localApiErrorMessage(remove.error, t("operationFailed"))}
          </p>
        ) : null}
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
            disabled={
              remove.isPending ||
              dependencies.isPending ||
              Boolean(dependencies.error) ||
              connectionDeletionBlocked(dependencies.data)
            }
            onClick={() => remove.mutate()}
          >
            {t("delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
