import { useTranslation } from "react-i18next";

import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import type {
  ExecutionSelection,
  ExecutionSelectionState,
} from "@/shared/hooks/use-execution-selection";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";
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
  presentation = "default",
  savedSelection,
}: {
  state: ExecutionSelectionState;
  onChange: (value: ExecutionSelectionState["selection"]) => void;
  disabled?: boolean;
  showPermissions?: boolean;
  presentation?: "default" | "settings";
  savedSelection?: { value: ExecutionSelection; placeholder: string };
}) {
  const { t } = useTranslation("common");
  const { selection, runtime } = state;
  const displayed = savedSelection?.value ?? selection;
  const placeholder = savedSelection?.placeholder;
  const showStatus = presentation === "default";
  const change = (patch: Partial<ExecutionSelectionState["selection"]>) =>
    onChange({ ...selection, ...patch });
  const modelName = displayed.model
    ? state.models.find((item) => item.id === displayed.model)?.name ||
      displayed.model
    : (!displayed.runtimeId && placeholder) ||
      t("executionSelection.inheritModel");
  return (
    <>
      <ExecutionRuntimeControl
        runtimes={state.runtimes}
        accounts={state.accountsQuery.data?.resources ?? []}
        selectedRuntime={
          savedSelection && runtime?.id !== displayed.runtimeId
            ? undefined
            : runtime
        }
        historicalRuntime={
          !showStatus && displayed.runtimeId
            ? { id: displayed.runtimeId }
            : undefined
        }
        selectedAccountId={displayed.accountId}
        placeholder={placeholder}
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
            fast: undefined,
            contextWindow: undefined,
          })
        }
      />
      <ExecutionModelControl
        selectedModelName={modelName}
        selectedModel={displayed.model ?? ""}
        models={state.models}
        selectedReasoning={displayed.reasoning ?? ""}
        showSavedReasoning={!showStatus}
        reasoningOptions={
          savedSelection && !displayed.reasoning ? [] : state.reasoningOptions
        }
        thinkingPending={
          showStatus && state.catalogPending && !state.reasoningOptions.length
        }
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
            fast: undefined,
          })
        }
        onReasoningChange={(reasoning) => change({ reasoning })}
      />
      {showPermissions ? (
        <ExecutionPermissionControl
          selectedPermissionLevel={
            savedSelection
              ? permissionLevelOf(displayed.permissionLevel)
              : selection.permissionLevel
          }
          placeholder={placeholder}
          supportedPermissions={state.supportedPermissions}
          disabled={disabled || state.loading}
          onChange={(permissionLevel) => change({ permissionLevel })}
        />
      ) : null}
      {showStatus && state.loading ? (
        <span
          role="status"
          className="text-content-secondary type-caption w-full text-right"
        >
          {t("executionSelection.loading")}
        </span>
      ) : null}
      {showStatus && state.error ? (
        <span
          role="alert"
          className="text-destructive type-caption w-full text-right"
        >
          {localApiErrorMessage(state.error, t("executionSelection.loadError"))}
        </span>
      ) : null}
      {showStatus &&
      !state.loading &&
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
