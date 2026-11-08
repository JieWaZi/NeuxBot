import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Checkbox } from "@/shared/ui/checkbox";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  updatePluginConfigurationMutation,
  invalidatePluginQueries,
} from "../api/plugin-queries";
import type { PluginDetail } from "../model/plugin-view";
import type { GetPluginConfigurationResponse } from "@/shared/api/generated/types.gen";
import { usePluginConfiguration } from "../api/use-plugin-configuration";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";

export function PluginConfigurationEditor({
  detail,
  wakerId = "",
  onSaved,
}: {
  detail: PluginDetail;
  wakerId?: string;
  onSaved?: () => void;
}) {
  const versionId = detail.resolvedVersionId ?? detail.versions?.[0]?.id ?? "";
  const query = usePluginConfiguration(
    detail.plugin?.id ?? "",
    versionId,
    wakerId,
  );
  return query.data ? (
    <>
      {query.error && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
      <ConfigurationEditorForm
        detail={detail}
        configuration={query.data}
        wakerId={wakerId}
        onSaved={onSaved}
        blocked={query.isFetching || Boolean(query.error)}
      />
    </>
  ) : (
    <QueryState
      loadingLayout="content"
      pending={query.isPending}
      error={query.error}
      onRetry={() => void query.refetch()}
    />
  );
}
function ConfigurationEditorForm({
  detail,
  configuration,
  wakerId,
  onSaved,
  blocked,
}: {
  detail: PluginDetail;
  configuration: GetPluginConfigurationResponse;
  blocked: boolean;
  wakerId: string;
  onSaved?: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const draft = usePluginConfigurationDraft(configuration);
  const save = useMutation({
    ...withLocalErrorMutation(updatePluginConfigurationMutation()),
    onSuccess: async () => {
      draft.setSecrets({});
      draft.setClear([]);
      await invalidatePluginQueries(client);
      onSaved?.();
    },
  });
  return (
    <section className="space-y-4">
      <fieldset disabled={save.isPending || blocked} className="min-w-0">
        <PluginConfigurationFields
          configuration={configuration}
          draft={draft}
        />
      </fieldset>
      {save.error && (
        <p role="alert" className="type-body text-destructive">
          {localApiErrorMessage(save.error, t("failed"))}
        </p>
      )}
      <Button
        size="catalog-form"
        disabled={save.isPending || blocked}
        onClick={() =>
          save.mutate({
            path: { pluginId: detail.plugin?.id ?? "" },
            query: { wakerId },
            body: draft.body,
          })
        }
      >
        {t("save")}
      </Button>
    </section>
  );
}

/** 未提交配置由调用表单持有，安装与编辑复用字段及密钥保留语义。 */
export function usePluginConfigurationDraft(
  configuration: GetPluginConfigurationResponse,
) {
  const [values, setValues] = useState<Record<string, string>>({
    ...configuration.values,
  });
  const [removedValues, setRemovedValues] = useState<string[]>([]);
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [clear, setClear] = useState<string[]>([]);
  const retiredValues = Object.keys(configuration.values ?? {}).filter(
    (key) =>
      !configuration.fields?.some(
        (field) => field.key === key && !field.secret,
      ),
  );
  const retiredSecrets = (configuration.configuredSecrets ?? []).filter(
    (key) =>
      !configuration.fields?.some((field) => field.key === key && field.secret),
  );
  const [initialValues] = useState({ ...configuration.values });
  const [expectedRevision] = useState(configuration.configurationRevision ?? 0);
  return {
    values,
    setValues,
    secrets,
    setSecrets,
    clear,
    setClear,
    removedValues,
    setRemovedValues,
    retiredValues,
    retiredSecrets,
    changed:
      JSON.stringify(values) !== JSON.stringify(initialValues) ||
      removedValues.length > 0 ||
      clear.length > 0 ||
      Object.values(secrets).some((value) => value !== ""),
    body: {
      expectedRevision,
      values: Object.fromEntries(
        Object.entries(values).filter(([key]) => !removedValues.includes(key)),
      ),
      secrets: [
        ...Object.entries(secrets)
          .filter(([key, value]) => value !== "" && !clear.includes(key))
          .map(([key, value]) => ({ key, value })),
        ...clear.map((key) => ({ key, clear: true })),
      ],
    },
  };
}

export function PluginConfigurationFields({
  configuration,
  draft,
}: {
  configuration: GetPluginConfigurationResponse;
  draft: ReturnType<typeof usePluginConfigurationDraft>;
}) {
  const { t } = useTranslation("plugin");
  const {
    values,
    setValues,
    secrets,
    setSecrets,
    clear,
    setClear,
    removedValues,
    setRemovedValues,
    retiredValues,
    retiredSecrets,
  } = draft;
  return (
    <section className="space-y-4">
      <div>
        <h3 className="type-label font-medium">{t("configuration")}</h3>
        <p className="type-body text-muted-foreground mt-1">
          {t("configurationHelp")}
        </p>
      </div>
      {(configuration.fields ?? []).map((field) => {
        const key = field.key;
        if (!key) return null;
        const configured = configuration.configuredSecrets?.includes(key);
        return (
          <div key={key} className="space-y-2">
            <label className="type-body grid gap-2">
              {field.label || key}
              {field.required && " *"}
              <Input
                appearance="catalog"
                type={field.secret ? "password" : "text"}
                value={(field.secret ? secrets : values)[key] ?? ""}
                placeholder={
                  configured
                    ? t("configured")
                    : field.default || field.label || key
                }
                autoComplete={field.secret ? "new-password" : "off"}
                onChange={(event) =>
                  field.secret
                    ? setSecrets({ ...secrets, [key]: event.target.value })
                    : setValues({ ...values, [key]: event.target.value })
                }
              />
            </label>
            {field.description && (
              <p className="type-caption text-muted-foreground">
                {field.description}
              </p>
            )}
            {field.secret && configured && (
              <label className="type-caption flex items-center gap-2">
                <Checkbox
                  checked={clear.includes(key)}
                  onCheckedChange={(checked) =>
                    setClear(
                      checked
                        ? [...clear, key]
                        : clear.filter((value) => value !== key),
                    )
                  }
                />
                {t("clearSecret")}
              </label>
            )}
          </div>
        );
      })}
      {retiredValues.map((key) => (
        <label key={key} className="type-body flex items-center gap-2">
          <Checkbox
            checked={removedValues.includes(key)}
            onCheckedChange={(checked) =>
              setRemovedValues(
                checked
                  ? [...removedValues, key]
                  : removedValues.filter((value) => value !== key),
              )
            }
          />
          {t("retiredSetting", { key })}
        </label>
      ))}
      {retiredSecrets.map((key) => (
        <label key={key} className="type-body flex items-center gap-2">
          <Checkbox
            checked={clear.includes(key)}
            onCheckedChange={(checked) =>
              setClear(
                checked
                  ? [...clear, key]
                  : clear.filter((value) => value !== key),
              )
            }
          />
          {t("retiredSetting", { key })}
        </label>
      ))}
    </section>
  );
}
