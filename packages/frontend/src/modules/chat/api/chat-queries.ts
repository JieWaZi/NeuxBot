export { listRuntimesOptions } from "@/shared/api/runtime-queries";
export {
  cancelChatExecutionMutation,
  createChatMutation,
  deleteChatMutation,
  getChatQueryKey,
  getChatMessageFileChangeOptions,
  getChatMessageFileChangesOptions,
  getChatMessageFileChangeQueryKey,
  listChatsOptions,
  listChatsQueryKey,
  listChatMessagesOptions,
  listChatMessagesQueryKey,
  listRuntimesQueryKey,
  resolveApprovalMutation,
  resolveElicitationMutation,
  listWakerSkillsOptions,
  listWakerSkillsQueryKey,
  listWakerChatsOptions,
  listWakerChatsQueryKey,
  sendMessageMutation,
  updateChatMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

import {
  infiniteQueryOptions,
  replaceEqualDeep,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
  type QueryFunctionContext,
} from "@tanstack/react-query";
import { useEffect } from "react";

import { clearChatLastActiveIfMatches } from "@/shared/lib/chat-navigation";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";

import {
  getChatOptions as generatedGetChatOptions,
  getChatQueryKey,
  listChatSkillSuggestionsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  getChat,
  listChatMessages,
  listChats,
} from "@/shared/api/generated/sdk.gen";
import type {
  GetChatResponse,
  ListChatMessagesResponse,
  ListChatsResponse,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
} from "@/shared/api/generated/types.gen";

import type { ChatEvent } from "@/shared/api/events";
import type { ChatMessageSource } from "../runtime/chat-projection";
import { chatEventCreatedAt } from "../model/chat-message-time";

type ChatCursor = {
  beforeUpdatedAt: number;
  beforeId: string;
};

type MessageCursor = {
  beforeSequence: number;
};

type ChatMessageHistory = InfiniteData<
  ListChatMessagesResponse,
  MessageCursor | undefined
> & {
  deliverySequence?: number;
};

function invalidateResourceTimeline(queryClient: QueryClient, chatId: string) {
  void queryClient.invalidateQueries({
    queryKey: listChatSkillSuggestionsQueryKey({ path: { chatId } }),
  });
}

import { chatListKey } from "@/shared/api/chat-list-key";
export { chatListKey };

export const chatMessagesKey = (chatId: string) =>
  ["chats", "messages", chatId] as const;

/** 详情和预取共用原查询身份，由聊天界面处理读取结果。 */
export function getChatOptions(
  options: Parameters<typeof generatedGetChatOptions>[0],
) {
  return {
    ...generatedGetChatOptions(options),
    queryFn: async ({
      queryKey,
      signal,
    }: QueryFunctionContext<ReturnType<typeof getChatQueryKey>>) => {
      const { data } = await getChat(
        withLocalErrorSurface({
          ...options,
          ...queryKey[0],
          signal,
          throwOnError: true,
        }),
      );
      return data;
    },
  };
}

export function chatReadError(
  query: {
    isError: boolean;
    isFetchNextPageError?: boolean;
    fetchStatus: "idle" | "fetching" | "paused";
    error: unknown;
    refetch: () => Promise<unknown>;
  },
  fallback: string,
) {
  if (
    !query.isError ||
    query.isFetchNextPageError ||
    query.fetchStatus !== "idle"
  )
    return undefined;
  return {
    error: query.error,
    message: localApiErrorMessage(query.error, fallback),
    onRetry: () => void query.refetch(),
  };
}

/** 接纳回执按原持久序号进入详情 Query；消息或终态先到时保留更新事实。 */
export function commitAcceptedChat(
  queryClient: QueryClient,
  acceptedChat: GetChatResponse["chat"],
): boolean {
  const chatId = acceptedChat?.id;
  const sequence = acceptedChat?.latestSequence ?? 0;
  if (!chatId || sequence <= 0) return false;
  const key = getChatQueryKey({ path: { chatId } });
  const current = queryClient.getQueryData<GetChatResponse>(key);
  const history = queryClient.getQueryData<
    InfiniteData<ListChatMessagesResponse>
  >(chatMessagesKey(chatId));
  const historySequence = history?.pages[0]?.latestSequence ?? 0;
  if (
    current?.chat &&
    Math.max(current.chat.latestSequence ?? 0, historySequence) >= sequence
  )
    return true;
  // 旧的在途摘要不能在接纳事实提交后再覆盖原 Query。
  void queryClient.cancelQueries({ queryKey: key, exact: true });
  const terminal = history?.pages
    .flatMap((page) => page.resources ?? [])
    .find(
      (message) =>
        message.executionId === acceptedChat.activeExecutionId &&
        ["completed", "failed", "canceled"].includes(message.status ?? ""),
    );
  queryClient.setQueryData<GetChatResponse>(key, {
    chat: terminal
      ? {
          ...acceptedChat,
          activeExecutionId: undefined,
          latestSequence: Math.max(sequence, historySequence),
        }
      : acceptedChat,
  });
  return true;
}

/** 普通 Chat 的持久事实只写原消息 Query；初次读取仍由该 Query 的活跃消费者负责。 */
export function chatMessageSource(
  queryClient: QueryClient,
  chatId: string | undefined,
  snapshot: ChatMessageSource["snapshot"],
): ChatMessageSource {
  return {
    snapshot,
    commit: (events) =>
      chatId ? mergeChatHistoryEvents(queryClient, chatId, events) : undefined,
    reconcile: async (signal) => {
      if (!chatId) return undefined;
      const key = chatMessagesKey(chatId);
      const before =
        queryClient.getQueryData<ChatMessageHistory>(key)?.pages[0]
          ?.latestSequence ?? 0;
      await refreshQueryFromEvent(
        queryClient,
        key,
        () =>
          queryClient.fetchInfiniteQuery({
            ...chatMessagesInfiniteOptions(chatId),
            staleTime: 0,
          }),
        0,
        signal,
      );
      const after =
        queryClient.getQueryData<InfiniteData<ListChatMessagesResponse>>(key)
          ?.pages[0]?.latestSequence ?? 0;
      if (after > before) invalidateResourceTimeline(queryClient, chatId);
      return after;
    },
  };
}

/** 合并共享传输按序交付的持久快照；内部记录不进入公开事件，游标可以稀疏。 */
export function mergeChatHistoryEvents(
  queryClient: QueryClient,
  chatId: string,
  events: readonly ChatEvent[],
): number | undefined {
  const key = chatMessagesKey(chatId);
  const current = queryClient.getQueryData<ChatMessageHistory>(key);
  const head = current?.pages[0];
  if (!current || !head) return undefined;
  let sequence = head.latestSequence ?? 0;
  let deliverySequence = current.deliverySequence;
  let forkLatestExecutionId = head.forkLatestExecutionId;
  let resourceChanged = false;
  const updates = new Map<string, ChatMessage>();
  const appendable = new Set<string>();
  for (const event of events) {
    if (event.transient) continue;
    const next = event.sequence;
    if (!Number.isSafeInteger(next) || next === undefined || next <= 0)
      return undefined;
    if (next <= sequence) continue;
    sequence = next;
    if (event.type === "skill.changed" || event.type === "connector.changed")
      resourceChanged = true;
    const delivery = event.type === "execution.delivery";
    if (delivery) deliverySequence = next;
    if (event.fileChanges) {
      const id = event.fileChanges.messageId;
      const message =
        updates.get(id) ??
        current.pages
          .flatMap((page) => page.resources ?? [])
          .find((item) => item.id === id);
      if (
        message &&
        (message.fileChangeRecord?.version ?? 0) <
          (event.fileChanges.record.version ?? 0)
      )
        updates.set(id, {
          ...message,
          fileChangeRecord: event.fileChanges.record,
          revision: Math.max(message.revision ?? 0, next),
        });
      continue;
    }
    if (!event.message) continue;
    if (!event.message.id) return undefined;
    if (!delivery) appendable.add(event.message.id);
    if (
      ["execution.created", "message.inherited"].includes(event.type ?? "") &&
      event.message.role === "assistant" &&
      !event.message.question &&
      (event.message.inherited ? event.message.id : event.message.executionId)
    )
      forkLatestExecutionId = event.message.inherited
        ? event.message.id
        : event.message.executionId;
    // 同一批保留最后一份完整快照，游标保留真实权威日志的位置。
    updates.set(event.message.id, {
      ...event.message,
      createdAt: chatEventCreatedAt(
        event,
        updates.get(event.message.id)?.createdAt,
      ),
      updatedAt: event.message.updatedAt ?? event.recordedAt,
    });
  }
  if (sequence === (head.latestSequence ?? 0)) return sequence;
  const pages = current.pages.map((page) => {
    let changed = false;
    const resources = page.resources?.map((message) => {
      const snapshot = message.id ? updates.get(message.id) : undefined;
      if (!snapshot) return message;
      changed = true;
      if (message.id) appendable.delete(message.id);
      return !snapshot.createdAt && message.createdAt
        ? { ...snapshot, createdAt: message.createdAt }
        : snapshot;
    });
    return changed ? { ...page, resources } : page;
  });
  const added = [...updates].flatMap(([id, message]) =>
    appendable.has(id) ? [message] : [],
  );
  queryClient.setQueryData<ChatMessageHistory>(key, {
    ...current,
    deliverySequence,
    pages: pages.map((page, index) =>
      index === 0
        ? {
            ...page,
            latestSequence: sequence,
            forkLatestExecutionId,
            resources: added.length
              ? [...(page.resources ?? []), ...added]
              : page.resources,
          }
        : page,
    ),
  });
  if (resourceChanged) invalidateResourceTimeline(queryClient, chatId);
  return sequence;
}

export function nextChatCursor(
  page: ListChatsResponse,
): ChatCursor | undefined {
  if (!page.hasMore || !page.nextBeforeUpdatedAt || !page.nextBeforeId) {
    return undefined;
  }
  return {
    beforeUpdatedAt: page.nextBeforeUpdatedAt,
    beforeId: page.nextBeforeId,
  };
}

export function nextMessageCursor(
  page: ListChatMessagesResponse,
): MessageCursor | undefined {
  if (!page.hasMore || !page.nextBeforeSequence) return undefined;
  return { beforeSequence: page.nextBeforeSequence };
}

export function chatListInfiniteOptions(wakerId: string) {
  return infiniteQueryOptions({
    queryKey: chatListKey(wakerId),
    initialPageParam: undefined as ChatCursor | undefined,
    queryFn: async ({ pageParam, signal }) => {
      const { data } = await listChats({
        query: { wakerId, limit: 50, ...pageParam },
        signal,
        throwOnError: true,
      });
      return data;
    },
    getNextPageParam: nextChatCursor,
  });
}

function isMessageHistory(value: unknown): value is ChatMessageHistory {
  return (
    typeof value === "object" &&
    value !== null &&
    "pages" in value &&
    Array.isArray(value.pages) &&
    "pageParams" in value &&
    Array.isArray(value.pageParams)
  );
}

/** 请求可能只追加旧页；整个 InfiniteData 提交也必须保留期间到达的持久事实。 */
function reconcileMessageHistory(current: unknown, incoming: unknown) {
  if (!isMessageHistory(current) || !isMessageHistory(incoming))
    return incoming;
  const currentHead = current.pages[0];
  const incomingHead = incoming.pages[0];
  const known = new Map<string | undefined, ChatMessage>();
  for (const message of current.pages.flatMap((page) => page.resources ?? [])) {
    const previous = known.get(message.id);
    if (!previous || (message.revision ?? -1) > (previous.revision ?? -1))
      known.set(message.id, message);
  }
  const pages = incoming.pages.map((page, index) => {
    if (
      index === 0 &&
      (currentHead?.latestSequence ?? 0) > (incomingHead?.latestSequence ?? 0)
    )
      return currentHead;
    return {
      ...page,
      resources: page.resources?.map((message) => {
        const previous = known.get(message.id);
        if (!previous) return message;
        const previousTerminal = ["completed", "failed", "canceled"].includes(
          previous.status ?? "",
        );
        const incomingTerminal = ["completed", "failed", "canceled"].includes(
          message.status ?? "",
        );
        if (
          (previousTerminal && !incomingTerminal) ||
          (previousTerminal === incomingTerminal &&
            (previous.revision ?? -1) > (message.revision ?? -1))
        )
          return previous;
        return !message.createdAt && previous.createdAt
          ? { ...message, createdAt: previous.createdAt }
          : message;
      }),
    };
  });
  return replaceEqualDeep(current, {
    ...incoming,
    deliverySequence: incoming.deliverySequence ?? current.deliverySequence,
    pages,
  });
}

export function chatMessagesInfiniteOptions(chatId: string) {
  const queryKey = chatMessagesKey(chatId);
  let refreshOlder = false;
  return infiniteQueryOptions({
    queryKey,
    initialPageParam: undefined as MessageCursor | undefined,
    queryFn: async ({ pageParam, signal, client }) => {
      const readCache = () => client.getQueryData<ChatMessageHistory>(queryKey);
      if (pageParam && !refreshOlder) {
        // 正常读取复用固定窗口；重连要补读期间未收到的交付。
        const cached = readCache();
        const index = cached?.pageParams.findIndex(
          (cursor) => cursor?.beforeSequence === pageParam.beforeSequence,
        );
        if (index !== undefined && index >= 0) {
          const page = cached?.pages[index];
          if (
            page &&
            !page.resources?.some(
              (message) => message.question && !message.question.result,
            )
          )
            return page;
        }
      }
      const readPage = async () =>
        (
          await listChatMessages(
            withLocalErrorSurface({
              path: { chatId },
              query: { limit: 50, ...pageParam },
              signal,
              throwOnError: true,
            }),
          )
        ).data;
      let deliverySequence = readCache()?.deliverySequence ?? 0;
      let data = await readPage();
      // 正在读取的旧窗口可能还不含交付所更新的消息，补读同一游标。
      while (pageParam && !signal.aborted) {
        const latestDelivery = readCache()?.deliverySequence ?? 0;
        if (
          latestDelivery <= deliverySequence ||
          latestDelivery <= (data.latestSequence ?? 0)
        )
          break;
        deliverySequence = latestDelivery;
        data = await readPage();
      }
      // 请求发起后，连续持久事件可能已接管头部；迟到快照不能回退事实或游标。
      const current = readCache()?.pages[0];
      if (!pageParam && !signal.aborted) {
        refreshOlder =
          (data.latestSequence ?? 0) > (current?.latestSequence ?? 0);
      }
      if (
        !pageParam &&
        current &&
        (current.latestSequence ?? 0) > (data.latestSequence ?? 0)
      ) {
        return current;
      }
      if (!pageParam) {
        const detailKey = getChatQueryKey({ path: { chatId } });
        const needsMetadata = (current: GetChatResponse | undefined) =>
          Boolean(
            current?.chat?.activeExecutionId &&
            (data.latestSequence ?? 0) > (current.chat.latestSequence ?? 0) &&
            data.resources?.some(
              (message) =>
                message.role === "assistant" &&
                !message.question &&
                message.executionId === current.chat?.activeExecutionId &&
                ["completed", "failed", "canceled"].includes(
                  message.status ?? "",
                ),
            ),
          );
        if (needsMetadata(client.getQueryData<GetChatResponse>(detailKey))) {
          // 真正的 HTTP 头部快照证明详情已落后；只淘汰旧元数据读取，不阻塞消息提交。
          const filters = { queryKey: detailKey, exact: true };
          void client
            .cancelQueries(filters)
            .then(() => {
              if (
                needsMetadata(client.getQueryData<GetChatResponse>(detailKey))
              )
                return client.invalidateQueries(filters);
            })
            .catch(() => {});
        }
      }
      return data;
    },
    getNextPageParam: nextMessageCursor,
    structuralSharing: reconcileMessageHistory,
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
  });
}

/** 隐藏会话只标记历史陈旧，恢复可见时对账最新窗口，保留已加载的旧页。 */
export function useChatHistoryVisibility(
  chatId: string | undefined,
  active: boolean,
) {
  const client = useQueryClient();
  useEffect(() => {
    if (!chatId) return;
    const markStale = () => {
      void client.invalidateQueries({
        queryKey: chatMessagesKey(chatId),
        refetchType: "none",
      });
    };
    if (!active) markStale();
    return markStale;
  }, [active, chatId, client]);
}

function filterDeletedChatsFromListPages<
  TPage extends { resources?: readonly { id?: string }[] },
>(pages: readonly TPage[], deletedIds: ReadonlySet<string>): TPage[] {
  return pages.map((page) => ({
    ...page,
    resources: page.resources?.filter((chat) => !deletedIds.has(chat.id ?? "")),
  }));
}

export function evictDeletedChatsFromCache(
  queryClient: QueryClient,
  chatIds: readonly string[],
) {
  const deletedIds = new Set(
    chatIds.map((chatId) => chatId.trim()).filter(Boolean),
  );
  if (deletedIds.size === 0) return;

  for (const chatId of deletedIds) {
    queryClient.removeQueries({
      queryKey: chatMessagesKey(chatId),
    });
    queryClient.removeQueries({
      queryKey: getChatQueryKey({ path: { chatId } }),
    });
    clearChatLastActiveIfMatches(chatId);
  }

  queryClient.setQueriesData<InfiniteData<ListChatsResponse>>(
    { queryKey: ["chats", "list"] },
    (current) => {
      if (!current) return current;
      return {
        ...current,
        pages: filterDeletedChatsFromListPages(current.pages, deletedIds),
      };
    },
  );
}
