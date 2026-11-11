import { useQuery } from "@tanstack/react-query";
import {
  lazy,
  Suspense,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";
import { getPluginOptions } from "../api/plugin-queries";
import { pluginLocale, type PluginDetail } from "../model/plugin-view";

const InstallContent = lazy(() =>
  import("./plugin-install-dialog").then((module) => ({
    default: module.PluginInstallDialog,
  })),
);
const UpdateContent = lazy(() =>
  import("./plugin-update-dialog").then((module) => ({
    default: module.PluginUpdateDialog,
  })),
);
const SettingsContent = lazy(() =>
  import("./plugin-settings-dialog").then((module) => ({
    default: module.PluginSettingsDialog,
  })),
);

/** 弹窗只在用户发起动作后加载，等待期间保持可关闭的模态和内容骨架。 */
function DeferredDialog({
  children,
  onClose,
}: {
  children: ReactNode;
  onClose: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <Suspense
      fallback={
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) onClose();
          }}
        >
          <DialogContent title={t("detailState.loading")}>
            <div className="px-6 pb-5">
              <ResourceLoadingState label={t("detailState.loading")} />
            </div>
          </DialogContent>
        </Dialog>
      }
    >
      {children}
    </Suspense>
  );
}
export function PluginInstallDialog(
  props: ComponentProps<typeof InstallContent>,
) {
  return (
    <DeferredDialog onClose={props.onClose}>
      <InstallContent {...props} />
    </DeferredDialog>
  );
}
export function PluginUpdateDialog({
  pluginId,
  name,
  wakerId,
  onClose,
}: {
  pluginId: string;
  name: string;
  wakerId?: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation("plugin");
  const [snapshot, setSnapshot] = useState<PluginDetail>();
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const detail = useQuery({
    ...getPluginOptions(
      withLocalErrorSurface({
        path: { pluginId },
        query: { locale, wakerId: wakerId || undefined },
      }),
    ),
    enabled: !snapshot,
    refetchOnMount: "always",
  });
  // 首次成功读回才打开确认内容，后续详情重取不卸载已经准备好的表单。
  if (!snapshot && detail.isSuccess && detail.isFetchedAfterMount)
    setSnapshot(detail.data);
  if (!snapshot) {
    return (
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      >
        <DialogContent title={t("updateNamed", { name })}>
          <div className="px-6 pb-5">
            <QueryState
              loadingLayout="content"
              pending={detail.isFetching || detail.isPending}
              error={detail.error}
              onRetry={() => void detail.refetch()}
            />
          </div>
        </DialogContent>
      </Dialog>
    );
  }
  return (
    <DeferredDialog onClose={onClose}>
      <UpdateContent detail={snapshot} wakerId={wakerId} onClose={onClose} />
    </DeferredDialog>
  );
}
export function PluginSettingsDialog(
  props: ComponentProps<typeof SettingsContent>,
) {
  return (
    <DeferredDialog onClose={props.onClose}>
      <SettingsContent {...props} />
    </DeferredDialog>
  );
}
