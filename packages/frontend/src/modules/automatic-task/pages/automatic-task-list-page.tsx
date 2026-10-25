import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Clock3,
  Plus,
  Settings,
  ListTodo,
  CircleCheck,
  CalendarClock,
  Webhook,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { PaginatedTable } from "@/shared/ui/paginated-table";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import {
  PageAction,
  PageActionLabel,
  ResourcePageHeader,
  ResourcePageLibraryLabel,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { Skeleton } from "@/shared/ui/skeleton";
import { OverviewCard } from "@/shared/ui/overview-card";
import { Switch } from "@/shared/ui/switch";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  listGlobalAutomaticTasksOptions,
  refreshAutomaticTasks,
  setAutomaticTaskEnabledMutation,
} from "../api/task-queries";
import {
  AutomaticTaskAPISettingsDialog,
  TaskFormDialog,
} from "../components/task-dialogs";
import {
  formatTime,
  RunState,
  scheduleSummary,
  useTaskPaginationLabels,
} from "../components/task-presentation";
import {
  WorkExecutorAvatar,
  automaticTaskExecutorRef,
} from "@/modules/work-board";
import type { SavedTask } from "../model/types";

export function AutomaticTaskListPage() {
  const { t, i18n } = useTranslation("automaticTask");
  const [params, setParams] = useSearchParams();
  const wakerId = params.get("wakerId") || "";
  const executorKind =
    params.get("executorKind") === "team"
      ? "team"
      : params.get("executorKind") === "waker"
        ? "waker"
        : params.get("executorKind") === "workflow"
          ? "workflow"
          : "all";
  const navigate = useNavigate();
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const queryText = params.get("search") || "";
  const trigger: "api" | "schedule" | "all" =
    params.get("trigger") === "api"
      ? "api"
      : params.get("trigger") === "schedule"
        ? "schedule"
        : "all";
  const enabled =
    params.get("status") === "enabled"
      ? "enabled"
      : params.get("status") === "disabled"
        ? "disabled"
        : "all";
  const number = Number(params.get("page") || 1);
  const page = Number.isSafeInteger(number) && number > 0 ? number : 1;
  const [apiSettingsOpen, setAPISettingsOpen] = useState(false);
  const [creatingLocally, setCreating] = useState(false);
  const creating = creatingLocally || params.get("create") === "1";
  const labels = useTaskPaginationLabels();
  const query = useQuery({
    ...listGlobalAutomaticTasksOptions(
      withLocalErrorSurface({
        query: {
          query: queryText,
          pageNum: page,
          pageSize: 20,
          ...(trigger !== "all" ? { trigger } : {}),
          ...(executorKind !== "all" ? { executorKind } : {}),
          ...(enabled !== "all" ? { enabled: enabled === "enabled" } : {}),
        },
      }),
    ),
  });
  useQueryEvents({
    topic: "automatic-tasks",
    resource: { kind: "global" },
    queryKeys: [listGlobalAutomaticTasksOptions().queryKey],
    active,
  });
  const toggle = useMutation({
    ...withLocalErrorMutation(setAutomaticTaskEnabledMutation()),
    onSuccess: (_, variables) =>
      refreshAutomaticTasks(
        client,
        variables.path.wakerId,
        variables.path.taskId,
      ),
  });
  const change = (key: string, value: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value && value !== "all") next.set(key, value);
        else next.delete(key);
        if (key !== "page") next.delete("page");
        return next;
      },
      { replace: true },
    );
  const saved = (receipt: SavedTask) => {
    setCreating(false);
    change("create", "");
    if (receipt.taskId) navigate(`/automatic-tasks/${receipt.taskId}`);
  };
  const counts = query.data?.counts;
  const filtered = Boolean(
    queryText ||
    trigger !== "all" ||
    enabled !== "all" ||
    executorKind !== "all",
  );
  return (
    <ResourcePageSurface>
      <ResourcePageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <>
            <PageAction
              variant="plain"
              hitArea="comfortable"
              aria-label={t("apiSettings")}
              onClick={() => setAPISettingsOpen(true)}
            >
              <Settings aria-hidden="true" />
            </PageAction>
            <PageAction
              aria-label={t("create")}
              onClick={() => setCreating(true)}
            >
              <Plus />
              <PageActionLabel>{t("create")}</PageActionLabel>
            </PageAction>
          </>
        }
      />
      <section className="mb-6" aria-label={t("overview")}>
        <div className="grid grid-cols-1 gap-3 @[24rem]/resource-page:grid-cols-2 @[48rem]/resource-page:grid-cols-4">
          {(
            [
              {
                metric: "total",
                value: counts?.total,
                Icon: ListTodo,
                description: t("totalHint"),
              },
              {
                metric: "enabledCount",
                value: counts?.enabled,
                Icon: CircleCheck,
                description: t("enabledHint"),
              },
              {
                metric: "scheduledCount",
                value: counts?.scheduled,
                Icon: CalendarClock,
                description: t("scheduleDescription"),
              },
              {
                metric: "apiCount",
                value: counts?.api,
                Icon: Webhook,
                description: t("apiDescription"),
              },
            ] as const
          ).map(({ metric, value, Icon, description }) => {
            const selected =
              metric === "total"
                ? trigger === "all" && enabled === "all"
                : metric === "enabledCount"
                  ? enabled === "enabled"
                  : trigger ===
                    (metric === "scheduledCount" ? "schedule" : "api");
            return (
              <OverviewCard
                key={metric}
                label={t(metric)}
                description={description}
                icon={<Icon aria-hidden="true" strokeWidth={1.5} />}
                selected={selected}
                onClick={() => {
                  if (selected) return;
                  if (metric === "total") {
                    setParams(
                      (previous) => {
                        const next = new URLSearchParams(previous);
                        next.delete("trigger");
                        next.delete("status");
                        next.delete("page");
                        return next;
                      },
                      { replace: true },
                    );
                  } else if (metric === "enabledCount") {
                    change("status", "enabled");
                  } else {
                    change(
                      "trigger",
                      metric === "scheduledCount" ? "schedule" : "api",
                    );
                  }
                }}
                value={
                  query.isPending && value === undefined ? (
                    <Skeleton className="h-control-md w-12" />
                  ) : value === undefined ? (
                    <span className="text-destructive type-ui">
                      {t("loadFailed")}
                    </span>
                  ) : (
                    value
                  )
                }
              />
            );
          })}
        </div>
      </section>
      <ResourcePageToolbar
        filters={
          <>
            <ResourceFilterSelect
              ariaLabel={t("executor")}
              value={executorKind}
              onValueChange={(value) => change("executorKind", value)}
              options={[
                { value: "all", label: t("allExecutors") },
                { value: "waker", label: t("partnerExecutor") },
                { value: "team", label: t("teamExecutor") },
                { value: "workflow", label: t("workflowExecutor") },
              ]}
            />
            <ResourceFilterSelect
              ariaLabel={t("filterTrigger")}
              value={trigger}
              onValueChange={(value) => change("trigger", value)}
              options={[
                { value: "all", label: t("allTriggers") },
                { value: "schedule", label: t("schedule") },
                { value: "api", label: t("api") },
              ]}
            />
            <ResourceFilterSelect
              ariaLabel={t("filterEnabled")}
              value={enabled}
              onValueChange={(value) => change("status", value)}
              options={[
                { value: "all", label: t("allStates") },
                { value: "enabled", label: t("enabled") },
                { value: "disabled", label: t("disabled") },
              ]}
            />
            <ResourcePageSearchField
              aria-label={t("search")}
              placeholder={t("search")}
              value={queryText}
              onChange={(event) => change("search", event.target.value)}
            />
          </>
        }
      >
        <ResourcePageLibraryLabel
          installed={t("all")}
          count={t("results", { count: query.data?.totalSize || 0 })}
        />
      </ResourcePageToolbar>
      <div className="mt-6 min-w-0">
        {toggle.error ? (
          <p role="alert" className="text-destructive type-ui mb-3">
            {localApiErrorMessage(toggle.error, t("actionFailed"))}
          </p>
        ) : null}
        {query.isPending ? (
          <Skeleton className="h-64 w-full rounded-lg" />
        ) : query.isError ? (
          <EmptyState
            title={t("loadFailed")}
            description={localApiErrorMessage(query.error, t("actionFailed"))}
            actions={
              <Button variant="secondary" onClick={() => void query.refetch()}>
                {t("retry")}
              </Button>
            }
          />
        ) : !query.data?.resources?.length ? (
          <EmptyState
            icon={Clock3}
            title={t(filtered ? "noResults" : "empty")}
            description={t(filtered ? "noResultsHint" : "emptyHint")}
          />
        ) : (
          <PaginatedTable
            className="min-w-[900px]"
            pagination={{
              labels,
              page,
              pageSize: 20,
              totalItems: query.data.totalSize || 0,
              onPageChange: (value) => change("page", String(value)),
            }}
          >
            <TableHeader>
              <TableRow>
                {[
                  "name",
                  "executor",
                  "filterTrigger",
                  "nextRun",
                  "lastRun",
                  "runCount",
                  "result",
                  "status",
                ].map((key) => (
                  <TableHead key={key}>{t(key)}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.data.resources.map((task) => (
                <TableRow
                  key={task.id}
                  tabIndex={0}
                  className="hover:bg-muted/50 focus-visible:ring-ring cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset"
                  onClick={() => navigate(`/automatic-tasks/${task.id}`)}
                  onKeyDown={(event) => {
                    if (
                      event.target === event.currentTarget &&
                      ["Enter", " "].includes(event.key)
                    ) {
                      event.preventDefault();
                      navigate(`/automatic-tasks/${task.id}`);
                    }
                  }}
                >
                  <TableCell>
                    <span className="font-medium">{task.name}</span>
                    {task.configurationError ? (
                      <p className="text-destructive type-caption mt-1">
                        {t("configurationInvalid")}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    <WorkExecutorAvatar
                      {...automaticTaskExecutorRef(task)}
                      name={task.executorName || t("configurationInvalid")}
                    />
                  </TableCell>
                  <TableCell>
                    <p>
                      {task.trigger === "api"
                        ? t("api")
                        : scheduleSummary(task.schedule, t)}
                    </p>
                    {task.trigger === "schedule" ? (
                      <p className="text-content-secondary type-caption mt-1">
                        {task.schedule?.timezone}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {task.trigger === "api"
                      ? "—"
                      : formatTime(
                          task.nextRunAt,
                          task.schedule?.timezone,
                          i18n.language,
                        )}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatTime(
                      task.lastRun?.createdAt,
                      task.schedule?.timezone,
                      i18n.language,
                    )}
                  </TableCell>
                  <TableCell>
                    {t("countRuns", { count: task.runCount || 0 })}
                  </TableCell>
                  <TableCell>
                    <RunState state={task.lastRun?.state} />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Switch
                        aria-label={t("toggle", { name: task.name })}
                        checked={Boolean(task.enabled)}
                        disabled={toggle.isPending}
                        onClick={(event) => {
                          event.stopPropagation();
                          toggle.mutate({
                            path: {
                              wakerId: task.wakerId || "",
                              taskId: task.id || "",
                            },
                            body: {
                              enabled: !task.enabled,
                              revision: task.revision || 0,
                            },
                          });
                        }}
                      />
                      <span className="type-ui whitespace-nowrap">
                        {t(task.enabled ? "enabled" : "disabled")}
                      </span>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </PaginatedTable>
        )}
      </div>
      <AutomaticTaskAPISettingsDialog
        open={apiSettingsOpen}
        onOpenChange={setAPISettingsOpen}
      />
      <TaskFormDialog
        key={wakerId}
        open={creating}
        onOpenChange={(open) => {
          setCreating(open);
          if (!open) change("create", "");
        }}
        wakerId={wakerId}
        onSaved={saved}
      />
    </ResourcePageSurface>
  );
}
