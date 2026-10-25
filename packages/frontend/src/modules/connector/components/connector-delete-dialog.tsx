import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { useTranslation } from "react-i18next";

import {
  deleteWakerConnectorMutation,
  invalidateVisibleConnectorQueries,
} from "../api/connector-queries";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";

type Props = {
  affectedWakerCount: number;
  connectorId: string;
  name: string;
  onDeleted?: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  wakerId: string;
};

export function ConnectorDeleteDialog({
  affectedWakerCount,
  connectorId,
  name,
  onDeleted,
  onOpenChange,
  open,
  wakerId,
}: Props) {
  const { t } = useTranslation("connector");
  const queryClient = useQueryClient();
  const deleteConnector = useMutation(
    withLocalErrorMutation(deleteWakerConnectorMutation()),
  );
  const remove = async () => {
    try {
      await deleteConnector.mutateAsync({
        path: { connectorId, wakerId },
        query: wakerId
          ? undefined
          : { confirmed: true, expectedAffectedCount: affectedWakerCount },
      });
      await invalidateVisibleConnectorQueries(queryClient);
      onOpenChange(false);
      onDeleted?.();
    } catch {
      // React Query 删除错误，由确认框就近展示。
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("delete.title")}
        description={t("delete.description")}
      >
        <div className="px-6 py-4">
          <p className="type-body">
            {t("delete.warning", { count: affectedWakerCount, name })}
          </p>
          {deleteConnector.isError ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {localApiErrorMessage(deleteConnector.error, t("delete.failed"))}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary" disabled={deleteConnector.isPending}>
              {t("common:cancel")}
            </Button>
          </DialogClose>
          <Button
            variant="danger"
            disabled={deleteConnector.isPending}
            onClick={() => void remove()}
          >
            {deleteConnector.isPending
              ? t("delete.deleting")
              : t("delete.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
