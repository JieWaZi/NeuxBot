import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import {
  getWorkflowOptions,
  getWorkflowTriggersOptions,
  getWorkflowTriggersQueryKey,
  saveWorkflowTriggersMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { useSchemaFormValidity } from "@/shared/json-schema/schema-form";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { FieldLabel } from "@/shared/ui/field-label";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { refreshAutomaticTasks } from "../api/task-queries";
import type {
  SavedTask,
  TaskConfiguration,
  TaskSchedule,
  TaskView,
} from "../model/types";
import {
  DEFAULT_SCHEDULE_TIMEZONE,
  withScheduleTimezone,
} from "../model/schedule-defaults";
import { ScheduleFields } from "./schedule-fields";
import { useTaskWorkflowVersions } from "../api/use-task-workflows";
import { WorkflowInputFields } from "./workflow-input-fields";
import {
  completeInputBindings,
  supportsInputBindings,
  type InputBindings,
} from "../model/workflow-inputs";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsAutomaticTask as TriggerTask } from "@/shared/api/generated/types.gen";

export function WorkflowTriggerDialog({
  definitionId,
  task,
  open,
  onOpenChange,
  onSaved,
}: {
  definitionId: string;
  task?: TaskView;
  open: boolean;
  onOpenChange: (value: boolean) => void;
  onSaved?: (value: SavedTask) => void;
}) {
  const { t } = useTranslation("workflow");
  const definition = useQuery({
    ...getWorkflowOptions({
      path: { id: definitionId },
      query: { revisionId: task?.workflowRevisionId },
    }),
    enabled: open,
  });
  const configuration = useQuery({
    ...getWorkflowTriggersOptions({ path: { id: definitionId } }),
    enabled: open,
  });
  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          title={t("editTrigger")}
          description={t("triggerConfigurationHelp")}
          size="lg"
          layout="form"
        >
          {definition.isError || configuration.isError ? (
            <p role="alert" className="text-destructive type-body px-6 pb-5">
              {localApiErrorMessage(
                definition.error ?? configuration.error,
                t("error"),
              )}
            </p>
          ) : definition.data?.definition &&
            definition.data.revision &&
            configuration.data?.fingerprint ? (
            <WorkflowTriggerForm
              key={configuration.data.fingerprint}
              definitionId={definitionId}
              tasks={configuration.data.tasks ?? []}
              fingerprint={configuration.data.fingerprint}
              name={definition.data.definition.title ?? ""}
              revisionId={definition.data.revision.id ?? ""}
              schema={
                definition.data.revision.manifest?.meta?.inputSchema ?? {}
              }
              onCancel={() => onOpenChange(false)}
              onSaved={(tasks) => {
                onOpenChange(false);
                if (tasks[0]) onSaved?.(tasks[0]);
              }}
            />
          ) : (
            <p role="status" className="type-body px-6 pb-5">
              {t("loading")}
            </p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

type TriggerDraft = {
  key: string;
  taskId?: string;
  trigger: "schedule" | "api";
  schedule: TaskSchedule;
  bindings: InputBindings;
  valid: boolean;
};
const defaultRule = (): TaskSchedule => ({
  kind: "daily",
  timezone: DEFAULT_SCHEDULE_TIMEZONE,
  hour: 9,
  minute: 0,
});
function WorkflowTriggerForm({
  definitionId,
  tasks,
  fingerprint,
  name,
  revisionId,
  schema,
  onCancel,
  onSaved,
}: {
  definitionId: string;
  tasks: TriggerTask[];
  fingerprint: string;
  name: string;
  revisionId: string;
  schema: Record<string, unknown>;
  onCancel: () => void;
  onSaved: (tasks: SavedTask[]) => void;
}) {
  const { t } = useTranslation("workflow");
  const formId = useId();
  const client = useQueryClient();
  const initial = tasks[0];
  const [triggers, setTriggers] = useState<TriggerDraft[]>(() =>
    tasks.length
      ? tasks.map((task) => ({
          key: task.id ?? crypto.randomUUID(),
          taskId: task.id,
          bindings: task.workflowInputBindings ?? {},
          valid: true,
          trigger: task.trigger === "api" ? "api" : "schedule",
          schedule:
            task.trigger === "schedule"
              ? withScheduleTimezone(task.schedule ?? defaultRule())
              : defaultRule(),
        }))
      : [
          {
            key: crypto.randomUUID(),
            trigger: "schedule",
            schedule: defaultRule(),
            bindings: {},
            valid: true,
          },
        ],
  );
  const [policy, setPolicy] = useState(
    initial?.workflowRevisionPolicy ?? "current",
  );
  const [pinned, setPinned] = useState(
    initial?.workflowRevisionId || revisionId,
  );
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [readonly, setReadonly] = useState(initial?.workspaceReadOnly ?? false);
  const [selection, setSelection] = useState<ExecutionSelection>({
    runtimeId: initial?.runtimeId,
    accountId: initial?.accountId,
    model: initial?.model,
    reasoning: initial?.reasoning,
    permissionLevel: initial?.permissionLevel ?? "default",
  });
  const execution = useExecutionSelection({ value: selection, enabled: true });
  const versions = useTaskWorkflowVersions(definitionId);
  const chosen = useQuery(
    getWorkflowOptions({
      path: { id: definitionId },
      query: { revisionId: policy === "pinned" ? pinned : undefined },
    }),
  );
  const selectedSchema =
    chosen.data?.revision?.manifest?.meta?.inputSchema ?? schema;
  const save = useMutation(
    withLocalErrorMutation(saveWorkflowTriggersMutation()),
  );

  const request = useRef<{ signature: string; id: string } | undefined>(
    undefined,
  );
  const setTrigger = (key: string, values: Partial<TriggerDraft>) =>
    setTriggers((current) =>
      current.map((value) =>
        value.key === key ? { ...value, ...values } : value,
      ),
    );
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (triggers.some((value) => !value.valid)) return;
    const value = execution.selection;
    const configuration: TaskConfiguration = {
      targetKind: "workflow",
      workflowId: definitionId,
      workflowRevisionPolicy: policy,
      workflowRevisionId: policy === "pinned" ? pinned : "",
      workflowInputRevisionId: chosen.data?.revision?.id,
      workspaceReadOnly: readonly,
      name: name.slice(0, 120),
      instruction: "",
      trigger: "schedule",
      schedule: {},
      enabled,
      executorWakerId: "",
      channelId: "",
      projectId: "",
      workingDirectory: "",
      runtimeId: value.runtimeId,
      accountId: value.accountId,
      model: value.model,
      reasoning: value.reasoning,
      permissionLevel: permissionLevelOf(value.permissionLevel),
    };
    const body = {
      expectedFingerprint: fingerprint,
      configuration,
      triggers: triggers.map((trigger) => ({
        taskId: trigger.taskId,
        inputBindings: completeInputBindings(selectedSchema, trigger.bindings),
        trigger: trigger.trigger,
        schedule: trigger.trigger === "schedule" ? trigger.schedule : {},
      })),
    };
    const signature = JSON.stringify(body);
    if (request.current?.signature !== signature)
      request.current = { signature, id: crypto.randomUUID() };
    try {
      const result = await save.mutateAsync({
        path: { id: definitionId },
        body: { ...body, requestId: request.current.id },
      });
      await Promise.all([
        refreshAutomaticTasks(client, initial?.wakerId ?? ""),
        client.invalidateQueries({
          queryKey: getWorkflowTriggersQueryKey({ path: { id: definitionId } }),
        }),
      ]);
      onSaved(result.tasks ?? []);
    } catch {
      /* Local mutation owns the error and preserves the draft. */
    }
  };
  return (
    <form
      id={formId}
      onSubmit={(event) => void submit(event)}
      className="contents"
    >
      <div className="grid min-h-0 gap-5 overflow-y-auto px-6 pb-5">
        {triggers.map((trigger, index) => (
          <section
            key={trigger.key}
            className="bg-surface-secondary grid gap-4 rounded-lg p-4"
          >
            <div className="flex items-center justify-between gap-2">
              <h2 className="type-label font-medium">
                {t("triggerOrdinal", { number: index + 1 })}
              </h2>
              <Button
                variant="ghost"
                size="icon-sm"
                type="button"
                aria-label={t("removeTrigger", { number: index + 1 })}
                onClick={() =>
                  setTriggers((current) =>
                    current.filter((value) => value.key !== trigger.key),
                  )
                }
                disabled={save.isPending}
              >
                <Trash2 aria-hidden />
              </Button>
            </div>
            <SegmentedControl
              ariaLabel={t("triggerType")}
              value={trigger.trigger}
              onValueChange={(value) =>
                setTrigger(trigger.key, {
                  trigger: value === "api" ? "api" : "schedule",
                })
              }
              options={[
                { value: "schedule", label: t("scheduleTrigger") },
                { value: "api", label: t("apiTrigger") },
              ]}
            />
            {trigger.trigger === "schedule" ? (
              <ScheduleFields
                compact
                rule={trigger.schedule}
                onChange={(schedule) => setTrigger(trigger.key, { schedule })}
              />
            ) : (
              <div className="grid gap-2">
                <p className="text-content-secondary type-body">
                  {t("apiTriggerHelp")}
                </p>
                {trigger.taskId ? (
                  <Button asChild variant="ghost">
                    <Link to={`/automatic-tasks/${trigger.taskId}`}>
                      {t("triggerHistory")}
                    </Link>
                  </Button>
                ) : null}
              </div>
            )}
            <TriggerInputs
              workflowId={definitionId}
              schema={selectedSchema}
              trigger={trigger}
              fieldOrder={chosen.data?.revision?.manifest?.inputPropertyOrder}
              onChange={(values) => setTrigger(trigger.key, values)}
            />
          </section>
        ))}
        <div className="flex items-center justify-between gap-2">
          <Button
            type="button"
            variant="secondary"
            disabled={triggers.length >= 5 || save.isPending}
            onClick={() =>
              setTriggers((current) => [
                ...current,
                {
                  key: crypto.randomUUID(),
                  trigger: "schedule",
                  schedule: defaultRule(),
                  bindings: {},
                  valid: true,
                },
              ])
            }
          >
            <Plus aria-hidden />
            {t("addTrigger")}
          </Button>
          <span className="text-content-secondary type-caption">
            {triggers.length}/5
          </span>
        </div>
        <details className="grid gap-3">
          <summary className="text-content-secondary type-body cursor-pointer">
            {t("defaults")}
          </summary>
          <div className="mt-3 grid gap-4">
            <div className="grid gap-2">
              <FieldLabel>{t("versionPolicy")}</FieldLabel>
              <SegmentedControl
                ariaLabel={t("versionPolicy")}
                value={policy}
                onValueChange={(value) =>
                  setPolicy(value === "pinned" ? "pinned" : "current")
                }
                options={[
                  { value: "current", label: t("useCurrentVersion") },
                  { value: "pinned", label: t("usePinnedVersion") },
                ]}
              />
              {policy === "pinned" ? (
                <Select value={pinned} onValueChange={setPinned}>
                  <SelectTrigger aria-label={t("versions")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {versions.data?.flatMap((item) =>
                      item.id
                        ? [
                            <SelectItem key={item.id} value={item.id}>
                              {t("version", { version: item.ordinal })}
                            </SelectItem>,
                          ]
                        : [],
                    )}
                  </SelectContent>
                </Select>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2">
              <ExecutionConfigurationControls
                state={execution}
                onChange={setSelection}
              />
            </div>
            <label className="type-body flex items-center gap-2">
              <Checkbox
                checked={readonly}
                onCheckedChange={(value) => setReadonly(value === true)}
              />
              {t("readonly")}
            </label>
            <label className="type-body flex items-center justify-between gap-3">
              {t("triggerEnabled")}
              <Switch
                checked={enabled}
                onClick={() => setEnabled((value) => !value)}
              />
            </label>
          </div>
        </details>
        {save.error ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(save.error, t("error"))}
          </p>
        ) : null}
      </div>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={onCancel}>
          {t("cancel")}
        </Button>
        <Button
          type="submit"
          form={formId}
          disabled={
            save.isPending ||
            chosen.isPending ||
            chosen.isError ||
            !supportsInputBindings(selectedSchema) ||
            triggers.some(
              (trigger) =>
                !trigger.valid ||
                (trigger.trigger === "schedule" &&
                  Object.values(trigger.bindings).some(
                    (binding) => binding.source === "api",
                  )),
            )
          }
        >
          {t("saveTrigger")}
        </Button>
      </DialogFooter>
    </form>
  );
}

function TriggerInputs({
  workflowId,
  schema,
  trigger,
  fieldOrder,
  onChange,
}: {
  workflowId: string;
  schema: Record<string, unknown>;
  trigger: TriggerDraft;
  fieldOrder?: Record<string, string[]>;
  onChange: (values: Partial<TriggerDraft>) => void;
}) {
  const validity = useSchemaFormValidity();
  useEffect(() => {
    if (trigger.valid !== validity.valid) onChange({ valid: validity.valid });
  }, [trigger.valid, validity.valid, onChange]);
  return (
    <WorkflowInputFields
      workflowId={workflowId}
      schema={schema}
      bindings={trigger.bindings}
      allowAPI={trigger.trigger === "api"}
      fieldOrder={fieldOrder}
      onChange={(bindings) => onChange({ bindings, valid: validity.valid })}
      onValidityChange={validity.onValidityChange}
    />
  );
}
