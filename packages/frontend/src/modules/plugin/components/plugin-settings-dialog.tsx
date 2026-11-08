import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { ResourceCategoryField } from "@/shared/ui/resource-category-field";
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
import { usePluginConfiguration } from "../api/use-plugin-configuration";
import type { GetPluginConfigurationResponse } from "@/shared/api/generated/types.gen";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";
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
  const selected =
    versionId ?? detail.resolvedVersionId ?? detail.versions?.[0]?.id ?? "";
  const query = usePluginConfiguration(detail.plugin?.id ?? "", selected);
  const { t } = useTranslation("plugin");
  const saving = useRef(false);
  const pkg = detail.versions?.find(
    (version) => version.id === selected,
  )?.package;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !saving.current) onClose();
      }}
    >
      <DialogContent
        title={t("editPlugin", {
          name: pluginDisplayName(pkg, detail.plugin?.name),
        })}
        description={t("editPluginHelp")}
        size="md"
      >
        {query.data && query.isFetchedAfterMount ? (
          <PluginSettingsForm
            detail={detail}
            versionId={selected}
            configuration={query.data}
            queryError={query.error}
            fetching={query.isFetching}
            onRetry={() => void query.refetch()}
            onClose={onClose}
            onPendingChange={(pending) => {
              saving.current = pending;
            }}
          />
        ) : (
          <div className="px-6 pb-6">
            <QueryState
              loadingLayout="content"
              pending={query.isPending || !query.isFetchedAfterMount}
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
function PluginSettingsForm({
  detail,
  versionId,
  configuration,
  queryError,
  fetching,
  onRetry,
  onClose,
  onPendingChange,
}: {
  detail: PluginDetail;
  versionId: string;
  configuration: GetPluginConfigurationResponse;
  queryError: unknown;
  fetching: boolean;
  onRetry: () => void;
  onPendingChange: (pending: boolean) => void;
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
  const [categories, setCategories] = useState(detail.plugin?.categories ?? []);
  const [savedCategories, setSavedCategories] = useState(categories);
  const categoriesChanged =
    JSON.stringify(categories) !== JSON.stringify(savedCategories);
  const [savedName, setSavedName] = useState(displayName);
  const draft = usePluginConfigurationDraft(configuration);
  const update = useMutation(withLocalErrorMutation(updatePluginMutation()));
  const saveConfiguration = useMutation(
    withLocalErrorMutation(updatePluginConfigurationMutation()),
  );
  const hasConfiguration = Boolean(
    configuration.fields?.length ||
    Object.keys(configuration.values ?? {}).length ||
    configuration.configuredSecrets?.length,
  );
  const save = useMutation({
    onMutate: () => onPendingChange(true),
    mutationFn: async () => {
      if (name.trim() !== savedName || categoriesChanged) {
        await update.mutateAsync({
          path: { pluginId },
          query: { wakerId: "" },
          body: {
            name: name.trim(),
            categories,
            description: detail.plugin?.description ?? "",
          },
        });
        // 后续配置失败时保留已完成的名称，重试只提交尚未保存的设置。
        setSavedName(name.trim());
        setSavedCategories(categories);
      }
      if (hasConfiguration && draft.changed) {
        await saveConfiguration.mutateAsync({
          path: { pluginId },
          query: { wakerId: "" },
          body: draft.body,
        });
      }
    },
    onSuccess: onClose,
    onSettled: async () => {
      try {
        await invalidatePluginQueries(client);
      } finally {
        onPendingChange(false);
      }
    },
  });
  return (
    <form
      className="flex min-h-0 flex-col"
      onSubmit={(event) => {
        event.preventDefault();
        if (name.trim() && !save.isPending && !fetching && !queryError)
          save.mutate();
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
              appearance="catalog"
              placeholder={t("name")}
              value={name}
              required
              maxLength={120}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <p className="type-caption text-content-secondary">{t("nameHelp")}</p>
        </section>
        <ResourceCategoryField
          type="plugin"
          value={categories}
          onValueChange={setCategories}
          disabled={save.isPending}
        />
        {pluginHasAuthorization(version?.package) && (
          <PluginAuthPanel
            preferredAction={pluginPreferredAuthAction(version?.package)}
            pluginId={pluginId}
            versionId={versionId}
            onReady={() => void invalidatePluginQueries(client)}
          />
        )}
        {Boolean(queryError) && (
          <QueryRefreshError
            error={queryError}
            pending={fetching}
            onRetry={onRetry}
          />
        )}
        {hasConfiguration && (
          <PluginConfigurationFields
            configuration={configuration}
            draft={draft}
          />
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
            size="catalog-form"
            variant="catalog-ghost"
            disabled={save.isPending}
          >
            {t("cancel")}
          </Button>
        </DialogClose>
        <Button
          type="submit"
          size="catalog-form"
          disabled={
            save.isPending ||
            fetching ||
            Boolean(queryError) ||
            !name.trim() ||
            (name.trim() === savedName && !categoriesChanged && !draft.changed)
          }
        >
          {t(save.isPending ? "busy" : "save")}
        </Button>
      </DialogFooter>
    </form>
  );
}
