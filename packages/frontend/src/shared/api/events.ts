import type {
  GithubComJieWaZiNeuxBotServerConstantsExecutionStatus as ExecutionStatus,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelEvent as ChannelEvent,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowEvent as WorkflowEvent,
  GithubComJieWaZiNeuxBotServerServicesChatPublicApprovalRequest as ApprovalRequest,
  GithubComJieWaZiNeuxBotServerServicesChatPublicApprovalResolution as ApprovalResolution,
  GithubComJieWaZiNeuxBotServerServicesChatPublicElicitationRequest as ElicitationRequest,
  GithubComJieWaZiNeuxBotServerServicesChatPublicElicitationResolution as ElicitationResolution,
} from "./generated/types.gen";
import { listWorkflowEvents } from "./generated/sdk.gen";
import {
  subscribeGlobalEvents,
  updateGlobalEventCursor,
  type GlobalEventResource,
  type GlobalEventTopic,
} from "./global-events";
import { logger } from "@/shared/diagnostics/logger";

export type StreamState =
  "connecting" | "open" | "reconnecting" | "unavailable" | "closed";

export type ChatEvent = {
  schemaVersion?: number;
  sequence?: number;
  transient?: boolean;
  recordedAt?: number;
  type?: string;
  message?: ChatMessage;
  approvalRequest?: ApprovalRequest;
  approvalResolution?: ApprovalResolution;
  elicitationRequest?: ElicitationRequest;
  elicitationResolution?: ElicitationResolution;
  executionStatus?: ExecutionStatus;
  errorCode?: string;
  errorMessage?: string;
};

type ChatSubscribeOptions = {
  after?: number;
  onEvent: (event: ChatEvent) => void;
  reconcileCursor: (signal: AbortSignal) => Promise<number | undefined>;
  onReconnect?: () => void;
  onUnavailable?: () => void;
  onState?: (state: StreamState) => void;
  isTerminal: () => boolean;
};

type ChannelSubscribeOptions = {
  after?: string;
  onEvent: (event: ChannelEvent) => void;
  reconcileCursor?: (signal: AbortSignal) => Promise<string | undefined>;
  onReconnect?: () => void;
  onUnavailable?: () => void;
  onState?: (state: StreamState) => void;
};

type SequencedEvent = {
  id?: number | string;
  sequence?: number;
  transient?: boolean;
};

