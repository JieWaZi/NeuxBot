import { integrationServiceOptions } from "@/shared/api/integration-service";
import { FailureNoticeContent } from "@/modules/notification";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Play, Settings2, Trash2 } from "lucide-react";
import { lazy, Suspense, useState, useRef } from "react";
import { evictDeletedChatsFromCache } from "@/modules/chat";
import { useTranslation } from "react-i18next";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  getChannelOptions,
  getProjectOptions,
  getWorkflowOptions,
  listAiAccountsOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import {
  permissionLevelOf,
  permissionLevelTranslationKeys,
} from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { DetailSection } from "@/shared/ui/detail-section";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { PageHeader } from "@/shared/ui/page-header";
import { QueryState } from "@/shared/ui/query-state";
import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { PaginatedTable } from "@/shared/ui/paginated-table";
import { PageAction, ResourcePageSurface } from "@/shared/ui/resource-page";
import { Switch } from "@/shared/ui/switch";
import { Skeleton } from "@/shared/ui/skeleton";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  deleteAutomaticTaskMutation,
  getGlobalAutomaticTaskOptions,
  listAutomaticRunsOptions,
  refreshAutomaticTasks,
  setAutomaticTaskEnabledMutation,
  triggerAutomaticTaskMutation,
} from "../api/task-queries";
import {
  SchemaForm,
  useSchemaFormValidity,
} from "@/shared/json-schema/schema-form";
import { schemaRecord } from "../model/workflow-inputs";

import { WorkflowInputSummary } from "../components/workflow-input-summary";
import {
  AutomaticTaskAPISettingsDialog,
  TaskFormDialog,
} from "../components/task-dialogs";
import { CopyValue, apiExamples } from "../components/task-api-examples";
import {
  formatTime,
  RunState,
  scheduleSummary,
  useTaskPaginationLabels,
} from "../components/task-presentation";
import { SchedulePreview } from "../components/schedule-fields";
import { TeamExecutionSummary } from "../components/team-execution-summary";
import { automaticRunHref } from "../model/task-navigation";
import type { TaskView } from "../model/types";
import type { ListAutomaticRunsResponse } from "@/shared/api/generated/types.gen";

const WorkflowAPIContract = lazy(() =>
  import("../components/workflow-api-contract").then((module) => ({
    default: module.WorkflowAPIContract,
  })),
);

const executionGridClassName =
  "grid min-w-0 grid-cols-1 gap-x-8 gap-y-4 @[32rem]/task-execution:grid-cols-2 @[54rem]/task-execution:grid-cols-4";

