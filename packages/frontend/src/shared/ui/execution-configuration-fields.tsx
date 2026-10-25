import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import type { ExecutionSelectionState } from "@/shared/hooks/use-execution-selection";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import {
  permissionLevels,
  permissionLevelOf,
  modelPermissionLevels,
  permissionLevelTranslationKeys,
  runtimeDefaultPermissionLevels,
  safeDefaultPermissionLevel,
} from "@/shared/lib/runtime-permissions";
import { FieldLabel } from "@/shared/ui/field-label";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";

/** 各表单共用字段外观和联动；智能体品牌仅出现在选值与选项内。 */
export function ExecutionConfigurationFields({
  state,
  onChange,
  disabled = false,
  runtimeHidden = false,
  permissionHidden = false,
  reasoningHidden = false,
  reasoningDefaultHidden = false,
  name,
  defaults = false,
  required = false,
  leadingField,
  helpHidden = false,
}: {
  state: ExecutionSelectionState;
  onChange: (value: ExecutionSelectionState["selection"]) => void;
  disabled?: boolean;
  runtimeHidden?: boolean;
  permissionHidden?: boolean;
  reasoningHidden?: boolean;
  reasoningDefaultHidden?: boolean;
  name?: string;
  defaults?: boolean;
  required?: boolean;
  leadingField?: ReactNode;
  helpHidden?: boolean;
}) {
  const { t } = useTranslation("common");
  const id = useId();
  const { selection, runtime, models, reasoningOptions } = state;
  const change = (patch: Partial<ExecutionSelectionState["selection"]>) =>
    onChange({ ...selection, ...patch });
  const field = (
    key:
      | "runtime"
      | "account"
      | "model"
      | "reasoning"
      | "permission"
      | "defaultModel"
      | "defaultReasoning",
    value: string,
    options: Array<{ id?: string; name?: string; disabled?: boolean }>,
    onValueChange: (value: string) => void,
    unavailable = false,
  ) => {
    const label = t(`executionSelection.${key}`);
    const fieldRequired =
      required && ["runtime", "account", "permission"].includes(key);
    return (
      <div className="relative grid min-w-0 gap-2">
        <FieldLabel htmlFor={`${id}-${key}`} required={fieldRequired}>
          {label}
        </FieldLabel>
        <Select
          value={value || "__default"}
          disabled={disabled || unavailable}
          onValueChange={(next) => {
            if (next) onValueChange(next);
          }}
        >
          <SelectTrigger
            id={`${id}-${key}`}
            aria-required={fieldRequired}
            aria-label={name ? `${name} · ${label}` : label}
          >
            <SelectValue placeholder={t("executionSelection.empty")} />
          </SelectTrigger>
          <SelectContent>
            {value && !options.some((item) => item.id === value) ? (
              <SelectItem value={value} disabled>
                {value} · {t("executionSelection.unavailable")}
              </SelectItem>
            ) : null}
            {options.flatMap((item) =>
              item.id
                ? [
                    <SelectItem
                      key={item.id}
                      value={item.id}
                      disabled={item.disabled}
                    >
                      {key === "runtime" ? (
                        <span className="inline-flex items-center gap-2">
                          <RuntimeCliIcon
                            runtimeId={
                              state.runtimes.find(
                                (entry) => entry.id === item.id,
                              )?.baseRuntimeId || item.id
                            }
                          />
                          <span>{runtimeDisplayName(item)}</span>
                        </span>
                      ) : (
                        item.name || item.id
                      )}
                    </SelectItem>,
                  ]
                : [],
            )}
          </SelectContent>
        </Select>
      </div>
    );
  };
  return (
    <div className="@container/execution-fields grid gap-3">
      <div className="grid grid-cols-1 gap-4 @min-[20rem]/execution-fields:grid-cols-2">
        {leadingField}
        {!runtimeHidden &&
          field("runtime", selection.runtimeId, state.runtimes, (runtimeId) => {
            const next = state.runtimes.find((item) => item.id === runtimeId);
            change({
              runtimeId,
              accountId: next?.defaultAccountId || "system",
              model: "",
              reasoning: "",
              permissionLevel: safeDefaultPermissionLevel(
                selection.permissionLevel,
                runtimeDefaultPermissionLevels(next ?? {}),
              ),
            });
          })}
        {field(
          "account",
          selection.accountId,
          [
            { id: "system", name: t("executionSelection.system") },
            ...state.accounts,
          ],
          (accountId) => change({ accountId, model: "", reasoning: "" }),
        )}
        {field(
          defaults ? "defaultModel" : "model",
          selection.model || "__default",
          [
            ...(!defaults
              ? [
                  {
                    id: "__default",
                    name: t("executionSelection.inheritModel"),
                  },
                ]
              : []),
            ...models,
          ],
          (model) =>
            change({
              model: model === "__default" ? "" : model,
              reasoning: "",
              permissionLevel: safeDefaultPermissionLevel(
                selection.permissionLevel,
                runtime?.models?.some((item) => item.id === model)
                  ? modelPermissionLevels(runtime.models, model)
                  : runtimeDefaultPermissionLevels(runtime ?? {}),
              ),
            }),
          state.nativeModelsQuery.isFetching,
        )}
        {!reasoningHidden &&
          field(
            defaults ? "defaultReasoning" : "reasoning",
            selection.reasoning || "__default",
            [
              ...(!reasoningDefaultHidden
                ? [
                    {
                      id: "__default",
                      name: t("executionSelection.defaultReasoning"),
                    },
                  ]
                : []),
              ...reasoningOptions,
            ],
            (reasoning) =>
              change({ reasoning: reasoning === "__default" ? "" : reasoning }),
            !reasoningOptions.length || state.thinkingQuery.isFetching,
          )}
        {!permissionHidden &&
          field(
            "permission",
            selection.permissionLevel,
            permissionLevels.map((level) => ({
              id: level,
              name: t(permissionLevelTranslationKeys[level].label),
              disabled: !state.supportedPermissions.has(level),
            })),
            (permissionLevel) =>
              change({
                permissionLevel:
                  permissionLevelOf(permissionLevel) ?? "default",
              }),
          )}
      </div>
      {!reasoningHidden &&
      !state.loading &&
      !reasoningOptions.length &&
      runtime?.models?.some((item) => item.reasoningOptions?.length) ? (
        <p className="text-content-secondary type-caption">
          {t("executionSelection.reasoningUnavailable")}
        </p>
      ) : null}
      {!helpHidden && (
        <p className="text-content-secondary type-caption">
          {t("executionSelection.help", {
            runtime:
              runtimeDisplayName(runtime) || t("executionSelection.runtime"),
          })}
        </p>
      )}
      {state.loading ? (
        <p role="status" className="text-content-secondary type-caption">
          {t("executionSelection.loading")}
        </p>
      ) : null}
      {state.error ? (
        <p role="alert" className="text-destructive type-caption">
          {localApiErrorMessage(state.error, t("executionSelection.loadError"))}
        </p>
      ) : null}
    </div>
  );
}
