import { useMutation, useQuery } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  getProjectOptions,
  listProjectsOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
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
import { StaticAvatar } from "@/shared/ui/avatar";
import { FormFooter } from "./form-footer";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { WorkspaceScopePicker } from "@/shared/ui/workspace-scope-picker";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Input } from "@/shared/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import { useSettingsDraftGuard, SettingsRow } from "@/modules/settings";
import type { IMBinding } from "../types";
import { canAuthorizeWithoutConfiguration } from "../model/binding-authorization";
import {
  BindingConfigurationFields,
  BindingExecutorKind,
} from "./binding-configuration-fields";
import { useBindingAuthorization } from "./use-binding-authorization";

function bindingDraft(binding: IMBinding) {
  const kind: "team" | "waker" = binding.channelId ? "team" : "waker";
  const options: ExecutionSelection = binding.options ?? {};
  return {
    kind,
    target: binding.channelId || binding.wakerId || "",
    projectID: binding.projectId || "",
    directory: binding.workingDirectory || "",
    controllers: binding.controllerIds?.join(", ") || "",
    allowWorkflow: binding.allowWorkflowDelivery ?? true,
    options,
  };
}

type BindingDraft = ReturnType<typeof bindingDraft>;

type BindingEditor = {
  initial: BindingDraft;
  draft: BindingDraft;
  directoryError?: unknown;
};

