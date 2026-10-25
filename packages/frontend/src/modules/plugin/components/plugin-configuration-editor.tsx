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

export function PluginConfigurationEditor({
  detail,
  wakerId = "",
  switches = [],
  onSaved,
}: {
  detail: PluginDetail;
  wakerId?: string;
  switches?: {
    wakerId: string;
    expectedVersionId: string;
    versionId: string;
  }[];
  onSaved?: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const draft = usePluginConfigurationDraft(detail);
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
      <PluginConfigurationFields detail={detail} draft={draft} />
      {save.error && (
        <p role="alert" className="type-body text-destructive">
          {localApiErrorMessage(save.error, t("failed"))}
        </p>
      )}
      <Button
        disabled={save.isPending}
        onClick={() =>
          save.mutate({
            path: { pluginId: detail.plugin?.id ?? "" },
            query: { wakerId },
            body: {
              ...draft.body,
              expectedRevision: detail.configurationRevision ?? 0,
              switches,
            },
          })
        }
      >
        {switches.length ? t("update") : t("save")}
      </Button>
    </section>
  );
}

/** 未提交配置由调用表单持有，安装与编辑复用字段及密钥保留语义。 */
export function usePluginConfigurationDraft(detail: PluginDetail) {
  const [values, setValues] = useState<Record<string, string>>({
    ...detail.values,
  });
  const [removedValues, setRemovedValues] = useState<string[]>([]);
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [clear, setClear] = useState<string[]>([]);
  const retiredValues = Object.keys(detail.values ?? {}).filter(
    (key) =>
      !detail.fields?.some((field) => field.key === key && !field.secret),
  );
  const retiredSecrets = (detail.configuredSecrets ?? []).filter(
    (key) => !detail.fields?.some((field) => field.key === key && field.secret),
  );
  const [initialValues] = useState({ ...detail.values });
  const [expectedRevision] = useState(detail.configurationRevision ?? 0);
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
  detail,
  draft,
}: {
  detail: PluginDetail;
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
      {(detail.fields ?? []).map((field) => {
        const key = field.key;
        if (!key) return null;
        const configured = detail.configuredSecrets?.includes(key);
        return (
          <div key={key} className="space-y-2">
            <label className="type-body grid gap-2">
              {field.label || key}
              {field.required && " *"}
              <Input
                type={field.secret ? "password" : "text"}
                value={(field.secret ? secrets : values)[key] ?? ""}
                placeholder={
                  configured ? t("configured") : (field.default ?? "")
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
