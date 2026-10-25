import { lazy, Suspense, type ComponentProps, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";

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
export function PluginUpdateDialog(
  props: ComponentProps<typeof UpdateContent>,
) {
  return (
    <DeferredDialog onClose={props.onClose}>
      <UpdateContent {...props} />
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
