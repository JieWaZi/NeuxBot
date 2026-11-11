import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { uninstallRuntimeMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GetRuntimeInstallationResponse as Installation } from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { runtimeInstallationOptions } from "./runtime-installation-controls";
import { SettingsGroup, SettingsRow } from "./settings-section-layout";

/** 只移除后端确认归属的 CLI 程序，账号与业务配置仍由原领域保留。 */
export function RuntimeProgramRemoval({
  runtimeId,
  name,
  installation,
  disabled,
}: {
  runtimeId: string;
  name: string;
  installation?: Installation;
  disabled?: boolean;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const remove = useMutation({
    ...withLocalErrorMutation(uninstallRuntimeMutation()),
    onSuccess: () => {
      setOpen(false);
      void client.invalidateQueries({
        queryKey: runtimeInstallationOptions(runtimeId).queryKey,
        exact: true,
      });
    },
  });
  return (
    <>
      <SettingsGroup title={t("agents.removal.danger")}>
        <SettingsRow
          title={t("agents.removal.title", { name })}
          description={
            <>
              {t("agents.removal.description", { name })}
              {installation?.maintenanceReason === "cleanup_pending" ? (
                <span className="mt-1 block">
                  {t("agents.removal.cleanupPending")}
                </span>
              ) : null}
              {installation && !installation.canUninstall ? (
                <span className="mt-1 block">
                  {t("agents.removal.manualOnly")}
                </span>
              ) : null}
            </>
          }
        >
          <Button
            size="settings"
            variant="danger-quiet"
            disabled={
              disabled || remove.isPending || !installation?.canUninstall
            }
            onClick={() => {
              remove.reset();
              setOpen(true);
            }}
          >
            {t("common:delete")}
          </Button>
        </SettingsRow>
      </SettingsGroup>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!remove.isPending) setOpen(value);
        }}
      >
        <DialogContent
          title={t("agents.removal.title", { name })}
          description={t("agents.removal.description", { name })}
        >
          {installation?.installationPath ? (
            <p className="text-content-secondary type-body px-6 pb-4 break-all">
              {installation.installationPath}
            </p>
          ) : null}
          {remove.isError ? (
            <p role="alert" className="text-destructive type-body px-6 pb-4">
              {localApiErrorMessage(remove.error, t("agents.removal.failed"))}
            </p>
          ) : null}
          <DialogFooter>
            <Button
              size="settings"
              variant="settings-outline"
              disabled={remove.isPending}
              onClick={() => setOpen(false)}
            >
              {t("common:cancel")}
            </Button>
            <Button
              size="settings"
              variant="danger"
              disabled={remove.isPending || !installation?.canUninstall}
              onClick={() => remove.mutate({ path: { runtimeId } })}
            >
              {t(remove.isPending ? "agents.removal.pending" : "common:delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
