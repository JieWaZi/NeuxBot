import {
  isCancelledError,
  useMutation,
  useQueries,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  ArchiveX,
  CircleCheck,
  CirclePause,
  CircleQuestionMark,
  CircleStop,
  CircleX,
  Clock3,
  LoaderCircle,
  PanelRight,
  RefreshCw,
  ShieldCheck,
  Workflow,
} from "lucide-react";
import { Suspense } from "react";
import { useTranslation } from "react-i18next";
import {
  WorkflowChatQuestion,
  WorkflowOperationRequestCard,
  WorkflowRequestLoadingState,
  WorkflowWorkerRequest,
  workflowStatusAppearance,
} from "@/modules/workflow";
import {
  getWorkflowRunQueryKey,
  listChatWorkflowRunsOptions,
  retryChatWorkflowDeliveryMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListChatWorkflowRunsResponse } from "@/shared/api/generated/types.gen";
import { useVisibleScene } from "@/shared/api/chat-reading";
import { chatWorkflowRunsScopeKey } from "@/shared/api/chat-workflow-query-keys";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  refreshQueryFromEvent,
  useQueryEvents,
} from "@/shared/api/use-query-events";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import type { BotChatMessage } from "../runtime/chat-projection";
import { openWorkflowRunTab } from "../workbench/workflow-run-reference";
import { useWorkbench } from "../workbench/workbench-store";
import { ChatQuestionRow, type ChatTimelineEvent } from "./chat-thread";
import { ChatTimelineMarker } from "./chat-timeline-marker";
import type { WorkflowInvocationStop } from "./chat-workflow-cancel";

type ChatWorkflowRun = NonNullable<
  ListChatWorkflowRunsResponse["resources"]
>[number];
type WorkflowRequest = NonNullable<ChatWorkflowRun["activeRequests"]>[number];
const workflowBatchSize = 100;

const workflowRunHover = {
  success:
    "hover:bg-status-success-surface focus-visible:bg-status-success-surface",
  default: "hover:bg-status-info-surface focus-visible:bg-status-info-surface",
  destructive:
    "hover:bg-status-danger-surface focus-visible:bg-status-danger-surface",
  warning:
    "hover:bg-status-warning-surface focus-visible:bg-status-warning-surface",
  secondary: "",
};
const workflowRunText = {
  success: "text-status-success-content",
  default: "text-status-info-content",
  destructive: "text-status-danger-content",
  warning: "text-status-warning-content",
  secondary: "text-content-secondary",
};
const workflowRunStatusIcons = {
  queued: Clock3,
  running: LoaderCircle,
  waiting: CirclePause,
  recovering: RefreshCw,
  cancelling: CircleStop,
  cancelled: CircleStop,
  failed: CircleX,
  succeeded: CircleCheck,
};

/** 范围扩大时沿用同一聊天最新的已确认结果，直到新批次有权威回执。 */
function cachedWorkflowRuns(
  client: QueryClient,
  chatId: string,
  uncovered: Set<string>,
) {
  if (!uncovered.size) return [];
  const candidates = client
    .getQueryCache()
    .findAll({ queryKey: chatWorkflowRunsScopeKey(chatId) })
    .filter((query) => query.state.data !== undefined)
    .sort(
      (left, right) => right.state.dataUpdatedAt - left.state.dataUpdatedAt,
    );
  const runs: ChatWorkflowRun[] = [];
  for (const candidate of candidates) {
    const key = candidate.queryKey[0];
    if (typeof key !== "object" || !key || !("query" in key)) continue;
    const params = key.query;
    if (
      typeof params !== "object" ||
      !params ||
      !("executionIds" in params) ||
      typeof params.executionIds !== "string"
    )
      continue;
    const covered = new Set(
      params.executionIds.split(",").filter((id) => uncovered.delete(id)),
    );
    if (covered.size) {
      const data = client.getQueryData<ListChatWorkflowRunsResponse>(
        candidate.queryKey,
      );
      for (const run of data?.resources ?? []) {
        if (run.executionId && covered.has(run.executionId)) runs.push(run);
      }
    }
    if (!uncovered.size) break;
  }
  return runs;
}

function requestTime(request: WorkflowRequest): number {
  return request.human?.createdAt ?? request.operation?.createdAt ?? 0;
}

function compareRequests(left: WorkflowRequest, right: WorkflowRequest) {
  return (
    requestTime(left) - requestTime(right) ||
    (left.requestId ?? "").localeCompare(right.requestId ?? "")
  );
}

