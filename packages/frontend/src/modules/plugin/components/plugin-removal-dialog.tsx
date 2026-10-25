import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import {
  deletePluginMutation,
  uninstallWakerPluginMutation,
  invalidatePluginQueries,
} from "../api/plugin-queries";
import type { PluginDetail } from "../model/plugin-view";

/** 列表和详情共用同一删除确认；伙伴卸载只提交指定伙伴的安装关系。 */
export function PluginRemovalDialog({
  detail,
  mode,
  wakerId = "",
  onClose,
  onRemoved,
}: {
  detail: PluginDetail;
  mode: "resource" | "installation";
  wakerId?: string;
  onClose: () => void;
  onRemoved?: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const onSuccess = async () => {
    await invalidatePluginQueries(client);
    onRemoved?.();
    onClose();
  };
  const remove = useMutation({
    ...withLocalErrorMutation(deletePluginMutation()),
    onSuccess,
  });
  const uninstall = useMutation({
    ...withLocalErrorMutation(uninstallWakerPluginMutation()),
    onSuccess,
  });
  const pending = remove.isPending || uninstall.isPending;
  const error = remove.error ?? uninstall.error;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent
        title={t(mode === "resource" ? "deleteTitle" : "uninstallTitle")}
        description={
          mode === "resource"
            ? t("deleteHelp", { count: detail.installations?.length ?? 0 })
            : t("uninstallHelp")
        }
      >
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={pending}
            onClick={() => {
              if (mode === "installation")
                uninstall.mutate({
                  path: { wakerId, pluginId: detail.plugin?.id ?? "" },
                });
              else
                remove.mutate({
                  path: { pluginId: detail.plugin?.id ?? "" },
                  query: { wakerId },
                  body: {
                    confirmed: true,
                    installations: (detail.installations ?? []).map((item) => ({
                      wakerId: item.wakerId ?? "",
                      expectedVersionId: item.versionId ?? "",
                      versionId: item.versionId ?? "",
                    })),
                  },
                });
            }}
          >
            {t("confirmDelete")}
          </Button>
        </DialogFooter>
        {error && (
          <p role="alert" className="type-body text-destructive px-6 pb-4">
            {localApiErrorMessage(error, t("failed"))}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}
