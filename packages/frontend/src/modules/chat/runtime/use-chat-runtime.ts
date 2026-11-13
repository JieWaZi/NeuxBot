import { ApiError } from "@/shared/api/errors";
import {
  reportErrorNotice,
  apiErrorNoticePresentation,
  clearErrorNoticeActions,
  dismissErrorNotice,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import { i18n } from "@/shared/i18n";
import {
  useExternalStoreRuntime,
  type AppendMessage,
  type AssistantRuntime,
  type ExternalThreadQueueAdapter,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import {
  applyChatEvent,
  isTerminalEvent,
  latestContextMetrics,
  mergeAuthoritativeMessages,
  uncommittedChatMessages,
  type AllyChatMessage,
  type ChatMessageVisibility,
  type ChatMessageSource,
  type ChatPendingElicitation,
  type ChatRunMetrics,
} from "./chat-projection";
import {
  attachmentPathsOf,
  chatAttachmentAdapter,
  pathFile,
} from "./chat-attachment-adapter";
import {
  useChatHandoff,
  useChatQueueEntries,
  useChatQueueStore,
  type ChatSendSelection,
  type QueuedChatEntry,
  type PreparedChatSubmission,
  type ChatSubmissionPreparer,
} from "./chat-queue-store";
import {
  classifyChatSendFailure,
  sendFailureDisplayText,
  NO_SEND_FAILURE,
  type ChatSendFailure,
} from "./chat-send-failure";
import { createChatEventBatcher } from "./chat-event-batcher";
import { ChatLiveTracker } from "./chat-live";
import type {
  ChatInvocation,
  ChatScopeKey,
} from "../model/conversation-identity";
import {
  subscribeChat,
  type ChatEvent,
  type StreamState,
} from "@/shared/api/events";

function overlayMatchesScope(
  overlayChatId: string | undefined,
  optionsChatId: string | undefined,
  messages: readonly AllyChatMessage[],
): boolean {
  if (overlayChatId === optionsChatId) return true;
  return !optionsChatId && overlayChatId !== undefined && messages.length > 0;
}
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval,
  GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as MessageResult,
} from "@/shared/api/generated/types.gen";

/** assistant-ui 仅承担输入适配，消息正文由领域消息直接渲染。 */
const COMPOSER_MESSAGES: readonly ThreadMessageLike[] = [];
const convertComposerMessage = (message: ThreadMessageLike) => message;

export type { PreparedChatSubmission } from "./chat-queue-store";

type ChatRuntimeOptions = {
  conversationKey: ChatScopeKey;
  queueKey: string;
  /** 隐藏 Scene 暂停临时队列出队，已开始的执行继续运行。 */
  queueActive?: boolean;
  chatId?: string;
  messageSource: ChatMessageSource;
  activeExecutionId?: string;
  /** 人工待办摘要的权威日志游标，与消息历史游标分别对账。 */
  initialInteractionSequence?: number;
  initialPendingApprovals?: PendingApproval[];
  initialPendingElicitations?: ChatPendingElicitation[];
  isLoading: boolean;
  isSendDisabled: boolean;
  /** 看板首发在得到接纳结果前只允许一个输入。 */
  singleSubmission?: boolean;
  serverManagedQueue?: boolean;
  messageVisibility?: ChatMessageVisibility;
  attachmentOnlyText: string;
  referenceOnlyText?: string;
  incompleteErrorText: string;
  selection: ChatSendSelection;
  onMessageAccepted?: () => void;
  onMessageRejected?: (selection: ChatSendSelection) => void;
  onApprovalRequested?: (approval: PendingApproval) => void;
  prepareSubmission: ChatSubmissionPreparer;
  cancel: () => Promise<void>;
  onSettled?: (
    result: MessageResult,
    invocation: ChatInvocation & { current: boolean },
  ) => void;
  onStarted: (result: MessageResult, invocation: ChatInvocation) => void;
  onTerminal: () => void;
};

type ChatRuntimeResult = {
  messages: readonly AllyChatMessage[];
  runtime: AssistantRuntime;
  streamState: StreamState;
  isRunning: boolean;
  hasVisibleMessages: boolean;
  sendFailure: ChatSendFailure;
  retryFirstSubmission?: () => void;
  contextMetrics?: ChatRunMetrics;
  pendingApprovals: PendingApproval[];
  pendingElicitations: ChatPendingElicitation[];
};

type SendSnapshot = {
  conversationKey: ChatScopeKey;
  queueKey: string;
  chatId?: string;
  selection: ChatSendSelection;
  prepareSubmission: ChatSubmissionPreparer;
};

type FirstSubmissionRecovery = {
  prepared: PreparedChatSubmission;
  message: AppendMessage;
  snapshot: SendSnapshot;
};

function textOf(message: AppendMessage): string {
  return message.content
    .filter((part) => part.type === "text")
    .map((part) => part.text)
    .join("\n")
    .trim();
}

function optimisticID(role: "user" | "assistant"): string {
  return `optimistic:${role}:${crypto.randomUUID()}`;
}

function promptOf(message: AppendMessage): string {
  const text = textOf(message);
  if (text) return text;
  const names = (message.attachments ?? []).map(
    (attachment) => attachment.name,
  );
  return names.join("、");
}

function optimisticAttachmentsOf(message: AppendMessage) {
  return (message.attachments ?? []).flatMap((attachment) =>
    (attachment.content ?? []).flatMap((part) => {
      if (part.type !== "file") return [];
      return [
        {
          name: attachment.name,
          path: part.data,
          mediaType: part.mimeType,
          managed: false,
        },
      ];
    }),
  );
}

function mergeLocalMessages(
  first: readonly AllyChatMessage[],
  second: readonly AllyChatMessage[],
): AllyChatMessage[] {
  const merged = [...first];
  const indexByID = new Map(
    merged.flatMap((message, index) =>
      message.id ? [[message.id, index]] : [],
    ),
  );

  for (const message of second) {
    const index = message.id ? indexByID.get(message.id) : undefined;
    if (index === undefined) {
      if (message.id) indexByID.set(message.id, merged.length);
      merged.push(message);
      continue;
    }
    merged[index] = message;
  }

  return merged;
}

export function useChatRuntime(options: ChatRuntimeOptions): ChatRuntimeResult {
  const history = options.messageSource.snapshot;
  const sourceRef = useRef(options.messageSource);
  useLayoutEffect(() => {
    sourceRef.current = options.messageSource;
  }, [options.messageSource]);
  const queueKey = options.queueKey;
  const cancel = options.cancel;
  const prepareSubmission = options.prepareSubmission;
  const onStarted = options.onStarted;
  const onSettled = options.onSettled;
  const attachmentOnlyText = options.attachmentOnlyText;
  const referenceOnlyText = options.referenceOnlyText;
  const [overlay, setOverlay] = useState<{
    chatId?: string;
    messages: AllyChatMessage[];
  }>({ chatId: options.chatId, messages: [] });
  const [sending, setSending] = useState<{
    queueKey: string;
    submission?: PreparedChatSubmission;
  }>();
  // 保留最近一次输入命令的所有权，HTTP 结算后其延迟回调也不能接管后续输入。
  const currentSubmissionRef = useRef<PreparedChatSubmission | undefined>(
    undefined,
  );
  const recoveryRef = useRef<FirstSubmissionRecovery | undefined>(undefined);
  const [firstSubmissionRecovery, setFirstSubmissionRecovery] =
    useState<FirstSubmissionRecovery>();
  const [sendFailure, setSendFailure] = useState<{
    chatId?: string;
    value: ChatSendFailure;
    noticeId?: string;
  }>({ chatId: options.chatId, value: NO_SEND_FAILURE });
  // 这里只保存等待接纳确认的原输入及其本地错误身份。
  const pendingSubmissionsRef = useRef(
    new Map<
      string,
      { submission: PreparedChatSubmission; noticeId?: string }
    >(),
  );
  const confirmSubmission = useCallback((submissionId: string | undefined) => {
    if (!submissionId) return;
    const pending = pendingSubmissionsRef.current.get(submissionId);
    if (!pending) return;
    pendingSubmissionsRef.current.delete(submissionId);
    pending.submission.confirmAccepted?.();
    if (recoveryRef.current?.prepared === pending.submission) {
      recoveryRef.current = undefined;
      setFirstSubmissionRecovery(undefined);
    }
    const { noticeId } = pending;
    if (noticeId) {
      dismissErrorNotice(noticeId);
      setSendFailure((current) =>
        current.noticeId === noticeId
          ? { chatId: current.chatId, value: NO_SEND_FAILURE }
          : current,
      );
    }
  }, []);
  useLayoutEffect(() => {
    for (const message of history.messages)
      confirmSubmission(message.submissionId);
  }, [confirmSubmission, history.messages]);
  const [streamState, setStreamState] = useState<StreamState>("closed");
  const [streamRecoveryGeneration, setStreamRecoveryGeneration] = useState(0);
  const [approvalOverlay, setApprovalOverlay] = useState<{
    chatId?: string;
    added: PendingApproval[];
    resolvedIds: string[];
  }>({
    chatId: options.chatId,
    added: [],
    resolvedIds: [],
  });
  const [elicitationOverlay, setElicitationOverlay] = useState<{
    chatId?: string;
    added: ChatPendingElicitation[];
    resolvedIds: string[];
  }>({
    chatId: options.chatId,
    added: [],
    resolvedIds: [],
  });
  const queueStore = useChatQueueStore();
  const queuedEntries = useChatQueueEntries(queueKey);
  const handoffMessages = useChatHandoff(options.chatId);
  const overlayRef = useRef(overlay);
  const handoffMessagesRef = useRef(handoffMessages);
  const cursorRef = useRef({
    chatId: options.chatId,
    sequence: history.latestSequence,
  });
  const runningRef = useRef(false);
  const runningByQueueRef = useRef<Record<string, boolean>>({});
  const terminalRef = useRef(options.onTerminal);
  const messageAcceptedRef = useRef(options.onMessageAccepted);
  const messageRejectedRef = useRef(options.onMessageRejected);
  const approvalRequestedRef = useRef(options.onApprovalRequested);
  const notifiedApprovalIDsRef = useRef(new Set<string>());
  const interactionPendingRef = useRef(
    Boolean(
      options.initialPendingApprovals?.length ||
      options.initialPendingElicitations?.length,
    ),
  );
  const runtimeRef = useRef<AssistantRuntime | null>(null);
  const mountedRef = useRef(false);
  const observedChatKeyRef = useRef(options.conversationKey);
  useLayoutEffect(() => {
    observedChatKeyRef.current = options.conversationKey;
  }, [options.conversationKey]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  useEffect(() => {
    overlayRef.current = overlay;
  }, [overlay]);
  useEffect(() => {
    handoffMessagesRef.current = handoffMessages;
  }, [handoffMessages]);
  const pendingApprovals = useMemo(() => {
    const overlay =
      approvalOverlay.chatId === options.chatId
        ? approvalOverlay
        : { added: [], resolvedIds: [] };
    const resolved = new Set(overlay.resolvedIds);
    const approvals = new Map<string, PendingApproval>();
    for (const approval of options.initialPendingApprovals ?? []) {
      if (approval.approvalId && !resolved.has(approval.approvalId)) {
        approvals.set(approval.approvalId, approval);
      }
    }
    for (const approval of overlay.added) {
      if (
        approval.approvalId &&
        !resolved.has(approval.approvalId) &&
        (approval.requestedSequence ?? 0) >
          (options.initialInteractionSequence ?? 0)
      ) {
        approvals.set(approval.approvalId, approval);
      }
    }
    return [...approvals.values()];
  }, [
    approvalOverlay,
    options.chatId,
    options.initialPendingApprovals,
    options.initialInteractionSequence,
  ]);

  const messages = useMemo(() => {
    const overlayMessages = overlayMatchesScope(
      overlay.chatId,
      options.chatId,
      overlay.messages,
    )
      ? overlay.messages
      : [];
    const localMessages = mergeLocalMessages(handoffMessages, overlayMessages);
    const merged = mergeAuthoritativeMessages(localMessages, history.messages);
    const pendingSteers = new Set(
      queuedEntries
        .map((entry) => entry.selection.steerExecutionId)
        .filter(Boolean),
    );
    const pendingSteerSubmissions = new Set(
      queuedEntries
        .map((entry) => entry.selection.steerSubmissionId)
        .filter(Boolean),
    );
    return merged.map((message) =>
      message.role === "assistant" &&
      !message.question &&
      ((message.executionId && pendingSteers.has(message.executionId)) ||
        (message.submissionId &&
          pendingSteerSubmissions.has(message.submissionId)))
        ? { ...message, steerPending: true }
        : message,
    );
  }, [
    handoffMessages,
    options.chatId,
    history.messages,
    overlay,
    queuedEntries,
  ]);
  const pendingElicitations = useMemo(() => {
    const overlay =
      elicitationOverlay.chatId === options.chatId
        ? elicitationOverlay
        : { added: [], resolvedIds: [] };
    const resolved = new Set(overlay.resolvedIds);
    for (const message of messages) {
      const question = message.question;
      if (question?.elicitationId && question.result)
        resolved.add(question.elicitationId);
    }
    const elicitations = new Map<string, ChatPendingElicitation>();
    const interactionSequence = options.initialInteractionSequence ?? 0;
    for (const elicitation of options.initialPendingElicitations ?? []) {
      if (
        elicitation.elicitationId &&
        !resolved.has(elicitation.elicitationId)
      ) {
        elicitations.set(elicitation.elicitationId, elicitation);
      }
    }
    for (const elicitation of overlay.added) {
      if (
        elicitation.elicitationId &&
        !resolved.has(elicitation.elicitationId) &&
        (elicitation.requestedSequence ?? 0) > interactionSequence
      ) {
        elicitations.set(elicitation.elicitationId, elicitation);
      }
    }
    // 初始 SSE 游标可能已覆盖新提问。只用比详情更新的已提交历史补齐该竞态。
    for (const message of history.messages) {
      const question = message.question;
      const id = question?.elicitationId;
      const sequence = message.createdSequence ?? 0;
      if (
        id &&
        !question.result &&
        !resolved.has(id) &&
        !elicitations.has(id) &&
        sequence > interactionSequence
      ) {
        elicitations.set(id, {
          ...question,
          requestedAt: message.createdAt,
          requestedSequence: sequence,
        });
      }
    }
    return [...elicitations.values()].sort(
      (left, right) =>
        (left.requestedSequence ?? 0) - (right.requestedSequence ?? 0),
    );
  }, [
    elicitationOverlay,
    history.messages,
    messages,
    options.chatId,
    options.initialPendingElicitations,
    options.initialInteractionSequence,
  ]);
  const hasPendingInteraction =
    pendingApprovals.length > 0 || pendingElicitations.length > 0;
  useLayoutEffect(() => {
    interactionPendingRef.current =
      pendingApprovals.length > 0 || pendingElicitations.length > 0;
  }, [pendingApprovals, pendingElicitations]);

  const isRunning = Boolean(
    (sending?.queueKey === options.queueKey &&
      !messages.some(
        (message) =>
          sending.submission?.submissionId &&
          message.submissionId === sending.submission.submissionId &&
          message.role === "assistant" &&
          ["completed", "failed", "canceled"].includes(message.status ?? ""),
      )) ||
    messages.some(
      (message) =>
        message.role === "assistant" &&
        ["pending", "running"].includes(message.status ?? ""),
    ) ||
    (options.activeExecutionId &&
      !messages.some(
        (message) =>
          message.executionId === options.activeExecutionId &&
          ["completed", "failed", "canceled"].includes(message.status ?? ""),
      )),
  );
  const reconcileHistory = useCallback(() => {
    void sourceRef.current.reconcile().catch(() => {});
  }, []);
  useEffect(() => () => clearErrorNoticeActions(queueKey), [queueKey]);

  const currentSendFailure =
    sendFailure.chatId === options.chatId ? sendFailure.value : NO_SEND_FAILURE;

  const submit = useCallback(
    async function submitMessage(
      message: AppendMessage,
      snapshot: SendSnapshot,
      original?: PreparedChatSubmission,
    ) {
      const attachmentPaths = attachmentPathsOf(message);
      const text =
        textOf(message) ||
        (attachmentPaths.length
          ? attachmentOnlyText
          : snapshot.selection.codeReferences?.length
            ? (referenceOnlyText ?? attachmentOnlyText)
            : "");
      if (!text) return;

      const now = Date.now();
      const prepared =
        original ??
        snapshot.prepareSubmission(
          text,
          snapshot.chatId,
          attachmentPaths,
          snapshot.selection,
        );
      currentSubmissionRef.current = prepared;
      if (
        prepared.submissionId &&
        (!original || !pendingSubmissionsRef.current.has(prepared.submissionId))
      )
        pendingSubmissionsRef.current.set(prepared.submissionId, {
          submission: prepared,
        });
      const operation = `send:${snapshot.queueKey}`;
      const recovery = captureErrorNoticeRecovery(operation);
      const userID = optimisticID("user");
      const assistantID = optimisticID("assistant");
      runningRef.current = true;
      runningByQueueRef.current[snapshot.queueKey] = true;
      setSending({
        queueKey: snapshot.queueKey,
        submission: prepared,
      });
      setSendFailure({
        chatId: snapshot.chatId,
        value: NO_SEND_FAILURE,
      });
      const currentOverlay = overlayRef.current;
      const optimisticMessages = [
        ...(currentOverlay.chatId === snapshot.chatId
          ? currentOverlay.messages
          : []),
        {
          id: userID,
          submissionId: prepared.submissionId,
          role: "user",
          content: text,
          attachments: optimisticAttachmentsOf(message),
          plugins: snapshot.selection.selectedPlugins?.map((plugin) => ({
            ...plugin,
          })),
          skills: (snapshot.selection.selectedSkills ?? []).map((skill) => ({
            ...skill,
          })),
          connectors: (snapshot.selection.selectedConnectors ?? []).map(
            (connector) => ({ ...connector }),
          ),
          codeReferences: (snapshot.selection.codeReferences ?? []).map(
            (reference) => ({ ...reference }),
          ),
          createdAt: now,
          updatedAt: now,
          optimistic: true,
        },
        {
          id: assistantID,
          submissionId: prepared.submissionId,
          role: "assistant",
          content: "",
          status: "pending",
          preparingSession: !snapshot.chatId,
          steerExecutionId: snapshot.selection.steerExecutionId,
          createdAt: now,
          updatedAt: now,
          optimistic: true,
        },
      ] satisfies AllyChatMessage[];
      const optimisticOverlay = {
        chatId: snapshot.chatId,
        messages: optimisticMessages,
      };
      overlayRef.current = optimisticOverlay;
      setOverlay(optimisticOverlay);

      try {
        const result = await prepared.accept();
        if (prepared.submissionId) confirmSubmission(prepared.submissionId);
        else prepared.confirmAccepted?.();
        const execution = result.execution;
        const resultChatID = result.chat?.id ?? snapshot.chatId;
        const committedMessages: AllyChatMessage[] = execution
          ? optimisticMessages.map((item) => {
              if (item.id === userID) {
                return {
                  ...item,
                  id: execution.userMessageId ?? item.id,
                  executionId: execution.id,
                  optimistic: false,
                };
              }
              if (item.id === assistantID) {
                return {
                  ...item,
                  id: execution.id ?? item.id,
                  executionId: execution.id,
                  status: execution.status ?? "pending",
                  optimistic: false,
                };
              }
              return item;
            })
          : snapshot.chatId
            ? optimisticMessages.filter(
                (item) => item.id !== userID && item.id !== assistantID,
              )
            : optimisticMessages;
        const committedOverlay = {
          chatId: resultChatID,
          messages: committedMessages,
        };
        const invocation: ChatInvocation = {
          chatId: snapshot.chatId,
          key: snapshot.conversationKey,
          queueKey: snapshot.queueKey,
        };
        const current =
          mountedRef.current &&
          observedChatKeyRef.current === snapshot.conversationKey;
        if (current) {
          // 推送可能先于接纳回执到达；只替换本次乐观身份，保留已经投影的事实。
          setOverlay((previous) => {
            const observed =
              previous.chatId === resultChatID
                ? previous.messages.filter(
                    (item) => item.id !== userID && item.id !== assistantID,
                  )
                : [];
            const next = {
              ...committedOverlay,
              messages: mergeAuthoritativeMessages(committedMessages, observed),
            };
            overlayRef.current = next;
            return next;
          });
        }
        if (resultChatID && (resultChatID !== snapshot.chatId || !current)) {
          queueStore.setHandoff(resultChatID, committedMessages);
        }
        if (current) {
          cursorRef.current = {
            chatId: resultChatID,
            sequence:
              cursorRef.current.chatId === resultChatID
                ? Math.max(
                    cursorRef.current.sequence,
                    sourceRef.current.snapshot.latestSequence,
                  )
                : sourceRef.current.snapshot.latestSequence,
          };
        }
        if (
          resultChatID &&
          resultChatID !== snapshot.queueKey &&
          queueStore.getEntries(snapshot.queueKey).length
        ) {
          queueStore.migrate(snapshot.queueKey, resultChatID);
        }
        resolveErrorNoticeRecovery(recovery);
        onSettled?.(result, { ...invocation, current });
        if (current) {
          onStarted(result, invocation);
        }
      } catch (error) {
        // 已被权威事实接纳，或已有后续命令取得当前输入所有权时，旧 HTTP 失败不回退界面。
        if (
          !mountedRef.current ||
          currentSubmissionRef.current !== prepared ||
          (prepared.submissionId &&
            !pendingSubmissionsRef.current.has(prepared.submissionId))
        )
          return;
        runningByQueueRef.current[snapshot.queueKey] = false;
        const current =
          mountedRef.current &&
          observedChatKeyRef.current === snapshot.conversationKey;
        if (current) runningRef.current = false;
        const failure = classifyChatSendFailure(error);
        const needsReceipt = prepared.hasUnconfirmedReceipt?.() === true;
        if (needsReceipt) {
          const next = { prepared, message, snapshot };
          recoveryRef.current = next;
          setFirstSubmissionRecovery(next);
        } else if (recoveryRef.current?.prepared === prepared) {
          recoveryRef.current = undefined;
          setFirstSubmissionRecovery(undefined);
        }
        // 只重放服务端明确拒绝的限流请求；响应丢失时先核对权威历史。
        const canRetry =
          current &&
          !needsReceipt &&
          error instanceof ApiError &&
          error.status === 429;
        const canCheck =
          current &&
          snapshot.chatId &&
          (failure.kind === "unknown" ||
            failure.kind === "active-execution" ||
            failure.kind === "create-request-in-progress");
        const noticeId = `send:${userID}`;
        if (prepared.submissionId) {
          const pending = pendingSubmissionsRef.current.get(
            prepared.submissionId,
          );
          if (pending) pending.noticeId = noticeId;
        }
        if (!needsReceipt)
          reportErrorNotice(
            {
              id: noticeId,
              scope: snapshot.queueKey,
              lifetime: "session",
              operation,
              ...apiErrorNoticePresentation(
                error,
                sendFailureDisplayText(failure, (key) =>
                  i18n.t(key, { ns: "chat" }),
                ),
              ),
              action: canRetry ? "retry" : canCheck ? "check" : undefined,
            },
            canRetry
              ? async () => {
                  if (
                    !mountedRef.current ||
                    observedChatKeyRef.current !== snapshot.conversationKey ||
                    currentSubmissionRef.current !== prepared ||
                    runningRef.current
                  )
                    return;
                  await submitMessage(message, snapshot);
                }
              : canCheck
                ? () => {
                    if (
                      mountedRef.current &&
                      observedChatKeyRef.current === snapshot.conversationKey
                    )
                      reconcileHistory();
                    terminalRef.current();
                  }
                : undefined,
          );
        if (current) {
          const authoritativeIDs = new Set<string>(
            sourceRef.current.snapshot.messages.flatMap((message) =>
              message.id ? [message.id] : [],
            ),
          );
          const failedOverlay = {
            chatId: snapshot.chatId,
            messages: overlayRef.current.messages.filter(
              (item) =>
                item.id !== userID &&
                item.id !== assistantID &&
                (!item.id || !authoritativeIDs.has(item.id)),
            ),
          };
          overlayRef.current = failedOverlay;
          setOverlay(failedOverlay);
          setSendFailure({
            chatId: snapshot.chatId,
            value: failure,
            noticeId,
          });
        }
        window.setTimeout(() => {
          if (
            !mountedRef.current ||
            observedChatKeyRef.current !== snapshot.conversationKey ||
            currentSubmissionRef.current !== prepared ||
            (prepared.submissionId &&
              !pendingSubmissionsRef.current.has(prepared.submissionId))
          ) {
            return;
          }
          if (
            !needsReceipt &&
            prepared.hasUnconfirmedReceipt &&
            prepared.submissionId
          )
            pendingSubmissionsRef.current.delete(prepared.submissionId);
          messageRejectedRef.current?.(snapshot.selection);
          const composer = runtimeRef.current?.thread?.composer;
          composer?.setText(text);
          if (composer && composer.getState().attachments.length === 0) {
            for (const attachmentPath of attachmentPaths) {
              void composer.addAttachment(pathFile(attachmentPath));
            }
          }
        }, 0);
        if (
          !needsReceipt &&
          failure.kind === "unknown" &&
          mountedRef.current &&
          observedChatKeyRef.current === snapshot.conversationKey
        ) {
          reconcileHistory();
          terminalRef.current();
        }
      } finally {
        setSending((current) =>
          current?.submission === prepared ? undefined : current,
        );
      }
    },
    [
      onStarted,
      attachmentOnlyText,
      onSettled,
      referenceOnlyText,
      confirmSubmission,
      reconcileHistory,
      queueStore,
    ],
  );

  const currentSnapshot = useMemo<SendSnapshot>(
    () => ({
      conversationKey: options.conversationKey,
      queueKey: options.queueKey,
      chatId: options.chatId,
      prepareSubmission,
      selection: {
        ...options.selection,
        skillIds: options.selection.skillIds
          ? [...options.selection.skillIds]
          : undefined,
        selectedSkills: options.selection.selectedSkills?.map((skill) => ({
          ...skill,
        })),
        pluginIds: options.selection.pluginIds
          ? [...options.selection.pluginIds]
          : undefined,
        selectedPlugins: options.selection.selectedPlugins?.map((plugin) => ({
          ...plugin,
        })),
        connectorIds: options.selection.connectorIds
          ? [...options.selection.connectorIds]
          : undefined,
        selectedConnectors: options.selection.selectedConnectors?.map(
          (connector) => ({ ...connector }),
        ),
        codeReferences: options.selection.codeReferences?.map((reference) => ({
          ...reference,
        })),
      },
    }),
    [
      options.chatId,
      options.conversationKey,
      options.queueKey,
      options.selection,
      prepareSubmission,
    ],
  );
  const advanceQueue = useCallback(
    (queueKey: string) => {
      if (options.queueActive === false) return;
      if (recoveryRef.current) return;
      if (queueKey !== options.queueKey) return;
      if (runningByQueueRef.current[queueKey]) return;
      if (interactionPendingRef.current) return;
      const head = queueStore.take(queueKey);
      if (!head) return;
      const steerExecutionId =
        head.selection.steerExecutionId ??
        (head.selection.steerSubmissionId
          ? [
              ...sourceRef.current.snapshot.messages,
              ...overlayRef.current.messages,
              ...handoffMessagesRef.current,
            ].find(
              (message) =>
                message.role === "assistant" &&
                message.submissionId === head.selection.steerSubmissionId &&
                message.executionId,
            )?.executionId
          : undefined);
      void submit(head.message, {
        conversationKey: options.conversationKey,
        queueKey,
        chatId: head.chatId,
        selection: { ...head.selection, steerExecutionId },
        prepareSubmission: head.prepareSubmission ?? prepareSubmission,
      });
    },
    [
      options.conversationKey,
      options.queueActive,
      options.queueKey,
      queueStore,
      submit,
      prepareSubmission,
    ],
  );

  useEffect(() => {
    runningRef.current = isRunning;
    terminalRef.current = options.onTerminal;
    messageAcceptedRef.current = options.onMessageAccepted;
    messageRejectedRef.current = options.onMessageRejected;
    approvalRequestedRef.current = options.onApprovalRequested;

    const wasRunning = runningByQueueRef.current[options.queueKey] ?? false;
    runningByQueueRef.current[options.queueKey] = isRunning;
    if (wasRunning && !isRunning) {
      const timer = window.setTimeout(() => advanceQueue(options.queueKey), 0);
      return () => window.clearTimeout(timer);
    }
  }, [
    advanceQueue,
    isRunning,
    history.latestSequence,
    history.messages,
    options.onMessageAccepted,
    options.onMessageRejected,
    options.onApprovalRequested,
    options.onTerminal,
    options.queueKey,
  ]);

  useEffect(() => {
    setOverlay((current) => {
      if (current.chatId !== options.chatId) return current;
      const pending = uncommittedChatMessages(
        current.messages,
        history.messages,
      );
      return pending.length === current.messages.length
        ? current
        : { ...current, messages: pending };
    });
  }, [options.chatId, history.messages]);

  useEffect(() => {
    if (!options.chatId || handoffMessages.length === 0) return;
    const handoffIsAuthoritative =
      history.ready &&
      uncommittedChatMessages(handoffMessages, history.messages).length === 0;
    if (handoffIsAuthoritative) {
      queueStore.clearHandoff(options.chatId);
    }
  }, [
    handoffMessages,
    options.chatId,
    history.ready,
    history.messages,
    queueStore,
  ]);

  useEffect(() => {
    if (
      hasPendingInteraction ||
      isRunning ||
      queueStore.getEntries(options.queueKey).length === 0
    ) {
      return;
    }
    const timer = window.setTimeout(() => advanceQueue(options.queueKey), 0);
    return () => window.clearTimeout(timer);
  }, [
    advanceQueue,
    hasPendingInteraction,
    isRunning,
    options.queueKey,
    queueStore,
    queuedEntries,
  ]);

  useEffect(() => {
    if (options.queueActive === false || !options.chatId || !history.ready) {
      return;
    }
    let subscribed = true;
    const subscribedChatKey = options.conversationKey;
    const isCurrentSubscription = () =>
      subscribed &&
      mountedRef.current &&
      observedChatKeyRef.current === subscribedChatKey;
    let pendingDurableEvents: ChatEvent[] = [];
    let historyMerged = false;
    let liveRecovering = false;
    let liveTracker = new ChatLiveTracker();
    const eventBatcher = createChatEventBatcher((events) => {
      if (!isCurrentSubscription()) return;
      // 原始持久事件使用递增的稀疏游标；UI 批次会折叠同一消息的中间快照。
      // 接收不等于投影，隐藏时丢弃的批次不能推进原历史游标。
      if (pendingDurableEvents.length > 0 && options.chatId) {
        const sequence = sourceRef.current.commit(pendingDurableEvents);
        historyMerged = sequence !== undefined;
        if (sequence !== undefined) cursorRef.current.sequence = sequence;
        pendingDurableEvents = [];
      }
      setOverlay((current) => {
        const localMessages = mergeLocalMessages(
          handoffMessagesRef.current,
          current.chatId === options.chatId ? current.messages : [],
        );
        let projected = localMessages;
        for (const event of events) {
          if (event.live) {
            const baseline = sourceRef.current.snapshot.messages.find(
              (message) => message.id === event.live?.messageId,
            );
            if (baseline) {
              const index = projected.findIndex(
                (message) => message.id === baseline.id,
              );
              const local = projected[index];
              if (!local) projected = [...projected, baseline];
              else if (
                (baseline.liveRevision ?? baseline.revision ?? -1) >
                (local.liveRevision ?? local.revision ?? -1)
              ) {
                projected = [...projected];
                projected[index] = {
                  ...baseline,
                  runMetrics: baseline.runMetrics ?? local.runMetrics,
                };
              }
            }
          }
          projected = applyChatEvent(
            projected,
            event,
            options.messageVisibility,
          );
        }
        return {
          chatId: options.chatId,
          messages: projected,
        };
      });
    });
    if (cursorRef.current.chatId !== options.chatId) {
      cursorRef.current = {
        chatId: options.chatId,
        sequence: sourceRef.current.snapshot.latestSequence,
      };
    } else {
      cursorRef.current.sequence = Math.max(
        cursorRef.current.sequence,
        sourceRef.current.snapshot.latestSequence,
      );
    }
    const streamOperation = `stream:${options.chatId}`;
    let streamRecovery = captureErrorNoticeRecovery(streamOperation);
    const unsubscribe = subscribeChat(options.chatId, {
      after: cursorRef.current.sequence,
      isTerminal: () => false,
      reconcileCursor: async (signal) => {
        const sequence = await sourceRef.current.reconcile(signal);
        if (isCurrentSubscription()) {
          if (sequence !== undefined) cursorRef.current.sequence = sequence;
          // 对账先于缓冲事件交付完成；重连后的代次和帧号从新基线核验。
          liveRecovering = false;
          liveTracker = new ChatLiveTracker();
        }
        return sequence;
      },
      onEvent: (event) => {
        if (!isCurrentSubscription()) return;
        if (event.live) {
          if (liveRecovering) return;
          const id = event.live.messageId;
          const baseline = [
            [...pendingDurableEvents]
              .reverse()
              .find((entry) => entry.message?.id === id)?.message,
            overlayRef.current.messages.find((message) => message.id === id),
            sourceRef.current.snapshot.messages.find(
              (message) => message.id === id,
            ),
          ]
            .filter((message): message is AllyChatMessage => Boolean(message))
            .sort(
              (a, b) =>
                (b.liveRevision ?? b.revision ?? -1) -
                (a.liveRevision ?? a.revision ?? -1),
            )[0];
          const decision = liveTracker.accept(event.live, baseline);
          if (decision === "duplicate") return;
          if (decision === "recover") {
            liveRecovering = true;
            eventBatcher.flush();
            void sourceRef.current
              .reconcile()
              .then((sequence) => {
                if (isCurrentSubscription()) {
                  if (sequence !== undefined)
                    cursorRef.current.sequence = sequence;
                  setStreamRecoveryGeneration((value) => value + 1);
                }
              })
              .catch(() => {
                if (isCurrentSubscription()) {
                  liveRecovering = false;
                  setStreamState("unavailable");
                }
              });
            return;
          }
        }
        resolveErrorNoticeRecovery(streamRecovery);
        if (!event.transient) {
          confirmSubmission(event.message?.submissionId);
          pendingDurableEvents.push(event);
        }
        const terminal = isTerminalEvent(event);
        const approvalRequest = event.approvalRequest;
        if (
          event.type === "approval.requested" &&
          approvalRequest?.approvalId
        ) {
          const approval: PendingApproval = {
            approvalId: approvalRequest.approvalId,
            toolCallId: approvalRequest.toolCallId,
            title: approvalRequest.title,
            summary: approvalRequest.summary,
            kind: approvalRequest.kind,
            locations: approvalRequest.locations?.map((location) => ({
              path: location.path,
              line: location.line,
            })),
            options: approvalRequest.options?.map((option) => ({
              id: option.id,
              kind: option.kind,
              name: option.name,
            })),
            requestedAt: event.recordedAt,
            requestedSequence: event.sequence,
          };
          setApprovalOverlay((current) => {
            const added =
              current.chatId === options.chatId ? current.added : [];
            if (added.some((item) => item.approvalId === approval.approvalId)) {
              return current;
            }
            return {
              chatId: options.chatId,
              added: [...added, approval],
              resolvedIds:
                current.chatId === options.chatId ? current.resolvedIds : [],
            };
          });
          interactionPendingRef.current = true;
          if (!notifiedApprovalIDsRef.current.has(approvalRequest.approvalId)) {
            notifiedApprovalIDsRef.current.add(approvalRequest.approvalId);
            approvalRequestedRef.current?.(approval);
          }
        }
        const approvalResolution = event.approvalResolution;
        const resolvedApprovalId = approvalResolution?.approvalId;
        if (event.type === "approval.resolved" && resolvedApprovalId) {
          setApprovalOverlay((current) => {
            const sameChat = current.chatId === options.chatId;
            return {
              chatId: options.chatId,
              added: sameChat
                ? current.added.filter(
                    (item) => item.approvalId !== resolvedApprovalId,
                  )
                : [],
              resolvedIds: Array.from(
                new Set([
                  ...(sameChat ? current.resolvedIds : []),
                  resolvedApprovalId,
                ]),
              ),
            };
          });
        }
        const elicitationRequest = event.elicitationRequest;
        if (
          event.type === "elicitation.requested" &&
          elicitationRequest?.elicitationId
        ) {
          interactionPendingRef.current = true;
          const elicitation: ChatPendingElicitation = {
            elicitationId: elicitationRequest.elicitationId,
            allowSkip: elicitationRequest.allowSkip,
            toolCallId: elicitationRequest.toolCallId,
            mode: elicitationRequest.mode,
            message: elicitationRequest.message,
            requestedSchema: elicitationRequest.requestedSchema,
            url: elicitationRequest.url,
            requestedAt: event.recordedAt,
            requestedSequence: event.sequence,
          };
          setElicitationOverlay((current) => {
            const added =
              current.chatId === options.chatId ? current.added : [];
            if (
              added.some(
                (item) => item.elicitationId === elicitation.elicitationId,
              )
            ) {
              return current;
            }
            return {
              chatId: options.chatId,
              added: [...added, elicitation],
              resolvedIds:
                current.chatId === options.chatId ? current.resolvedIds : [],
            };
          });
        }
        const elicitationResolution = event.elicitationResolution;
        const resolvedElicitationId = elicitationResolution?.elicitationId;
        if (event.type === "elicitation.resolved" && resolvedElicitationId) {
          setElicitationOverlay((current) => {
            const sameChat = current.chatId === options.chatId;
            return {
              chatId: options.chatId,
              added: sameChat
                ? current.added.filter(
                    (item) => item.elicitationId !== resolvedElicitationId,
                  )
                : [],
              resolvedIds: Array.from(
                new Set([
                  ...(sameChat ? current.resolvedIds : []),
                  resolvedElicitationId,
                ]),
              ),
            };
          });
        }
        eventBatcher.push(event, terminal);
        if (terminal) {
          runningRef.current = false;
          if (!historyMerged) reconcileHistory();
          terminalRef.current();
        }
      },
      onReconnect: () => {
        if (!isCurrentSubscription()) return;
        resolveErrorNoticeRecovery(streamRecovery);
        // 来源快照与游标已由同一恢复端口提交；保留其后收到的有效事件批次。
        terminalRef.current();
      },
      onUnavailable: () => {
        if (!isCurrentSubscription()) return;
        reportErrorNotice({
          id: `stream:${options.chatId}`,
          scope: `chat:${options.chatId}`,
          operation: streamOperation,
          message: i18n.t("runtimeFailure.networkError", { ns: "chat" }),
          messageSource: "public",
        });
        streamRecovery = captureErrorNoticeRecovery(streamOperation);
        setApprovalOverlay({
          chatId: options.chatId,
          added: [],
          resolvedIds: [],
        });
        setElicitationOverlay({
          chatId: options.chatId,
          added: [],
          resolvedIds: [],
        });
        terminalRef.current();
      },
      onState: (state) => {
        if (!isCurrentSubscription()) return;
        if (state === "reconnecting") {
          setApprovalOverlay({
            chatId: options.chatId,
            added: [],
            resolvedIds: [],
          });
          setElicitationOverlay({
            chatId: options.chatId,
            added: [],
            resolvedIds: [],
          });
        }
        setStreamState(state);
      },
    });
    return () => {
      subscribed = false;
      unsubscribe();
      eventBatcher.close();
    };
  }, [
    options.chatId,
    options.conversationKey,
    history.ready,
    options.messageVisibility,
    options.queueActive,
    reconcileHistory,
    confirmSubmission,
    streamRecoveryGeneration,
  ]);

  const onNew = useCallback(
    async (message: AppendMessage) => {
      if (
        interactionPendingRef.current ||
        recoveryRef.current ||
        (options.singleSubmission && runningRef.current)
      )
        return;
      messageAcceptedRef.current?.();
      return submit(message, currentSnapshot);
    },
    [currentSnapshot, options.singleSubmission, submit],
  );
  const queue = useMemo<ExternalThreadQueueAdapter>(() => {
    const currentAssistant = messages.findLast(
      (item) =>
        item.role === "assistant" &&
        ["pending", "running"].includes(item.status ?? ""),
    );
    const steerTarget = isRunning
      ? {
          steerExecutionId: currentAssistant
            ? currentAssistant.executionId
            : options.activeExecutionId,
          steerSubmissionId: currentAssistant?.submissionId,
        }
      : {};
    return {
      items: options.serverManagedQueue
        ? []
        : queuedEntries.map(({ id, prompt }) => ({ id, prompt })),
      enqueue: (message, { steer }) => {
        if (
          interactionPendingRef.current ||
          recoveryRef.current ||
          (options.singleSubmission && runningRef.current)
        )
          return;
        if (options.serverManagedQueue) {
          messageAcceptedRef.current?.();
          void submit(message, currentSnapshot);
          return;
        }
        const entry: QueuedChatEntry = {
          id: crypto.randomUUID(),
          prompt: promptOf(message),
          message,
          chatId: currentSnapshot.chatId,
          selection: {
            ...currentSnapshot.selection,
            ...(steer ? steerTarget : {}),
          },
          prepareSubmission: currentSnapshot.prepareSubmission,
        };
        messageAcceptedRef.current?.();
        const current = queueStore.getEntries(queueKey);
        const queueIsRunning = runningByQueueRef.current[queueKey] ?? isRunning;
        if (
          options.queueActive !== false &&
          !queueIsRunning &&
          current.length === 0
        ) {
          void submit(message, currentSnapshot);
          return;
        }
        queueStore.enqueue(queueKey, entry, steer);
        if (!queueIsRunning) advanceQueue(queueKey);
        else if (steer) void cancel();
      },
      steer: (queueItemID) => {
        if (interactionPendingRef.current) return;
        const current = queueStore.getEntries(queueKey);
        const target = current.find((item) => item.id === queueItemID);
        if (!target) return;
        queueStore.promote(queueKey, queueItemID, steerTarget);
        if (isRunning) void cancel();
        else advanceQueue(queueKey);
      },
      remove: (queueItemID) => {
        queueStore.remove(queueKey, queueItemID);
      },
      clear: (reason) => {
        if (reason === "cancel-run") return;
        queueStore.clear(queueKey);
      },
    };
  }, [
    advanceQueue,
    currentSnapshot,
    isRunning,
    cancel,
    queueKey,
    queuedEntries,
    messages,
    options.activeExecutionId,
    queueStore,
    submit,
    options.serverManagedQueue,
    options.singleSubmission,
    options.queueActive,
  ]);

  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages: COMPOSER_MESSAGES,
    convertMessage: convertComposerMessage,
    isRunning,
    isLoading: options.isLoading,
    isSendDisabled:
      options.isSendDisabled ||
      hasPendingInteraction ||
      (options.singleSubmission && isRunning) ||
      Boolean(firstSubmissionRecovery),
    queue,
    onNew,
    onCancel: options.cancel,
    adapters: { attachments: chatAttachmentAdapter },
    unstable_capabilities: { copy: true },
  });
  useEffect(() => {
    runtimeRef.current = runtime;
    return () => {
      if (runtimeRef.current === runtime) runtimeRef.current = null;
    };
  }, [runtime]);

  return {
    runtime,
    messages,
    streamState: isRunning ? streamState : "closed",
    isRunning,
    hasVisibleMessages: messages.length > 0,
    sendFailure: currentSendFailure,
    retryFirstSubmission:
      firstSubmissionRecovery?.snapshot.conversationKey ===
      options.conversationKey
        ? () => {
            const recovery = recoveryRef.current;
            if (
              !recovery ||
              recovery !== firstSubmissionRecovery ||
              runningRef.current ||
              options.queueActive === false
            )
              return;
            messageAcceptedRef.current?.();
            void submit(recovery.message, recovery.snapshot, recovery.prepared);
          }
        : undefined,
    contextMetrics: latestContextMetrics(messages),
    pendingApprovals,
    pendingElicitations,
  };
}