export function BindingForm({
  binding: currentBinding,
  onClose,
  refreshBindings,
  manageAuthorization = false,
  readOnly = false,
  authorizationDisabled = false,
}: {
  binding: IMBinding;
  onClose: () => void;
  refreshBindings: () => Promise<unknown>;
  manageAuthorization?: boolean;
  readOnly?: boolean;
  authorizationDisabled?: boolean;
}) {
  const { t } = useTranslation("im");
  const id = useId();
  // 查看态展示已保存设置。编辑开始时初始化草稿，后续读取保留用户输入。
  const [editor, setEditor] = useState<BindingEditor>();
  if (!readOnly && !editor) {
    const initial = bindingDraft(currentBinding);
    setEditor({ initial, draft: initial });
  } else if (readOnly && editor) {
    setEditor(undefined);
  }
  function changeDraft(patch: Partial<BindingDraft>) {
    setEditor((current) =>
      current && current.initial === editor?.initial
        ? {
            ...current,
            draft: { ...current.draft, ...patch },
            directoryError: undefined,
          }
        : current,
    );
  }
  const approved = currentBinding.state === "approved";
  const initialDraft = editor?.initial ?? bindingDraft(currentBinding);
  const draft = editor?.draft ?? initialDraft;
  const {
    kind,
    target,
    projectID,
    directory,
    controllers,
    allowWorkflow,
    options,
  } = draft;
  const dirty = JSON.stringify(draft) !== JSON.stringify(initialDraft);
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
    enabled: !readOnly && Boolean(owner),
  });
  const listedProject = projects.data?.resources?.find(
    (item) => item.id === projectID,
  );
  const selectedProject = useQuery({
    ...getProjectOptions(
      withLocalErrorSurface({ path: { wakerId: owner, projectId: projectID } }),
    ),
    enabled: Boolean(owner && projectID && !listedProject),
  });
  const execution = useExecutionSelection({
    value: options,
    enabled: kind === "waker" && (!readOnly || Boolean(options.runtimeId)),
    loadNativeCatalog: !readOnly,
  });
  // 其他字段的编辑沿用已选运行配置，Core 负责校验提交。
  const executionOptions = options.runtimeId ? options : execution.selection;
  const canReuseConfiguration =
    !approved && !dirty && canAuthorizeWithoutConfiguration(currentBinding);
  const save = useMutation({
    mutationFn: () =>
      updateImBinding(
        withLocalErrorSurface({
          path: { bindingId: currentBinding.id || "" },
          body: {
            state: approved ? undefined : "approved",
            configuration: {
              name: currentBinding.name || currentBinding.externalChatId || "",
              wakerId: kind === "waker" ? target : "",
              channelId: kind === "team" ? target : "",
              projectId: projectID,
              workingDirectory: directory,
              options:
                kind === "waker"
                  ? {
                      ...executionOptions,
                      permissionLevel: execution.selection.permissionLevel,
                    }
                  : {},
              controllerIds: controllers.split(/[,，\s]+/).filter(Boolean),
              allowWorkflowDelivery: allowWorkflow,
            },
          },
          throwOnError: true,
        }),
      ),
    onSuccess: async () => {
      await refreshBindings();
      onClose();
    },
  });
  const authorization = useBindingAuthorization({
    binding: currentBinding,
    refresh: refreshBindings,
    onAuthorized: () => {
      if (!readOnly) onClose();
    },
  });
  const busy = save.isPending || authorization.isPending;
  const executionReady = kind === "team" || Boolean(executionOptions.runtimeId);
  const canSubmit =
    !readOnly &&
    !busy &&
    !authorization.confirmingRevocation &&
    (canReuseConfiguration
      ? !authorizationDisabled
      : dirty && Boolean(target) && executionReady);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!canSubmit) return;
    if (canReuseConfiguration) authorization.approve();
    else save.mutate();
  };
  const controlsDisabled =
    readOnly || busy || authorization.confirmingRevocation;
  const canRevoke = manageAuthorization && approved;
  const canApproveSaved =
    manageAuthorization &&
    !approved &&
    readOnly &&
    canAuthorizeWithoutConfiguration(currentBinding);
  const waker =
    kind === "waker"
      ? executors.data?.wakers.find((item) => item.id === target)
      : undefined;
  const executorName =
    (kind === "team" ? team?.channel?.name : waker?.name) || target;
  useSettingsDraftGuard({
    dirty: !readOnly && dirty,
    pending: busy,
  });
  const inlineApproval = !readOnly && !approved;
  const scope =
    directory || listedProject?.name || selectedProject.data?.name || projectID;
  const fieldError =
    executors.error ||
    projects.error ||
    selectedProject.error ||
    editor?.directoryError;
  const executionError = !readOnly && execution.error;
  const authorizationError = authorization.error ? (
    <p role="alert" className="text-destructive type-body basis-full">
      {localApiErrorMessage(authorization.error, t("operationFailed"))}
    </p>
  ) : null;
  const actions = (
    <>
      {save.error ? (
        <p role="alert" className="text-destructive type-body basis-full">
          {localApiErrorMessage(save.error, t("bindingSaveFailed"))}
        </p>
      ) : null}
      {!inlineApproval && dirty ? (
        <span className="type-body text-content-secondary mr-auto">
          {t("unsavedChanges")}
        </span>
      ) : null}
      <Button
        type="button"
        variant="secondary"
        disabled={busy}
        onClick={() => {
          save.reset();
          onClose();
        }}
      >
        {t("cancel")}
      </Button>
      <Button
        type="submit"
        form={id + "-form"}
        aria-busy={busy}
        disabled={!canSubmit}
      >
        {t(approved ? "save" : "authorize")}
      </Button>
    </>
  );
  return (
    <form id={id + "-form"} className="contents" onSubmit={submit}>
      <fieldset disabled={controlsDisabled} className="grid min-h-0 gap-6">
        <BindingConfigurationFields
          executor={
            <>
              <BindingExecutorKind
                value={readOnly && !target ? "" : kind}
                onChange={
                  controlsDisabled
                    ? undefined
                    : (value) =>
                        changeDraft({
                          kind: value,
                          target: "",
                          projectID: "",
                          directory: "",
                        })
                }
              />
              <Select
                value={target}
                onValueChange={(value) =>
                  changeDraft({ target: value, projectID: "", directory: "" })
                }
                disabled={controlsDisabled}
              >
                <SelectTrigger
                  appearance="settings"
                  size="settings"
                  aria-label={t("executor")}
                >
                  <SelectValue placeholder={t("chooseExecutor")}>
                    {target ? (
                      <span className="flex items-center gap-2">
                        {kind === "waker" ? (
                          <StaticAvatar
                            name={executorName}
                            src={waker?.avatar}
                            size="xs"
                          />
                        ) : null}
                        <span className="min-w-0 truncate">{executorName}</span>
                      </span>
                    ) : null}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {kind === "waker"
                    ? executors.data?.wakers
                        .filter((item) => item.id)
                        .map((item) => (
                          <SelectItem key={item.id} value={item.id || ""}>
                            <span className="flex items-center gap-2">
                              <StaticAvatar
                                name={item.name || item.id || ""}
                                src={item.avatar}
                                size="xs"
                              />
                              <span className="min-w-0 truncate">
                                {item.name || item.id}
                              </span>
                            </span>
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
            </>
          }
          runtime={
            kind === "waker" ? (
              <>
                <ExecutionConfigurationControls
                  state={execution}
                  onChange={(value) => changeDraft({ options: value })}
                  presentation="settings"
                  savedSelection={
                    readOnly
                      ? { value: options, placeholder: t("notConfigured") }
                      : undefined
                  }
                  disabled={controlsDisabled}
                />
                {executionError ? (
                  <div className="flex w-full flex-wrap items-center gap-2">
                    <p role="alert" className="text-destructive type-caption">
                      {localApiErrorMessage(
                        executionError,
                        t("common:executionSelection.loadError"),
                      )}
                    </p>
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      disabled={
                        execution.runtimesQuery.isFetching ||
                        execution.accountsQuery.isFetching ||
                        execution.catalogRefreshing
                      }
                      onClick={() => {
                        if (execution.runtimesQuery.isError)
                          void execution.runtimesQuery.refetch();
                        if (execution.accountsQuery.isError)
                          void execution.accountsQuery.refetch();
                        if (execution.catalogIssue)
                          void execution.refreshCatalog();
                      }}
                    >
                      {t("reload")}
                    </Button>
                  </div>
                ) : null}
              </>
            ) : (
              <p className="text-content-secondary type-caption">
                {t("teamDefaults")}
              </p>
            )
          }
          scope={
            <WorkspaceScopePicker
              ownerId={owner}
              projects={projects.data?.resources || []}
              projectId={projectID}
              onSelectManaged={() =>
                changeDraft({ projectID: "", directory: "" })
              }
              onSelectProject={(value) =>
                changeDraft({ projectID: value, directory: "" })
              }
              onPickDirectory={async () => {
                try {
                  const path = await selectDirectory();
                  if (path) changeDraft({ directory: path, projectID: "" });
                } catch (error) {
                  setEditor((current) =>
                    current && current.initial === editor?.initial
                      ? { ...current, directoryError: error }
                      : current,
                  );
                }
              }}
              trigger={
                <FieldControlTrigger
                  aria-label={t("scope")}
                  title={scope || undefined}
                  disabled={controlsDisabled}
                >
                  <span>{scope || t("scopePlaceholder")}</span>
                </FieldControlTrigger>
              }
            />
          }
          controllers={
            currentBinding.kind === "group"
              ? {
                  id: id + "-controllers",
                  control: (
                    <Input
                      appearance="catalog"
                      id={id + "-controllers"}
                      aria-describedby={id + "-controllers-hint"}
                      required
                      placeholder={
                        currentBinding.controllerIds?.join(", ") ||
                        t("controllers")
                      }
                      value={controllers}
                      onChange={(e) =>
                        changeDraft({ controllers: e.target.value })
                      }
                    />
                  ),
                }
              : undefined
          }
          workflow={{
            id: id + "-workflow",
            control: (
              <Switch
                id={id + "-workflow"}
                checked={allowWorkflow}
                onClick={() => changeDraft({ allowWorkflow: !allowWorkflow })}
              />
            ),
          }}
        />
      </fieldset>
      {fieldError ? (
        <div className="flex flex-wrap items-center gap-2">
          <p role="alert" className="text-destructive type-caption">
            {localApiErrorMessage(fieldError, t("operationFailed"))}
          </p>
          {executors.isError || projects.isError || selectedProject.isError ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                if (executors.isError) void executors.refetch();
                if (projects.isError) void projects.refetch();
                if (selectedProject.isError) void selectedProject.refetch();
              }}
            >
              {t("reload")}
            </Button>
          ) : null}
        </div>
      ) : null}
      {canRevoke ? (
        <SettingsRow grouped title={t("revoke")} description={t("revokeHint")}>
          <div className="flex flex-wrap items-center justify-end gap-2">
            {authorizationError}
            {authorization.confirmingRevocation ? (
              <>
                <Button
                  type="button"
                  size="settings"
                  variant="ghost"
                  disabled={busy}
                  onClick={authorization.cancelRevocation}
                >
                  {t("keepAuthorization")}
                </Button>
                <Button
                  type="button"
                  size="settings"
                  variant="danger-quiet"
                  aria-busy={authorization.isPending}
                  disabled={busy || authorizationDisabled}
                  onClick={authorization.confirmRevocation}
                >
                  {t("confirmRevoke")}
                </Button>
              </>
            ) : (
              <Button
                type="button"
                size="settings"
                variant="danger-quiet"
                disabled={busy || authorizationDisabled}
                onClick={authorization.startRevocation}
              >
                {t("revoke")}
              </Button>
            )}
          </div>
        </SettingsRow>
      ) : inlineApproval || canApproveSaved ? (
        <p className="type-body text-content-secondary px-4">
          {t(
            currentBinding.kind === "group"
              ? "groupAuthorization"
              : "directAuthorization",
          )}
        </p>
      ) : null}
      {canApproveSaved ? (
        <div className="flex flex-wrap items-center justify-end gap-2 px-4">
          {authorizationError}
          <Button
            type="button"
            aria-busy={authorization.isPending}
            disabled={busy || authorizationDisabled}
            onClick={authorization.approve}
          >
            {t("authorize")}
          </Button>
        </div>
      ) : inlineApproval ? (
        <div className="flex flex-wrap items-center justify-end gap-2 px-4">
          {authorizationError}
          {actions}
        </div>
      ) : !readOnly ? (
        <FormFooter host="page">{actions}</FormFooter>
      ) : null}
    </form>
  );
}
