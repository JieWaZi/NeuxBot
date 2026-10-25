import { useMutation, useQuery } from "@tanstack/react-query";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { listProjectsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { updateImBinding } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { selectDirectory } from "@/shared/platform";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { WorkspaceScopePicker } from "@/shared/ui/workspace-scope-picker";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import { SettingsFieldGroup, SettingsFieldRow } from "@/modules/settings";
import type { IMBinding } from "../types";

export function BindingForm({
  binding,
  onClose,
  onSaved,
  beforeFields,
  onBeforeSave,
  disabled = false,
  purpose = "edit",
}: {
  binding: IMBinding;
  onClose: () => void;
  onSaved: () => void;
  beforeFields?: ReactNode;
  onBeforeSave?: () => Promise<unknown>;
  disabled?: boolean;
  purpose?: "edit" | "authorization";
}) {
  const { t } = useTranslation("im");
  const id = useId();
  const [kind, setKind] = useState(binding.channelId ? "team" : "waker");
  const [target, setTarget] = useState(
    binding.channelId || binding.wakerId || "",
  );
  const [projectID, setProjectID] = useState(binding.projectId || "");
  const [directory, setDirectory] = useState(binding.workingDirectory || "");
  const [controllers, setControllers] = useState(
    binding.controllerIds?.join(", ") || "",
  );
  const [allowWorkflow, setAllowWorkflow] = useState(
    binding.allowWorkflowDelivery ?? true,
  );
  const [options, setOptions] = useState<ExecutionSelection>(
    binding.options || {},
  );
  const [directoryError, setDirectoryError] = useState<unknown>();
  const executors = useWorkExecutors();
  const team = executors.data?.teams.find(
    (item) => item.channel?.id === target,
  );
  const owner = kind === "team" ? team?.channel?.leadWakerId || "" : target;
  const projects = useQuery({
    ...listProjectsOptions(
      withLocalErrorSurface({
        path: { wakerId: owner },
        query: { pageNum: 1, pageSize: 100 },
      }),
    ),
    enabled: Boolean(owner),
  });
  const execution = useExecutionSelection({
    value: options,
    enabled: kind === "waker",
  });
  const save = useMutation({
    mutationFn: async () => {
      await onBeforeSave?.();
      // 尚未选择响应目标时，只保存连接设置，不隐式批准聊天。
      if (!target) return;
      return updateImBinding(
        withLocalErrorSurface({
          path: { bindingId: binding.id || "" },
          body: {
            name: binding.name || binding.externalChatId || "",
            state: "approved",
            wakerId: kind === "waker" ? target : "",
            channelId: kind === "team" ? target : "",
            projectId: projectID,
            workingDirectory: directory,
            options:
              kind === "waker"
                ? {
                    ...execution.selection,
                    permissionLevel: execution.selection.permissionLevel as
                      "default" | "auto" | "full-access",
                  }
                : {},
            controllerIds: controllers.split(/[,，\s]+/).filter(Boolean),
            allowWorkflowDelivery: allowWorkflow,
            revision: binding.revision,
          },
          throwOnError: true,
        }),
      );
    },
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    save.mutate();
  };
  const chooseTarget = (value: string) => {
    setTarget(value);
    setProjectID("");
    setDirectory("");
  };
  return (
    <form className="contents" onSubmit={submit}>
      <div className="grid min-h-0 gap-6 overflow-y-auto px-6 pb-6">
        {beforeFields}
        <SettingsFieldGroup title={t("runtimeConfiguration")}>
          <SettingsFieldRow label={t("executor")}>
            <div className="grid gap-3">
              <SegmentedControl
                ariaLabel={t("executor")}
                value={kind}
                onValueChange={(value) => {
                  setKind(value);
                  chooseTarget("");
                }}
                options={[
                  { value: "waker", label: t("partner") },
                  { value: "team", label: t("team") },
                ]}
              />
              <Select value={target} onValueChange={chooseTarget}>
                <SelectTrigger aria-label={t("executor")}>
                  <SelectValue placeholder={t("chooseExecutor")} />
                </SelectTrigger>
                <SelectContent>
                  {kind === "waker"
                    ? executors.data?.wakers
                        .filter((item) => item.id)
                        .map((item) => (
                          <SelectItem key={item.id} value={item.id || ""}>
                            {item.name}
                          </SelectItem>
                        ))
                    : executors.data?.teams
                        .filter((item) => item.channel?.id)
                        .map((item) => (
                          <SelectItem
                            key={item.channel?.id}
                            value={item.channel?.id || ""}
                          >
                            {item.channel?.name}
                          </SelectItem>
                        ))}
                </SelectContent>
              </Select>
              <p className="text-content-secondary type-caption">
                {t("executorHint")}
              </p>
            </div>
          </SettingsFieldRow>
          <SettingsFieldRow label={t("runtimeConfiguration")}>
            {kind === "waker" ? (
              <div className="flex flex-wrap items-center gap-2">
                <ExecutionConfigurationControls
                  state={execution}
                  onChange={setOptions}
                  disabled={save.isPending}
                />
              </div>
            ) : (
              <p className="text-content-secondary type-caption">
                {t("teamDefaults")}
              </p>
            )}
          </SettingsFieldRow>
        </SettingsFieldGroup>
        <SettingsFieldGroup title={t("permissionsAndScope")}>
          <SettingsFieldRow label={t("scope")}>
            <div className="grid gap-2">
              <WorkspaceScopePicker
                ownerId={owner}
                projects={projects.data?.resources || []}
                projectId={projectID}
                onSelectManaged={() => {
                  setProjectID("");
                  setDirectory("");
                }}
                onSelectProject={(value) => {
                  setProjectID(value);
                  setDirectory("");
                }}
                onPickDirectory={async () => {
                  try {
                    const path = await selectDirectory();
                    if (path) {
                      setDirectory(path);
                      setProjectID("");
                    }
                  } catch (error) {
                    setDirectoryError(error);
                  }
                }}
                trigger={
                  <FieldControlTrigger
                    aria-label={t("scope")}
                    title={directory || undefined}
                    disabled={save.isPending}
                  >
                    <span>
                      {projects.data?.resources?.find(
                        (item) => item.id === projectID,
                      )?.name ||
                        directory ||
                        t("scopePlaceholder")}
                    </span>
                  </FieldControlTrigger>
                }
              />
              <p className="text-content-secondary type-caption">
                {t("scopeHint")}
              </p>
            </div>
          </SettingsFieldRow>
          {binding.kind === "group" ? (
            <SettingsFieldRow
              label={
                <FieldLabel required htmlFor={id + "-controllers"}>
                  {t("controllers")}
                </FieldLabel>
              }
            >
              <div className="grid gap-2">
                <Input
                  id={id + "-controllers"}
                  required
                  placeholder={
                    binding.controllerIds?.join(", ") || t("controllers")
                  }
                  value={controllers}
                  onChange={(e) => setControllers(e.target.value)}
                />
                <p className="text-content-secondary type-caption">
                  {t("controllerHint")}
                </p>
              </div>
            </SettingsFieldRow>
          ) : null}
          <SettingsFieldRow
            label={
              <FieldLabel htmlFor={id + "-workflow"}>
                {t("allowWorkflow")}
              </FieldLabel>
            }
          >
            <div className="grid justify-items-start gap-2">
              <Switch
                id={id + "-workflow"}
                checked={allowWorkflow}
                onClick={() => setAllowWorkflow(!allowWorkflow)}
              />
              <p className="text-content-secondary type-caption">
                {t("workflowHint")}
              </p>
            </div>
          </SettingsFieldRow>
        </SettingsFieldGroup>
        {save.error || executors.error || projects.error || directoryError ? (
          <p role="alert" className="text-destructive type-caption">
            {localApiErrorMessage(
              save.error || executors.error || projects.error || directoryError,
              t("operationFailed"),
            )}
          </p>
        ) : null}
      </div>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button
          type="submit"
          disabled={
            (!target && !onBeforeSave) ||
            disabled ||
            save.isPending ||
            (kind === "waker" &&
              Boolean(target) &&
              (execution.loading ||
                Boolean(execution.error) ||
                !execution.selection.runtimeId))
          }
        >
          {t(
            purpose === "authorization"
              ? "authorize"
              : !target || binding.state === "approved"
                ? "save"
                : "authorize",
          )}
        </Button>
      </DialogFooter>
    </form>
  );
}
