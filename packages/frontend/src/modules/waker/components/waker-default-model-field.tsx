import { useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { ExecutionPicker } from "@/shared/ui/execution-picker";
import type { WakerDefaultModel } from "../model/types";

export function WakerDefaultModelField({
  value,
  onChange,
  disabled,
  layout = "stacked",
  saving,
  trigger,
}: {
  value: WakerDefaultModel | undefined;
  onChange: (value: WakerDefaultModel | undefined) => void;
  disabled?: boolean;
  layout?: "row" | "stacked";
  saving?: boolean;
  trigger?: ReactElement;
}) {
  const { t } = useTranslation("waker");
  const [pickerOpen, setPickerOpen] = useState(false);
  const state = useExecutionSelection({
    value: value ? { ...value, reasoning: value.reasoning ?? "" } : {},
    enabled: pickerOpen || (!trigger && Boolean(value)),
  });
  const { runtimeId, accountId, reasoning } = state.selection;
  const modelId = state.effectiveModel;
  const invalid = Boolean(
    value &&
    !state.loading &&
    (!state.valid || state.catalogIssue === "catalogModelUnavailable"),
  );
  const modelName = value
    ? state.models.find((item) => item.id === modelId)?.name ||
      value.model ||
      t("form.inheritModel")
    : t("form.inheritModel");
  const label = t("form.defaultModel");

  const picker = (
    <ExecutionPicker
      trigger={trigger}
      onOpenChange={setPickerOpen}
      runtimes={state.runtimesQuery.data?.resources ?? []}
      accounts={state.accountsQuery.data?.resources ?? []}
      runtime={state.runtime}
      accountId={accountId}
      models={state.models}
      modelId={modelId}
      modelName={modelName}
      defaultModel={state.defaultModel}
      reasoning={reasoning}
      reasoningOptions={state.reasoningOptions}
      allowDefaultReasoning
      catalogState={state.catalogState}
      catalogIssue={state.catalogIssue}
      pending={state.loading || state.catalogPending}
      refreshing={state.catalogRefreshing}
      onRefresh={state.refreshCatalog}
      disabled={disabled}
      sessionSwitchPending={saving}
      field={{
        label,
        stacked: layout === "stacked",
        invalid,
        inherited: !value,
        reset: value
          ? {
              label: t("form.inheritModel"),
              onSelect: () => onChange(undefined),
            }
          : undefined,
      }}
      onModel={(runtime, selectedAccountId, model) => {
        if (!runtime.id || !model.id) return;
        onChange({
          runtimeId: runtime.id,
          accountId: selectedAccountId,
          model: model.id,
          ...(runtime.id === runtimeId &&
          selectedAccountId === accountId &&
          model.id === modelId &&
          reasoning
            ? { reasoning }
            : {}),
        });
      }}
      onReasoning={(next) => {
        if (!runtimeId || !modelId) return;
        onChange({
          runtimeId,
          accountId,
          model: modelId,
          ...(next ? { reasoning: next } : {}),
        });
      }}
    />
  );

  return trigger ? (
    picker
  ) : (
    <div className="grid min-w-0 gap-2">
      {layout === "stacked" ? (
        <span className="type-label font-medium">{label}</span>
      ) : null}
      {picker}
      {invalid ? (
        <p role="alert" className="text-destructive type-caption">
          {t("form.modelUnavailable")}
        </p>
      ) : null}
    </div>
  );
}