function WorkflowRunMarker({
  run,
  onOpen,
}: {
  run: ChatWorkflowRun;
  onOpen: () => void;
}) {
  const { t } = useTranslation("chat");
  const { t: workflowText } = useTranslation("workflow");
  const request =
    !run.removed && (run.status === "running" || run.status === "waiting")
      ? run.activeRequests?.[0]
      : undefined;
  let StatusIcon =
    (run.status && workflowRunStatusIcons[run.status]) ?? CircleQuestionMark;
  let status = run.status
    ? workflowText(`status.${run.status}`, t("workflow.statusUnavailable"))
    : t("workflow.statusUnavailable");
  if (run.removed) {
    StatusIcon = ArchiveX;
    status = t("workflow.removed");
  } else if (request) {
    if (request.kind === "operation" || request.kind === "approval") {
      StatusIcon = ShieldCheck;
      status = t("workflow.awaitingApproval");
    } else {
      StatusIcon = CircleQuestionMark;
      const kind = request.human?.options?.kind;
      status = t(
        kind === "confirm" || kind === "select" || kind === "multiselect"
          ? "workflow.awaitingChoice"
          : "workflow.awaitingInput",
      );
    }
  } else if (run.status === "cancelled") {
    status = t("workflow.stopped");
  }
  const appearance = workflowStatusAppearance(
    run.removed
      ? undefined
      : request
        ? "waiting"
        : run.status === "recovering" || run.status === "cancelling"
          ? "running"
          : run.status,
  );
  const animating =
    !run.removed &&
    !request &&
    (run.status === "running" || run.status === "recovering");
  const content = (
    <>
      <Workflow aria-hidden="true" className="size-icon-xs shrink-0" />
      <span className="min-w-0 truncate" title={run.name}>
        {run.name}
      </span>
      <span
        className={cn(
          "inline-flex shrink-0 items-center gap-1 whitespace-nowrap",
          workflowRunText[appearance.badge],
        )}
      >
        <StatusIcon
          aria-hidden="true"
          className={cn(
            "size-icon-xs shrink-0",
            animating && "motion-safe:animate-spin",
          )}
        />
        <span>{status}</span>
      </span>
    </>
  );
  return (
    <ChatTimelineMarker>
      {run.removed ? (
        <span className="h-button-sm inline-flex max-w-full min-w-0 items-center gap-1.5 px-3 font-medium">
          {content}
        </span>
      ) : (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          shape="capsule"
          aria-label={t("workflow.runLabel", { name: run.name, status })}
          onClick={onOpen}
          className={cn(
            "max-w-full min-w-0",
            workflowRunHover[appearance.badge],
          )}
        >
          {content}
        </Button>
      )}
    </ChatTimelineMarker>
  );
}

function WorkflowRunReadFeedback({
  state,
  retry,
  retrying,
}: {
  state: "loading" | "offline" | "runsFailed" | "runsStale";
  retry: () => void;
  retrying: boolean;
}) {
  const { t } = useTranslation("chat");
  const { t: workflowText } = useTranslation("workflow");
  return (
    <ChatTimelineMarker>
      {state === "loading" ? (
        <div
          role="status"
          aria-label={t("workflow.runsLoading")}
          aria-busy="true"
          className="h-button-sm flex w-36 max-w-full items-center"
        >
          <Skeleton className="h-4 w-full" />
        </div>
      ) : state === "offline" ? (
        <p role="status" className="min-h-button-sm flex items-center">
          {t("workflow.runsOffline")}
        </p>
      ) : (
        <>
          <p role="alert" className="text-destructive">
            {t(`workflow.${state}`)}
          </p>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            loading={retrying}
            onClick={retry}
          >
            {workflowText("retry")}
          </Button>
        </>
      )}
    </ChatTimelineMarker>
  );
}

function WorkflowTimelineQuestion({
  request,
  chatId,
  active,
  waitingForTurn,
  onOpenChild,
  onSettled,
}: {
  request: WorkflowRequest;
  chatId: string;
  active: boolean;
  waitingForTurn: boolean;
  onOpenChild?: () => void;
  onSettled: () => void;
}) {
  const { t } = useTranslation("chat");
  const { kind, definitionId, runId, requestId, instanceId } = request;
  if (!definitionId || !runId || !requestId) return null;
  return (
    <ChatQuestionRow interactionId={requestId}>
      <Suspense fallback={<WorkflowRequestLoadingState />}>
        {kind === "human" && request.human ? (
          <WorkflowChatQuestion
            id={definitionId}
            runId={runId}
            chatId={chatId}
            human={request.human}
            onAnswered={onSettled}
            waitingForTurn={waitingForTurn}
            navigationAction={
              onOpenChild && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={onOpenChild}
                >
                  <PanelRight />
                  {t("workflow.openChildRun")}
                </Button>
              )
            }
          />
        ) : kind === "operation" && request.operation ? (
          <WorkflowOperationRequestCard
            id={definitionId}
            runId={runId}
            chatId={chatId}
            operation={request.operation}
            onSettled={onSettled}
            compact
          />
        ) : instanceId && (kind === "approval" || kind === "elicitation") ? (
          <WorkflowWorkerRequest
            id={definitionId}
            runId={runId}
            instanceId={instanceId}
            request={{
              id: requestId,
              chatId: request.chatId,
              kind,
              active,
              onSettled,
            }}
          />
        ) : null}
      </Suspense>
    </ChatQuestionRow>
  );
}

