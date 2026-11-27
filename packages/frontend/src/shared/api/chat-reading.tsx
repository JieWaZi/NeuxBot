import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
  type RefObject,
  type ComponentProps,
} from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { observeContentPresentation } from "@/shared/lib/content-presentation";
import { isElementVisible } from "@/shared/lib/element-visibility";
import { ApiError } from "./errors";
import {
  conversationReadingOptions,
  markConversationRead,
  markConversationUnread,
  type ChatReadCommand,
  type ConversationReadingState,
  type MainConversationReadingSubject,
  type ReadingTarget,
} from "./reading-queries";
import { refreshQueryFromEvent, useQueryEvents } from "./use-query-events";

type OpenIntent = { token: string; readingKey: string; generation: number };
type VisibleBoundary = { messageId: string; sequence: number };

const openIntents = new WeakMap<QueryClient, OpenIntent>();
const chatOperations = new WeakMap<QueryClient, Map<string, Promise<void>>>();
const unreadWrites = new WeakMap<QueryClient, Map<string, number>>();

function readingKeyOf(target: ReadingTarget) {
  return typeof target === "string"
    ? target
    : `main:${target.subjectType}:${target.subjectId}`;
}

function pendingUnread(client: QueryClient, readingKey: string) {
  return Boolean(unreadWrites.get(client)?.get(readingKey));
}

function knownReading(state?: ConversationReadingState | null) {
  if (
    !state?.complete ||
    typeof state.unreadGeneration !== "number" ||
    typeof state.throughSequence !== "number" ||
    typeof state.holdUnread !== "boolean"
  )
    return;
  return {
    ...state,
    unreadGeneration: state.unreadGeneration,
    throughSequence: state.throughSequence,
    holdUnread: state.holdUnread,
  };
}

function cachedReading(client: QueryClient, target: ReadingTarget) {
  const { queryKey } = conversationReadingOptions(target);
  const query = client.getQueryState<ConversationReadingState | null>(queryKey);
  if (query?.status === "success") return knownReading(query.data);
}

function openToken(state: unknown) {
  return state && typeof state === "object" && "chatOpenToken" in state
    ? state.chatOpenToken
    : undefined;
}

/** 仅保存一次用户点击时的已有代次。路由恢复不能重建该意图。 */
export function captureChatOpenIntent(client: QueryClient, chatId: string) {
  return captureReadingOpenIntent(client, chatId);
}

export function captureMainConversationOpenIntent(
  client: QueryClient,
  subject: MainConversationReadingSubject,
  options?: { observedGeneration?: number; freezeObservedGeneration?: boolean },
) {
  return captureReadingOpenIntent(client, subject, options);
}

function captureReadingOpenIntent(
  client: QueryClient,
  target: ReadingTarget,
  {
    observedGeneration,
    freezeObservedGeneration = false,
  }: {
    observedGeneration?: number;
    freezeObservedGeneration?: boolean;
  } = {},
) {
  const cachedGeneration = cachedReading(client, target)?.unreadGeneration;
  const generation =
    observedGeneration === undefined
      ? cachedGeneration
      : freezeObservedGeneration
        ? observedGeneration
        : Math.max(observedGeneration, cachedGeneration ?? 0);
  openIntents.delete(client);
  if (generation === undefined) return undefined;
  const token = crypto.randomUUID();
  openIntents.set(client, {
    token,
    readingKey: readingKeyOf(target),
    generation,
  });
  return { chatOpenToken: token };
}

export function discardUnmatchedChatOpenIntent(
  client: QueryClient,
  locationState: unknown,
) {
  const pending = openIntents.get(client);
  if (pending && pending.token !== openToken(locationState))
    openIntents.delete(client);
}