type SharedSubscribeOptions<Event extends SequencedEvent> = {
  after?: number | string;
  topic: GlobalEventTopic;
  resource: GlobalEventResource;
  contiguous: boolean;
  isTerminal: () => boolean;
  loadHistory?: (
    after: number | string,
    signal: AbortSignal,
  ) => Promise<Event[]>;
  reconcileCursor?: (
    signal: AbortSignal,
  ) => Promise<number | string | undefined>;
  validate: (payload: unknown) => payload is Event;
  onEvent: (event: Event) => void;
  onGap?: (expected: number, received: number) => void;
  onReconnect?: () => void;
  onState?: (state: StreamState) => void;
  onUnavailable?: () => void;
};
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
function isChatEvent(value: unknown): value is ChatEvent {
  return (
    isRecord(value) &&
    (value.sequence === undefined || Number.isSafeInteger(value.sequence)) &&
    (value.transient === undefined || typeof value.transient === "boolean") &&
    (value.type === undefined || typeof value.type === "string")
  );
}
function isChannelEvent(value: unknown): value is ChannelEvent {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    (value.channelId === undefined || typeof value.channelId === "string") &&
    (value.threadId === undefined || typeof value.threadId === "string") &&
    (value.payload === undefined || isRecord(value.payload))
  );
}
function isWorkflowEvent(value: unknown): value is WorkflowEvent {
  return (
    isRecord(value) &&
    Number.isSafeInteger(value.sequence) &&
    (value.type === undefined || typeof value.type === "string") &&
    (value.payload === undefined || isRecord(value.payload))
  );
}
/** 传输共享，领域消费各自保留权威游标与恢复过程，transient 不推进持久游标。 */
function subscribeDomain<Event extends SequencedEvent>(
  options: SharedSubscribeOptions<Event>,
): () => void {
  const controller = new AbortController();
  let cursor = options.after ?? (options.contiguous ? 0 : "");
  const seen = new Set<string>();
  const seenOrder: string[] = [];
  let waiting = false;
  let recovery: Promise<void> | undefined;
  let pendingRecovery = false;
  let buffered: Event[] = [];
  let bufferBytes = 0;
  let closed = false;
  let connected = false;
  let unavailable = false;
  let stop = () => {};
  const close = () => {
    if (closed) return;
    closed = true;
    controller.abort();
    stop();
    buffered = [];
    options.onState?.("closed");
  };
  const deliver = (event: Event) => {
    if (closed || options.isTerminal()) return;
    if (event.transient) {
      options.onEvent(event);
      return;
    }
    const next = options.contiguous
      ? event.sequence
      : (event.id ?? event.sequence);
    if (next === undefined) return;
    if (
      typeof next === "number" &&
      typeof cursor === "number" &&
      next <= cursor
    )
      return;
    if (typeof next === "string" && (next === cursor || seen.has(next))) return;
    if (
      options.contiguous &&
      typeof next === "number" &&
      typeof cursor === "number" &&
      cursor > 0 &&
      next !== cursor + 1
    ) {
      options.onGap?.(cursor + 1, next);
      throw new Error("领域事件游标出现缺口");
    }
    cursor = next;
    updateGlobalEventCursor(options.topic, options.resource, cursor);
    if (typeof next === "string") {
      seen.add(next);
      seenOrder.push(next);
      if (seenOrder.length > 512) {
        const removed = seenOrder.shift();
        if (removed) seen.delete(removed);
      }
    }
    options.onEvent(event);
    if (options.isTerminal()) close();
  };
  const recover = (): Promise<void> => {
    if (closed) return Promise.resolve();
    if (recovery) {
      pendingRecovery = true;
      return recovery;
    }
    waiting = true;
    recovery = (async () => {
      do {
        pendingRecovery = false;
        const attempt = new AbortController();
        const abort = () => attempt.abort();
        controller.signal.addEventListener("abort", abort, { once: true });
        const timer = setTimeout(abort, 20_000);
        try {
          if (options.reconcileCursor) {
            const next = await options.reconcileCursor(attempt.signal);
            if (closed) return;
            if (next !== undefined) {
              cursor = next;
              updateGlobalEventCursor(options.topic, options.resource, cursor);
            }
            seen.clear();
            seenOrder.length = 0;
          } else if (options.loadHistory) {
            const events = await options.loadHistory(cursor, attempt.signal);
            if (closed) return;
            for (const event of events) deliver(event);
          }
          if (closed) return;
          const pending = buffered;
          buffered = [];
          bufferBytes = 0;
          for (const event of pending) deliver(event);
          waiting = false;
          if (connected) options.onReconnect?.();
        } catch {
          if (!closed) {
            logger.warn("全局事件权威对账未完成", { topic: options.topic });
            options.onState?.("unavailable");
            options.onUnavailable?.();
            // 保留等待语义；下一次连接/推送会重试，不接受越过原游标的事件。
            waiting = true;
            throw new Error("领域事件权威恢复失败");
          }
        } finally {
          clearTimeout(timer);
          controller.signal.removeEventListener("abort", abort);
          attempt.abort();
        }
      } while (pendingRecovery && !closed);
    })().finally(() => {
      recovery = undefined;
    });
    return recovery;
  };
  const onEvent = (event: Event) => {
    if (closed) return;
    if (waiting) {
      bufferBytes += JSON.stringify(event).length;
      if (buffered.length >= 512 || bufferBytes > 1_048_576) {
        buffered = [];
        bufferBytes = 0;
        // 缓冲丢弃必须立即按持久历史对账，transient 由当前快照重建。
        void recover().catch(() => {});
      } else buffered.push(event);
      if (!recovery) void recover().catch(() => {});
      return;
    }
    try {
      deliver(event);
    } catch {
      buffered.push(event);
      void recover().catch(() => {});
    }
  };
  stop = subscribeGlobalEvents(options.topic, options.resource, {
    after: options.after,
    onEvent: (frame) => {
      if (
        frame.type === `${options.topic}.event` &&
        options.validate(frame.payload)
      )
        onEvent(frame.payload);
    },
    onState: (state) => {
      if (closed) return;
      options.onState?.(state);
      if (state === "open") {
        connected = true;
        unavailable = false;
      }
      if (state === "reconnecting" || state === "unavailable") waiting = true;
      if (state === "unavailable" && !unavailable) {
        unavailable = true;
        options.onUnavailable?.();
      }
    },
    onResync: () => {
      if (!closed) return recover();
    },
  });
  return close;
}

export function subscribeChat(
  chatId: string,
  options: ChatSubscribeOptions,
): () => void {
  return subscribeDomain({
    ...options,
    topic: "chat",
    resource: { kind: "chat", id: chatId },
    contiguous: true,
    validate: isChatEvent,
  });
}
export function subscribeChannel(
  channelId: string,
  threadId: string,
  options: ChannelSubscribeOptions,
): () => void {
  return subscribeDomain({
    ...options,
    topic: "team",
    resource: { kind: "thread", id: threadId, parentId: channelId },
    contiguous: false,
    validate: isChannelEvent,
    isTerminal: () => false,
  });
}
export function subscribeWorkflow(
  id: string,
  options: {
    runId?: string;
    after?: number;
    onEvent: (event: WorkflowEvent) => void;
    onGap?: (expected: number, received: number) => void;
    onState?: (state: StreamState) => void;
    onReconnect?: () => void;
  },
): () => void {
  return subscribeDomain({
    ...options,
    topic: "workflow",
    resource: {
      kind: "workflow",
      id,
      ...(options.runId ? { parentId: options.runId } : {}),
    },
    contiguous: true,
    validate: isWorkflowEvent,
    isTerminal: () => false,
    loadHistory: async (after, signal) => {
      const { data } = await listWorkflowEvents({
        path: { id },
        query: {
          after: typeof after === "number" ? after : 0,
          runId: options.runId,
        },
        signal,
        throwOnError: true,
      });
      return data.resources ?? [];
    },
  });
}
