import { i18n } from "@/shared/i18n";
import {
  automaticTaskApiZh,
  automaticTaskApiEn,
} from "@/shared/i18n/automatic-task-api";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Clock3,
  Folder,
  FolderKanban,
  LoaderCircle,
  Waypoints,
  UserRound,
  UsersRound,
} from "lucide-react";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { getWorkflowOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { useSchemaFormValidity } from "@/shared/json-schema/schema-form";
import { WorkflowInputFields } from "./workflow-input-fields";
import {
  useTaskWorkflows,
  useTaskWorkflowVersions,
} from "../api/use-task-workflows";
import {
  completeInputBindings,
  supportsInputBindings,
} from "../model/workflow-inputs";
import { listProjectsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { selectDirectory } from "@/shared/platform";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Avatar } from "@/shared/ui/avatar";
import { TeamAvatarStack } from "@/modules/team";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { FieldLabel } from "@/shared/ui/field-label";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { Textarea } from "@/shared/ui/textarea";
import {
  createWorkflowAutomaticTaskMutation,
  updateWorkflowAutomaticTaskMutation,
  createAutomaticTaskMutation,
  refreshAutomaticTasks,
  updateAutomaticTaskMutation,
} from "../api/task-queries";
import type { SavedTask, TaskConfiguration, TaskView } from "../model/types";
import {
  DEFAULT_SCHEDULE_TIMEZONE,
  withScheduleTimezone,
} from "../model/schedule-defaults";
import { ScheduleFields } from "./schedule-fields";
import { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import { TeamExecutionControls } from "./team-execution-controls";

function Field({
  label,
  children,
  htmlFor,
  icon,
  required = false,
}: {
  label: string;
  children: ReactNode;
  htmlFor?: string;
  icon?: ReactNode;
  required?: boolean;
}) {
  return (
    <div className="grid min-w-0 gap-2">
      {label ? (
        <FieldLabel htmlFor={htmlFor} required={required}>
          {icon ? (
            <span
              aria-hidden
              className="text-content-secondary [&_svg]:size-icon-sm"
            >
              {icon}
            </span>
          ) : null}
          {label}
        </FieldLabel>
      ) : null}
      {children}
    </div>
  );
}

i18n.addResourceBundle(
  "zh-CN",
  "automaticTask",
  automaticTaskApiZh,
  true,
  true,
);
i18n.addResourceBundle(
  "en-US",
  "automaticTask",
  automaticTaskApiEn,
  true,
  true,
);

export function TaskForm({
  task,
  wakerId,
  onSaved,
  onCancel,
}: {
  task?: TaskView;
  wakerId: string;
  onSaved: (value: SavedTask) => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const client = useQueryClient();
  const [config, setConfig] = useState<TaskConfiguration>(() =>
    task
      ? {
          name: task.name,
          targetKind: task.targetKind,
          workflowId: task.workflowId,
          workflowRevisionPolicy: task.workflowRevisionPolicy,
          workflowRevisionId: task.workflowRevisionId,
          workflowInputRevisionId: task.workflowInputRevisionId,
          workflowInputBindings: task.workflowInputBindings,
          workspaceReadOnly: task.workspaceReadOnly,
          instruction: task.instruction,
          runtimeId: task.runtimeId,
          trigger: task.trigger,
          schedule:
            task.trigger === "schedule"
              ? withScheduleTimezone(task.schedule ?? {})
              : task.schedule,
          enabled: task.enabled,
          projectId: task.projectId,
          workingDirectory: task.workingDirectory,
          accountId: task.accountId,
          model: task.model,
          reasoning: task.reasoning,
          permissionLevel: task.permissionLevel,
          executorWakerId: task.channelId
            ? ""
            : task.executorWakerId || task.wakerId || wakerId,
          channelId: task.channelId || "",
          memberConfigs: task.memberConfigs || [],
        }
      : {
          name: "",
          instruction: "",
          runtimeId: "",
          executorWakerId: wakerId,
          channelId: "",
          trigger: "schedule",
          enabled: true,
          permissionLevel: "default",
          schedule: {
            kind: "daily",
            timezone: DEFAULT_SCHEDULE_TIMEZONE,
            hour: 9,
            minute: 0,
            weekday: 1,
            dayOfMonth: 1,
            cron: "0 9 * * *",
          },
        },
  );
  const [scope, setScope] = useState<"project" | "directory">(
    task?.workingDirectory ? "directory" : "project",
  );
  const [directoryError, setDirectoryError] = useState<unknown>();
  const [teamExecutionValid, setTeamExecutionValid] = useState(false);
  const [executorKind, setExecutorKind] = useState(
    task?.workflowId ? "workflow" : task?.channelId ? "team" : "waker",
  );
  const executors = useWorkExecutors();
  const team = executors.data?.teams.find(
    (item) => item.channel?.id === config.channelId,
  );
  const selectedWakerId =
    executorKind === "team"
      ? team?.channel?.leadWakerId ||
        (config.channelId === task?.channelId ? task?.wakerId : "") ||
        ""
      : config.executorWakerId || "";
  const execution = useExecutionSelection({
    value: config,
    enabled: executorKind === "waker",
  });
  const projects = useQuery({
    ...listProjectsOptions({
      path: { wakerId: selectedWakerId },
      query: { pageNum: 1, pageSize: 100 },
    }),
    enabled: executorKind !== "workflow" && Boolean(selectedWakerId),
  });
  const workflows = useTaskWorkflows(executorKind === "workflow");
  const policy =
    config.workflowRevisionPolicy ||
    (config.trigger === "api" ? "pinned" : "current");
  const workflow = useQuery({
    ...getWorkflowOptions({
      path: { id: config.workflowId || "" },
      query: {
        revisionId:
          policy === "pinned"
            ? config.workflowRevisionId || undefined
            : undefined,
      },
    }),
    enabled: executorKind === "workflow" && Boolean(config.workflowId),
  });
  const versions = useTaskWorkflowVersions(
    config.workflowId || "",
    executorKind === "workflow",
  );
  const inputValidity = useSchemaFormValidity();
  const createWorkflow = useMutation(
    withLocalErrorMutation(createWorkflowAutomaticTaskMutation()),
  );
  const updateWorkflow = useMutation(
    withLocalErrorMutation(updateWorkflowAutomaticTaskMutation()),
  );
  const schema = workflow.data?.revision?.manifest?.meta?.inputSchema ?? {};
  const create = useMutation(
    withLocalErrorMutation(createAutomaticTaskMutation()),
  );
  const update = useMutation(
    withLocalErrorMutation(updateAutomaticTaskMutation()),
  );
  const set = (value: Partial<TaskConfiguration>) =>
    setConfig((current) => ({ ...current, ...value }));
  const pending =
    create.isPending ||
    update.isPending ||
    createWorkflow.isPending ||
    updateWorkflow.isPending;
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (executorKind === "workflow" && !inputValidity.valid) return;
    const body: TaskConfiguration =
      executorKind === "workflow"
        ? {
            ...config,
            targetKind: "workflow",
            workflowRevisionPolicy: policy,
            workflowRevisionId:
              policy === "pinned" ? workflow.data?.revision?.id : "",
            workflowInputRevisionId: workflow.data?.revision?.id,
            workflowInputBindings: completeInputBindings(
              schema,
              config.workflowInputBindings ?? {},
            ),
            instruction: "",
            executorWakerId: "",
            channelId: "",
            memberConfigs: [],
            projectId: "",
            workingDirectory: "",
          }
        : {
            ...config,
            ...(executorKind === "waker"
              ? execution.selection
              : {
                  runtimeId: "",
                  accountId: "system",
                  model: "",
                  reasoning: "",
                  permissionLevel: "default",
                }),
            targetKind: executorKind === "team" ? "team" : "waker",
            workflowId: "",
            workflowRevisionId: "",
            workflowRevisionPolicy: "",
            workflowInputRevisionId: "",
            workflowInputBindings: {},
            executorWakerId: executorKind === "waker" ? selectedWakerId : "",
            channelId: executorKind === "team" ? config.channelId : "",
            memberConfigs:
              executorKind === "team" ? config.memberConfigs || [] : [],
            projectId: scope === "project" ? config.projectId : "",
            workingDirectory:
              scope === "directory" ? config.workingDirectory : "",
          };
    try {
      const saved =
        executorKind === "workflow"
          ? task?.id
            ? await updateWorkflow.mutateAsync({
                path: { taskId: task.id },
                body: { ...body, revision: task.revision || 0 },
              })
            : await createWorkflow.mutateAsync({ body })
          : task?.id
            ? await update.mutateAsync({
                path: { wakerId: task.wakerId || wakerId, taskId: task.id },
                body: { ...body, revision: task.revision || 0 },
              })
            : await create.mutateAsync({
                path: { wakerId: selectedWakerId },
                body,
              });
      await refreshAutomaticTasks(client, selectedWakerId, saved.taskId);
      onSaved(saved);
    } catch {
      /* 表单就近展示 Query 保存的错误。 */
    }
  };
  const pickDirectory = async () => {
    try {
      const value = await selectDirectory();
      if (value) set({ workingDirectory: value });
      setDirectoryError(undefined);
    } catch (error) {
      setDirectoryError(error);
    }
  };
  const select = (
    label: string,
    value: string,
    options: Array<{ id?: string; name?: string }>,
    onChange: (value: string) => void,
    placeholder?: string,
    icon?: ReactNode,
    hideLabel = false,
    required = true,
  ) => (
    <Field
      label={hideLabel ? "" : label}
      htmlFor={`${id}-${label}`}
      icon={icon}
    >
      <Select
        value={value}
        onValueChange={(next) => {
          if (next) onChange(next);
        }}
      >
        <SelectTrigger
          id={`${id}-${label}`}
          aria-label={label}
          aria-required={required || undefined}
        >
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {value && !options.some((option) => option.id === value) ? (
            <SelectItem value={value} disabled>
              {value}
            </SelectItem>
          ) : null}
          {options
            .filter((option) => option.id)
            .map((option) => (
              <SelectItem key={option.id} value={option.id || ""}>
                {option.name || option.id}
              </SelectItem>
            ))}
        </SelectContent>
      </Select>
    </Field>
  );
  return (
    <form
      onSubmit={(event) => void save(event)}
      className="flex min-h-0 flex-1 flex-col"
    >
      <div className="relative min-h-0 flex-1 space-y-6 overflow-y-auto px-6 pb-6">
        <Field label={t("name")} htmlFor={`${id}-name`} required>
          <Input
            autoFocus
            id={`${id}-name`}
            aria-label={t("name")}
            required
            maxLength={120}
            value={config.name}
            placeholder={t("namePlaceholder")}
            onChange={(e) => set({ name: e.target.value })}
          />
        </Field>
        <Card variant="section" className="overflow-hidden p-0">
          <div className="bg-surface-tertiary p-4">
            <Field label={t("filterTrigger")} required>
              <SegmentedControl
                appearance="cards"
                ariaLabel={t("filterTrigger")}
                value={config.trigger === "api" ? "api" : "schedule"}
                options={[
                  {
                    value: "schedule",
                    label: t("schedule"),
                    icon: <Clock3 />,
                    description: t("scheduleDescription"),
                  },
                  {
                    value: "api",
                    label: t("api"),
                    icon: <Waypoints />,
                    description: t("apiDescription"),
                  },
                ]}
                onValueChange={(trigger) => set({ trigger })}
              />
            </Field>
          </div>
          <div className="grid gap-3 p-4">
            {config.trigger === "schedule" ? (
              <ScheduleFields
                wakerId={
                  executorKind === "workflow" ? undefined : selectedWakerId
                }
                rule={config.schedule || {}}
                onChange={(schedule) => set({ schedule })}
              />
            ) : (
              <p className="text-content-secondary type-caption">
                {t(task?.trigger === "api" ? "apiEditHint" : "apiHint")}
              </p>
            )}
            {task?.trigger === "api" && config.trigger !== "api" ? (
              <p className="text-warning type-ui">{t("switchWarning")}</p>
            ) : null}
          </div>
        </Card>
        <div className="grid gap-3">
          <Field label={t("executor")} required>
            <SegmentedControl
              ariaLabel={t("executorKind")}
              value={executorKind}
              options={[
                {
                  value: "waker",
                  label: t("partnerExecutor"),
                  icon: <UserRound />,
                },
                {
                  value: "workflow",
                  label: t("workflowExecutor"),
                  icon: <Waypoints />,
                },
                {
                  value: "team",
                  label: t("teamExecutor"),
                  icon: <UsersRound />,
                },
              ]}
              onValueChange={(value) => {
                setExecutorKind(value);
                set({
                  executorWakerId: "",
                  channelId: "",
                  projectId: "",
                  memberConfigs: [],
                  workflowId: "",
                  workflowRevisionId: "",
                  workflowInputRevisionId: "",
                  workflowInputBindings: {},
                });
              }}
            />
          </Field>
          {executorKind === "workflow" ? (
            select(
              t("workflowExecutor"),
              config.workflowId || "",
              (workflows.data ?? [])
                .filter((item) => item.currentRevisionId)
                .map((item) => ({ id: item.id, name: item.title })),
              (workflowId) =>
                set({
                  workflowId,
                  workflowRevisionId: "",
                  workflowInputRevisionId: "",
                  workflowInputBindings: {},
                }),
              t("chooseWorkflow"),
            )
          ) : (
            <Select
              value={
                (executorKind === "team"
                  ? config.channelId
                  : config.executorWakerId) || ""
              }
              disabled={pending || executors.isPending}
              onValueChange={(value) => {
                if (value)
                  set(
                    executorKind === "team"
                      ? {
                          channelId: value,
                          executorWakerId: "",
                          projectId: "",
                          memberConfigs: [],
                        }
                      : {
                          executorWakerId: value,
                          channelId: "",
                          projectId: "",
                        },
                  );
              }}
            >
              <SelectTrigger aria-label={t("executor")} aria-required>
                <SelectValue
                  placeholder={t(
                    executorKind === "team" ? "chooseTeam" : "choosePartner",
                  )}
                />
              </SelectTrigger>
              <SelectContent>
                {executorKind === "team"
                  ? executors.data?.teams.map((item) =>
                      item.channel?.id ? (
                        <SelectItem
                          key={item.channel.id}
                          value={item.channel.id}
                        >
                          <span className="flex items-center gap-2">
                            <TeamAvatarStack
                              animated={false}
                              members={item.members || []}
                              size="xs"
                            />
                            {item.channel.name}
                          </span>
                        </SelectItem>
                      ) : null,
                    )
                  : executors.data?.wakers.map((item) =>
                      item.id ? (
                        <SelectItem key={item.id} value={item.id}>
                          <span className="flex items-center gap-2">
                            <Avatar
                              name={item.name || ""}
                              src={item.avatar}
                              size="xs"
                              interactive={false}
                              animated={false}
                            />
                            {item.name}
                          </span>
                        </SelectItem>
                      ) : null,
                    )}
              </SelectContent>
            </Select>
          )}
          {executors.error ? (
            <p role="alert" className="text-destructive type-ui">
              {localApiErrorMessage(executors.error, t("actionFailed"))}
            </p>
          ) : null}
        </div>
        {executorKind === "workflow" ? (
          <div className="grid gap-4">
            {task?.workflowInputRevisionId &&
            workflow.data?.revision?.id &&
            task.workflowInputRevisionId !== workflow.data.revision.id ? (
              <p role="status" className="text-warning type-body">
                {t("inputVersionChanged")}
              </p>
            ) : null}
            {workflow.isError || workflows.isError ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(
                  workflow.error || workflows.error,
                  t("actionFailed"),
                )}
              </p>
            ) : workflow.data?.revision ? (
              <WorkflowInputFields
                workflowId={config.workflowId}
                key={workflow.data.revision.id}
                schema={schema}
                bindings={config.workflowInputBindings ?? {}}
                allowAPI={config.trigger === "api"}
                onChange={(workflowInputBindings) =>
                  set({ workflowInputBindings })
                }
                fieldOrder={workflow.data.revision.manifest?.inputPropertyOrder}
                onValidityChange={inputValidity.onValidityChange}
              />
            ) : config.workflowId ? (
              <p role="status" className="type-body">
                {t("chooseWorkflow")}
              </p>
            ) : null}
            <details>
              <summary className="text-content-secondary type-body cursor-pointer">
                {t("workflowAdvanced")}
              </summary>
              <div className="mt-3 grid gap-4">
                <Field label={t("workflowVersion")}>
                  <Select
                    value={policy}
                    onValueChange={(workflowRevisionPolicy) =>
                      set({
                        workflowRevisionPolicy:
                          workflowRevisionPolicy === "pinned"
                            ? "pinned"
                            : "current",
                      })
                    }
                  >
                    <SelectTrigger aria-label={t("workflowVersion")}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="pinned">
                        {t("pinnedVersion")}
                      </SelectItem>
                      <SelectItem value="current">
                        {t("currentVersion")}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  {policy === "pinned" && versions.data?.length ? (
                    <Select
                      value={
                        config.workflowRevisionId || workflow.data?.revision?.id
                      }
                      onValueChange={(workflowRevisionId) =>
                        set({ workflowRevisionId, workflowInputRevisionId: "" })
                      }
                    >
                      <SelectTrigger aria-label={t("pinnedVersion")}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {versions.data.map((item) =>
                          item.id ? (
                            <SelectItem key={item.id} value={item.id}>
                              {item.ordinal}
                            </SelectItem>
                          ) : null,
                        )}
                      </SelectContent>
                    </Select>
                  ) : null}
                  <p className="text-content-secondary type-caption">
                    {t("versionHelp")}
                  </p>
                </Field>
              </div>
            </details>
          </div>
        ) : (
          <Field
            label={t("instruction")}
            htmlFor={`${id}-instruction`}
            required
          >
            <Textarea
              size="lg"
              id={`${id}-instruction`}
              aria-label={t("instruction")}
              required
              maxLength={10000}
              value={config.instruction}
              placeholder={t("instructionPlaceholder")}
              onChange={(e) => set({ instruction: e.target.value })}
              footer={
                executorKind === "waker" ? (
                  <ExecutionConfigurationControls
                    state={execution}
                    onChange={set}
                    disabled={pending}
                  />
                ) : team ? (
                  <TeamExecutionControls
                    key={team.channel?.id}
                    team={team}
                    overrides={config.memberConfigs || []}
                    onChange={(memberConfigs) => set({ memberConfigs })}
                    onValidityChange={setTeamExecutionValid}
                    disabled={pending}
                  />
                ) : undefined
              }
            />
            <p className="text-content-secondary type-caption">
              {t(
                executorKind === "team"
                  ? "teamConfigurationHint"
                  : "instructionHint",
              )}
            </p>
          </Field>
        )}

        {executorKind !== "workflow" ? (
          <div className="grid gap-3">
            <Field label={t("workspace")}>
              <SegmentedControl
                ariaLabel={t("workspace")}
                value={scope}
                options={[
                  {
                    value: "project",
                    label: t("project"),
                    icon: <FolderKanban />,
                  },
                  {
                    value: "directory",
                    label: t("directory"),
                    icon: <Folder />,
                  },
                ]}
                onValueChange={setScope}
              />
            </Field>
            {scope === "project" ? (
              select(
                t("project"),
                config.projectId || "temporary",
                [
                  { id: "temporary", name: t("temporaryWorkspace") },
                  ...(projects.data?.resources || []).filter(
                    (item) => item.available,
                  ),
                ],
                (projectId) =>
                  set({
                    projectId: projectId === "temporary" ? "" : projectId,
                  }),
                t("chooseProject"),
                undefined,
                true,
                false,
              )
            ) : (
              <FieldControlTrigger
                aria-label={t("directory")}
                disabled={pending}
                showChevron={false}
                onClick={() => void pickDirectory()}
                title={config.workingDirectory || undefined}
              >
                <span
                  className={
                    config.workingDirectory
                      ? undefined
                      : "text-muted-foreground"
                  }
                >
                  {config.workingDirectory || t("chooseDirectory")}
                </span>
                <Folder
                  className="text-content-secondary size-icon-sm shrink-0"
                  aria-hidden
                />
              </FieldControlTrigger>
            )}
            {scope === "directory" && config.workingDirectory ? (
              <div>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={pending}
                  onClick={() => set({ workingDirectory: "" })}
                >
                  {t("useTemporaryWorkspace")}
                </Button>
              </div>
            ) : null}
            <p className="text-content-secondary type-caption">
              {t("workspaceHint")}
            </p>
            {projects.isError || directoryError ? (
              <p role="alert" className="text-destructive type-ui">
                {localApiErrorMessage(
                  directoryError || projects.error,
                  t("actionFailed"),
                )}
              </p>
            ) : null}
          </div>
        ) : null}
        <div className="flex items-center justify-between gap-3">
          <span className="type-ui font-medium">{t("status")}</span>
          <div className="flex items-center gap-2">
            <span className="type-ui">
              {t(config.enabled ? "enabled" : "disabled")}
            </span>
            <Switch
              aria-label={t("status")}
              checked={Boolean(config.enabled)}
              onClick={() => set({ enabled: !config.enabled })}
            />
          </div>
        </div>
        {create.error ||
        update.error ||
        createWorkflow.error ||
        updateWorkflow.error ? (
          <p role="alert" className="text-destructive type-ui">
            {localApiErrorMessage(
              create.error ||
                update.error ||
                createWorkflow.error ||
                updateWorkflow.error,
              t("actionFailed"),
            )}
          </p>
        ) : null}
      </div>
      <DialogFooter>
        <Button
          type="button"
          variant="secondary"
          onClick={onCancel}
          disabled={pending}
        >
          {t("cancel")}
        </Button>
        <Button
          type="submit"
          disabled={
            pending ||
            (executorKind === "workflow"
              ? !workflow.data?.revision ||
                !inputValidity.valid ||
                !supportsInputBindings(schema) ||
                Object.values(config.workflowInputBindings ?? {}).some(
                  (binding) =>
                    config.trigger !== "api" && binding.source === "api",
                )
              : !selectedWakerId ||
                (executorKind === "team"
                  ? !team || !teamExecutionValid
                  : !execution.valid) ||
                !config.instruction?.trim()) ||
            (config.trigger === "schedule" &&
              config.schedule?.kind === "once" &&
              !config.schedule.onceAt) ||
            !config.name.trim()
          }
        >
          {pending ? <LoaderCircle className="animate-spin" /> : null}
          {t("save")}
        </Button>
      </DialogFooter>
    </form>
  );
}
