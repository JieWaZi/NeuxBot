export { listRuntimesOptions } from "@/shared/api/runtime-queries";
export {
  cancelChatExecutionMutation,
  createChatMutation,
  deleteChatMutation,
  getChatOptions,
  getChatQueryKey,
  getChatMessageFileChangeOptions,
  getChatMessageFileChangeQueryKey,
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
} from "@tanstack/react-query";
import { useEffect } from "react";

import { clearChatLastActiveIfMatches } from "@/shared/lib/chat-navigation";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";

import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { listChatMessages, listChats } from "@/shared/api/generated/sdk.gen";
import type {
  GetChatResponse,
  ListChatMessagesResponse,
  ListChatsResponse,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
} from "@/shared/api/generated/types.gen";

import type { ChatEvent } from "@/shared/api/events";
import type { ChatMessageSource } from "../runtime/chat-projection";

type ChatCursor = {
  beforeUpdatedAt: number;
  beforeId: string;
};

type MessageCursor = {
  beforeSequence: number;
};

export const chatListKey = (wakerId: string) =>
  ["chats", "list", wakerId] as const;

export const chatMessagesKey = (chatId: string) =>
  ["chats", "messages", chatId] as const;

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
  void queryClient.cancelQueries(
    { queryKey: key, exact: true },
    { revert: false },
  );
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
      return (
        queryClient.getQueryData<InfiniteData<ListChatMessagesResponse>>(key)
          ?.pages[0]?.latestSequence ?? 0
      );
    },
  };
}

/** 只接纳从原历史边界连续到达的持久快照，保留已加载窗口和消息身份。 */
export function mergeChatHistoryEvents(
  queryClient: QueryClient,
  chatId: string,
  events: readonly ChatEvent[],
): number | undefined {
  const key = chatMessagesKey(chatId);
  const current =
    queryClient.getQueryData<
      InfiniteData<ListChatMessagesResponse, MessageCursor | undefined>
    >(key);
  const head = current?.pages[0];
  if (!current || !head) return undefined;
  let sequence = head.latestSequence ?? 0;
  let forkLatestExecutionId = head.forkLatestExecutionId;
  const updates = new Map<string, ChatMessage>();
  for (const event of events) {
    if (event.transient) continue;
    const next = event.sequence;
    if (!Number.isSafeInteger(next) || next === undefined || next <= 0)
      return undefined;
    if (next <= sequence) continue;
    if (next !== sequence + 1) return undefined;
    sequence = next;
    if (!event.message) continue;
    if (!event.message.id) return undefined;
    if (
      ["execution.created", "message.inherited"].includes(event.type ?? "") &&
      event.message.role === "assistant" &&
      (event.message.inherited ? event.message.id : event.message.executionId)
    )
      forkLatestExecutionId = event.message.inherited
        ? event.message.id
        : event.message.executionId;
    // 同一批保留最后一份完整快照，连续性仍基于全部原始事件验证。
    updates.set(event.message.id, {
      ...event.message,
      createdAt: event.message.createdAt ?? event.recordedAt,
      updatedAt: event.message.updatedAt ?? event.recordedAt,
    });
  }
  if (sequence === (head.latestSequence ?? 0)) return sequence;
  const added = new Map(updates);
  const pages = current.pages.map((page) => {
    let changed = false;
    const resources = page.resources?.map((message) => {
      const snapshot = message.id ? updates.get(message.id) : undefined;
      if (!snapshot) return message;
      changed = true;
      added.delete(message.id ?? "");
      return snapshot;
    });
    return changed ? { ...page, resources } : page;
  });
  queryClient.setQueryData(key, {
    ...current,
    pages: pages.map((page, index) =>
      index === 0
        ? {
            ...page,
            latestSequence: sequence,
            forkLatestExecutionId,
            resources: added.size
              ? [...(page.resources ?? []), ...added.values()]
              : page.resources,
          }
        : page,
    ),
  });
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

function isMessageHistory(
  value: unknown,
): value is InfiniteData<ListChatMessagesResponse, MessageCursor | undefined> {
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
        if (previousTerminal !== incomingTerminal)
          return previousTerminal ? previous : message;
        return (previous.revision ?? -1) > (message.revision ?? -1)
          ? previous
          : message;
      }),
    };
  });
  return replaceEqualDeep(current, { ...incoming, pages });
}

export function chatMessagesInfiniteOptions(chatId: string) {
  return infiniteQueryOptions({
    queryKey: chatMessagesKey(chatId),
    initialPageParam: undefined as MessageCursor | undefined,
    queryFn: async ({ pageParam, signal, client }) => {
      if (pageParam) {
        // JSONL 只追加；固定游标的旧窗口可复用，新边界必须重新读取。
        const cached = client.getQueryData<
          InfiniteData<ListChatMessagesResponse, MessageCursor | undefined>
        >(chatMessagesKey(chatId));
        const index = cached?.pageParams.findIndex(
          (cursor) => cursor?.beforeSequence === pageParam.beforeSequence,
        );
        if (index !== undefined && index >= 0) {
          const page = cached?.pages[index];
          if (page) return page;
        }
      }
      const { data } = await listChatMessages({
        path: { chatId },
        query: { limit: 50, ...pageParam },
        signal,
        throwOnError: true,
      });
      // 请求发起后，连续持久事件可能已接管头部；迟到快照不能回退事实或游标。
      const current = client.getQueryData<
        InfiniteData<ListChatMessagesResponse, MessageCursor | undefined>
      >(chatMessagesKey(chatId))?.pages[0];
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
            .cancelQueries(filters, { revert: false })
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
