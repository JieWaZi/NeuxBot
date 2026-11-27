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
import { ChatHistoryLoadingState } from "../components/chat-history-loading-state";
import { useChatMessageLocator } from "../components/chat-message-locator";
import { ChatMessageReplyActions } from "../components/chat-message-reply";
import { SettledChatQuestion } from "../components/chat-timeline-interactions";
import { ChatWorkspaceSurface } from "../components/chat-workspace-surface";
import { PublicConversationMessage } from "../components/main-conversation-message";
import { MainConversationDeleteMessage } from "../components/main-conversation-delete-message";
import { MainConversationSource } from "../components/main-conversation-source";
import { MainConversationWait } from "../components/main-conversation-wait";
import { chatContentColumnClassName } from "../components/chat-layout";
import { MainConversationComposer } from "../components/main-conversation-composer";
import { useChatViewportScroll } from "../components/use-chat-viewport-scroll";
import { useChatHistoryScroll } from "../components/use-chat-history-scroll";
import { mainConversationMessageGroups } from "../model/main-conversation-message-group";
import { useMainConversationSubmissions } from "../model/main-conversation-submission";
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
  const [reply, setReply] = useState<ChatReplyReference>();
  const [deletion, setDeletion] = useState<{
    messageId: string;
    trigger: HTMLButtonElement;
  }>();
  const visible = active && !source;
  const [replyLocation, setReplyLocation] = useState<{
    replyId: string;
    targetId: string;
    keyboardTrigger?: HTMLButtonElement;
    request: number;
  }>();
  if (!visible && replyLocation) setReplyLocation(undefined);
  const viewport = useRef<HTMLDivElement>(null);
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
  const workQueryKeys = source
    ? [sourceHistoryOptions(subject, source).queryKey]
    : visible
      ? (activeWork ?? []).flatMap((work) =>
          work.workChatId && (work.waitingApproval || work.waitingElicitation)
            ? [sourceScopeOptions(subject, work.workChatId).queryKey]
            : [],
        )
      : [];
  const { messages: committed, byId: committedById } = useMemo(() => {
    const byId = new Map<PublicMessage["id"], PublicMessage>();
    for (const page of history.data?.pages.toReversed() ?? []) {
      for (const row of page.messages ?? []) byId.set(row.id, row);
    }
    return { messages: [...byId.values()], byId };
  }, [history.data]);
  const conversationId = history.data?.pages[0]?.id;
  const reading = useMainConversationReading({
    ...subject,
    conversationId: committed.some((row) => row.readable)
      ? conversationId
      : undefined,
    active: visible,
  });
  const { messages, waiting } = useMainConversationSubmissions(
    subject,
    conversationId,
    committed,
    history.hasNextPage,
    client.getQueryState(options.queryKey)?.dataUpdateCount ?? 0,
  );
  const byId =
    messages === committed
      ? committedById
      : new Map(messages.map((row) => [row.id, row]));
  const {
    adapter,
    surface,
    bindings: workspaceBindings,
    codeReferenceRequest,
  } = useMainConversationWorkspace({
    subject,
    source,
    active,
    messages: committed,
    activeWork,
  });
  const { rows, trailingWork } = useMemo(
    () => mainConversationMessageGroups(messages, activeWork),
    [messages, activeWork],
  );
  const scope = subject.subjectType + ":" + subject.subjectId;
  const resultMessageId = visible
    ? params.get("message") || undefined
    : undefined;
  const requestTarget = visible
    ? mainConversationRequestTarget(params)
    : undefined;
  const locationTargetId = replyLocation?.targetId ?? resultMessageId;
  const locationKey = replyLocation
    ? JSON.stringify([scope, replyLocation.request])
    : resultMessageId || requestTarget
      ? JSON.stringify([scope, location.key])
      : undefined;
  const scrolling = useChatHistoryScroll({
    scope: locationKey ?? scope,
    viewport,
    hasMore: history.hasNextPage,
    enabled: visible,
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
    viewport,
    active: visible && !history.isPending,
    routeKey: locationKey,
    messageId: locationTargetId,
    requestedInteractionId: requestTarget?.requestId,
    requestedInteractionScope: requestTarget?.workChatId,
    cancelReplyLocation: replyLocation
      ? () => setReplyLocation(undefined)
      : undefined,
    keyboardReplyTrigger: replyLocation?.keyboardTrigger,
    hasEarlierMessages: history.hasNextPage,
    inlineReplyFeedback: Boolean(replyLocation),
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
      visible && locationTargetId
        ? { targetId: locationTargetId, positioning }
        : null;
    return () => {
      focusedReply.current = null;
    };
  }, [positioning, locationTargetId, visible]);
  const visibleTarget = useCallback(() => {
    const container = viewport.current;
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
  }, []);
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
  useQueryEvents({
    topic: "main-conversations",
    resource: { kind: subjectType, id: subjectId },
    eventTypes: ["main-conversations.changed"],
    queryKeys: [options.queryKey, ...workQueryKeys],
    active,
    enabled: active,
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
  });
  usePresentedChatBoundary({
    root: viewport,
    active: visible && !positioning && reading.state?.complete === true,
    present: reading.present,
    revision: reading.revision,
  });
  const openSource = (workChatId: string) =>
    navigate(
      mainConversationHref({
        kind: subject.subjectType,
        id: subject.subjectId,
        workChatId,
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
  ) => (
    <MainConversationWait
      key={`work:${work.workChatId}:${work.executionId}`}
      subject={subject}
      active={visible}
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
      openSource={openSource}
    />
  );
  // 保留请求卡的 DOM 和输入值，加载原交办所在旧页时只移动它的位置。
  const timeline = [
    ...rows.flatMap(
      ({ row, continued, replyActivity, work, showWorkRecord }) => [
        <PublicConversationMessage
          key={`message:${row.id}`}
          subject={subject}
          row={row}
          continued={continued}
          replyActivity={replyActivity}
          quoted={
            row.replyToMessageId ? byId.get(row.replyToMessageId) : undefined
          }
          quoteFeedback={
            row.id === replyLocation?.replyId ? feedback : undefined
          }
          active={visible}
          select={setReply}
          openSource={openSource}
          showWorkRecord={showWorkRecord}
          sourceBusy={activeWorkSources.has(row.sourceChatId)}
          onMarkUnread={
            reading.state?.complete
              ? (messageId) => {
                  void reading
                    .markUnread(messageId)
                    .catch(() => toast.error(t("reading.failed")));
                }
              : undefined
          }
          onDelete={(messageId, trigger) => setDeletion({ messageId, trigger })}
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
        work && renderWork(work, row.role === "assistant"),
      ],
    ),
    ...trailingWork.map((work) => renderWork(work, false)),
  ];
  const content = (
    <div className="relative flex h-full min-h-0 flex-col">
      <div hidden={Boolean(source)} className="flex min-h-0 flex-1 flex-col">
        <div
          ref={viewport}
          onScroll={scrolling.onScroll}
          data-chat-scroll-viewport
          className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]"
        >
          <div
            data-chat-message-column
            className={`mx-auto grid w-full gap-2 py-4 ${chatContentColumnClassName}`}
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
            {waiting && !activeWork?.length && (
              <MainConversationWait
                subject={subject}
                active={visible}
                openSource={openSource}
              />
            )}
          </div>
        </div>
        <MainConversationComposer
          subject={subject}
          conversationId={conversationId}
          reply={reply}
          replySourceId={
            reply?.messageId
              ? byId.get(reply.messageId)?.sourceChatId
              : undefined
          }
          setReply={setReply}
          active={visible}
          ready={Boolean(history.data)}
        />
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
      {source && (
        <div className="flex min-h-0 flex-1 flex-col">
          <MainConversationSource
            key={source}
            subject={subject}
            conversationId={conversationId}
            workChatId={source}
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
    <ChatWorkspaceSurface {...surface} adapter={{ ...adapter, content }} />
  );
}