function serializeReadingWrite(
  client: QueryClient,
  readingKey: string,
  write: () => Promise<void>,
) {
  let operations = chatOperations.get(client);
  if (!operations) {
    operations = new Map();
    chatOperations.set(client, operations);
  }
  const pending = operations;
  const operation = (pending.get(readingKey) ?? Promise.resolve())
    .catch(() => {})
    .then(write);
  pending.set(readingKey, operation);
  return operation.finally(() => {
    if (pending.get(readingKey) === operation) pending.delete(readingKey);
  });
}

async function refreshConversationReading(
  client: QueryClient,
  target: ReadingTarget,
) {
  const options = conversationReadingOptions(target);
  await refreshQueryFromEvent(client, options.queryKey, () =>
    client.fetchQuery({ ...options, staleTime: 0 }),
  );
  return cachedReading(client, target);
}

async function writeConversationUnread(
  client: QueryClient,
  target: ReadingTarget,
  fromMessageID: string,
  generation: number,
) {
  const readingKey = readingKeyOf(target);
  if (openIntents.get(client)?.readingKey === readingKey)
    openIntents.delete(client);
  const pending = unreadWrites.get(client) ?? new Map<string, number>();
  unreadWrites.set(client, pending);
  pending.set(readingKey, (pending.get(readingKey) ?? 0) + 1);
  try {
    await serializeReadingWrite(client, readingKey, async () => {
      try {
        await markConversationUnread(target, {
          fromMessageID,
          observedUnreadGeneration: generation,
        });
      } finally {
        await refreshConversationReading(client, target);
      }
    });
  } finally {
    const count = pending.get(readingKey) ?? 0;
    if (count > 1) pending.set(readingKey, count - 1);
    else pending.delete(readingKey);
  }
}

/** 侧栏手动未读复用对话游标、写入顺序和持久 hold。 */
export async function markLatestChatUnread(
  client: QueryClient,
  chatId: string,
) {
  return markLatestConversationUnread(client, chatId);
}

export async function markLatestConversationUnread(
  client: QueryClient,
  target: ReadingTarget,
) {
  const state = await refreshConversationReading(client, target);
  if (!state) throw new Error("Chat reading boundary is unavailable");
  const boundary = state.firstUnreadMessageID ?? state.latestReadableMessageID;
  if (!boundary) return false;
  if (!state.holdUnread)
    await writeConversationUnread(
      client,
      target,
      boundary,
      state.unreadGeneration,
    );
  return true;
}

function subscribeVisibility(update: () => void) {
  document.addEventListener("visibilitychange", update);
  return () => document.removeEventListener("visibilitychange", update);
}

export function useVisibleScene() {
  const active = useRouteSceneActive();
  const visible = useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState === "visible",
    () => false,
  );
  return active && visible;
}

export function useChatReading({
  chatId,
  active,
}: {
  chatId: string;
  active: boolean;
}) {
  return useConversationReading(chatId, chatId, active);
}

export function useMainConversationReading({
  subjectType,
  subjectId,
  conversationId,
  active,
}: MainConversationReadingSubject & {
  conversationId?: string;
  active: boolean;
}) {
  const target = useMemo(
    () => ({ subjectType, subjectId }),
    [subjectType, subjectId],
  );
  return useConversationReading(target, conversationId, active);
}

