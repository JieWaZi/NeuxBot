import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Button } from "@/shared/ui/button";
import { ExecutionConfigurationFields } from "@/shared/ui/execution-configuration-fields";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { FieldLabel } from "@/shared/ui/field-label";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import type { WakerDefaultModel } from "../model/types";

export function WakerDefaultModelField({
  value,
  onChange,
  disabled,
  layout = "stacked",
}: {
  value: WakerDefaultModel | undefined;
  onChange: (value: WakerDefaultModel | undefined) => void;
  disabled?: boolean;
  layout?: "row" | "stacked";
}) {
  const { t } = useTranslation(["waker", "common"]);
  const id = useId();
  const [candidate, setCandidate] = useState<WakerDefaultModel>();
  const open = candidate !== undefined;
  const selection = candidate ?? value;
  const state = useExecutionSelection({
    value: {
      ...selection,
      model: selection?.model ?? "",
      reasoning: selection?.reasoning ?? "",
    },
    enabled: selection !== undefined,
  });
  const invalid = Boolean(
    value &&
    !open &&
    !state.loading &&
    (!state.valid || state.catalogIssue === "catalogModelUnavailable"),
  );
  const modelName =
    state.models.find((item) => item.id === value?.model)?.name ??
    value?.model ??
    t("form.inheritModel");

  const apply = (next: WakerDefaultModel | undefined) => {
    onChange(next);
    setCandidate(undefined);
  };

  return (
    <div className="grid min-w-0 gap-2">
      {layout === "stacked" ? (
        <FieldLabel htmlFor={id}>{t("form.defaultModel")}</FieldLabel>
      ) : null}
      <Popover
        open={open}
        onOpenChange={(next) => setCandidate(next ? (value ?? {}) : undefined)}
      >
        <PopoverTrigger asChild>
          <FieldControlTrigger
            id={id}
            disabled={disabled}
            aria-invalid={invalid || undefined}
            aria-labelledby={layout === "row" ? `${id}-label` : undefined}
            aria-describedby={`${id}-value`}
          >
            {layout === "row" ? (
              <span className="flex flex-1 items-center justify-between gap-2">
                <span id={`${id}-label`} className="shrink-0 font-medium">
                  {t("form.defaultModel")}
                </span>
                <span
                  id={`${id}-value`}
                  className="text-content-secondary min-w-0 truncate"
                >
                  {modelName}
                </span>
              </span>
            ) : (
              <span id={`${id}-value`}>{modelName}</span>
            )}
          </FieldControlTrigger>
        </PopoverTrigger>
        <PopoverContent
          width="trigger"
          align="start"
          aria-label={t("form.defaultModel")}
        >
          <div className="grid gap-3">
            <ExecutionConfigurationFields
              state={state}
              disabled={disabled}
              permissionHidden
              helpHidden
              modelRequired
              onChange={({ runtimeId, accountId, model, reasoning }) =>
                setCandidate({
                  runtimeId,
                  accountId,
                  model,
                  ...(reasoning ? { reasoning } : {}),
                })
              }
            />
            <div className="flex flex-wrap items-center justify-between gap-2">
              <Button
                size="sm"
                variant="ghost"
                type="button"
                onClick={() => apply(undefined)}
              >
                {t("form.inheritModel")}
              </Button>
              <Button
                size="sm"
                type="button"
                disabled={
                  !state.selection.runtimeId ||
                  !state.selection.accountId ||
                  !state.selection.model
                }
                onClick={() => {
                  const { runtimeId, accountId, model, reasoning } =
                    state.selection;
                  apply({
                    runtimeId,
                    accountId,
                    model,
                    ...(reasoning ? { reasoning } : {}),
                  });
                }}
              >
                {t("form.applyModel")}
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
      {invalid ? (
        <p role="alert" className="text-destructive type-caption">
          {t("form.modelUnavailable")}
        </p>
      ) : null}
    </div>
  );
}
