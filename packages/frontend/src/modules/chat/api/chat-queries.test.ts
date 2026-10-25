import {
  InfiniteQueryObserver,
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";

import {
  chatListKey,
  chatMessagesKey,
  chatMessagesInfiniteOptions,
  useChatHistoryVisibility,
  evictDeletedChatsFromCache,
  nextChatCursor,
  nextMessageCursor,
} from "./chat-queries";
import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";

describe("chat query cursors", () => {
  it("按伙伴和 Chat 隔离分页缓存", () => {
    expect(chatListKey("waker-1")).not.toEqual(chatListKey("waker-2"));
    expect(chatMessagesKey("chat-1")).not.toEqual(chatMessagesKey("chat-2"));
  });

  it("只在服务端确认还有下一页时返回稳定游标", () => {
    expect(
      nextChatCursor({
        hasMore: true,
        nextBeforeUpdatedAt: 100,
        nextBeforeId: "chat-50",
      }),
    ).toEqual({ beforeUpdatedAt: 100, beforeId: "chat-50" });
    expect(
      nextChatCursor({
        hasMore: false,
        nextBeforeUpdatedAt: 100,
        nextBeforeId: "chat-50",
      }),
    ).toBeUndefined();
    expect(
      nextMessageCursor({ hasMore: true, nextBeforeSequence: 81 }),
    ).toEqual({ beforeSequence: 81 });
    expect(nextMessageCursor({ hasMore: true })).toBeUndefined();
  });

  it("evicts deleted chats from list, detail and message caches", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(chatMessagesKey("chat-1"), {
      pages: [{ resources: [{ id: "message-1" }], hasMore: false }],
      pageParams: [undefined],
    });
    queryClient.setQueryData(getChatQueryKey({ path: { chatId: "chat-1" } }), {
      chat: { id: "chat-1", wakerId: "waker-1" },
    });
    queryClient.setQueryData(chatListKey("waker-1"), {
      pages: [
        {
          resources: [
            { id: "chat-1", wakerId: "waker-1" },
            { id: "chat-2", wakerId: "waker-1" },
          ],
          hasMore: false,
        },
      ],
      pageParams: [undefined],
    });

    evictDeletedChatsFromCache(queryClient, ["chat-1"]);

    expect(queryClient.getQueryData(chatMessagesKey("chat-1"))).toBeUndefined();
    expect(
      queryClient.getQueryData(getChatQueryKey({ path: { chatId: "chat-1" } })),
    ).toBeUndefined();
    expect(queryClient.getQueryData(chatListKey("waker-1"))).toEqual({
      pages: [
        {
          resources: [{ id: "chat-2", wakerId: "waker-1" }],
          hasMore: false,
        },
      ],
      pageParams: [undefined],
    });
  });
});

describe("chat history network budget", () => {
  const server = setupServer();
  beforeAll(() => {
    client.setConfig({ baseUrl: "http://127.0.0.1:19876" });
    server.listen({ onUnhandledRequest: "error" });
  });
  afterAll(() => server.close());

  it("隐藏时仅标记历史陈旧，不发请求；五分钟内恢复可见也会对账最新窗口", async () => {
    let reads = 0;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-visible/messages",
        () => {
          reads++;
          return HttpResponse.json({
            resources: [
              { id: "assistant", content: reads === 1 ? "处理中" : "已完成" },
            ],
            hasMore: false,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: queryClient }, children);
    const { result, rerender, unmount } = renderHook(
      ({ active }) => {
        useChatHistoryVisibility("history-visible", active);
        return useInfiniteQuery({
          ...chatMessagesInfiniteOptions("history-visible"),
          enabled: active,
        });
      },
      {
        wrapper,
        initialProps: { active: true },
      },
    );
    try {
      await waitFor(() =>
        expect(result.current.data?.pages[0]?.resources?.[0]?.content).toBe(
          "处理中",
        ),
      );
      rerender({ active: false });
      await act(async () => {
        await Promise.resolve();
      });
      expect(reads).toBe(1);
      rerender({ active: true });
      await waitFor(() =>
        expect(result.current.data?.pages[0]?.resources?.[0]?.content).toBe(
          "已完成",
        ),
      );
      expect(reads).toBe(2);
    } finally {
      unmount();
      queryClient.clear();
    }
  });

  it("首屏只读最近50条，上翻按游标读；刷新复用游标未变化的旧页", async () => {
    const received: (number | null)[] = [];
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-budget/messages",
        ({ request }) => {
          const params = new URL(request.url).searchParams;
          expect(params.get("limit")).toBe("50");
          const before = params.has("beforeSequence")
            ? Number(params.get("beforeSequence"))
            : null;
          received.push(before);
          return HttpResponse.json({
            resources: [
              {
                id: `message-${before ?? "latest"}`,
                role: "assistant",
                status: "completed",
              },
            ],
            hasMore: before !== 41,
            nextBeforeSequence:
              before === null ? 81 : before === 81 ? 41 : undefined,
            latestSequence: before === null ? 120 : undefined,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-budget"),
    );
    const unsubscribe = observer.subscribe(() => {});
    try {
      await observer.refetch();
      expect(received).toEqual([null]);
      await observer.fetchNextPage();
      await observer.fetchNextPage();
      expect(received).toEqual([null, 81, 41]);
      await observer.refetch();
      expect(received).toEqual([null, 81, 41, null]);
      expect(observer.getCurrentResult().data?.pages).toHaveLength(3);
    } finally {
      unsubscribe();
      queryClient.clear();
    }
  });

  it("新消息改变分页边界时读取新游标窗口，不能把不匹配的旧页当作完整历史", async () => {
    const received: (number | null)[] = [];
    let updated = false;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-shift/messages",
        ({ request }) => {
          const params = new URL(request.url).searchParams;
          const before = params.has("beforeSequence")
            ? Number(params.get("beforeSequence"))
            : null;
          received.push(before);
          return HttpResponse.json({
            resources: [
              { id: `message-${before ?? (updated ? "new" : "old")}` },
            ],
            hasMore: before === null,
            nextBeforeSequence:
              before === null ? (updated ? 91 : 81) : undefined,
            latestSequence: before === null ? (updated ? 130 : 120) : undefined,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-shift"),
    );
    const unsubscribe = observer.subscribe(() => {});
    try {
      await observer.refetch();
      await observer.fetchNextPage();
      updated = true;
      await observer.refetch();
      expect(received).toEqual([null, 81, null, 91]);
      expect(observer.getCurrentResult().data?.pageParams).toEqual([
        undefined,
        { beforeSequence: 91 },
      ]);
      expect(
        observer.getCurrentResult().data?.pages[1]?.resources?.[0]?.id,
      ).toBe("message-91");
    } finally {
      unsubscribe();
      queryClient.clear();
    }
  });
});