function WorkflowDeliveryActions({
  chatId,
  runId,
  snapshotUnavailable,
  fileError,
  summaryNeeded,
  onRefresh,
}: {
  chatId: string;
  runId: string;
  snapshotUnavailable: boolean;
  fileError?: string;
  summaryNeeded: boolean;
  onRefresh: () => void;
}) {
  const { t } = useTranslation("chat");
  const retry = useMutation(
    withLocalErrorMutation(retryChatWorkflowDeliveryMutation()),
  );
  const deliver = (summary: boolean) =>
    retry.mutate(
      { path: { chatId, runId }, body: { summary } },
      { onSettled: onRefresh },
    );
  const mutationError = retry.error;
  const summaryRetried = retry.variables?.body.summary;
  const fileFailure =
    mutationError && !summaryRetried
      ? localApiErrorMessage(mutationError, t("workflow.deliveryFailed"))
      : fileError &&
        applicationErrorMessage({
          code: fileError,
          fallback: t("workflow.deliveryFailed"),
        });
  const summaryFailure =
    mutationError && summaryRetried
      ? localApiErrorMessage(mutationError, t("workflow.summaryFailed"))
      : undefined;
  return (
    <ChatQuestionRow>
      <div className="type-ui text-content-secondary grid gap-2">
        {snapshotUnavailable ? (
          <p role="status" className="text-destructive">
            {t("workflow.outputSnapshotUnavailable")}
          </p>
        ) : null}
        {fileError ? (
          <div className="flex flex-wrap items-center gap-2">
            <p role="status" className="text-destructive">
              {fileFailure}
            </p>
            <Button
              variant="ghost"
              size="sm"
              disabled={retry.isPending}
              onClick={() => deliver(false)}
            >
              {t("workflow.retryAttachments")}
            </Button>
          </div>
        ) : null}
        {summaryNeeded ? (
          <div className="flex flex-wrap items-center gap-2">
            {summaryFailure ? (
              <p role="status" className="text-destructive">
                {summaryFailure}
              </p>
            ) : null}
            <Button
              variant="ghost"
              size="sm"
              disabled={retry.isPending}
              onClick={() => deliver(true)}
            >
              {t("workflow.retrySummary")}
            </Button>
          </div>
        ) : null}
      </div>
    </ChatQuestionRow>
  );
}