function useConversationReading(
  target: ReadingTarget,
  resourceId: string | undefined,
  active: boolean,
) {
  const readingKey = readingKeyOf(target);
  const client = useQueryClient();
  const { state: navigationState } = useLocation();
  const token = openToken(navigationState);
  const options = conversationReadingOptions(target);
  const query = useQuery({
    ...options,
    enabled: active && Boolean(resourceId),
  });
  useQueryEvents({
    topic: "reading",
    resource: {
      kind: typeof target === "string" ? "chat" : "main_conversation",
      id: resourceId,
    },
    queryKeys: [options.queryKey],
    eventTypes: ["reading.changed"],
    active,
    enabled: Boolean(resourceId),
    authority: true,
  });
  const scope = useRef({ active, readingKey, resourceId, token });
  const queuedExplicitToken = useRef<string>(undefined);
  const automatic = useRef<{
    readingKey: string;
    generation: number;
    sequence: number;
  }>(undefined);
  useEffect(() => {
    scope.current = { active, readingKey, resourceId, token };
    const ownsOpenIntent = () => {
      const pending = openIntents.get(client);
      return pending?.readingKey === readingKey && pending.token === token;
    };
    if (!active && ownsOpenIntent()) openIntents.delete(client);
    return () => {
      scope.current.active = false;
      // Scene 切换的 cleanup 与 Strict Mode 重挂载相同，等新 scope 稳定再丢弃。
      queueMicrotask(() => {
        if (
          (!scope.current.active || scope.current.token !== token) &&
          ownsOpenIntent()
        )
          openIntents.delete(client);
      });
    };
  }, [active, readingKey, client, resourceId, token]);

  const readState = useCallback(
    () => cachedReading(client, target),
    [client, target],
  );
  const recover = useCallback(
    () => refreshConversationReading(client, target),
    [client, target],
  );

  const submit = useCallback(
    (
      command: ChatReadCommand,
      boundary?: VisibleBoundary,
      needsScene = false,
      begin?: () => boolean,
    ) => {
      const lease = scope.current;
      const sceneAvailable = () =>
        !needsScene ||
        (scope.current === lease &&
          lease.active &&
          document.visibilityState === "visible" &&
          document.hasFocus());
      return serializeReadingWrite(client, readingKey, async () => {
        if (
          !sceneAvailable() ||
          (command.intent === "automatic" &&
            pendingUnread(client, readingKey)) ||
          (begin && !begin())
        )
          return;
        try {
          await markConversationRead(target, command);
        } catch (error) {
          const state = await recover();
          if (error instanceof ApiError && error.code !== "NETWORK")
            throw error;
          if (
            !state?.complete ||
            state.unreadGeneration !== command.observedUnreadGeneration ||
            (state.holdUnread && command.intent === "automatic") ||
            (boundary &&
              state.throughSequence >= boundary.sequence &&
              !state.holdUnread)
          )
            return;
          if (sceneAvailable()) await markConversationRead(target, command);
        }
      });
    },
    [readingKey, client, recover, target],
  );

  const present = useCallback(
    (boundary: VisibleBoundary) => {
      if (!active || !resourceId || pendingUnread(client, readingKey)) return;
      const state = readState();
      if (!state?.complete) return;
      if (
        typeof target !== "string" &&
        (!("conversationId" in state) || state.conversationId !== resourceId)
      )
        return;
      const pending = openIntents.get(client);
      if (
        pending &&
        pending.readingKey === readingKey &&
        pending.token === token
      ) {
        if (queuedExplicitToken.current === pending.token) return;
        queuedExplicitToken.current = pending.token;
        void submit(
          {
            intent: "explicit",
            observedUnreadGeneration: pending.generation,
            throughMessageID: boundary.messageId,
          },
          boundary,
          true,
          () => {
            if (openIntents.get(client) !== pending) return false;
            openIntents.delete(client);
            automatic.current = {
              readingKey,
              generation: pending.generation,
              sequence: boundary.sequence,
            };
            return true;
          },
        )
          .catch(() => {})
          .finally(() => {
            if (queuedExplicitToken.current === pending.token)
              queuedExplicitToken.current = undefined;
          });
        return;
      }
      if (state.holdUnread || boundary.sequence <= state.throughSequence)
        return;
      const previous = automatic.current;
      if (
        previous?.readingKey === readingKey &&
        previous.generation === state.unreadGeneration &&
        previous.sequence >= boundary.sequence
      )
        return;
      const attempt = {
        readingKey,
        generation: state.unreadGeneration,
        sequence: boundary.sequence,
      };
      automatic.current = attempt;
      let started = false;
      void submit(
        {
          intent: "automatic",
          observedUnreadGeneration: state.unreadGeneration,
          throughMessageID: boundary.messageId,
        },
        boundary,
        true,
        () => {
          started = true;
          return true;
        },
      )
        .catch(() => {
          if (automatic.current === attempt) automatic.current = undefined;
        })
        .finally(() => {
          if (!started && automatic.current === attempt)
            automatic.current = undefined;
        });
    },
    [active, readingKey, client, readState, resourceId, submit, target, token],
  );

  const markRead = useCallback(async () => {
    const state = readState();
    if (!state?.complete || !state.latestReadableMessageID) return;
    if (openIntents.get(client)?.readingKey === readingKey)
      openIntents.delete(client);
    await submit({
      intent: "explicit",
      observedUnreadGeneration: state.unreadGeneration,
      throughMessageID: state.latestReadableMessageID,
    });
  }, [readingKey, client, readState, submit]);

  const markUnread = useCallback(
    async (fromMessageID: string) => {
      const state = readState();
      if (!state?.complete || !fromMessageID) return;
      await writeConversationUnread(
        client,
        target,
        fromMessageID,
        state.unreadGeneration,
      );
    },
    [client, readState, target],
  );
  return {
    state: query.isSuccess ? query.data : undefined,
    revision: query.dataUpdatedAt,
    present,
    markRead,
    markUnread,
  };
}

