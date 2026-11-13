import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
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
  chatReadingOptions,
  markChatRead,
  markChatUnread,
  type ChatReadCommand,
  type ChatReadingState,
} from "./reading-queries";
import { refreshQueryFromEvent, useQueryEvents } from "./use-query-events";

type OpenIntent = { token: string; chatId: string; generation: number };
type VisibleBoundary = { messageId: string; sequence: number };

const openIntents = new WeakMap<QueryClient, OpenIntent>();
const chatOperations = new WeakMap<QueryClient, Map<string, Promise<void>>>();
const unreadWrites = new WeakMap<QueryClient, Map<string, number>>();

function pendingUnread(client: QueryClient, chatId: string) {
  return Boolean(unreadWrites.get(client)?.get(chatId));
}

function knownReading(state?: ChatReadingState | null) {
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

function cachedReading(client: QueryClient, chatId: string) {
  const { queryKey } = chatReadingOptions(chatId);
  const query = client.getQueryState<ChatReadingState | null>(queryKey);
  if (query?.status === "success") return knownReading(query.data);
}

function openToken(state: unknown) {
  return state && typeof state === "object" && "chatOpenToken" in state
    ? state.chatOpenToken
    : undefined;
}

/** 仅保存一次用户点击时的已有代次。路由恢复不能重建该意图。 */
export function captureChatOpenIntent(client: QueryClient, chatId: string) {
  const reading = cachedReading(client, chatId);
  openIntents.delete(client);
  if (!reading?.complete) return undefined;
  const token = crypto.randomUUID();
  openIntents.set(client, {
    token,
    chatId,
    generation: reading.unreadGeneration,
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

function serializeChatWrite(
  client: QueryClient,
  chatId: string,
  write: () => Promise<void>,
) {
  let operations = chatOperations.get(client);
  if (!operations) {
    operations = new Map();
    chatOperations.set(client, operations);
  }
  const pending = operations;
  const operation = (pending.get(chatId) ?? Promise.resolve())
    .catch(() => {})
    .then(write);
  pending.set(chatId, operation);
  return operation.finally(() => {
    if (pending.get(chatId) === operation) pending.delete(chatId);
  });
}

async function refreshChatReading(client: QueryClient, chatId: string) {
  const options = chatReadingOptions(chatId);
  await refreshQueryFromEvent(client, options.queryKey, () =>
    client.fetchQuery({ ...options, staleTime: 0 }),
  );
  return cachedReading(client, chatId);
}

async function writeChatUnread(
  client: QueryClient,
  chatId: string,
  fromMessageID: string,
  generation: number,
) {
  if (openIntents.get(client)?.chatId === chatId) openIntents.delete(client);
  const pending = unreadWrites.get(client) ?? new Map<string, number>();
  unreadWrites.set(client, pending);
  pending.set(chatId, (pending.get(chatId) ?? 0) + 1);
  try {
    await serializeChatWrite(client, chatId, async () => {
      try {
        await markChatUnread(chatId, {
          fromMessageID,
          observedUnreadGeneration: generation,
        });
      } finally {
        await refreshChatReading(client, chatId);
      }
    });
  } finally {
    const count = pending.get(chatId) ?? 0;
    if (count > 1) pending.set(chatId, count - 1);
    else pending.delete(chatId);
  }
}

/** 侧栏手动未读复用对话游标、写入顺序和持久 hold。 */
export async function markLatestChatUnread(
  client: QueryClient,
  chatId: string,
) {
  const state = await refreshChatReading(client, chatId);
  if (!state) throw new Error("Chat reading boundary is unavailable");
  const boundary = state.firstUnreadMessageID ?? state.latestReadableMessageID;
  if (!boundary) return false;
  if (!state.holdUnread)
    await writeChatUnread(client, chatId, boundary, state.unreadGeneration);
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
  const client = useQueryClient();
  const { state: navigationState } = useLocation();
  const token = openToken(navigationState);
  const options = chatReadingOptions(chatId);
  const query = useQuery({ ...options, enabled: active && Boolean(chatId) });
  useQueryEvents({
    topic: "reading",
    resource: { kind: "chat", id: chatId },
    queryKeys: [options.queryKey],
    eventTypes: ["reading.changed"],
    active,
    enabled: Boolean(chatId),
    authority: true,
  });
  const scope = useRef({ active, chatId, token });
  const queuedExplicitToken = useRef<string>(undefined);
  const automatic = useRef<{
    chatId: string;
    generation: number;
    sequence: number;
  }>(undefined);
  useEffect(() => {
    scope.current = { active, chatId, token };
    if (!active && openIntents.get(client)?.token === token)
      openIntents.delete(client);
    return () => {
      scope.current.active = false;
      // Scene 切换的 cleanup 与 Strict Mode 重挂载相同，等新 scope 稳定再丢弃。
      queueMicrotask(() => {
        if (
          (!scope.current.active || scope.current.token !== token) &&
          openIntents.get(client)?.token === token
        )
          openIntents.delete(client);
      });
    };
  }, [active, chatId, client, token]);

  const readState = useCallback(
    () => cachedReading(client, chatId),
    [client, chatId],
  );
  const recover = useCallback(
    () => refreshChatReading(client, chatId),
    [chatId, client],
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
      return serializeChatWrite(client, chatId, async () => {
        if (
          !sceneAvailable() ||
          (command.intent === "automatic" && pendingUnread(client, chatId)) ||
          (begin && !begin())
        )
          return;
        try {
          await markChatRead(chatId, command);
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
          if (sceneAvailable()) await markChatRead(chatId, command);
        }
      });
    },
    [chatId, client, recover],
  );

  const present = useCallback(
    (boundary: VisibleBoundary) => {
      if (!active || !chatId || pendingUnread(client, chatId)) return;
      const state = readState();
      if (!state?.complete) return;
      const pending = openIntents.get(client);
      if (pending && pending.chatId === chatId && pending.token === token) {
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
              chatId,
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
        previous?.chatId === chatId &&
        previous.generation === state.unreadGeneration &&
        previous.sequence >= boundary.sequence
      )
        return;
      const attempt = {
        chatId,
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
    [active, chatId, client, readState, submit, token],
  );

  const markRead = useCallback(async () => {
    const state = readState();
    if (!state?.complete || !state.latestReadableMessageID) return;
    if (openIntents.get(client)?.chatId === chatId) openIntents.delete(client);
    await submit({
      intent: "explicit",
      observedUnreadGeneration: state.unreadGeneration,
      throughMessageID: state.latestReadableMessageID,
    });
  }, [chatId, client, readState, submit]);

  const markUnread = useCallback(
    async (fromMessageID: string) => {
      const state = readState();
      if (!state?.complete || !fromMessageID) return;
      await writeChatUnread(
        client,
        chatId,
        fromMessageID,
        state.unreadGeneration,
      );
    },
    [chatId, client, readState],
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
  onClick,
  to,
  ...props
}: ComponentProps<typeof Link> & { chatId?: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  return (
    <Link
      {...props}
      to={to}
      onClick={(event) => {
        onClick?.(event);
        if (
          !chatId ||
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
        navigate(to, { state: captureChatOpenIntent(client, chatId) });
      }}
    />
  );
}