/** 使用消息窗口的执行身份批量读取；节点详情只由已打开画布或实际子节点请求读取。 */
export function useChatWorkflowTimeline({
  chatId,
  scopeKey,
  active,
  enabled = true,
  messages,
  onOpenWorkbench,
}: {
  chatId?: string;
  scopeKey: string;
  active: boolean;
  enabled?: boolean;
  messages: readonly BotChatMessage[];
  onOpenWorkbench: () => void;
}): {
  events: ChatTimelineEvent[];
  workflowInvocation?: WorkflowInvocationStop;
} {
  const { dispatch } = useWorkbench(scopeKey);
  const { t } = useTranslation("chat");
  const client = useQueryClient();
  const sceneVisible = useVisibleScene();
  const visible = enabled && active && sceneVisible;
  const inputs = new Map<string, string>();
  const replies = new Map<string, BotChatMessage>();
  const workflowExecutions = new Set<string>();
  let runningExecutionId: string | undefined;
  const inputMessages = new Set<string>();
  for (const message of messages) {
    if (
      message.optimistic ||
      message.inherited ||
      !message.id ||
      message.question
    )
      continue;
    if (message.role === "user") {
      inputMessages.add(message.id);
      if (message.executionId && !inputs.has(message.executionId))
        inputs.set(message.executionId, message.id);
      if (message.executionId && message.workflow?.id)
        workflowExecutions.add(message.executionId);
    }
    if (message.role === "assistant" && message.executionId) {
      if (!replies.has(message.executionId))
        replies.set(message.executionId, message);
      if (message.status === "pending" || message.status === "running")
        runningExecutionId = message.executionId;
    }
  }
  const executionIds = [
    ...new Set([...inputs.keys(), ...replies.keys()]),
  ].sort();
  const batches: string[][] = [];
  for (
    let offset = 0;
    offset < executionIds.length;
    offset += workflowBatchSize
  )
    batches.push(executionIds.slice(offset, offset + workflowBatchSize));
  const path = { chatId: chatId ?? "" };
  const options =
    chatId && enabled
      ? batches.map((executionIds) => ({
          ...listChatWorkflowRunsOptions(
            withLocalErrorSurface({
              path,
              query: { executionIds: executionIds.join(",") },
            }),
          ),
          enabled: visible,
        }))
      : [];
  const keys = options.map((option) => option.queryKey);
  const queries = useQueries({ queries: options });
  useQueryEvents({
    topic: "workflow",
    resource: { kind: "chat", id: path.chatId },
    active: visible,
    enabled: visible && Boolean(chatId && batches.length),
    eventTypes: ["workflow.changed"],
    queryKeys: keys,
  });
  if (!chatId || !enabled) return { events: [] };
  const refreshTimeline = (runPath?: { id: string; runId: string }) => {
    const cache = client.getQueryCache();
    const timelineQueries = keys.flatMap((queryKey) =>
      cache.findAll({ queryKey, exact: true }),
    );
    const runQueries = runPath
      ? cache.findAll({ queryKey: getWorkflowRunQueryKey({ path: runPath }) })
      : [];
    return Promise.allSettled(
      [...timelineQueries, ...runQueries].map((query) => {
        const { queryKey } = query;
        const filters = { queryKey, exact: true };
        return query.isActive()
          ? refreshQueryFromEvent(client, queryKey, () =>
              client.refetchQueries(filters, {
                cancelRefetch: false,
                throwOnError: true,
              }),
            )
          : client.invalidateQueries({ ...filters, refetchType: "none" });
      }),
    );
  };
  const uncovered = new Set(
    batches.flatMap((batch, index) =>
      queries[index]?.data === undefined ? batch : [],
    ),
  );
  const runs = [
    ...new Map(
      [
        ...queries.flatMap(({ data }) => data?.resources ?? []),
        ...cachedWorkflowRuns(client, chatId, uncovered),
      ]
        .filter((run) => run.id)
        .map((run) => [run.id, run]),
    ).values(),
  ].sort(
    (a, b) =>
      (a.createdAt ?? 0) - (b.createdAt ?? 0) ||
      (a.id ?? "").localeCompare(b.id ?? ""),
  );
  const firstHuman = runs
    .flatMap((run) => run.activeRequests ?? [])
    .filter((request) => request.kind === "human")
    .sort(compareRequests)[0];
  const events: ChatTimelineEvent[] = runs.flatMap((run) => {
    if (!run.id || !run.executionId || !run.definitionId || !run.name)
      return [];
    const runId = run.id;
    const definitionId = run.definitionId;
    const name = run.name;
    const terminal =
      run.status === "succeeded" ||
      run.status === "failed" ||
      run.status === "cancelled";
    const reply = replies.get(run.executionId);
    const replyStatus = reply?.status;
    const replyInterrupted =
      replyStatus === "failed" || replyStatus === "canceled";
    const replyStartedAt = reply?.runMetrics?.startedAt;
    const replyEndedEarly =
      replyInterrupted ||
      (replyStatus === "completed" &&
        (!terminal ||
          (run.completedAt != null &&
            replyStartedAt != null &&
            replyStartedAt + (reply?.runMetrics?.durationMs ?? 0) <
              run.completedAt)));
    const summaryNeeded = replyEndedEarly && run.summaryFinishedAt == null;
    const beforeMessageId = reply?.id;
    const afterMessageId =
      run.inputMessageId && inputMessages.has(run.inputMessageId)
        ? run.inputMessageId
        : inputs.get(run.executionId);
    if (!beforeMessageId && !afterMessageId) return [];
    const requests = [
      ...new Map(
        [...(run.answeredRequests ?? []), ...(run.activeRequests ?? [])].map(
          (request) => [`${request.runId}:${request.requestId}`, request],
        ),
      ).values(),
    ].sort(compareRequests);
    return [
      {
        id: `workflow-run:${runId}`,
        createdAt: run.createdAt ?? 0,
        beforeMessageId,
        afterMessageId: beforeMessageId ? undefined : afterMessageId,
        content: (
          <>
            <WorkflowRunMarker
              run={run}
              onOpen={() => {
                dispatch(
                  openWorkflowRunTab({
                    definitionId,
                    runId,
                    name,
                    chatId,
                  }),
                );
                onOpenWorkbench();
              }}
            />
            {replyEndedEarly &&
            !terminal &&
            !run.sourceWorkbench &&
            run.status !== "cancelling" ? (
              <ChatQuestionRow>
                <p className="type-ui text-content-muted">
                  {t("workflow.replyInterrupted")}
                </p>
              </ChatQuestionRow>
            ) : null}
            {terminal &&
            !run.removed &&
            !run.sourceWorkbench &&
            (run.snapshotError || run.deliveryError || summaryNeeded) ? (
              <WorkflowDeliveryActions
                chatId={chatId}
                runId={runId}
                snapshotUnavailable={Boolean(run.snapshotError)}
                fileError={run.deliveryError}
                summaryNeeded={summaryNeeded}
                onRefresh={() => {
                  void refreshTimeline();
                }}
              />
            ) : null}
            {requests.map((request) => {
              const child =
                request.kind === "human" &&
                !run.removed &&
                request.runId !== runId &&
                request.definitionId &&
                request.runId &&
                request.instanceId
                  ? {
                      definitionId: request.definitionId,
                      runId: request.runId,
                      instanceId: request.instanceId,
                      name: t("workflow.childRunLabel", { name }),
                      chatId,
                    }
                  : undefined;
              return (
                <WorkflowTimelineQuestion
                  key={`${request.runId}:${request.requestId}`}
                  request={request}
                  chatId={chatId}
                  active={visible}
                  onOpenChild={
                    child &&
                    (() => {
                      dispatch(openWorkflowRunTab(child));
                      onOpenWorkbench();
                    })
                  }
                  waitingForTurn={Boolean(
                    request.kind === "human" &&
                    request.human?.status === "waiting" &&
                    firstHuman &&
                    firstHuman !== request,
                  )}
                  onSettled={() => {
                    void refreshTimeline(
                      request.definitionId && request.runId
                        ? { id: request.definitionId, runId: request.runId }
                        : undefined,
                    );
                  }}
                />
              );
            })}
          </>
        ),
      },
    ];
  });
  if (visible) {
    const affectedBatches = queries.flatMap((query, index) => {
      const batch = batches[index];
      if (!batch) return [];
      // 时间戳会跨读取保留，只有当前批次的实际失败才进入时间线。
      const failed = query.isError && !isCancelledError(query.error);
      const loading =
        query.isLoading &&
        batch.some((id) => uncovered.has(id) && workflowExecutions.has(id));
      return failed || query.isPaused || loading
        ? [{ batch, query, failed }]
        : [];
    });
    const failedBatches = affectedBatches.filter(({ failed }) => failed);
    const affectedIds = new Set(affectedBatches.flatMap(({ batch }) => batch));
    const anchor = messages.findLast((message) => {
      const { id, executionId } = message;
      return (
        id &&
        executionId &&
        affectedIds.has(executionId) &&
        (id === replies.get(executionId)?.id || id === inputs.get(executionId))
      );
    });
    if (anchor?.id) {
      const paused = affectedBatches.some(({ query }) => query.isPaused);
      const stale =
        failedBatches.some(({ query }) => query.data !== undefined) ||
        runs.some(
          ({ executionId }) => executionId && affectedIds.has(executionId),
        );
      const state = paused
        ? "offline"
        : failedBatches.length
          ? stale
            ? "runsStale"
            : "runsFailed"
          : "loading";
      events.push({
        id: "workflow-run-read",
        createdAt: anchor.createdAt ?? 0,
        beforeMessageId: anchor.role === "assistant" ? anchor.id : undefined,
        afterMessageId: anchor.role === "user" ? anchor.id : undefined,
        content: (
          <WorkflowRunReadFeedback
            state={state}
            retrying={failedBatches.every(({ query }) => query.isFetching)}
            retry={() => {
              for (const { query } of failedBatches)
                void query.refetch({ cancelRefetch: false });
            }}
          />
        ),
      });
    }
  }
  const currentReplyState = runningExecutionId
    ? replies.get(runningExecutionId)?.status
    : undefined;
  const workflowInvocation =
    visible &&
    runningExecutionId &&
    (currentReplyState === "pending" || currentReplyState === "running") &&
    workflowExecutions.has(runningExecutionId) &&
    !runs.some((run) => run.executionId === runningExecutionId)
      ? {
          executionId: runningExecutionId,
          beforeStart: batches.some(
            (batch, index) =>
              batch.includes(runningExecutionId) && queries[index]?.isSuccess,
          ),
        }
      : undefined;
  return { events, workflowInvocation };
}
