import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  ListChecks,
  Check,
  GitCommitHorizontal,
  GitCompare,
  PanelRight,
  PanelLeft,
  FileText,
  WandSparkles,
  RotateCcw,
  Trash2,
  Workflow,
} from "lucide-react";
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { subscribeWorkflow, type StreamState } from "@/shared/api/events";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { PageAction } from "@/shared/ui/resource-page";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { NavigationItem } from "@/shared/ui/navigation-panel";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Badge } from "@/shared/ui/badge";
import { Avatar } from "@/shared/ui/avatar";
import { TypedPhrases } from "@/shared/ui/typed-phrases";
import { runOriginKey, runTime } from "../model/run-presentation";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { useWorkflowPaneWidth } from "../model/use-workflow-pane-width";
import type {
  ExecutionDefaults,
  WorkflowRevision,
  WorkflowRun,
} from "../model/types";
import { WorkflowDeleteDialog } from "../components/workflow-delete-dialog";
import { RunDeleteDialog } from "../components/run-delete-dialog";
import { BasicInformationDialog } from "../components/basic-information-dialog";
import {
  useOptionalAppSceneWorkspace,
  useRouteSceneActive,
} from "@/shared/workspace/app-scene-workspace";
import {
  getWorkflowOptions,
  getWorkflowQueryKey,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsQueryKey,
  listWorkflowRunsQueryKey,
  listWorkflowsQueryKey,
  listWorkflowRevisionsInfiniteOptions,
  listWorkflowRunsInfiniteOptions,
  restoreWorkflowRevisionMutation,
  deleteWorkflowRevisionMutation,
  useWorkflowDefinitionEvents,
} from "../api/workflow-queries";
import { WorkflowChatPane } from "../components/workflow-chat-pane";
import { WorkflowChatHistory } from "../components/workflow-chat-history";
import { NodeInspector } from "../components/node-inspector";
import { PhaseAdjustmentComposer } from "../components/phase-adjustment-composer";
import { RunForm } from "../components/run-form";
import { WorkflowInputNode } from "../components/workflow-input-node";
import { ResultOpenScope } from "../presentation/node-result";
import { WorkflowCanvas } from "../components/workflow-canvas";
import { WorkflowCanvasPlaceholder } from "../components/workflow-canvas-placeholder";
import { WorkflowDetailDock } from "../components/workflow-detail-dock";
import {
  WorkflowLoadingState,
  WorkflowQueryError,
} from "../components/query-feedback";

const VersionDiffDialog = lazy(() =>
  import("../components/version-diff-dialog").then((module) => ({
    default: module.VersionDiffDialog,
  })),
);

const WorkflowRunView = lazy(() =>
  import("../components/run-view").then((module) => ({
    default: module.WorkflowRunView,
  })),
);

