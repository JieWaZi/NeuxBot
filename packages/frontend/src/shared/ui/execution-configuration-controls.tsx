import { useTranslation } from "react-i18next";

import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import type { ExecutionSelectionState } from "@/shared/hooks/use-execution-selection";
import {
  modelPermissionLevels,
  runtimeDefaultPermissionLevels,
  safeDefaultPermissionLevel,
} from "@/shared/lib/runtime-permissions";
import { ExecutionModelControl } from "@/shared/ui/execution-model-control";
import {
  ExecutionPermissionControl,
  ExecutionRuntimeControl,
} from "@/shared/ui/execution-selection-controls";

/** 指令输入中的三个配置入口，沿用共享目录查询和表单选择语义。 */
export function ExecutionConfigurationControls({
  state,
  onChange,
  disabled = false,
  showPermissions = true,
}: {
  state: ExecutionSelectionState;
  onChange: (value: ExecutionSelectionState["selection"]) => void;
  disabled?: boolean;
  showPermissions?: boolean;
}) {
  const { t } = useTranslation("common");
  const { selection, runtime } = state;
  const change = (patch: Partial<ExecutionSelectionState["selection"]>) =>
    onChange({ ...selection, ...patch });
  const modelName = selection.model
    ? state.models.find((item) => item.id === selection.model)?.name ||
      selection.model
    : t("executionSelection.inheritModel");
  return (
    <>
      <ExecutionRuntimeControl
        runtimes={state.runtimes}
        accounts={state.accountsQuery.data?.resources ?? []}
        selectedRuntime={runtime}
        selectedAccountId={selection.accountId}
        disabled={
          disabled ||
          state.runtimesQuery.isPending ||
          state.accountsQuery.isPending
        }
        onSelect={(nextRuntime, accountId) =>
          change({
            runtimeId: nextRuntime.id || "",
            accountId,
            model: "",
            reasoning: "",
            permissionLevel: safeDefaultPermissionLevel(
              selection.permissionLevel,
              runtimeDefaultPermissionLevels(nextRuntime),
            ),
          })
        }
      />
      <ExecutionModelControl
        selectedModelName={modelName}
        selectedModel={selection.model}
        models={state.models}
        selectedReasoning={selection.reasoning}
        reasoningOptions={state.reasoningOptions}
        thinkingPending={state.thinkingQuery.isFetching}
        allowDefaultModel
        allowDefaultReasoning
        defaultReasoning={
          state.accounts.find((account) => account.id === selection.accountId)
            ?.defaultReasoning || runtime?.defaultReasoning
        }
        disabled={disabled || state.loading}
        onModelChange={(model) =>
          change({
            model,
            reasoning: "",
            permissionLevel: safeDefaultPermissionLevel(
              selection.permissionLevel,
              runtime?.models?.some((item) => item.id === model)
                ? modelPermissionLevels(runtime.models, model)
                : runtimeDefaultPermissionLevels(runtime ?? {}),
            ),
          })
        }
        onReasoningChange={(reasoning) => change({ reasoning })}
      />
      {showPermissions ? (
        <ExecutionPermissionControl
          selectedPermissionLevel={selection.permissionLevel}
          supportedPermissions={state.supportedPermissions}
          disabled={disabled || state.loading}
          onChange={(permissionLevel) => change({ permissionLevel })}
        />
      ) : null}
      {state.loading ? (
        <span
          role="status"
          className="text-content-secondary type-caption w-full text-right"
        >
          {t("executionSelection.loading")}
        </span>
      ) : null}
      {state.error ? (
        <span
          role="alert"
          className="text-destructive type-caption w-full text-right"
        >
          {localApiErrorMessage(state.error, t("executionSelection.loadError"))}
        </span>
      ) : null}
      {!state.loading &&
      !state.error &&
      !state.reasoningOptions.length &&
      runtime?.models?.some((item) => item.reasoningOptions?.length) ? (
        <span className="text-content-secondary type-caption w-full text-right">
          {t("executionSelection.reasoningUnavailable")}
        </span>
      ) : null}
    </>
  );
}
