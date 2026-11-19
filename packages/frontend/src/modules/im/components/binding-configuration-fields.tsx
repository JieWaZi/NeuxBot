import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { SettingsFieldGroup, SettingsFieldRow } from "@/modules/settings";
import { FieldLabel } from "@/shared/ui/field-label";
import { SegmentedControl } from "@/shared/ui/segmented-control";

export function BindingExecutorKind({
  value,
  onChange,
}: {
  value: "" | "waker" | "team";
  onChange?: (value: "waker" | "team") => void;
}) {
  const { t } = useTranslation("im");
  return (
    <SegmentedControl
      ariaLabel={t("executor")}
      value={value}
      disabled={!onChange}
      onValueChange={(next) => {
        if (next) onChange?.(next);
      }}
      options={[
        { value: "waker", label: t("partner") },
        { value: "team", label: t("team") },
      ]}
    />
  );
}

export function BindingConfigurationFields({
  executor,
  runtime,
  scope,
  controllers,
  workflow,
}: {
  executor: ReactNode;
  runtime: ReactNode;
  scope: ReactNode;
  controllers?: { id: string; control: ReactNode };
  workflow: { id: string; control: ReactNode };
}) {
  const { t } = useTranslation("im");
  return (
    <>
      <SettingsFieldGroup surface="plain" title={t("runtimeConfiguration")}>
        <SettingsFieldRow label={t("executor")}>
          <div className="grid gap-3">{executor}</div>
        </SettingsFieldRow>
        <SettingsFieldRow label={t("runtimeConfiguration")}>
          <div className="flex flex-wrap items-center gap-2">{runtime}</div>
        </SettingsFieldRow>
      </SettingsFieldGroup>
      <SettingsFieldGroup surface="plain" title={t("permissionsAndScope")}>
        <SettingsFieldRow label={t("scope")}>{scope}</SettingsFieldRow>
        {controllers ? (
          <SettingsFieldRow
            label={
              <FieldLabel required htmlFor={controllers.id}>
                {t("controllers")}
              </FieldLabel>
            }
          >
            <div className="grid gap-2">
              {controllers.control}
              <p
                id={`${controllers.id}-hint`}
                className="text-content-secondary type-caption"
              >
                {t("controllerHint")}
              </p>
            </div>
          </SettingsFieldRow>
        ) : null}
        <SettingsFieldRow label={t("allowWorkflow")} htmlFor={workflow.id}>
          <div className="flex justify-end">{workflow.control}</div>
        </SettingsFieldRow>
      </SettingsFieldGroup>
    </>
  );
}