export function WorkflowDetailPage() {
  const { t, i18n } = useTranslation("workflow");
  const welcomePhrases = useMemo(
    () => [
      t("welcome"),
      t("welcomeHelp"),
      t("welcomePhrases.idea"),
      t("welcomePhrases.steps"),
    ],
    [t],
  );
  const { workflowId: id = "", runId: routeRunId } = useParams();
  const isNew = id === "new";
  const [params, setParams] = useSearchParams();
  const client = useQueryClient();
  const navigate = useNavigate();
  const active = useRouteSceneActive();
  const historyPaneId = useId();
  const runsPaneId = useId();
  const [runActionsContainer, setRunActionsContainer] =
    useState<HTMLDivElement | null>(null);
  const [runInputContainer, setRunInputContainer] =
    useState<HTMLDivElement | null>(null);
  const workspace = useOptionalAppSceneWorkspace();
  const revisionId = params.get("revision") || undefined;
  const selected = params.get("node") || undefined;
  const change = (key: string, value: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        if (value) next.set(key, value);
        else next.delete(key);
        return next;
      },
      { replace: true },
    );
  const detail = useQuery({
    ...getWorkflowOptions({ path: { id }, query: { revisionId } }),
    enabled: !isNew,
    placeholderData: (previous) =>
      previous?.definition?.id === id ? previous : undefined,
  });
  const versions = useInfiniteQuery({
    ...listWorkflowRevisionsInfiniteOptions({
      path: { id },
      query: { limit: 50 },
    }),
    initialPageParam: { path: { id }, query: { limit: 50 } },
    getNextPageParam: (page) =>
      page.nextBefore
        ? { path: { id }, query: { before: page.nextBefore, limit: 50 } }
        : undefined,
    enabled: !isNew,
  });
  const versionItems =
    versions.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const runs = useInfiniteQuery({
    ...listWorkflowRunsInfiniteOptions({ path: { id }, query: { limit: 50 } }),
    initialPageParam: { path: { id }, query: { limit: 50 } },
    getNextPageParam: (page) =>
      page.nextBefore && page.nextBeforeId
        ? {
            path: { id },
            query: {
              before: page.nextBefore,
              beforeId: page.nextBeforeId,
              limit: 50,
            },
          }
        : undefined,
    enabled: !isNew,
  });
  const runItems = runs.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const hasRuns = Boolean(routeRunId) || runItems.length > 0;
  const tab =
    routeRunId || (params.get("tab") === "runs" && hasRuns) ? "runs" : "canvas";
  const selectedRunId = routeRunId || params.get("run") || runItems[0]?.id;
  const [, setStream] = useState<StreamState>("connecting");
  const [running, setRunning] = useState(false);
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [runsOpen, setRunsOpen] = useState(true);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [deleteRevision, setDeleteRevision] = useState<WorkflowRevision>();
  const [deleteRun, setDeleteRun] = useState<{
    run: WorkflowRun;
    number: number;
  }>();
  const removeRevision = useMutation(
    withLocalErrorMutation(deleteWorkflowRevisionMutation()),
  );
  const [basicOpen, setBasicOpen] = useState(false);
  const [creationDraft, setCreationDraft] = useState("");
  const optimizeRun = params.get("optimizeRun");
  const requestedChat = params.get("chat");
  const requestedInteraction = params.get("request");
  const [chatExpansion, setChatExpansion] = useState({
    expanded: Boolean(optimizeRun || requestedChat),
    optimizeRun,
    requestedChat,
    requestedInteraction,
  });
  if (
    chatExpansion.optimizeRun !== optimizeRun ||
    chatExpansion.requestedChat !== requestedChat ||
    chatExpansion.requestedInteraction !== requestedInteraction
  )
    setChatExpansion({
      expanded: optimizeRun || requestedChat ? true : chatExpansion.expanded,
      optimizeRun,
      requestedChat,
      requestedInteraction,
    });
  const chatExpanded = chatExpansion.expanded;
  const setChatExpanded = (value: boolean | ((expanded: boolean) => boolean)) =>
    setChatExpansion((previous) => ({
      ...previous,
      expanded: typeof value === "function" ? value(previous.expanded) : value,
    }));
  const [creationActivity, setCreationActivity] = useState({
    submitted: false,
    running: false,
  });
  const onCreationRunningChange = useCallback((running: boolean) => {
    setCreationActivity((previous) =>
      previous.running === running
        ? previous
        : { submitted: previous.submitted || running, running },
    );
  }, []);
  const emptyChat = isNew && !creationActivity.submitted;
  const {
    available: paneWidth,
    rightWidth,
    hostRef: canvasHostRef,
  } = useWorkflowPaneWidth();
  const restore = useMutation(
    withLocalErrorMutation(restoreWorkflowRevisionMutation()),
  );
  const [restoreRequest, setRestoreRequest] = useState<{
    content: string;
    id: string;
  }>();
  useWorkflowDefinitionEvents(id, active, !isNew);
  useEffect(() => {
    if (!active || !id || isNew) return;
    const release = subscribeWorkflow(id, {
      onEvent: (event) => {
        if (event.type !== "workflow.definition.deleted") return;
        for (const queryKey of [
          getWorkflowQueryKey({ path: { id } }),
          getWorkflowSourceQueryKey({ path: { id } }),
          listWorkflowRevisionsQueryKey({ path: { id } }),
          listWorkflowRunsQueryKey({ path: { id } }),
        ]) {
          void client.cancelQueries({ queryKey });
        }
        navigate("/resources?category=workflows", { replace: true });
        void client.invalidateQueries({ queryKey: listWorkflowsQueryKey() });
      },
      onState: setStream,
    });
    return () => {
      release();
    };
  }, [active, client, id, isNew, navigate]);
  const revision = detail.data?.revision;
  const [sessionSelection, setSelectedSession] = useState<string | null>();
  const setSessionSelection = (
    sessionId: string | null,
    clearParam?: string,
  ) => {
    setSelectedSession(sessionId);
    // 会话切换与清理通知定位共用一次导航，避免同一事件内的参数更新相互覆盖。
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("chat");
        next.delete("request");
        if (clearParam) next.delete(clearParam);
        return next;
      },
      { replace: true },
    );
  };
  const [closedChatIds, setClosedChatIds] = useState<string[]>([]);
  const [conversationGeneration, setChatGeneration] = useState(0);
  const session = requestedChat
    ? detail.data?.creationSessions?.find(
        (item) => item.chatId === requestedChat,
      )
    : sessionSelection === null
      ? undefined
      : (detail.data?.creationSessions?.find(
          (item) => item.id === sessionSelection,
        ) ?? detail.data?.creationSessions?.[0]);
  const configurationScope = `${conversationGeneration}:${session?.id ?? "new"}`;
  const [chatConfiguration, setChatConfiguration] = useState<{
    scope: string;
    defaults?: ExecutionDefaults;
  }>();
  const onConfigurationChange = useCallback(
    (defaults: ExecutionDefaults | undefined) =>
      setChatConfiguration({ scope: configurationScope, defaults }),
    [configurationScope],
  );
  const phaseDefaults =
    chatConfiguration?.scope === configurationScope
      ? chatConfiguration.defaults
      : session?.defaults;
  const title =
    detail.data?.definition?.title || (isNew ? t("newTitle") : undefined);
  const routeTabLabel = workspace?.snapshot.tabs.find(
    (item) => item.id === workspace.routeSceneId,
  )?.label;
  useEffect(() => {
    if (workspace && title && routeTabLabel !== title) {
      workspace.dispatch({
        type: "rename",
        tabId: workspace.routeSceneId,
        label: title,
      });
    }
  }, [workspace, routeTabLabel, title]);
  const historyVisible = historyOpen && tab === "canvas";
  const runsVisible = tab === "runs" && runsOpen;
  const canvasWidth = Math.max(
    0,
    paneWidth - (historyVisible || runsVisible ? rightWidth : 0),
  );
  const confirmRestore = async () => {
    const content = JSON.stringify([
      revision?.id,
      detail.data?.definition?.currentRevisionId,
    ]);
    const requestId =
      restoreRequest?.content === content
        ? restoreRequest.id
        : crypto.randomUUID();
    setRestoreRequest({ content, id: requestId });
    try {
      await restore.mutateAsync({
        path: { id },
        body: {
          requestId,
          expectedRevisionId: detail.data?.definition?.currentRevisionId ?? "",
          targetRevisionId: revision?.id ?? "",
        },
      });
      setRestoreOpen(false);
      change("revision", "");
    } catch {
      /* Mutation owns error. */
    }
  };
  const newChat = () => {
    setChatExpanded(true);
    setSessionSelection(null, "revision");
    setCreationDraft("");
    setChatGeneration((value) => value + 1);
  };
  const canvas = (canvasActive: boolean) => (
    <div
      ref={canvasHostRef}
      className="bg-surface-scene @container/workflow-scene relative flex h-full min-h-0 min-w-0"
    >
      <main className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {
          <div className="surface-pattern-dots flex shrink-0 flex-wrap items-center gap-2 p-3">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t(chatExpanded ? "collapseChat" : "expandChat")}
                  aria-expanded={chatExpanded}
                  onClick={() => setChatExpanded((expanded) => !expanded)}
                >
                  <PanelLeft aria-hidden="true" strokeWidth={1.5} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {t(chatExpanded ? "collapseChat" : "expandChat")}
              </TooltipContent>
            </Tooltip>
            <SegmentedControl
              size="sm"
              ariaLabel={t("title")}
              value={tab}
              onValueChange={(value) => {
                if (value === "runs") {
                  setRunsOpen(true);
                  setHistoryOpen(false);
                }
                if (routeRunId && value === "canvas") {
                  navigate(`/workflows/${id}`);
                } else {
                  change("tab", value);
                }
              }}
              options={[
                {
                  value: "canvas",
                  label: t("definition"),
                  icon: <Workflow />,
                },
                {
                  value: "runs",
                  label: t("executionProcess"),
                  icon: <ListChecks />,
                  disabled: !hasRuns,
                },
              ]}
            />

            <div
              ref={setRunActionsContainer}
              className="ml-auto flex shrink-0 items-center gap-2"
            >
              {tab === "runs" && !runsOpen ? (
                <Button
                  size="icon-sm"
                  variant="surface"
                  aria-label={t("expandRuns")}
                  aria-controls={runsPaneId}
                  aria-expanded={false}
                  onClick={() => setRunsOpen(true)}
                >
                  <PanelRight aria-hidden="true" strokeWidth={1.5} />
                </Button>
              ) : null}
            </div>
          </div>
        }
        {detail.isError ? (
          <WorkflowQueryError
            error={detail.error}
            className="px-5 py-3"
            retrying={detail.isFetching}
            onRetry={() => void detail.refetch()}
          />
        ) : null}
        {detail.isError && !detail.data ? null : !isNew && detail.isPending ? (
          <WorkflowLoadingState className="surface-pattern-dots min-h-0 flex-1" />
        ) : tab === "runs" ? (
          selectedRunId ? (
            <Suspense
              fallback={
                <WorkflowLoadingState className="surface-pattern-dots min-h-0 flex-1" />
              }
            >
              <WorkflowRunView
                id={id}
                runId={selectedRunId}
                active={canvasActive}
                actionsContainer={runActionsContainer}
              />
            </Suspense>
          ) : runs.isPending ? (
            <WorkflowLoadingState className="surface-pattern-dots min-h-0 flex-1 p-6" />
          ) : (
            <EmptyState title={t("noRuns")} />
          )
        ) : revision?.manifest?.nodes?.length ? (
          <div
            data-motion="view"
            data-state="open"
            className="flex min-h-0 flex-1 flex-col"
          >
            <WorkflowCanvas
              hasDock
              manifest={revision.manifest}
              inputNode={
                <WorkflowInputNode
                  manifest={revision.manifest}
                  open={active && canvasActive && running}
                />
              }
              inputDetails={{
                title: t("inputNodeTitle"),
                open: active && canvasActive && running,
                onOpenChange: setRunning,
                content: (
                  <div ref={setRunInputContainer} className="contents" />
                ),
              }}
              selected={active && canvasActive ? selected : undefined}
              listMode={params.get("view") === "list"}
              phaseEditor={
                active && canvasActive && !revisionId
                  ? (phase, close) => (
                      <PhaseAdjustmentComposer
                        key={phase.id}
                        id={id}
                        revisionId={revision.id ?? ""}
                        phase={phase}
                        ordinal={
                          (revision.manifest?.meta?.phases?.findIndex(
                            (item) => item.id === phase.id,
                          ) ?? 0) + 1
                        }
                        session={session}
                        defaults={phaseDefaults}
                        onAccepted={(sessionId) => {
                          setChatExpanded(true);
                          setSessionSelection(sessionId);
                          setCreationDraft("");
                          close();
                        }}
                      />
                    )
                  : undefined
              }
              nodeContent={(node) => (
                <NodeInspector
                  key={node.id}
                  id={id}
                  revisionId={revision.id ?? ""}
                  node={node}
                  manifest={revision.manifest}
                  readOnly={Boolean(revisionId)}
                />
              )}
              onDismiss={() => change("node", "")}
              onSelect={(item) => change("node", item.id ?? "")}
            />
          </div>
        ) : (
          <WorkflowCanvasPlaceholder
            state={creationActivity.running ? "generating" : "empty"}
            active={active && canvasActive}
          />
        )}
        {revision && tab === "canvas" ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-4 z-30 flex justify-center px-2">
            <WorkflowDetailDock
              availableWidth={canvasWidth}
              showingHistory={historyVisible}
              historyPaneId={historyPaneId}
              readOnly={Boolean(revisionId)}
              onEdit={() => setBasicOpen(true)}
              onRun={() => setRunning(true)}
              onVersions={() => setHistoryOpen((value) => !value)}
              onDelete={() => setDeleteOpen(true)}
            />
          </div>
        ) : null}
      </main>
      <aside
        id={runsPaneId}
        aria-label={t("runs")}
        aria-hidden={!runsVisible}
        inert={!runsVisible}
        className={cn(
          "bg-surface-scene duration-layout ease-ui @container/workflow-runs flex min-h-0 min-w-0 shrink-0 overflow-hidden transition-[width] motion-reduce:transition-none",
          !runsVisible && "pointer-events-none",
        )}
        style={{ width: runsVisible ? rightWidth : 0 }}
      >
        <div
          className={cn(
            "border-border-subtle duration-layout ease-ui flex min-h-0 shrink-0 flex-col border-l transition-opacity motion-reduce:transition-none",
            runsVisible ? "opacity-100" : "opacity-0",
          )}
          style={{ width: rightWidth }}
        >
          <div className="flex shrink-0 items-center justify-between gap-2 px-4 py-2">
            <h2 className="type-body font-medium">{t("runs")}</h2>
            <div className="p-1">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t("collapseRuns")}
                    aria-controls={runsPaneId}
                    aria-expanded={runsVisible}
                    onClick={() => setRunsOpen(false)}
                  >
                    <PanelRight aria-hidden="true" strokeWidth={1.5} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("collapseRuns")}</TooltipContent>
              </Tooltip>
            </div>
          </div>
          <p className="text-content-caption type-caption px-4 pb-2">
            {t("runHistoryRetention")}
          </p>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
            <div className="grid content-start gap-1">
              {runItems.map((run, index) => (
                <div
                  key={run.id}
                  className={cn(
                    "hover:bg-action-quiet-hover focus-within:bg-action-quiet-hover grid grid-cols-1 items-center gap-1 rounded-md @min-[14rem]/workflow-runs:grid-cols-[minmax(0,1fr)_auto]",
                    selectedRunId === run.id && "bg-selection-surface",
                  )}
                >
                  <Button
                    asChild
                    size="option"
                    variant="ghost"
                    className="min-w-0"
                  >
                    <Link
                      to={`/workflows/${id}/runs/${run.id}`}
                      aria-current={
                        selectedRunId === run.id ? "page" : undefined
                      }
                    >
                      <span className="grid min-w-0 gap-0.5">
                        <span className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                          <span className="text-content-primary whitespace-nowrap">
                            {t("runNumber", {
                              number: runItems.length - index,
                            })}
                          </span>
                          <span className="text-content-caption type-caption font-normal">
                            {t(runOriginKey(run))}
                          </span>
                        </span>
                        <span className="text-content-caption type-caption flex flex-wrap items-center gap-x-2 gap-y-0.5 font-normal">
                          <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                            <span
                              aria-hidden="true"
                              className={cn(
                                "size-1.5 shrink-0 rounded-full",
                                run.status === "succeeded"
                                  ? "bg-success"
                                  : run.status === "failed" ||
                                      run.status === "cancelled"
                                    ? "bg-destructive"
                                    : run.status === "waiting"
                                      ? "bg-warning"
                                      : run.status === "running" ||
                                          run.status === "recovering"
                                        ? "bg-status-info-content"
                                        : "bg-content-caption",
                              )}
                            />
                            {run.status ? t(`status.${run.status}`) : ""}
                          </span>
                          {run.createdAt ? (
                            <time
                              className="whitespace-nowrap"
                              dateTime={new Date(run.createdAt).toISOString()}
                            >
                              {runTime(run.createdAt, i18n.language)}
                            </time>
                          ) : null}
                        </span>
                      </span>
                    </Link>
                  </Button>
                  <div className="flex items-center justify-self-end p-1">
                    <div className="p-1">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            size="icon-sm"
                            variant="ghost"
                            aria-label={t("optimizeWorkflow")}
                            onClick={() => {
                              navigate(
                                `/workflows/${id}?tab=canvas&optimizeRun=${run.id}`,
                              );
                            }}
                          >
                            <WandSparkles
                              aria-hidden="true"
                              strokeWidth={1.5}
                            />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t("optimizeWorkflow")}</TooltipContent>
                      </Tooltip>
                    </div>
                    <div className="p-1">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            asChild
                            size="icon-sm"
                            variant="ghost"
                            aria-label={t("runResult")}
                          >
                            <Link
                              to={`/workflows/${id}/runs/${run.id}?result=1`}
                            >
                              <FileText aria-hidden="true" strokeWidth={1.5} />
                            </Link>
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t("runResult")}</TooltipContent>
                      </Tooltip>
                    </div>
                    {run.status === "succeeded" ||
                    run.status === "failed" ||
                    run.status === "cancelled" ? (
                      <PageAction
                        hitArea="comfortable"
                        intent="delete"
                        aria-label={t("deleteRun", {
                          number: runItems.length - index,
                        })}
                        onClick={() =>
                          setDeleteRun({ run, number: runItems.length - index })
                        }
                      >
                        <Trash2 aria-hidden="true" strokeWidth={1.5} />
                      </PageAction>
                    ) : null}
                  </div>
                </div>
              ))}
            </div>
            {tab === "runs" && runs.isPending ? (
              <WorkflowLoadingState className="p-2" />
            ) : runs.isError ? (
              <WorkflowQueryError
                className="p-2"
                error={runs.error}
                retrying={runs.isFetching}
                onRetry={() => void runs.refetch()}
              />
            ) : !runItems.length ? (
              <p className="text-content-secondary type-ui px-2 py-3">
                {t("noRuns")}
              </p>
            ) : null}
            {runs.hasNextPage ? (
              <Button
                variant="ghost"
                size="sm"
                className="mt-2 w-full"
                disabled={runs.isFetchingNextPage}
                onClick={() => void runs.fetchNextPage()}
              >
                {t("loadMore")}
              </Button>
            ) : null}
          </div>
        </div>
      </aside>
      <aside
        id={historyPaneId}
        aria-label={t("versions")}
        aria-hidden={!historyVisible}
        inert={!historyVisible}
        className={cn(
          "bg-surface-scene duration-layout ease-ui flex min-h-0 min-w-0 shrink-0 overflow-hidden transition-[width] motion-reduce:transition-none",
          !historyVisible && "pointer-events-none",
        )}
        style={{ width: historyVisible ? rightWidth : 0 }}
      >
        <div
          className={cn(
            "border-border-subtle duration-layout ease-ui flex min-h-0 shrink-0 flex-col border-l transition-opacity motion-reduce:transition-none",
            historyVisible ? "opacity-100" : "opacity-0",
          )}
          style={{ width: rightWidth }}
        >
          <div className="flex shrink-0 items-center justify-between gap-2 px-4 py-2">
            <h2 className="type-body font-medium">{t("versions")}</h2>
            <div className="flex items-center gap-1">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t("versionDiff")}
                    disabled={!revision || versionItems.length < 2}
                    onClick={() => setDiffOpen(true)}
                  >
                    <GitCompare aria-hidden="true" strokeWidth={1.5} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("versionDiff")}</TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t("collapseVersions")}
                    onClick={() => setHistoryOpen(false)}
                  >
                    <PanelRight aria-hidden="true" strokeWidth={1.5} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("collapseVersions")}</TooltipContent>
              </Tooltip>
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
            <div className="grid content-start gap-1">
              {versionItems.map((item) => {
                const current =
                  item.id === detail.data?.definition?.currentRevisionId;
                const viewing =
                  item.id ===
                  (revisionId || detail.data?.definition?.currentRevisionId);
                return (
                  <div
                    key={item.id}
                    className={cn(
                      "hover:bg-action-quiet-hover focus-within:bg-action-quiet-hover flex items-center gap-1 rounded-md",
                      viewing && "bg-selection-surface",
                    )}
                  >
                    <NavigationItem
                      active={viewing && canvasActive}
                      aria-label={t("version", { version: item.ordinal })}
                      icon={
                        viewing ? (
                          <Check
                            className="text-primary"
                            aria-hidden="true"
                            strokeWidth={1.5}
                          />
                        ) : (
                          <GitCommitHorizontal
                            aria-hidden="true"
                            strokeWidth={1.5}
                          />
                        )
                      }
                      label={t("version", { version: item.ordinal })}
                      description={
                        item.createdAt ? (
                          <time
                            dateTime={new Date(item.createdAt).toISOString()}
                          >
                            {runTime(item.createdAt, i18n.language)}
                          </time>
                        ) : undefined
                      }
                      trailing={
                        current ? (
                          <Badge variant="secondary">{t("current")}</Badge>
                        ) : undefined
                      }
                      className="min-w-0 flex-1"
                      onClick={() => {
                        change("revision", current ? "" : (item.id ?? ""));
                      }}
                    />
                    {viewing && !current && revision?.id === item.id ? (
                      <div className="shrink-0 p-1">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={t("restoreVersion", {
                                version: item.ordinal,
                              })}
                              onClick={() => {
                                restore.reset();
                                setRestoreOpen(true);
                              }}
                            >
                              <RotateCcw aria-hidden="true" strokeWidth={1.5} />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            {t("restoreVersion", { version: item.ordinal })}
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    ) : null}
                    {!current ? (
                      <div className="shrink-0 p-1">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={t("deleteVersion", {
                                version: item.ordinal,
                              })}
                              onClick={() => {
                                removeRevision.reset();
                                setDeleteRevision(item);
                              }}
                            >
                              <Trash2 aria-hidden="true" strokeWidth={1.5} />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            {t("deleteVersion", { version: item.ordinal })}
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
            {versions.isPending ? (
              <WorkflowLoadingState className="p-2" />
            ) : versions.isError ? (
              <WorkflowQueryError
                className="p-2"
                error={versions.error}
                retrying={versions.isFetching}
                onRetry={() => void versions.refetch()}
              />
            ) : !versionItems.length ? (
              <p className="text-content-secondary type-ui px-2 py-3">
                {t("noVersions")}
              </p>
            ) : null}
            {versions.hasNextPage ? (
              <Button
                size="sm"
                variant="ghost"
                className="mt-2 w-full"
                disabled={versions.isFetchingNextPage}
                onClick={() => void versions.fetchNextPage()}
              >
                {t("loadMore")}
              </Button>
            ) : null}
          </div>
        </div>
      </aside>
    </div>
  );
  const conversation = (
    <WorkflowChatPane
      id={id}
      draftId={params.get("draftId") ?? undefined}
      onCreated={(definitionId) =>
        navigate(`/workflows/${definitionId}`, { replace: true })
      }
      conversationKey={String(conversationGeneration)}
      onSession={(sessionId) => {
        setCreationDraft("");
        setSessionSelection(sessionId, "optimizeRun");
      }}
      session={session}
      closedChatIds={closedChatIds}
      onNewChat={newChat}
      onFirstMessageSubmitted={() => setChatExpanded(true)}
      mainContent={{
        contentId: "workflow-canvas",
        title: t("canvas"),
        chatExpanded,
        resizeLabel: t("resizeChat"),
        content: (canvasActive, context) => (
          <ResultOpenScope scopeKey={context?.scopeKey ?? "workflow-canvas"}>
            {canvas(canvasActive)}
          </ResultOpenScope>
        ),
      }}
      history={
        <WorkflowChatHistory
          sessions={detail.data?.creationSessions ?? []}
          selectedId={session?.id}
          onNew={newChat}
          onRefresh={() => detail.refetch().then(() => {})}
          onSelect={(sessionId) => {
            setChatExpanded(true);
            setSessionSelection(sessionId, "revision");
            setCreationDraft("");
          }}
          onDeleted={(sessionId, chatId) => {
            setClosedChatIds((ids) => [...ids, chatId]);
            if (sessionId === session?.id) newChat();
          }}
        />
      }
      onConfigurationChange={onConfigurationChange}
      onRunningChange={onCreationRunningChange}
      draft={
        params.get("optimizeRun")
          ? t("optimizeRunDraft", { runId: params.get("optimizeRun") })
          : creationDraft
      }
      isHistoryLoading={
        !isNew && detail.isPending && sessionSelection === undefined
      }
      emptyState={
        emptyChat
          ? ({ wakerName, avatar, isRunning }) => (
              <div className="mx-auto grid w-full max-w-[560px] justify-items-center px-6 pb-5 text-center">
                <Avatar
                  name={wakerName}
                  src={avatar}
                  size="docked"
                  preview
                  summon={false}
                  activity={isRunning ? "starting" : "idle"}
                />
                <TypedPhrases
                  as="h1"
                  phrases={welcomePhrases}
                  className="type-page-title mt-6 text-center font-semibold"
                />
              </div>
            )
          : null
      }
    />
  );
  return (
    <div
      tabIndex={-1}
      className="bg-surface-scene flex h-full min-h-0 min-w-0 flex-col"
    >
      {conversation}
      {revision && basicOpen ? (
        <BasicInformationDialog
          key={`basic-information-${revision.id}`}
          id={id}
          revisionId={revision.id ?? ""}
          title={title ?? t("title")}
          description={detail.data?.definition?.description ?? ""}
          open={active && basicOpen}
          onOpenChange={setBasicOpen}
        />
      ) : null}
      {revision ? (
        <RunForm
          key={`run-${revision.id}`}
          id={id}
          revision={revision}
          defaults={detail.data?.creationSessions?.[0]?.defaults}
          container={runInputContainer}
          onClose={() => setRunning(false)}
        />
      ) : null}
      {revision && active && diffOpen ? (
        <Suspense fallback={null}>
          <VersionDiffDialog
            id={id}
            revisionId={revision.id ?? ""}
            ordinal={revision.ordinal ?? 0}
            open={diffOpen}
            onOpenChange={setDiffOpen}
          />
        </Suspense>
      ) : null}
      <Dialog open={active && restoreOpen} onOpenChange={setRestoreOpen}>
        <DialogContent title={t("restore")} description={t("restoreHelp")}>
          <div className="px-6 pb-5">
            <p className="type-body">
              {t("version", { version: revision?.ordinal })}
            </p>
            {restore.error ? (
              <p role="alert" className="text-destructive type-body mt-3">
                {localApiErrorMessage(restore.error, t("error"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setRestoreOpen(false)}>
              {t("cancel")}
            </Button>
            <Button
              disabled={restore.isPending}
              onClick={() => void confirmRestore()}
            >
              {t("restoreConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={active && Boolean(deleteRevision)}
        onOpenChange={(open) => {
          if (!open && !removeRevision.isPending) setDeleteRevision(undefined);
        }}
      >
        <DialogContent
          title={t("deleteVersionTitle")}
          description={t("deleteVersionHelp")}
        >
          <div className="grid gap-2 px-6 pb-5">
            <p className="type-body">
              {t("version", { version: deleteRevision?.ordinal })}
            </p>
            {removeRevision.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(removeRevision.error, t("error"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={removeRevision.isPending}
              onClick={() => setDeleteRevision(undefined)}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="danger"
              disabled={removeRevision.isPending}
              onClick={async () => {
                if (!deleteRevision?.id) return;
                try {
                  await removeRevision.mutateAsync({
                    path: { id, revisionId: deleteRevision.id },
                  });
                  if (revisionId === deleteRevision.id) {
                    change("revision", "");
                  }
                  setDeleteRevision(undefined);
                } catch {
                  /* 请求失败保留所选版本。 */
                }
              }}
            >
              {t("deleteVersionConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {deleteRun ? (
        <RunDeleteDialog
          key={deleteRun.run.id}
          id={id}
          run={deleteRun.run}
          number={deleteRun.number}
          open={active}
          onClose={() => setDeleteRun(undefined)}
          onDeleted={(runId) => {
            if (selectedRunId !== runId) return;
            const next = runItems.find((run) => run.id && run.id !== runId);
            navigate(
              next?.id
                ? `/workflows/${id}/runs/${next.id}`
                : `/workflows/${id}?tab=canvas`,
              { replace: true },
            );
          }}
        />
      ) : null}
      {deleteOpen ? (
        <WorkflowDeleteDialog
          id={id}
          revisionId={detail.data?.definition?.currentRevisionId ?? ""}
          open={active && deleteOpen}
          onOpenChange={setDeleteOpen}
          onDeleted={() => navigate("/resources?category=workflows")}
        />
      ) : null}
    </div>
  );
}
