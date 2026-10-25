import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogClose,
} from "@/shared/ui/dialog";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  invalidatePluginQueries,
  updatePluginMutation,
  updatePluginConfigurationMutation,
} from "../api/plugin-queries";
import {
  pluginDisplayName,
  pluginPreferredAuthAction,
  pluginHasAuthorization,
  type PluginDetail,
} from "../model/plugin-view";
import { PluginAuthPanel } from "./plugin-auth-panel";
import {
  PluginConfigurationFields,
  usePluginConfigurationDraft,
} from "./plugin-configuration-editor";

/** 名称与配置由同一表单提交，账号连接通过官方授权独立完成。 */
export function PluginSettingsDialog({
  detail,
  versionId,
  onClose,
}: {
  detail: PluginDetail;
  versionId?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const version =
    detail.versions?.find((item) => item.id === versionId) ??
    detail.versions?.[0];
  const pluginId = detail.plugin?.id ?? "";
  const displayName = pluginDisplayName(version?.package, detail.plugin?.name);
  const [name, setName] = useState(displayName);
  const [savedName, setSavedName] = useState(displayName);
  const draft = usePluginConfigurationDraft(detail);
  const update = useMutation(withLocalErrorMutation(updatePluginMutation()));
  const configuration = useMutation(
    withLocalErrorMutation(updatePluginConfigurationMutation()),
  );
  const hasConfiguration = Boolean(
    detail.fields?.length ||
    Object.keys(detail.values ?? {}).length ||
    detail.configuredSecrets?.length,
  );
  const save = useMutation({
    mutationFn: async () => {
      if (name.trim() !== savedName) {
        await update.mutateAsync({
          path: { pluginId },
          query: { wakerId: "" },
          body: {
            name: name.trim(),
            description: detail.plugin?.description ?? "",
          },
        });
        // 后续配置失败时保留已完成的名称，重试只提交尚未保存的设置。
        setSavedName(name.trim());
      }
      if (hasConfiguration && draft.changed) {
        await configuration.mutateAsync({
          path: { pluginId },
          query: { wakerId: "" },
          body: { ...draft.body, switches: [] },
        });
      }
    },
    onSuccess: onClose,
    onSettled: () => invalidatePluginQueries(client),
  });
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose();
      }}
    >
      <DialogContent
        title={t("editPlugin", {
          name: pluginDisplayName(version?.package, detail.plugin?.name),
        })}
        description={t("editPluginHelp")}
        size="md"
      >
        <form
          className="flex min-h-0 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            if (name.trim() && !save.isPending) save.mutate();
          }}
        >
          <fieldset
            disabled={save.isPending}
            className="min-h-0 min-w-0 space-y-6 overflow-y-auto px-6 pb-6"
          >
            <section className="space-y-2">
              <label className="type-body grid gap-2">
                {t("name")}
                <Input
                  placeholder={t("name")}
                  value={name}
                  required
                  maxLength={120}
                  onChange={(event) => setName(event.target.value)}
                />
              </label>
              <p className="type-caption text-content-secondary">
                {t("nameHelp")}
              </p>
            </section>
            {pluginHasAuthorization(version?.package) && (
              <PluginAuthPanel
                preferredAction={pluginPreferredAuthAction(version?.package)}
                pluginId={pluginId}
                versionId={version?.id}
                onReady={() => void invalidatePluginQueries(client)}
              />
            )}
            {hasConfiguration && (
              <PluginConfigurationFields detail={detail} draft={draft} />
            )}
            {save.error && (
              <p role="alert" className="type-body text-destructive">
                {localApiErrorMessage(save.error, t("failed"))}
              </p>
            )}
          </fieldset>
          <DialogFooter>
            <DialogClose asChild>
              <Button
                type="button"
                variant="secondary"
                disabled={save.isPending}
              >
                {t("cancel")}
              </Button>
            </DialogClose>
            <Button
              type="submit"
              disabled={
                save.isPending ||
                !name.trim() ||
                (name.trim() === savedName && !draft.changed)
              }
            >
              {t(save.isPending ? "busy" : "save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