/** DOM 观察只提交服务端确认合格且确实呈现的正文，一次观察取最新可见边界。 */
export function usePresentedChatBoundary({
  root,
  active,
  present,
  revision,
}: {
  root: RefObject<HTMLElement | null>;
  active: boolean;
  present: (boundary: VisibleBoundary) => void;
  revision: number;
}) {
  useEffect(() => {
    const container = root.current;
    if (!container || !active) return;
    const visible = new Set<HTMLElement>();
    return observeContentPresentation({
      root: container,
      selector:
        "[data-chat-readable-sequence] [data-chat-readable-body], [data-chat-readable-body][data-chat-readable-sequence]",
      onVisible: (element) => visible.add(element),
      onChange: () => {
        let latest: VisibleBoundary | undefined;
        for (const element of visible) {
          visible.delete(element);
          const row = element.closest<HTMLElement>(
            "[data-chat-readable-sequence]",
          );
          const messageId =
            element.dataset.chatReadingMessageId ?? row?.dataset.messageId;
          if (!messageId || !isElementVisible(element)) continue;
          const sequence = Number(row?.dataset.chatReadableSequence);
          if (
            Number.isSafeInteger(sequence) &&
            sequence > 0 &&
            (!latest || sequence > latest.sequence)
          )
            latest = { messageId, sequence };
        }
        if (latest) present(latest);
      },
    });
  }, [root, active, present, revision]);
}

/** 建立确切 Chat 的显式打开意图，普通浏览器修饰点击仍使用原 Link。 */
export function ChatReadingLink({
  chatId,
  mainConversation,
  onClick,
  to,
  ...props
}: ComponentProps<typeof Link> & {
  chatId?: string;
  mainConversation?: MainConversationReadingSubject & {
    observedGeneration?: number;
  };
}) {
  const client = useQueryClient();
  const navigate = useNavigate();
  return (
    <Link
      {...props}
      to={to}
      onClick={(event) => {
        onClick?.(event);
        if (
          (!chatId && !mainConversation) ||
          event.defaultPrevented ||
          event.button !== 0 ||
          event.metaKey ||
          event.altKey ||
          event.ctrlKey ||
          event.shiftKey ||
          (props.target && props.target !== "_self")
        )
          return;
        event.preventDefault();
        if (mainConversation) {
          const { observedGeneration, ...subject } = mainConversation;
          navigate(to, {
            state: captureMainConversationOpenIntent(client, subject, {
              observedGeneration,
              freezeObservedGeneration: true,
            }),
          });
        } else if (chatId) {
          navigate(to, { state: captureChatOpenIntent(client, chatId) });
        }
      }}
    />
  );
}
