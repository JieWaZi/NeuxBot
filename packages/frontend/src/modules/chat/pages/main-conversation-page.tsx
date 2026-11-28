import {
  useInfiniteQuery,
  useQueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Navigate,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  mainConversationHref,
  mainConversationRecordId,
  mainConversationRequestTarget,
  mainConversationSourceId,
} from "@/shared/lib/main-conversation-navigation";
import {
  useMainConversationReading,
  usePresentedChatBoundary,
  useVisibleScene,
} from "@/shared/api/chat-reading";
import { ApiError } from "@/shared/api/errors";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { toast } from "@/shared/ui/sonner";
import {
  mainConversationOptions,
  refreshMainConversationHistory,
  sourceHistoryOptions,
  sourceScopeOptions,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import { useWorkTickets, type WorkTicketOrigin } from "../api/work-tickets";
import { ChatHistoryLoadingState } from "../components/chat-history-loading-state";
import { useChatMessageLocator } from "../components/chat-message-locator";
import { ChatMessageReplyActions } from "../components/chat-message-reply";
import { SettledChatQuestion } from "../components/chat-timeline-interactions";
import { ChatWorkspaceSurface } from "../components/chat-workspace-surface";
import {
  MainConversationTimeDivider,
  PublicConversationMessage,
} from "../components/main-conversation-message";
import { MainConversationDeleteMessage } from "../components/main-conversation-delete-message";
import { MainConversationSource } from "../components/main-conversation-source";
import { MainConversationSummary } from "../components/main-conversation-summary";
import { MainConversationRequests } from "../components/main-conversation-requests";
import { MainConversationWait } from "../components/main-conversation-wait";
import { WorkTicketLoadError } from "../components/work-ticket-load-error";
import { chatContentColumnClassName } from "../components/chat-layout";
import { MainConversationComposer } from "../components/main-conversation-composer";
import { useChatViewportScroll } from "../components/use-chat-viewport-scroll";
import { useChatHistoryScroll } from "../components/use-chat-history-scroll";
import { useMainConversationMotion } from "../components/use-main-conversation-motion";
import { mainConversationMessageGroups } from "../model/main-conversation-message-group";
import {
  pendingMainConversationMessage,
  useMainConversationSubmissions,
} from "../model/main-conversation-submission";
import type { ChatReplyReference } from "../runtime/chat-projection";
import { useMainConversationWorkspace } from "./use-main-conversation-workspace";

export function MainConversationPage() {
  const { subjectType, subjectId } = useParams();
  if ((subjectType !== "waker" && subjectType !== "team") || !subjectId)
    return <Navigate to="/chat" replace />;
  return (
    <Conversation
      key={subjectType + ":" + subjectId}
      subject={{ subjectType, subjectId }}
    />
  );
}

function Conversation({ subject }: { subject: ConversationSubject }) {
  const { t } = useTranslation("chat");
  const client = useQueryClient();
  const active = useVisibleScene();
  const [params] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const source = mainConversationSourceId(params);
  const sourcePublicationId = source
    ? params.get("publication") || undefined
    : undefined;
  const recordId = source ? undefined : mainConversationRecordId(params);
  const [reply, setReply] = useState<ChatReplyReference>();
  const [deletion, setDeletion] = useState<{
    messageId: string;
    trigger: HTMLButtonElement;
  }>();
  const viewingRecord = Boolean(source || recordId);
  const visible = active && !viewingRecord;
  const recordVisible = active && Boolean(recordId);
  const [replyLocation, setReplyLocation] = useState<{
    replyId: string;
    targetId: string;
    keyboardTrigger?: HTMLButtonElement;
    request: number;
  }>();
  if (!visible && replyLocation) setReplyLocation(undefined);
  const viewport = useRef<HTMLDivElement>(null);
  const recordViewport = useRef<HTMLDivElement>(null);
  const activeViewport = recordId ? recordViewport : viewport;
  const { subjectType, subjectId } = subject;
  const options = useMemo(
    () => mainConversationOptions({ subjectType, subjectId }),
    [subjectType, subjectId],
  );
  const history = useInfiniteQuery({
    ...options,
    enabled: active,
    retry: false,
  });
  const activeWork = history.data?.pages[0]?.activeWork;
  const activeWorkSources = new Set(activeWork?.map((work) => work.workChatId));
  const { messages: committed, byId: committedById } = useMemo(() => {
    const byId = new Map<PublicMessage["id"], PublicMessage>();
    for (const page of history.data?.pages.toReversed() ?? []) {
      for (const row of page.messages ?? []) byId.set(row.id, row);
    }
    return { messages: [...byId.values()], byId };
  }, [history.data]);
  const conversationId = history.data?.pages[0]?.id;
  const pendingInputs = history.data?.pages[0]?.pendingInputs;
  const knownInputs = useMemo(() => {
    const pending = pendingInputs
      ?.filter((row) => !committedById.has(row.id))
      .map((row) => pendingMainConversationMessage(row, conversationId));
    return pending?.length ? [...committed, ...pending] : committed;
  }, [committed, committedById, conversationId, pendingInputs]);
  const sourceMessageId = source
    ? params.get("message") || undefined
    : undefined;
  const sourceRow = sourceMessageId
    ? committedById.get(sourceMessageId)
    : undefined;
  const sourceExecutionId = sourceMessageId
    ? sourceRow?.role === "assistant" && sourceRow.sourceChatId === source
      ? sourceRow.forkExecutionId
      : activeWork?.find(
          (work) =>
            work.workChatId === source &&
            work.inputMessageId === sourceMessageId,
        )?.executionId
    : undefined;
  const recordRow = recordId ? committedById.get(recordId) : undefined;
  const recordReply =
    recordRow?.role === "assistant" && !recordRow.sourceChatId
      ? recordRow
      : undefined;
  const recordInputId = recordReply?.replyToMessageId;
  const recordInputRow = recordInputId
    ? committedById.get(recordInputId)
    : undefined;
  const recordInput =
    recordInputRow?.role === "user" ? recordInputRow : undefined;
  const recordTarget =
    recordInput?.id ??
    (recordReply && recordInputId && !recordInputRow
      ? recordInputId
      : recordId);
  const invalidRecord = Boolean(recordRow && !recordReply);
  const recordAccessDenied =
    history.error instanceof ApiError &&
    history.error.status !== undefined &&
    [401, 403, 404].includes(history.error.status);
  const ticketOrigin: WorkTicketOrigin | undefined = conversationId
    ? { originKind: "main", originId: conversationId }
    : undefined;
  const tickets = useWorkTickets(
    ticketOrigin,
    history.data?.pages.map((page) =>
      (page.messages ?? []).flatMap(({ id }) => (id ? [id] : [])),
    ) ?? [],
    visible,
  );
  const reading = useMainConversationReading({
    ...subject,
    conversationId: committed.some((row) => row.readable)
      ? conversationId
      : undefined,
    active: visible,
  });
  const historyUpdates =
    client.getQueryState(options.queryKey)?.dataUpdateCount ?? 0;
  const { messages, pendingMainInputs } = useMainConversationSubmissions(
    subject,
    conversationId,
    knownInputs,
    history.hasNextPage,
    historyUpdates,
  );
  const byId =
    messages === committed
      ? committedById
      : new Map(messages.map((row) => [row.id, row]));
  const replyRow = reply?.messageId ? byId.get(reply.messageId) : undefined;
  const replyTicket = reply?.messageId
    ? tickets.byMessage.get(reply.messageId)
    : undefined;
  const replySourceId =
    !replyRow?.sourceReadOnly &&
    (!replyTicket || replyTicket.workChatId === replyRow?.sourceChatId)
      ? replyRow?.sourceChatId
      : undefined;
  const {
    adapter,
    surface,
    bindings: workspaceBindings,
    codeReferenceRequest,
  } = useMainConversationWorkspace({
    subject,
    source: sourcePublicationId ? undefined : source,
    active,
    messages: recordId || sourcePublicationId ? [] : committed,
    activeWork: recordId || sourcePublicationId ? [] : activeWork,
  });
  const { rows, trailingWork } = useMemo(() => {
    const awaitingReply = new Set(pendingMainInputs);
    for (const input of pendingInputs ?? []) {
      if (input.id && !input.failureCode && !input.workChatId)
        awaitingReply.add(input.id);
    }
    return mainConversationMessageGroups(messages, activeWork, awaitingReply);
  }, [messages, activeWork, pendingMainInputs, pendingInputs]);
  const canContinueRecord = Boolean(
    recordReply?.replyToMessageId &&
    !recordReply.originalWork &&
    !recordAccessDenied,
  );
  const recordMessageIds = new Set([recordReply?.id]);
  const defaultReplyIds = new Set<string>();
  if (recordReply && canContinueRecord) {
    // 按原作者恢复默认会话的公开问答和仍在等待的后续输入。
    for (const { row } of rows) {
      if (
        row.role === "assistant" &&
        row.id &&
        row.replyToMessageId &&
        !row.sourceChatId &&
        !row.originalWork &&
        row.sourceWakerId === recordReply.sourceWakerId
      ) {
        defaultReplyIds.add(row.id);
        recordMessageIds.add(row.id);
        recordMessageIds.add(row.replyToMessageId);
      }
    }
  }
  const recordRows = recordReply?.id
    ? rows.filter(({ row }) => {
        if (!row.id) return false;
        if (recordMessageIds.has(row.id) && !row.sourceChatId) return true;
        return Boolean(
          row.role === "user" &&
          !row.sourceChatId &&
          row.replyToMessageId &&
          defaultReplyIds.has(row.replyToMessageId),
        );
      })
    : [];
  const recordContext = recordRows.findLast(
    ({ row }) =>
      row.role === "assistant" && !row.sourceChatId && !row.originalWork,
  )?.row.id;
  const latestAcceptedInput = (recordId ? recordRows : rows).reduce<
    PublicMessage | undefined
  >((latest, { row }) => {
    if (row.role !== "user" || !row.id || pendingMainInputs?.has(row.id))
      return latest;
    // 未进入时间线的队列回执也可能比已提交的消息更早。
    if (latest?.createdAt && row.createdAt && latest.createdAt > row.createdAt)
      return latest;
    return row;
  }, undefined);
  const workQueryKeys = source
    ? [sourceHistoryOptions(subject, source, sourcePublicationId).queryKey]
    : (visible
        ? (activeWork ?? [])
        : recordVisible && canContinueRecord
          ? recordRows.map(({ work }) => work)
          : []
      ).flatMap((work) =>
        work?.workChatId && (work.waitingApproval || work.waitingElicitation)
          ? [sourceScopeOptions(subject, work.workChatId).queryKey]
          : [],
      );
  const scope = subject.subjectType + ":" + subject.subjectId;
  const resultMessageId = visible
    ? params.get("message") || undefined
    : undefined;
  const requestTarget = visible
    ? mainConversationRequestTarget(params)
    : undefined;
  const locationTargetId = recordId
    ? invalidRecord
      ? undefined
      : recordTarget
    : (replyLocation?.targetId ?? resultMessageId);
  const locationKey = recordId
    ? JSON.stringify([scope, location.key, locationTargetId])
    : replyLocation
      ? JSON.stringify([scope, replyLocation.request])
      : resultMessageId || requestTarget
        ? JSON.stringify([scope, location.key])
        : undefined;
  const scrolling = useChatHistoryScroll({
    scope: locationKey ?? scope,
    viewport: activeViewport,
    hasMore: history.hasNextPage,
    enabled: visible || recordVisible,
    load: async (signal) => {
      const query = client.getQueryCache().find({
        queryKey: options.queryKey,
        exact: true,
      });
      // 让已有实时对账提交后再沿新游标翻页，二者共用同一查询。
      while (!signal.aborted && query && query.state.fetchStatus !== "idle") {
        const pending = query.promise;
        if (!pending) break;
        await pending.catch(() => {});
      }
      signal.throwIfAborted();
      if (
        positioning &&
        locationTargetId &&
        client
          .getQueryData(options.queryKey)
          ?.pages.some((page) =>
            page.messages?.some((row) => row.id === locationTargetId),
          )
      )
        return;
      await history.fetchNextPage({ cancelRefetch: false, throwOnError: true });
    },
    onError: () => {},
  });
  const { feedback, positioning } = useChatMessageLocator({
    viewport: activeViewport,
    active:
      (visible || (recordVisible && !recordAccessDenied)) && !history.isPending,
    routeKey: locationKey,
    messageId: locationTargetId,
    requestedInteractionId: requestTarget?.requestId,
    requestedInteractionScope: requestTarget?.workChatId,
    cancelReplyLocation: replyLocation
      ? () => setReplyLocation(undefined)
      : undefined,
    keyboardReplyTrigger: replyLocation?.keyboardTrigger,
    hasEarlierMessages: history.hasNextPage,
    inlineReplyFeedback: replyLocation
      ? byId.get(replyLocation.replyId)?.role === "assistant"
        ? "assistant"
        : "user"
      : undefined,
    messageLookup: Boolean(recordId),
    onLoadEarlier: async () => {
      const loaded = await scrolling.loadEarlier();
      if (loaded === false) {
        const error = client.getQueryState(options.queryKey)?.error;
        if (
          error instanceof ApiError &&
          (error.status === 401 || error.status === 403 || error.status === 404)
        )
          return "unavailable";
      }
      return loaded;
    },
    onProgrammaticScroll: scrolling.ignoreProgrammaticScroll,
  });
  const focusedReply = useRef<{ targetId: string; positioning: boolean }>(null);
  useLayoutEffect(() => {
    focusedReply.current =
      (visible || recordVisible) && locationTargetId
        ? { targetId: locationTargetId, positioning }
        : null;
    return () => {
      focusedReply.current = null;
    };
  }, [positioning, locationTargetId, visible, recordVisible]);
  const visibleTarget = useCallback(() => {
    const container = activeViewport.current;
    const selected = focusedReply.current;
    if (!container || !selected) return;
    if (selected.positioning) return selected.targetId;
    const target = container.querySelector<HTMLElement>(
      `[data-message-id="${CSS.escape(selected.targetId)}"]`,
    );
    if (!target) return;
    const message = target.getBoundingClientRect();
    const visible = container.getBoundingClientRect();
    if (
      visible.height > 0 &&
      message.bottom > visible.top &&
      message.top < visible.bottom
    )
      return selected.targetId;
  }, [activeViewport]);
  const read = useCallback(
    (queryKey: QueryKey) =>
      queryKey[0] === options.queryKey[0]
        ? refreshMainConversationHistory(client, options, visibleTarget)
        : client.refetchQueries(
            { queryKey, exact: true, type: "active" },
            { cancelRefetch: false },
          ),
    [client, options, visibleTarget],
  );
  const motion = useMainConversationMotion({
    viewport,
    messages,
    localInputs: pendingMainInputs,
    acceptedInputs: pendingInputs,
    historyKey: options.queryKey,
    historyUpdates,
    enabled: visible && !positioning && Boolean(history.data),
  });
  useQueryEvents({
    topic: "main-conversations",
    resource: { kind: subjectType, id: subjectId },
    eventTypes: ["main-conversations.changed"],
    queryKeys: [
      options.queryKey,
      ...workQueryKeys,
      ...(visible && replySourceId
        ? [sourceScopeOptions(subject, replySourceId).queryKey]
        : []),
    ],
    active,
    enabled: active,
    onRefreshCommit: motion.onRefreshCommit,
    read,
  });
  useChatViewportScroll({
    scope,
    viewport,
    revision: messages,
    enabled: visible && !positioning,
    followInitially: !locationTargetId && !requestTarget,
    isHistoryAnchoring: scrolling.isAnchoring,
    onProgrammaticScroll: scrolling.ignoreProgrammaticScroll,
    onFollowRevision: motion.onFollowRevision,
    onUserScroll: motion.onUserScroll,
  });
  usePresentedChatBoundary({
    root: viewport,
    active: visible && !positioning && reading.state?.complete === true,
    present: reading.present,
    revision: reading.revision,
  });
  const navigateToSource = (
    workChatId: string,
    messageId?: string,
    publicationId?: string,
  ) =>
    navigate(
      mainConversationHref({
        kind: subject.subjectType,
        id: subject.subjectId,
        workChatId,
        messageId,
        publicationId,
      }),
      { preventScrollReset: true },
    );
  const openPublicRecord = (replyId: string) =>
    navigate(
      mainConversationHref({
        kind: subject.subjectType,
        id: subject.subjectId,
        recordMessageId: replyId,
      }),
      { preventScrollReset: true },
    );
  const locatePublicMessage = (messageId: string) =>
    navigate(
      mainConversationHref({
        kind: subject.subjectType,
        id: subject.subjectId,
        messageId,
      }),
      { preventScrollReset: true },
    );
  const removeFromHistory = async (messageId: string) => {
    const { queryKey } = options;
    // 移除已确认，丢弃可能早于此次提交的历史快照，再按原游标对账。
    await client.cancelQueries({ queryKey, exact: true });
    client.setQueryData(
      queryKey,
      (history) =>
        history && {
          ...history,
          pages: history.pages.map((page) => ({
            ...page,
            messages: page.messages?.filter(
              (message) => message.id !== messageId,
            ),
          })),
        },
    );
    setReply((current) =>
      current?.messageId === messageId ? undefined : current,
    );
    void client.invalidateQueries({ queryKey, exact: true });
  };
  const renderWork = (
    work: (typeof trailingWork)[number],
    continued: boolean,
    isActive = visible,
  ) => (
    <MainConversationRequests
      key={`work:${work.workChatId}:${work.executionId}`}
      subject={subject}
      active={isActive}
      work={work}
      continued={continued}
      requestedInteractionId={
        requestTarget?.workChatId === work.workChatId
          ? requestTarget?.requestId
          : undefined
      }
      settledQuestions={
        work.inputMessageId
          ? byId.get(work.inputMessageId)?.questions
          : undefined
      }
      openSource={(workChatId) =>
        navigateToSource(workChatId, work.inputMessageId)
      }
    />
  );
  // 保留请求卡的 DOM 和输入值，加载原交办所在旧页时只移动它的位置。
  const renderTimelineRow = (
    {
      row,
      timeDivider,
      continued,
      work,
      waitingWork,
      awaitingReply,
      showWorkRecord,
    }: (typeof rows)[number],
    inTimeline: boolean,
  ) => {
    const rowVisible = inTimeline ? visible : recordVisible;
    const rowId = row.id;
    const showTime = inTimeline && rowId && timeDivider;
    const ticket =
      inTimeline && rowId ? tickets.byMessage.get(rowId) : undefined;
    const openRecord = (workChatId: string) =>
      navigateToSource(
        workChatId,
        row.id,
        row.sourceReadOnly && row.role === "assistant" ? row.id : undefined,
      );
    return [
      showTime && (
        <MainConversationTimeDivider
          key={`time:${rowId}`}
          date={showTime}
          messageId={rowId}
        />
      ),
      <PublicConversationMessage
        key={`message:${row.id}`}
        subject={subject}
        row={row}
        inTimeline={inTimeline}
        continued={continued && !showTime}
        quoted={
          row.replyToMessageId ? byId.get(row.replyToMessageId) : undefined
        }
        quoteFeedback={
          inTimeline && row.id === replyLocation?.replyId ? feedback : undefined
        }
        active={rowVisible}
        select={inTimeline ? setReply : undefined}
        openSource={
          row.sourceReadOnly && row.role === "user" ? undefined : openRecord
        }
        openRecord={
          inTimeline && rowId ? () => openPublicRecord(rowId) : undefined
        }
        showWorkRecord={
          showWorkRecord || Boolean(ticket && ticket.id === row.id)
        }
        sourceBusy={activeWorkSources.has(row.sourceChatId)}
        onMarkUnread={
          inTimeline && reading.state?.complete
            ? (messageId) => {
                void reading
                  .markUnread(messageId)
                  .catch(() => toast.error(t("reading.failed")));
              }
            : undefined
        }
        onDelete={
          inTimeline
            ? (messageId, trigger) => setDeletion({ messageId, trigger })
            : undefined
        }
        openLink={workspaceBindings.openBrowserLink}
        openFile={workspaceBindings.openAttachmentFile}
        openCodeReference={workspaceBindings.openCodeReference}
      />,
      ...(row.questions ?? []).map((question) => (
        <SettledChatQuestion
          key={`question:${row.id}:${question.elicitationId}`}
          question={question}
        />
      )),
      work && renderWork(work, row.role === "assistant", rowVisible),
      rowVisible && awaitingReply && (
        <MainConversationWait
          key={`wait:${row.id}`}
          subject={subject}
          work={waitingWork}
          openSource={openRecord}
        />
      ),
    ];
  };
  const timeline = [
    ...rows.flatMap((row) => renderTimelineRow(row, true)),
    ...trailingWork.map((work) => renderWork(work, false)),
  ];
  const publicTimeline = (
    <div hidden={viewingRecord} className="flex min-h-0 flex-1 flex-col">
      <div
        ref={viewport}
        onScroll={scrolling.onScroll}
        data-chat-scroll-viewport
        className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]"
      >
        <div
          data-chat-message-column
          className={`mx-auto grid w-full gap-2 overflow-y-clip py-4 ${chatContentColumnClassName}`}
        >
          {history.isPending && <ChatHistoryLoadingState />}
          {!replyLocation && feedback}
          {history.isError &&
            !history.isFetchNextPageError &&
            (history.data ? (
              <QueryRefreshError
                error={history.error}
                pending={history.isFetching}
                onRetry={() => void history.refetch()}
              />
            ) : (
              <QueryState
                error={history.error}
                onRetry={() => void history.refetch()}
              />
            ))}
          {history.hasNextPage && (
            <div className="flex justify-center">
              <Button
                variant="ghost"
                size="sm"
                disabled={history.isFetchingNextPage || positioning}
                onClick={() => void scrolling.loadEarlier()}
              >
                {t("chat:loadEarlierMessages")}
              </Button>
            </div>
          )}
          {visible && tickets.loadError && (
            <div className="px-6">
              <WorkTicketLoadError {...tickets.loadError} />
            </div>
          )}
          {scrolling.error && !positioning && !feedback ? (
            <QueryRefreshError
              error={scrolling.error}
              pending={history.isFetchingNextPage}
              onRetry={() => void scrolling.loadEarlier()}
            />
          ) : null}
          <ChatMessageReplyActions
            value={{
              select: setReply,
              locate: (targetId, keyboardTrigger, replyId) => {
                if (!visible || !replyId) return;
                setReplyLocation((current) => ({
                  replyId,
                  targetId,
                  keyboardTrigger,
                  request: (current?.request ?? 0) + 1,
                }));
              },
            }}
          >
            {timeline}
          </ChatMessageReplyActions>
        </div>
      </div>
      {visible && deletion && (
        <MainConversationDeleteMessage
          subject={subject}
          messageId={deletion.messageId}
          trigger={deletion.trigger}
          close={() => setDeletion(undefined)}
          onDeleted={removeFromHistory}
        />
      )}
    </div>
  );
  const content = (
    <div className="relative flex h-full min-h-0 flex-col">
      {publicTimeline}
      <section
        aria-label={
          recordId
            ? t(
                canContinueRecord
                  ? "defaultConversationTitle"
                  : "workRecordTitle",
              )
            : undefined
        }
        hidden={Boolean(source)}
        className={`flex min-h-0 flex-col ${recordId ? "flex-1" : "shrink-0"}`}
      >
        {recordId && (
          <div
            ref={recordViewport}
            onScroll={scrolling.onScroll}
            data-chat-scroll-viewport
            className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]"
          >
            <div
              data-chat-message-column
              className={`mx-auto grid w-full gap-2 py-4 ${chatContentColumnClassName}`}
            >
              {history.isPending && <ChatHistoryLoadingState />}
              {history.isError &&
                !history.isFetchNextPageError &&
                (history.data && !recordAccessDenied ? (
                  <QueryRefreshError
                    error={history.error}
                    pending={history.isFetching}
                    onRetry={() => void history.refetch()}
                  />
                ) : (
                  <QueryState
                    error={history.error}
                    onRetry={() => void history.refetch()}
                  />
                ))}
              {canContinueRecord && history.hasNextPage && (
                <div className="flex justify-center">
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={history.isFetchingNextPage || positioning}
                    onClick={() => void scrolling.loadEarlier()}
                  >
                    {t("chat:loadEarlierMessages")}
                  </Button>
                </div>
              )}
              {!recordAccessDenied &&
                (invalidRecord ? (
                  <p
                    role="status"
                    className="type-ui text-content-muted px-6 py-2"
                  >
                    {t("replyUnavailable")}
                  </p>
                ) : (
                  <ChatMessageReplyActions
                    value={{ locate: locatePublicMessage }}
                  >
                    {recordRows.flatMap((row) => renderTimelineRow(row, false))}
                    {feedback}
                  </ChatMessageReplyActions>
                ))}
            </div>
          </div>
        )}
        <div hidden={Boolean(recordId) && !canContinueRecord}>
          <MainConversationComposer
            subject={subject}
            conversationId={conversationId}
            reply={recordId ? undefined : reply}
            contextReplyId={recordContext}
            replyHasTicket={Boolean(!recordId && replyTicket)}
            replySourceId={recordId ? undefined : replySourceId}
            setReply={recordId ? undefined : setReply}
            executionFailure={
              !source && latestAcceptedInput?.failureCode
                ? {
                    id: `main-conversation:${latestAcceptedInput.id}`,
                    role: "user",
                    status: "failed",
                    errorCode: latestAcceptedInput.failureCode,
                    createdAt: latestAcceptedInput.createdAt,
                  }
                : undefined
            }
            active={visible || (recordVisible && canContinueRecord)}
            ready={Boolean(history.data) && (!recordId || canContinueRecord)}
          />
        </div>
      </section>
      {source && (
        <div className="flex min-h-0 flex-1 flex-col">
          <MainConversationSource
            key={JSON.stringify([source, sourcePublicationId])}
            subject={subject}
            conversationId={conversationId}
            workChatId={source}
            pendingInputs={pendingInputs}
            publicMessageId={sourcePublicationId ?? sourceMessageId}
            readOnlyPublicationId={sourcePublicationId}
            executionId={sourceExecutionId}
            active={active}
            onOpenLink={workspaceBindings.openBrowserLink}
            onOpenFile={workspaceBindings.openAttachmentFile}
            onOpenCodeReference={workspaceBindings.openCodeReference}
            onOpenFileChange={workspaceBindings.openExecutionFileChange}
            codeReferenceRequest={codeReferenceRequest}
          />
        </div>
      )}
    </div>
  );
  return (
    <ChatWorkspaceSurface
      {...surface}
      adapter={{ ...adapter, content }}
      renderMainSummary={
        !viewingRecord
          ? (close) => (
              <MainConversationSummary
                activeWork={activeWork}
                pending={history.isPending}
                onOpenSource={(chatId, messageId) => {
                  close();
                  void navigateToSource(chatId, messageId);
                }}
              />
            )
          : undefined
      }
    />
  );
}