export function AutomaticTaskDetailPage() {
  const { t, i18n } = useTranslation("automaticTask");
  const { taskId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const pageNumber = Number(params.get("historyPage") || 1);
  const page =
    Number.isSafeInteger(pageNumber) && pageNumber > 0 ? pageNumber : 1;
  const active = useRouteSceneActive();
  const navigate = useNavigate();
  const client = useQueryClient();
  const labels = useTaskPaginationLabels();
  const query = useQuery({
    ...getGlobalAutomaticTaskOptions(
      withLocalErrorSurface({ path: { taskId } }),
    ),
  });
  const wakerId = query.data?.wakerId || "";
  const path = { wakerId, taskId };
  const history = useQuery({
    ...listAutomaticRunsOptions(
      withLocalErrorSurface({ path, query: { pageNum: page, pageSize: 20 } }),
    ),
    enabled: Boolean(wakerId),
  });
  useQueryEvents({
    topic: "automatic-tasks",
    resource: { kind: "automatic_task", id: taskId },
    queryKeys: [
      getGlobalAutomaticTaskOptions({ path: { taskId } }).queryKey,
      listAutomaticRunsOptions({ path }).queryKey,
    ],
    active,
    enabled: Boolean(taskId),
  });
  const team = useQuery({
    ...getChannelOptions({ path: { channelId: query.data?.channelId || "" } }),
    enabled: Boolean(query.data?.channelId),
  });
  const runtimes = useQuery(listRuntimesOptions());
  const workflow = useQuery({
    ...getWorkflowOptions({ path: { id: query.data?.workflowId ?? "" } }),
    enabled: Boolean(query.data?.workflowId),
  });
  const accounts = useQuery(listAiAccountsOptions());
  const project = useQuery({
    ...getProjectOptions(
      withLocalErrorSurface({
        path: { wakerId, projectId: query.data?.projectId || "" },
      }),
    ),
    enabled: Boolean(query.data?.projectId),
  });
  const api = useQuery(integrationServiceOptions());
  const [apiSettingsOpen, setAPISettingsOpen] = useState(false);
  const [editing, setEditing] = useState<TaskView>();
  const [confirm, setConfirm] = useState<"delete">();
  const request = useRef<{ signature: string; id: string } | undefined>(
    undefined,
  );
  const [manualOpen, setManualOpen] = useState(false);
  const [manualArgs, setManualArgs] = useState<unknown>({});
  const manualValidity = useSchemaFormValidity();
  const remove = useMutation(
    withLocalErrorMutation(deleteAutomaticTaskMutation()),
  );
  const toggle = useMutation({
    ...withLocalErrorMutation(setAutomaticTaskEnabledMutation()),
    onSuccess: () => refreshAutomaticTasks(client, wakerId, taskId),
  });
  const run = useMutation(
    withLocalErrorMutation(triggerAutomaticTaskMutation()),
  );
  const task = query.data;
  const confirmAction = async () => {
    if (!task) return;
    try {
      if (confirm === "delete") {
        await remove.mutateAsync({ path });
        const deletedChatIds = new Set(
          [
            task.lastRun?.chatId,
            task.activeRun?.chatId,
            ...client
              .getQueriesData<ListAutomaticRunsResponse>({
                queryKey: listAutomaticRunsOptions({ path }).queryKey,
              })
              .flatMap(
                ([, data]) =>
                  data?.resources?.map((entry) => entry.chatId) || [],
              ),
          ].filter((id): id is string => Boolean(id)),
        );
        evictDeletedChatsFromCache(client, [...deletedChatIds]);
        await refreshAutomaticTasks(client, wakerId);
        client.removeQueries({
          queryKey: getGlobalAutomaticTaskOptions({ path: { taskId } })
            .queryKey,
        });
        client.removeQueries({
          queryKey: listAutomaticRunsOptions({ path }).queryKey,
        });
        navigate("/automatic-tasks");
        // 聚合删除同时改变伙伴统计及已打开的历史；刷新其余仍挂载的服务器事实。
        await client.invalidateQueries();
      }
      setConfirm(undefined);
    } catch {
      /* 确认弹窗保留错误和重试入口。 */
    }
  };
  const start = async (args?: Record<string, unknown>) => {
    const signature = JSON.stringify({
      taskId,
      revision: task?.revision,
      args,
    });
    if (request.current?.signature !== signature)
      request.current = { signature, id: crypto.randomUUID() };
    const clientRequestId = request.current.id;
    try {
      const receipt = await run.mutateAsync({
        path,
        body: { clientRequestId, args },
      });
      request.current = undefined;
      setManualOpen(false);
      await refreshAutomaticTasks(client, wakerId, taskId);
      if (receipt.chatId || receipt.workflowRunId)
        navigate(automaticRunHref(receipt));
    } catch {
      /* 同一次点击的网络重试复用原身份。 */
    }
  };
  const closeConfirm = () => {
    if (remove.isPending) return;
    setConfirm(undefined);
    remove.reset();
  };
  const backAction = {
    label: t("common:backToList"),
    onClick: () => navigate("/automatic-tasks"),
  };
  if (query.isPending)
    return (
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          title={t("title")}
          backAction={backAction}
        />
        <main className="mt-6">
          <QueryState pending onRetry={() => void query.refetch()} />
        </main>
      </ResourcePageSurface>
    );
  if (!task)
    return (
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          title={t("title")}
          backAction={backAction}
        />
        <main className="mt-6">
          <QueryState
            error={query.error}
            emptyLabel={t("loadFailed")}
            onRetry={() => void query.refetch()}
          />
        </main>
      </ResourcePageSurface>
    );
  const runtime = runtimes.data?.resources?.find(
    (item) => item.id === task.runtimeId,
  );
  const model = runtime?.models?.find((item) => item.id === task.model);
  const disabledReason = !task.enabled
    ? t("disabledHint")
    : task.activeRunId
      ? t("busyHint")
      : task.configurationError
        ? t("configurationInvalid")
        : undefined;
  const examples =
    api.data?.publicBaseUrl && (!task.workflowId || task.workflowInputContract)
      ? apiExamples(
          taskId,
          api.data.publicBaseUrl,
          Boolean(task.workflowId),
          task.workflowInputContract,
        )
      : undefined;
  const timezone = task.schedule?.timezone;
  const isPartnerTask = !task.workflowId && !task.channelId;
  const identityFieldClassName = isPartnerTask
    ? undefined
    : "@[54rem]/task-execution:col-span-2";
  const errorLabel = (code?: string) =>
    code === "AutomaticTaskBusy"
      ? t("busyHint")
      : code === "AutomaticTaskAdmissionRevoked"
        ? t("state.interrupted")
        : code
          ? t("runFailedHint")
          : "—";
  return (
    <ResourcePageSurface>
      <PageHeader
        layout="detail"
        backAction={backAction}
        title={task.name}
        description={[
          task.executorName,
          t(task.trigger === "api" ? "api" : "schedule"),
        ]
          .filter(Boolean)
          .join(" · ")}
        actions={
          <>
            <div className="flex items-center gap-2">
              <Switch
                hitArea="comfortable"
                aria-label={t("toggle", { name: task.name })}
                checked={Boolean(task.enabled)}
                disabled={toggle.isPending}
                onClick={() =>
                  toggle.mutate({
                    path,
                    body: {
                      revision: task.revision || 0,
                      enabled: !task.enabled,
                    },
                  })
                }
              />
              <span className="text-content-secondary type-body">
                {t(task.enabled ? "enabled" : "disabled")}
              </span>
            </div>
            <PageAction
              intent="edit"
              label={t("edit")}
              onClick={() => setEditing(task)}
            >
              <Pencil />
            </PageAction>
            <PageAction
              intent="delete"
              label={t("remove")}
              onClick={() => setConfirm("delete")}
            >
              <Trash2 />
            </PageAction>
            <span title={disabledReason}>
              <PageAction
                intent="primary"
                label={t("run")}
                disabled={Boolean(disabledReason) || run.isPending}
                onClick={() => {
                  if (
                    task.workflowInputContract?.fields?.some(
                      (field) => field.source === "api",
                    )
                  ) {
                    setManualArgs({});
                    setManualOpen(true);
                  } else void start();
                }}
              >
                <Play />
              </PageAction>
            </span>
          </>
        }
      />
      <main className="mt-6 grid min-w-0 gap-8">
        {query.isError ? (
          <QueryState
            error={query.error}
            onRetry={() => void query.refetch()}
          />
        ) : null}
        {task.configurationError ? (
          <p
            role="alert"
            className="bg-destructive/8 text-destructive type-body rounded-md p-3"
          >
            {t("configurationInvalid")}
          </p>
        ) : null}
        {toggle.error || run.error ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(toggle.error || run.error, t("actionFailed"))}
          </p>
        ) : null}
        <DetailSection title={t("overview")} surface="wash">
          <div className="grid min-w-0 gap-4">
            <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-x-8 gap-y-4">
              {[
                [
                  t("lastRun"),
                  formatTime(task.lastRun?.createdAt, timezone, i18n.language),
                ],
                [t("runCount"), t("countRuns", { count: task.runCount || 0 })],
                [
                  t("result"),
                  <RunState state={task.lastRun?.state} key="state" />,
                ],
              ].map(([label, value], index) => (
                <ReadOnlyField key={index} label={label}>
                  {value}
                </ReadOnlyField>
              ))}
            </dl>
            {task.trigger === "schedule" ? (
              <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,12rem),1fr))] gap-x-8 gap-y-4">
                <ReadOnlyField label={t("nextRun")}>
                  {formatTime(task.nextRunAt, timezone, i18n.language)}
                  {task.schedule?.kind === "once" && !task.nextRunAt
                    ? ` · ${t(task.missedCount ? "onceMissed" : "onceDone")}`
                    : ""}
                </ReadOnlyField>
                <ReadOnlyField label={t("lastMissed")}>
                  {formatTime(task.lastMissedAt, timezone, i18n.language)}
                </ReadOnlyField>
                <ReadOnlyField label={t("missedCount")}>
                  {task.missedCount || 0}
                </ReadOnlyField>
              </dl>
            ) : null}
            {task.activeRun?.chatId || task.activeRun?.workflowRunId ? (
              <div className="flex flex-wrap items-center gap-3">
                <RunState state={task.activeRun.state} />
                <Button asChild variant="secondary" size="sm">
                  <Link to={automaticRunHref(task.activeRun)}>
                    {t(
                      task.activeRun.workflowRunId
                        ? "openWorkflowRun"
                        : "openChat",
                    )}
                  </Link>
                </Button>
              </div>
            ) : null}
          </div>
        </DetailSection>
        <DetailSection
          title={t("triggerAndExecution")}
          actions={
            task.trigger === "api" ? (
              <>
                {!api.data?.available ? (
                  <PageAction
                    intent="edit"
                    label={t("apiSettings")}
                    onClick={() => setAPISettingsOpen(true)}
                  >
                    <Settings2 />
                  </PageAction>
                ) : null}
              </>
            ) : undefined
          }
        >
          <div className="grid min-w-0 gap-6">
            <Card asChild variant="section">
              <section aria-label={t("triggerConfig")}>
                {task.trigger === "schedule" ? (
                  <div className="grid gap-4">
                    <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,16rem),1fr))] gap-x-8 gap-y-4">
                      <ReadOnlyField label={t("rule")}>
                        {scheduleSummary(task.schedule, t)}
                      </ReadOnlyField>
                      <ReadOnlyField label={t("timezone")}>
                        {timezone}
                      </ReadOnlyField>
                    </dl>
                    <SchedulePreview
                      surface="plain"
                      wakerId={wakerId}
                      rule={task.schedule || {}}
                    />
                    <p className="text-content-secondary type-body">
                      {t("scheduleHint")}
                    </p>
                  </div>
                ) : (
                  <div className="grid gap-4">
                    <p
                      className={
                        api.data?.available
                          ? "text-success type-body"
                          : "text-warning type-body"
                      }
                    >
                      {t(
                        api.data?.available ? "apiAvailable" : "apiUnavailable",
                      )}
                    </p>
                    <p className="type-body text-content-secondary">
                      {t("apiIntegrationHint")}
                    </p>
                    <CopyValue label={t("taskID")} value={taskId} />
                    {task.workflowInputContract ? (
                      <Suspense
                        fallback={
                          <div role="status" aria-busy="true">
                            <Skeleton className="h-32 w-full" />
                          </div>
                        }
                      >
                        <WorkflowAPIContract
                          contract={task.workflowInputContract}
                        />
                      </Suspense>
                    ) : null}
                    {examples ? (
                      <>
                        <CopyValue
                          label={t("endpoint")}
                          value={examples.endpoint}
                        />
                        <CopyValue
                          label={t("triggerExample")}
                          value={examples.trigger}
                        />
                        <CopyValue
                          label={t("statusExample")}
                          value={examples.status}
                        />
                        {task.workflowId ? (
                          <>
                            <CopyValue
                              label={t("fileUploadExample")}
                              value={examples.upload}
                            />
                            <CopyValue
                              label={t("resultExample")}
                              value={examples.result}
                            />
                            <CopyValue
                              label={t("fileDownloadExample")}
                              value={examples.download}
                            />
                          </>
                        ) : null}
                      </>
                    ) : task.workflowId && !task.workflowInputContract ? (
                      <p role="alert" className="text-destructive type-body">
                        {t("configurationInvalid")}
                      </p>
                    ) : api.isError ? (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => void api.refetch()}
                      >
                        {t("retry")}
                      </Button>
                    ) : (
                      <ContentLoadingState
                        label={t("common:detailState.loading")}
                      />
                    )}
                  </div>
                )}
              </section>
            </Card>
            <Card asChild variant="section">
              <section
                aria-label={t("executionConfig")}
                className="@container/task-execution"
              >
                <div className="grid min-w-0 gap-5">
                  <dl className={executionGridClassName}>
                    <ReadOnlyField
                      label={t("executor")}
                      className={identityFieldClassName}
                    >
                      {task.executorName || t("configurationInvalid")} ·{" "}
                      {t(
                        task.workflowId
                          ? "workflowExecutor"
                          : task.channelId
                            ? "teamExecutor"
                            : "partnerExecutor",
                      )}
                    </ReadOnlyField>
                    {task.workflowId ? (
                      <>
                        <ReadOnlyField
                          label={i18n.t("workflow:definition")}
                          className={identityFieldClassName}
                        >
                          <Link
                            className="text-link hover:underline"
                            to={`/workflows/${task.workflowId}`}
                          >
                            {workflow.data?.definition?.title ??
                              task.workflowId}
                          </Link>
                        </ReadOnlyField>
                        <ReadOnlyField
                          label={i18n.t("workflow:versionPolicy")}
                          className={identityFieldClassName}
                        >
                          {i18n.t(
                            task.workflowRevisionPolicy === "pinned"
                              ? "workflow:usePinnedVersion"
                              : "workflow:useCurrentVersion",
                          )}{" "}
                          {task.workflowRevisionId}
                        </ReadOnlyField>
                      </>
                    ) : null}
                    {isPartnerTask ? (
                      <ReadOnlyField label={t("permission")}>
                        {i18n.t(
                          `common:${permissionLevelTranslationKeys[permissionLevelOf(task.permissionLevel) || "default"].label}`,
                        )}
                      </ReadOnlyField>
                    ) : null}
                    <ReadOnlyField
                      label={t("workspace")}
                      className={
                        isPartnerTask
                          ? "col-span-full @[54rem]/task-execution:col-span-2"
                          : identityFieldClassName
                      }
                    >
                      {task.workingDirectory ||
                        workflow.data?.definition?.scopeBinding
                          ?.workingDirectory ||
                        project.data?.name ||
                        (task.workflowId
                          ? i18n.t("workflow:noScope")
                          : task.projectId
                            ? task.projectId
                            : t("temporaryWorkspace"))}
                    </ReadOnlyField>
                  </dl>
                  {task.channelId ? (
                    <div className="min-w-0">
                      {team.data ? (
                        <TeamExecutionSummary
                          team={team.data}
                          overrides={task.memberConfigs}
                        />
                      ) : team.error ? (
                        <p role="alert" className="text-destructive type-body">
                          {localApiErrorMessage(
                            team.error,
                            t("configurationInvalid"),
                          )}
                        </p>
                      ) : (
                        <ContentLoadingState
                          className="min-h-36"
                          label={t("common:detailState.loading")}
                        />
                      )}
                    </div>
                  ) : isPartnerTask ? (
                    <dl className={executionGridClassName}>
                      <ReadOnlyField label={t("runtime")}>
                        <span className="inline-flex min-w-0 items-start gap-2">
                          <RuntimeCliIcon
                            runtimeId={runtime?.baseRuntimeId || task.runtimeId}
                          />
                          <span className="min-w-0">
                            {runtimeDisplayName(
                              runtime || { id: task.runtimeId },
                            )}
                          </span>
                        </span>
                      </ReadOnlyField>
                      <ReadOnlyField label={t("account")}>
                        {task.accountId === "system" || !task.accountId
                          ? t("nativeAccount")
                          : accounts.data?.resources?.find(
                              (item) => item.id === task.accountId,
                            )?.name || t("configurationInvalid")}
                      </ReadOnlyField>
                      <ReadOnlyField label={t("model")}>
                        {model?.name ||
                          task.model ||
                          i18n.t("common:executionSelection.inheritModel")}
                      </ReadOnlyField>
                      <ReadOnlyField label={t("reasoning")}>
                        {model?.reasoningOptions?.find(
                          (option) => option.id === task.reasoning,
                        )?.name ||
                          task.reasoning ||
                          t("defaultReasoning")}
                      </ReadOnlyField>
                    </dl>
                  ) : null}
                  <dl>
                    {task.workflowId ? (
                      <ReadOnlyField label={t("workflowParameters")}>
                        <WorkflowInputSummary task={task} />
                      </ReadOnlyField>
                    ) : (
                      <ReadOnlyField label={t("instruction")}>
                        <div className="bg-surface-scene min-h-16 rounded-md px-4 py-3">
                          {task.instruction}
                        </div>
                      </ReadOnlyField>
                    )}
                  </dl>
                </div>
              </section>
            </Card>
          </div>
        </DetailSection>
        <DetailSection title={t("history")}>
          {history.isPending ? (
            <ContentLoadingState
              className="min-h-36"
              label={t("common:detailState.loading")}
            />
          ) : history.isError ? (
            <EmptyState
              title={t("loadFailed")}
              description={localApiErrorMessage(
                history.error,
                t("actionFailed"),
              )}
              actions={
                <Button
                  variant="secondary"
                  onClick={() => void history.refetch()}
                >
                  {t("retry")}
                </Button>
              }
            />
          ) : !history.data?.resources?.length ? (
            <EmptyState
              density="compact"
              title={t("noHistory")}
              description={t("noHistoryHint")}
            />
          ) : (
            <PaginatedTable
              className="min-w-[700px]"
              pagination={{
                labels,
                page,
                pageSize: 20,
                totalItems: history.data.totalSize || 0,
                onPageChange: (value) =>
                  setParams(
                    (previous) => {
                      const next = new URLSearchParams(previous);
                      next.set("historyPage", String(value));
                      return next;
                    },
                    { replace: true },
                  ),
              }}
            >
              <TableHeader>
                <TableRow>
                  {[
                    "triggeredAt",
                    "source",
                    "runResult",
                    "reason",
                    task.workflowId ? "openWorkflowRun" : "openChat",
                  ].map((key) => (
                    <TableHead key={key}>{t(key)}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {history.data.resources.map((entry) => (
                  <TableRow key={entry.id}>
                    <TableCell className="whitespace-nowrap">
                      {formatTime(entry.createdAt, timezone, i18n.language)}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      {t(
                        entry.source === "schedule"
                          ? "schedule"
                          : entry.source === "api"
                            ? "api"
                            : "manual",
                      )}
                    </TableCell>
                    <TableCell>
                      <RunState state={entry.state} />
                    </TableCell>
                    <TableCell>
                      {entry.state === "failed" ? (
                        <FailureNoticeContent runId={entry.id ?? ""}>
                          {errorLabel(entry.errorCode)}
                        </FailureNoticeContent>
                      ) : (
                        errorLabel(entry.errorCode)
                      )}
                    </TableCell>
                    <TableCell>
                      {(entry.workflowRunId &&
                        entry.configuration?.workflowId &&
                        entry.errorCode !== "WorkflowNotFound") ||
                      (entry.chatId && entry.errorCode !== "ChatNotFound") ? (
                        <Button asChild variant="ghost" size="sm">
                          <Link to={automaticRunHref(entry)}>
                            {t(
                              entry.workflowRunId
                                ? "openWorkflowRun"
                                : "openChat",
                            )}
                          </Link>
                        </Button>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </PaginatedTable>
          )}
        </DetailSection>
      </main>
      <AutomaticTaskAPISettingsDialog
        open={apiSettingsOpen}
        onOpenChange={setAPISettingsOpen}
      />
      <TaskFormDialog
        task={editing}
        open={Boolean(editing)}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined);
        }}
        wakerId={wakerId}
        onSaved={() => setEditing(undefined)}
      />
      <Dialog
        open={manualOpen}
        onOpenChange={(open) => {
          if (!run.isPending) setManualOpen(open);
        }}
      >
        <DialogContent
          title={t("manualInputTitle")}
          description={t("manualInputHelp")}
          size="lg"
        >
          <div className="grid min-h-0 gap-4 overflow-y-auto px-6 pb-5">
            <WorkflowInputSummary task={task} configuredOnly />
            <SchemaForm
              fileWorkflowId={task.workflowId}
              schema={task.workflowInputContract?.schema ?? {}}
              value={manualArgs}
              onChange={setManualArgs}
              onValidityChange={manualValidity.onValidityChange}
            />
            {run.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(run.error, t("actionFailed"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={run.isPending}
              onClick={() => setManualOpen(false)}
            >
              {t("cancel")}
            </Button>
            <Button
              disabled={run.isPending || !manualValidity.valid}
              onClick={() => void start(schemaRecord(manualArgs))}
            >
              {t("run")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={Boolean(confirm)}
        onOpenChange={(open) => {
          if (!open) closeConfirm();
        }}
      >
        <DialogContent
          size="sm"
          title={t("deleteTitle")}
          description={t("deleteWarning", {
            name: task.name,
            runCount: task.deletionRunCount ?? 0,
            chatCount: task.deletionChatCount ?? 0,
          })}
        >
          <div className="px-6 pb-4">
            {remove.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(remove.error, t("actionFailed"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={remove.isPending}
              onClick={closeConfirm}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="danger"
              disabled={remove.isPending}
              onClick={() => void confirmAction()}
            >
              {t("remove")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ResourcePageSurface>
  );
}
