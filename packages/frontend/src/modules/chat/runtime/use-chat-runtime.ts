import { retryChatExecution } from "@/shared/api/generated/sdk.gen";
import { ApiError } from "@/shared/api/errors";
import {
  reportErrorNotice,
  clearErrorNoticeActions,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import { i18n } from "@/shared/i18n";
import {
  useExternalStoreRuntime,
  type AppendMessage,
  type AssistantRuntime,
  type ExternalThreadQueueAdapter,
  type ThreadSuggestion,
} from "@assistant-ui/react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";

import {
  applyChatEvent,
  isTerminalEvent,
  latestContextMetrics,
  mergeAuthoritativeMessages,
  toThreadMessage,
  type AllyChatMessage,
  type ChatMessageVisibility,
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
} from "./chat-queue-store";
import {
  classifyChatSendFailure,
  sendFailureDisplayText,
  NO_SEND_FAILURE,
  type ChatSendFailure,
} from "./chat-send-failure";
import { createChatEventBatcher } from "./chat-event-batcher";
import type {
  ChatInvocation,
  ChatScopeKey,
} from "../model/conversation-identity";
import { subscribeChat, type StreamState } from "@/shared/api/events";

function overlayMatchesScope(
  overlayChatId: string | undefined,
  optionsChatId: string | undefined,
  messages: readonly AllyChatMessage[],
): boolean {
  if (overlayChatId === optionsChatId) return true;
  return (
    !optionsChatId &&
    overlayChatId !== undefined &&
    messages.some((message) => message.awaitingFirstHistory)
  );
}
import type {
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingElicitation as PendingElicitation,
  GithubComJieWaZiNeuxBotServerServicesExecutionAcceptedExecution as MessageResult,
} from "@/shared/api/generated/types.gen";

type ChatRuntimeOptions = {
  conversationKey: ChatScopeKey;
  queueKey: string;
  /** 隐藏 Scene 暂停临时队列出队，已开始的执行继续运行。 */
  queueActive?: boolean;
  chatId?: string;
  messages: ChatMessage[];
  latestSequence: number;
  activeExecutionId?: string;
  initialPendingApprovals?: PendingApproval[];
  initialPendingElicitations?: PendingElicitation[];
  isHistoryReady: boolean;
  isLoading: boolean;
  isSendDisabled: boolean;
  serverManagedQueue?: boolean;
  messageVisibility?: ChatMessageVisibility;
  attachmentOnlyText: string;
  referenceOnlyText?: string;
  incompleteErrorText: string;
  selection: ChatSendSelection;
  suggestions?: readonly ThreadSuggestion[];
  onMessageAccepted?: () => void;
  onMessageRejected?: (selection: ChatSendSelection) => void;
  onApprovalRequested?: (approval: PendingApproval) => void;
  send: (
    text: string,
    chatId?: string,
    attachmentPaths?: string[],
    selection?: ChatSendSelection,
  ) => Promise<MessageResult>;
  cancel: () => Promise<void>;
  onSettled?: (
    result: MessageResult,
    invocation: ChatInvocation & { current: boolean },
  ) => void;
  onStarted: (result: MessageResult, invocation: ChatInvocation) => void;
  onTerminal: () => void;
};

type ChatRuntimeResult = {
  runtime: AssistantRuntime;
  streamState: StreamState;
  isRunning: boolean;
  isHistorySynchronized: boolean;
  boundChatId?: string;
  hasVisibleMessages: boolean;
  sendFailure: ChatSendFailure;
  retryExecution: (executionId: string) => Promise<void>;
  contextMetrics?: ChatRunMetrics;
  pendingApprovals: PendingApproval[];
  pendingElicitations: PendingElicitation[];
};

type SendSnapshot = {
  conversationKey: ChatScopeKey;
  queueKey: string;
  chatId?: string;
  selection: ChatSendSelection;
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

function isActiveLocalMessage(message: AllyChatMessage) {
  return (
    Boolean(message.executionId) &&
    (message.status === "pending" || message.status === "running")
  );
}

export function useChatRuntime(options: ChatRuntimeOptions): ChatRuntimeResult {
  const queueKey = options.queueKey;
  const cancel = options.cancel;
  const send = options.send;
  const onStarted = options.onStarted;
  const onSettled = options.onSettled;
  const attachmentOnlyText = options.attachmentOnlyText;
  const referenceOnlyText = options.referenceOnlyText;
  const [overlay, setOverlay] = useState<{
    chatId?: string;
    messages: AllyChatMessage[];
  }>({ chatId: options.chatId, messages: [] });
  const [localExecution, setLocalExecution] = useState<{
    chatId?: string;
    id?: string;
  }>({ chatId: options.chatId });
  const [sendingKey, setSendingKey] = useState<string>();
  const [streamState, setStreamState] = useState<StreamState>("closed");
  const [terminalExecution, setTerminalExecution] = useState<{
    chatId?: string;
    executionId?: string;
  }>({});
  const [sendFailure, setSendFailure] = useState<{
    chatId?: string;
    value: ChatSendFailure;
  }>({ chatId: options.chatId, value: NO_SEND_FAILURE });
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
    added: PendingElicitation[];
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
    sequence: options.latestSequence,
  });
  const runningRef = useRef(false);
  const runningByQueueRef = useRef<Record<string, boolean>>({});
  const terminalRef = useRef(options.onTerminal);
  const messageAcceptedRef = useRef(options.onMessageAccepted);
  const messageRejectedRef = useRef(options.onMessageRejected);
  const approvalRequestedRef = useRef(options.onApprovalRequested);
  const notifiedApprovalIDsRef = useRef(new Set<string>());
  const approvalPendingRef = useRef(
    Boolean(options.initialPendingApprovals?.length),
  );
  const elicitationPendingRef = useRef(
    Boolean(options.initialPendingElicitations?.length),
  );
  const authoritativeMessagesRef = useRef(options.messages);
  const latestSequenceRef = useRef(options.latestSequence);
  const terminalSnapshotRef = useRef<
    | {
        chatId: string;
        messageId: string;
        sequence: number;
      }
    | undefined
  >(undefined);
  const runtimeRef = useRef<AssistantRuntime | null>(null);
  const mountedRef = useRef(false);
  const observedQueueKeyRef = useRef(queueKey);
  const observedChatKeyRef = useRef(options.conversationKey);
  useLayoutEffect(() => {
    observedQueueKeyRef.current = queueKey;
    observedChatKeyRef.current = options.conversationKey;
  }, [options.conversationKey, queueKey]);

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
      if (approval.approvalId && !resolved.has(approval.approvalId)) {
        approvals.set(approval.approvalId, approval);
      }
    }
    return [...approvals.values()];
  }, [approvalOverlay, options.chatId, options.initialPendingApprovals]);
  useEffect(() => {
    approvalPendingRef.current = pendingApprovals.length > 0;
  }, [pendingApprovals]);
  const pendingElicitations = useMemo(() => {
    const overlay =
      elicitationOverlay.chatId === options.chatId
        ? elicitationOverlay
        : { added: [], resolvedIds: [] };
    const resolved = new Set(overlay.resolvedIds);
    const elicitations = new Map<string, PendingElicitation>();
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
        !resolved.has(elicitation.elicitationId)
      ) {
        elicitations.set(elicitation.elicitationId, elicitation);
      }
    }
    return [...elicitations.values()];
  }, [elicitationOverlay, options.chatId, options.initialPendingElicitations]);
  useEffect(() => {
    elicitationPendingRef.current = pendingElicitations.length > 0;
  }, [pendingElicitations]);

  const messages = useMemo(() => {
    const overlayMessages = overlayMatchesScope(
      overlay.chatId,
      options.chatId,
      overlay.messages,
    )
      ? overlay.messages
      : [];
    const localMessages = mergeLocalMessages(handoffMessages, overlayMessages);
    return mergeAuthoritativeMessages(
      options.isHistoryReady && options.messages.length > 0
        ? localMessages.filter(
            (message) =>
              !message.awaitingFirstHistory || Boolean(message.executionId),
          )
        : localMessages,
      options.messages,
    );
  }, [
    handoffMessages,
    options.chatId,
    options.isHistoryReady,
    options.messages,
    overlay,
  ]);
  const convertMessage = useCallback(
    (message: AllyChatMessage) =>
      toThreadMessage(message, options.incompleteErrorText),
    [options.incompleteErrorText],
  );
  const localExecutionID =
    localExecution.chatId === options.chatId
      ? localExecution.id
      : !options.chatId &&
          localExecution.chatId &&
          overlay.chatId === localExecution.chatId &&
          overlay.messages.some((message) => message.awaitingFirstHistory)
        ? localExecution.id
        : handoffMessages.find(isActiveLocalMessage)?.executionId;
  const localExecutionIsTerminal = Boolean(
    localExecutionID &&
    options.messages.some(
      (message) =>
        message.executionId === localExecutionID &&
        ["completed", "failed", "canceled"].includes(message.status ?? ""),
    ),
  );
  const overlayExecutionIsActive = Boolean(
    overlayMatchesScope(overlay.chatId, options.chatId, overlay.messages) &&
    overlay.messages.some(isActiveLocalMessage),
  );
  const pendingFirstHistory = Boolean(
    handoffMessages.some((message) => message.awaitingFirstHistory) ||
    (overlayMatchesScope(overlay.chatId, options.chatId, overlay.messages) &&
      overlay.messages.some((message) => message.awaitingFirstHistory)),
  );
  const isRunning = Boolean(
    sendingKey === options.queueKey ||
    pendingFirstHistory ||
    messages.some((message) => message.awaitingFirstHistory) ||
    overlayExecutionIsActive ||
    (options.activeExecutionId &&
      !(
        terminalExecution.chatId === options.chatId &&
        terminalExecution.executionId === options.activeExecutionId
      )) ||
    (localExecutionID && !localExecutionIsTerminal),
  );
  useEffect(() => () => clearErrorNoticeActions(queueKey), [queueKey]);

  const retryExecution = useCallback(
    async (executionId: string) => {
      if (!options.chatId || runningRef.current || options.isSendDisabled)
        return;
      const chatId = options.chatId;
      const key = options.conversationKey;
      runningRef.current = true;
      setSendingKey(queueKey);
      try {
        const { data } = await retryChatExecution({
          path: { chatId, executionId },
          throwOnError: true,
        });
        if (mountedRef.current && observedChatKeyRef.current === key) {
          if (
            data.execution?.status === "pending" ||
            data.execution?.status === "running"
          ) {
            setLocalExecution({ chatId, id: data.execution.id });
          }
          onStarted(data, { chatId, key, queueKey });
          terminalRef.current();
        }
      } finally {
        if (observedChatKeyRef.current === key) runningRef.current = false;
        setSendingKey((value) => (value === queueKey ? undefined : value));
      }
    },
    [
      options.chatId,
      options.conversationKey,
      options.isSendDisabled,
      queueKey,
      onStarted,
    ],
  );

  const currentSendFailure =
    sendFailure.chatId === options.chatId ? sendFailure.value : NO_SEND_FAILURE;

  const submit = useCallback(
    async function submitMessage(
      message: AppendMessage,
      snapshot: SendSnapshot,
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
      const operation = `send:${snapshot.queueKey}`;
      const recovery = captureErrorNoticeRecovery(operation);
      const userID = optimisticID("user");
      const assistantID = optimisticID("assistant");
      runningRef.current = true;
      runningByQueueRef.current[snapshot.queueKey] = true;
      setSendingKey(snapshot.queueKey);
      setSendFailure({
        chatId: snapshot.chatId,
        value: NO_SEND_FAILURE,
      });
      const currentOverlay = overlayRef.current;
      const optimisticMessages = [
        ...mergeAuthoritativeMessages(
          currentOverlay.chatId === snapshot.chatId
            ? currentOverlay.messages
            : [],
          authoritativeMessagesRef.current,
        ),
        {
          id: userID,
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
          role: "assistant",
          content: "",
          status: "pending",
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
        const result = await send(
          text,
          snapshot.chatId,
          attachmentPaths,
          snapshot.selection,
        );
        const execution = result.execution;
        const resultChatID = result.chat?.id ?? snapshot.chatId;
        const pendingCreateHandoff = !snapshot.chatId && Boolean(resultChatID);
        let committedMessages: AllyChatMessage[] = execution
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
          : pendingCreateHandoff
            ? optimisticMessages.map((item) => ({
                ...item,
                awaitingFirstHistory: true,
              }))
            : optimisticMessages.filter(
                (item) => item.id !== userID && item.id !== assistantID,
              );
        if (pendingCreateHandoff) {
          committedMessages = committedMessages.map((item) => ({
            ...item,
            awaitingFirstHistory: true,
          }));
        }
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
          overlayRef.current = committedOverlay;
          setOverlay(committedOverlay);
        }
        if (resultChatID && (resultChatID !== snapshot.chatId || !current)) {
          queueStore.setHandoff(resultChatID, committedMessages);
        }
        if (execution) {
          setLocalExecution({
            chatId: resultChatID,
            id: execution.id,
          });
        }
        if (current) {
          cursorRef.current = {
            chatId: resultChatID,
            sequence: latestSequenceRef.current,
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
        runningByQueueRef.current[snapshot.queueKey] = false;
        const current =
          mountedRef.current &&
          observedChatKeyRef.current === snapshot.conversationKey;
        if (current) runningRef.current = false;
        const failure = classifyChatSendFailure(error);
        // 只重放服务端明确拒绝的限流请求；响应丢失时先核对权威历史。
        const canRetry =
          current && error instanceof ApiError && error.status === 429;
        const canCheck =
          current &&
          snapshot.chatId &&
          (failure.kind === "unknown" ||
            failure.kind === "active-execution" ||
            failure.kind === "create-request-in-progress");
        reportErrorNotice(
          {
            id: `send:${userID}`,
            scope: snapshot.queueKey,
            operation,
            message: sendFailureDisplayText(failure, (key) =>
              i18n.t(key, { ns: "chat" }),
            ),
            action: canRetry ? "retry" : canCheck ? "check" : undefined,
          },
          canRetry
            ? async () => {
                if (
                  !mountedRef.current ||
                  observedChatKeyRef.current !== snapshot.conversationKey ||
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
                    terminalRef.current();
                }
              : undefined,
        );
        if (current) {
          const authoritativeIDs = new Set<string>(
            authoritativeMessagesRef.current.flatMap((message) =>
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
          });
        }
        window.setTimeout(() => {
          if (
            !mountedRef.current ||
            observedChatKeyRef.current !== snapshot.conversationKey
          ) {
            return;
          }
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
          failure.kind === "unknown" &&
          mountedRef.current &&
          observedChatKeyRef.current === snapshot.conversationKey
        ) {
          terminalRef.current();
        }
      } finally {
        setSendingKey((current) =>
          current === snapshot.queueKey ? undefined : current,
        );
      }
    },
    [
      onStarted,
      attachmentOnlyText,
      onSettled,
      referenceOnlyText,
      send,
      queueStore,
    ],
  );

  const currentSnapshot = useMemo<SendSnapshot>(
    () => ({
      conversationKey: options.conversationKey,
      queueKey: options.queueKey,
      chatId: options.chatId,
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
        workflowIds: options.selection.workflowIds
          ? [...options.selection.workflowIds]
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
    ],
  );
  const advanceQueue = useCallback(
    (queueKey: string) => {
      if (options.queueActive === false) return;
      if (queueKey !== options.queueKey) return;
      if (runningByQueueRef.current[queueKey]) return;
      if (approvalPendingRef.current) return;
      const head = queueStore.take(queueKey);
      if (!head) return;
      void submit(head.message, {
        conversationKey: options.conversationKey,
        queueKey,
        chatId: head.chatId,
        selection: head.selection,
      });
    },
    [
      options.conversationKey,
      options.queueActive,
      options.queueKey,
      queueStore,
      submit,
    ],
  );

  useEffect(() => {
    runningRef.current = isRunning;
    terminalRef.current = options.onTerminal;
    messageAcceptedRef.current = options.onMessageAccepted;
    messageRejectedRef.current = options.onMessageRejected;
    approvalRequestedRef.current = options.onApprovalRequested;
    authoritativeMessagesRef.current = options.messages;
    latestSequenceRef.current = options.latestSequence;

    const wasRunning = runningByQueueRef.current[options.queueKey] ?? false;
    runningByQueueRef.current[options.queueKey] = isRunning;
    if (wasRunning && !isRunning) {
      const timer = window.setTimeout(() => advanceQueue(options.queueKey), 0);
      return () => window.clearTimeout(timer);
    }
  }, [
    advanceQueue,
    isRunning,
    options.latestSequence,
    options.messages,
    options.onMessageAccepted,
    options.onMessageRejected,
    options.onApprovalRequested,
    options.onTerminal,
    options.queueKey,
  ]);

  useEffect(() => {
    const terminalSnapshot = terminalSnapshotRef.current;
    if (terminalSnapshot && terminalSnapshot.chatId !== options.chatId) {
      terminalSnapshotRef.current = undefined;
      return;
    }
    if (
      !terminalSnapshot ||
      options.latestSequence < terminalSnapshot.sequence
    ) {
      return;
    }
    const authoritativeTerminalMessage = options.messages.find(
      (message) => message.id === terminalSnapshot.messageId,
    );
    if (
      !authoritativeTerminalMessage ||
      !["completed", "failed", "canceled"].includes(
        authoritativeTerminalMessage.status ?? "",
      )
    ) {
      return;
    }

    const authoritativeIDs = new Set<string>(
      options.messages.flatMap((message) => (message.id ? [message.id] : [])),
    );
    setOverlay((current) => {
      if (current.chatId !== options.chatId) return current;
      const unreconciled = current.messages.filter(
        (message) => !authoritativeIDs.has(message.id ?? ""),
      );
      if (unreconciled.length === current.messages.length) return current;
      return {
        chatId: current.chatId,
        messages: unreconciled,
      };
    });
    terminalSnapshotRef.current = undefined;
  }, [options.chatId, options.latestSequence, options.messages]);

  useEffect(() => {
    if (!options.chatId || handoffMessages.length === 0) return;
    const authoritativeIDs = new Set<string>(
      options.messages.flatMap((message) => (message.id ? [message.id] : [])),
    );
    const handoffIsAuthoritative =
      options.isHistoryReady &&
      (handoffMessages.every(
        (message) => message.id && authoritativeIDs.has(message.id),
      ) ||
        (options.messages.length > 0 &&
          handoffMessages.every(
            (message) => message.awaitingFirstHistory && !message.executionId,
          )));
    if (handoffIsAuthoritative) {
      queueStore.clearHandoff(options.chatId);
    }
  }, [
    handoffMessages,
    options.chatId,
    options.isHistoryReady,
    options.messages,
    queueStore,
  ]);

  useEffect(() => {
    if (isRunning || queueStore.getEntries(options.queueKey).length === 0) {
      return;
    }
    const timer = window.setTimeout(() => advanceQueue(options.queueKey), 0);
    return () => window.clearTimeout(timer);
  }, [advanceQueue, isRunning, options.queueKey, queueStore, queuedEntries]);

  useEffect(() => {
    if (
      options.queueActive === false ||
      !options.chatId ||
      !isRunning ||
      !options.isHistoryReady
    ) {
      return;
    }
    let subscribed = true;
    const subscribedChatKey = options.conversationKey;
    const isCurrentSubscription = () =>
      subscribed &&
      mountedRef.current &&
      observedChatKeyRef.current === subscribedChatKey;
    let pendingSequence = 0;
    const eventBatcher = createChatEventBatcher((events) => {
      if (!isCurrentSubscription()) return;
      // 接收不等于投影；隐藏时丢弃的短批次不能推进恢复游标。
      cursorRef.current.sequence = Math.max(
        cursorRef.current.sequence,
        pendingSequence,
      );
      setOverlay((current) => {
        const localMessages = mergeLocalMessages(
          handoffMessagesRef.current,
          current.chatId === options.chatId ? current.messages : [],
        );
        let projected = mergeAuthoritativeMessages(
          localMessages,
          authoritativeMessagesRef.current,
        );
        for (const event of events) {
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
        sequence: latestSequenceRef.current,
      };
    } else {
      cursorRef.current.sequence = Math.max(
        cursorRef.current.sequence,
        latestSequenceRef.current,
      );
    }
    const streamOperation = `stream:${options.chatId}`;
    let streamRecovery = captureErrorNoticeRecovery(streamOperation);
    const unsubscribe = subscribeChat(options.chatId, {
      after: cursorRef.current.sequence,
      isTerminal: () => !runningRef.current,
      onEvent: (event) => {
        if (!isCurrentSubscription()) return;
        resolveErrorNoticeRecovery(streamRecovery);
        pendingSequence = Math.max(pendingSequence, event.sequence ?? 0);
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
          approvalPendingRef.current = true;
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
          const elicitation: PendingElicitation = {
            elicitationId: elicitationRequest.elicitationId,
            allowSkip: elicitationRequest.allowSkip,
            toolCallId: elicitationRequest.toolCallId,
            mode: elicitationRequest.mode,
            message: elicitationRequest.message,
            requestedSchema: elicitationRequest.requestedSchema,
            url: elicitationRequest.url,
            requestedAt: event.recordedAt,
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
          if (
            options.chatId &&
            event.message?.id &&
            event.sequence !== undefined
          ) {
            terminalSnapshotRef.current = {
              chatId: options.chatId,
              messageId: event.message.id,
              sequence: event.sequence,
            };
          }
          runningRef.current = false;
          setTerminalExecution({
            chatId: options.chatId,
            executionId: options.activeExecutionId,
          });
          setLocalExecution({ chatId: options.chatId });
          terminalRef.current();
        }
      },
      onGap: () => {
        if (isCurrentSubscription()) terminalRef.current();
      },
      onReconnect: () => {
        if (!isCurrentSubscription()) return;
        resolveErrorNoticeRecovery(streamRecovery);
        eventBatcher.close();
        pendingSequence = cursorRef.current.sequence;
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
        setOverlay((current) => ({
          chatId: options.chatId,
          messages:
            current.chatId === options.chatId
              ? current.messages.filter((message) => message.optimistic)
              : [],
        }));
        terminalRef.current();
      },
      onUnavailable: () => {
        if (!isCurrentSubscription()) return;
        reportErrorNotice({
          id: `stream:${options.chatId}`,
          scope: `chat:${options.chatId}`,
          operation: streamOperation,
          message: i18n.t("runtimeFailure.networkError", { ns: "chat" }),
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
        if (isCurrentSubscription()) setStreamState(state);
      },
    });
    return () => {
      subscribed = false;
      unsubscribe();
      eventBatcher.close();
    };
  }, [
    isRunning,
    options.activeExecutionId,
    options.chatId,
    options.conversationKey,
    options.isHistoryReady,
    options.messageVisibility,
    options.queueActive,
  ]);

  const onNew = useCallback(
    (message: AppendMessage) => {
      messageAcceptedRef.current?.();
      return submit(message, currentSnapshot);
    },
    [currentSnapshot, submit],
  );
  const queue = useMemo<ExternalThreadQueueAdapter>(() => {
    return {
      items: options.serverManagedQueue
        ? []
        : queuedEntries.map(({ id, prompt }) => ({ id, prompt })),
      enqueue: (message, { steer }) => {
        if (approvalPendingRef.current) return;
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
          selection: currentSnapshot.selection,
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
        const current = queueStore.getEntries(queueKey);
        const target = current.find((item) => item.id === queueItemID);
        if (!target) return;
        queueStore.promote(queueKey, queueItemID);
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
    queueStore,
    submit,
    options.serverManagedQueue,
    options.queueActive,
  ]);

  const runtime = useExternalStoreRuntime<AllyChatMessage>({
    messages,
    convertMessage,
    isRunning,
    isLoading: options.isLoading,
    isSendDisabled:
      options.isSendDisabled ||
      pendingApprovals.length > 0 ||
      pendingElicitations.length > 0,
    suggestions: options.suggestions,
    queue,
    onNew,
    onCancel: options.cancel,
    adapters: { attachments: chatAttachmentAdapter },
    unstable_capabilities: { copy: true },
  });
  const [historySync, synchronizeHistory] = useReducer(
    (
      current: { chatId?: string; ready: boolean },
      next: { chatId?: string; ready: boolean },
    ) =>
      current.chatId === next.chatId && current.ready === next.ready
        ? current
        : next,
    {
      chatId: options.chatId,
      ready: options.isHistoryReady,
    },
  );
  useEffect(() => {
    // assistant-ui 通过被动 Effect 应用外部仓库。这里只确认已提交的
    // Chat 身份和首次历史就绪，防止切换时展示旧 Chat；同一 Chat 的后续
    // 消息修订由 ChatThread 订阅 Runtime 快照驱动，不依赖此标志补帧。
    synchronizeHistory({
      chatId: options.chatId,
      ready: options.isHistoryReady,
    });
  }, [options.chatId, options.isHistoryReady]);
  const isHistorySynchronized =
    !options.chatId ||
    (options.isHistoryReady &&
      historySync.chatId === options.chatId &&
      historySync.ready);
  useEffect(() => {
    runtimeRef.current = runtime;
    return () => {
      if (runtimeRef.current === runtime) runtimeRef.current = null;
    };
  }, [runtime]);

  return {
    runtime,
    streamState: isRunning ? streamState : "closed",
    isRunning,
    isHistorySynchronized,
    boundChatId: historySync.chatId,
    hasVisibleMessages: messages.length > 0,
    sendFailure: currentSendFailure,
    retryExecution,
    contextMetrics: latestContextMetrics(messages),
    pendingApprovals,
    pendingElicitations,
  };
}
