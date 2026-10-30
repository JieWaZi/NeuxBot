import {
  InfiniteQueryObserver,
  QueryObserver,
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
  commitAcceptedChat,
  chatListKey,
  chatMessagesKey,
  chatMessagesInfiniteOptions,
  useChatHistoryVisibility,
  evictDeletedChatsFromCache,
  nextChatCursor,
  nextMessageCursor,
  mergeChatHistoryEvents,
  chatMessageSource,
} from "./chat-queries";
import {
  getChatOptions,
  getChatQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";

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

  it("持久消息修订更新原所属分页，新增消息只追加当前窗口", () => {
    const queryClient = new QueryClient();
    const unchanged = {
      resources: [{ id: "oldest", role: "user", content: "最早" }],
      hasMore: false,
    };
    queryClient.setQueryData(chatMessagesKey("pages"), {
      pages: [
        {
          resources: [
            {
              id: "current",
              role: "assistant",
              status: "running",
              revision: 20,
            },
          ],
          latestSequence: 20,
          hasMore: true,
          nextBeforeSequence: 10,
        },
        {
          resources: [
            {
              id: "old-assistant",
              role: "assistant",
              revision: 5,
              displayParts: [
                { id: "tool", type: "tool", toolStatus: "running" },
              ],
            },
          ],
          hasMore: true,
          nextBeforeSequence: 3,
        },
        unchanged,
      ],
      pageParams: [undefined, { beforeSequence: 10 }, { beforeSequence: 3 }],
    });
    expect(
      mergeChatHistoryEvents(queryClient, "pages", [
        {
          sequence: 21,
          type: "approval.resolved",
          message: {
            id: "old-assistant",
            role: "assistant",
            revision: 21,
            displayParts: [
              { id: "tool", type: "tool", toolStatus: "completed" },
            ],
          },
        },
        {
          sequence: 22,
          type: "message.created",
          message: {
            id: "next-user",
            role: "user",
            content: "新输入",
            revision: 22,
          },
        },
      ]),
    ).toBe(22);
    const data = queryClient.getQueryData<{
      pages: {
        resources: { id: string; displayParts?: { toolStatus?: string }[] }[];
      }[];
      pageParams: unknown[];
    }>(chatMessagesKey("pages"));
    expect(data).toMatchObject({
      pages: [
        {
          resources: [
            { id: "current" },
            { id: "next-user", content: "新输入" },
          ],
          latestSequence: 22,
          hasMore: true,
          nextBeforeSequence: 10,
        },
        {
          resources: [
            {
              id: "old-assistant",
              displayParts: [{ toolStatus: "completed" }],
            },
          ],
          nextBeforeSequence: 3,
        },
        unchanged,
      ],
      pageParams: [undefined, { beforeSequence: 10 }, { beforeSequence: 3 }],
    });
    queryClient.clear();
  });

  it("缺口批次不提交部分消息和游标，transient 也不写入持久缓存", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(chatMessagesKey("gap"), {
      pages: [{ resources: [], latestSequence: 4, hasMore: false }],
      pageParams: [undefined],
    });
    const before = queryClient.getQueryData(chatMessagesKey("gap"));
    expect(
      mergeChatHistoryEvents(queryClient, "gap", [
        {
          sequence: 5,
          type: "message.created",
          message: { id: "new", role: "user" },
        },
        {
          sequence: 7,
          type: "execution.completed",
          message: { id: "assistant", role: "assistant", status: "completed" },
        },
      ]),
    ).toBeUndefined();
    expect(queryClient.getQueryData(chatMessagesKey("gap"))).toBe(before);
    expect(
      mergeChatHistoryEvents(queryClient, "gap", [
        {
          transient: true,
          sequence: 999,
          message: { id: "live", role: "assistant" },
        },
      ]),
    ).toBe(4);
    expect(queryClient.getQueryData(chatMessagesKey("gap"))).toBe(before);
    expect(
      mergeChatHistoryEvents(queryClient, "unknown", [{ sequence: 1 }]),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(chatMessagesKey("unknown")),
    ).toBeUndefined();
    queryClient.clear();
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

  it("迟到的旧历史响应不覆盖已经合并的持久消息和游标", async () => {
    let release!: () => void;
    let reads = 0;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-late/messages",
        async () => {
          reads++;
          if (reads > 1)
            await new Promise<void>((resolve) => {
              release = resolve;
            });
          return HttpResponse.json({
            resources: [
              {
                id: "assistant",
                role: "assistant",
                status: "running",
                revision: 2,
              },
            ],
            latestSequence: 2,
            hasMore: false,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-late"),
    );
    const unsubscribe = observer.subscribe(() => {});
    try {
      await observer.refetch();
      const refresh = observer.refetch();
      await waitFor(() => expect(reads).toBe(2));
      queryClient.setQueryData(chatMessagesKey("history-late"), {
        pages: [
          {
            resources: [
              {
                id: "assistant",
                role: "assistant",
                status: "completed",
                revision: 4,
              },
              {
                id: "extra",
                role: "assistant",
                content: "第二条答复",
                revision: 3,
              },
            ],
            latestSequence: 4,
            hasMore: false,
          },
        ],
        pageParams: [undefined],
      });
      release();
      await refresh;
      expect(observer.getCurrentResult().data?.pages[0]).toMatchObject({
        latestSequence: 4,
        resources: [
          { id: "assistant", status: "completed", revision: 4 },
          { id: "extra", content: "第二条答复" },
        ],
      });
    } finally {
      unsubscribe();
      queryClient.clear();
    }
  });

  it("旧分页读取期间到达的消息修订不会被迟到分页覆盖，后续修订更新所有重叠窗口", async () => {
    let release!: () => void;
    let olderStarted = false;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-old-page/messages",
        async ({ request }) => {
          const older = new URL(request.url).searchParams.has("beforeSequence");
          if (older) {
            olderStarted = true;
            await new Promise<void>((resolve) => {
              release = resolve;
            });
            return HttpResponse.json({
              resources: [
                {
                  id: "old-assistant",
                  role: "assistant",
                  status: "running",
                  revision: 3,
                },
              ],
              hasMore: false,
            });
          }
          return HttpResponse.json({
            resources: [{ id: "current", role: "user", revision: 10 }],
            latestSequence: 10,
            hasMore: true,
            nextBeforeSequence: 5,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-old-page"),
    );
    const unsubscribe = observer.subscribe(() => {});
    try {
      await observer.refetch();
      const older = observer.fetchNextPage();
      await waitFor(() => expect(olderStarted).toBe(true));
      mergeChatHistoryEvents(queryClient, "history-old-page", [
        {
          sequence: 11,
          type: "execution.completed",
          message: {
            id: "old-assistant",
            role: "assistant",
            status: "completed",
            revision: 11,
          },
        },
      ]);
      release();
      await older;
      expect(
        observer.getCurrentResult().data?.pages[1]?.resources?.[0],
      ).toMatchObject({
        id: "old-assistant",
        status: "completed",
        revision: 11,
      });
      mergeChatHistoryEvents(queryClient, "history-old-page", [
        {
          sequence: 12,
          type: "runtime.event",
          message: {
            id: "old-assistant",
            role: "assistant",
            status: "completed",
            revision: 12,
            content: "补充工具结果",
          },
        },
      ]);
      const copies = observer
        .getCurrentResult()
        .data?.pages.flatMap((page) => page.resources ?? [])
        .filter((message) => message.id === "old-assistant");
      expect(copies).toHaveLength(2);
      expect(copies?.map((message) => message.revision)).toEqual([12, 12]);
    } finally {
      unsubscribe();
      queryClient.clear();
    }
  });

  it("普通消息来源恢复只读取原权威快照，保留较新事实后返回同一持久游标", async () => {
    let releaseOld!: () => void;
    let reads = 0;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-recovery/messages",
        async () => {
          reads++;
          const captured = reads;
          if (captured === 2)
            await new Promise<void>((resolve) => {
              releaseOld = resolve;
            });
          return HttpResponse.json({
            resources: [
              {
                id: "assistant",
                role: "assistant",
                status: captured < 3 ? "running" : "completed",
                revision: captured < 3 ? 1 : 2002,
              },
            ],
            latestSequence: captured < 3 ? 1 : 2002,
            hasMore: false,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-recovery"),
    );
    const unsubscribe = observer.subscribe(() => {});
    try {
      await observer.refetch();
      const old = observer.refetch();
      await waitFor(() => expect(reads).toBe(2));
      mergeChatHistoryEvents(queryClient, "history-recovery", [
        {
          sequence: 2,
          type: "runtime.event",
          message: {
            id: "assistant",
            role: "assistant",
            status: "running",
            revision: 2,
          },
        },
      ]);
      const source = chatMessageSource(queryClient, "history-recovery", {
        messages: [],
        latestSequence: 2,
        ready: true,
      });
      expect(await source.reconcile()).toBe(2002);
      expect(reads).toBe(3);
      expect(observer.getCurrentResult().data?.pages[0]).toMatchObject({
        latestSequence: 2002,
        resources: [{ id: "assistant", status: "completed" }],
      });
      releaseOld();
      await old;
      expect(observer.getCurrentResult().data?.pages[0]?.latestSequence).toBe(
        2002,
      );
    } finally {
      releaseOld?.();
      unsubscribe();
      queryClient.clear();
    }
  });

  it("真实头部终态快照淘汰在途旧metadata GET，消息提交不等待metadata", async () => {
    let releaseOld!: () => void;
    let releaseFresh!: () => void;
    let detailReads = 0;
    server.use(
      http.get("http://127.0.0.1:19876/api/v1/chats/history-meta", async () => {
        detailReads++;
        const captured = detailReads;
        await new Promise<void>((resolve) => {
          if (captured === 1) releaseOld = resolve;
          else releaseFresh = resolve;
        });
        return HttpResponse.json({
          chat: {
            id: "history-meta",
            latestSequence: captured === 1 ? 4 : 8,
            activeExecutionId: captured === 1 ? "execution-1" : "",
            title: captured === 1 ? "旧标题" : "完成后的标题",
          },
        });
      }),
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-meta/messages",
        () =>
          HttpResponse.json({
            resources: [
              {
                id: "execution-1",
                executionId: "execution-1",
                role: "assistant",
                status: "completed",
                revision: 8,
              },
            ],
            latestSequence: 8,
            hasMore: false,
          }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const detailKey = getChatQueryKey({ path: { chatId: "history-meta" } });
    queryClient.setQueryData(detailKey, {
      chat: {
        id: "history-meta",
        activeExecutionId: "execution-1",
        latestSequence: 4,
        title: "旧标题",
      },
    });
    const detail = new QueryObserver(queryClient, {
      ...getChatOptions({ path: { chatId: "history-meta" } }),
      staleTime: 10_000,
    });
    const stopDetail = detail.subscribe(() => {});
    const old = detail.refetch();
    await waitFor(() => expect(detailReads).toBe(1));
    const messages = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-meta"),
    );
    const stopMessages = messages.subscribe(() => {});
    try {
      await messages.refetch();
      expect(messages.getCurrentResult().data?.pages[0]?.latestSequence).toBe(
        8,
      );
      await waitFor(() => expect(detailReads).toBe(2));
      expect(detail.getCurrentResult().data?.chat?.latestSequence).toBe(4);
      releaseFresh();
      await waitFor(() =>
        expect(detail.getCurrentResult().data?.chat?.latestSequence).toBe(8),
      );
      releaseOld();
      await old;
      expect(detail.getCurrentResult().data?.chat?.title).toBe("完成后的标题");
      expect(detailReads).toBe(2);
    } finally {
      releaseOld?.();
      releaseFresh?.();
      stopMessages();
      stopDetail();
      queryClient.clear();
    }
  });

  it("运行中快照里的已完成用户消息不能触发执行终态metadata对账", async () => {
    let detailReads = 0;
    server.use(
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-meta-running",
        () => {
          detailReads++;
          return HttpResponse.json({
            chat: {
              id: "history-meta-running",
              latestSequence: 8,
              activeExecutionId: "execution-1",
            },
          });
        },
      ),
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-meta-running/messages",
        () =>
          HttpResponse.json({
            resources: [
              {
                id: "user-1",
                executionId: "execution-1",
                role: "user",
                status: "completed",
                revision: 6,
              },
              {
                id: "execution-1",
                executionId: "execution-1",
                role: "assistant",
                status: "running",
                revision: 8,
              },
            ],
            latestSequence: 8,
            hasMore: false,
          }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const detailKey = getChatQueryKey({
      path: { chatId: "history-meta-running" },
    });
    queryClient.setQueryData(detailKey, {
      chat: {
        id: "history-meta-running",
        activeExecutionId: "execution-1",
        latestSequence: 4,
      },
    });
    const detail = new QueryObserver(queryClient, {
      ...getChatOptions({ path: { chatId: "history-meta-running" } }),
      staleTime: 10_000,
    });
    const stopDetail = detail.subscribe(() => {});
    const messages = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-meta-running"),
    );
    const stopMessages = messages.subscribe(() => {});
    try {
      await messages.refetch();
      await new Promise<void>((resolve) => setTimeout(resolve, 0));
      expect(detailReads).toBe(0);
      expect(queryClient.getQueryState(detailKey)?.isInvalidated).toBe(false);
    } finally {
      stopDetail();
      stopMessages();
      queryClient.clear();
    }
  });

  it("头部快照未领先metadata时不再刷新详情", async () => {
    let detailReads = 0;
    server.use(
      http.get("http://127.0.0.1:19876/api/v1/chats/history-meta-fresh", () => {
        detailReads++;
        return HttpResponse.json({
          chat: { id: "history-meta-fresh", latestSequence: 9 },
        });
      }),
      http.get(
        "http://127.0.0.1:19876/api/v1/chats/history-meta-fresh/messages",
        () =>
          HttpResponse.json({
            resources: [
              {
                id: "execution-1",
                executionId: "execution-1",
                role: "assistant",
                status: "completed",
                revision: 8,
              },
            ],
            latestSequence: 8,
            hasMore: false,
          }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(
      getChatQueryKey({ path: { chatId: "history-meta-fresh" } }),
      {
        chat: {
          id: "history-meta-fresh",
          activeExecutionId: "execution-1",
          latestSequence: 9,
        },
      },
    );
    const detail = new QueryObserver(queryClient, {
      ...getChatOptions({ path: { chatId: "history-meta-fresh" } }),
      staleTime: 10_000,
    });
    const stopDetail = detail.subscribe(() => {});
    const messages = new InfiniteQueryObserver(
      queryClient,
      chatMessagesInfiniteOptions("history-meta-fresh"),
    );
    const stopMessages = messages.subscribe(() => {});
    try {
      await messages.refetch();
      await Promise.resolve();
      expect(detailReads).toBe(0);
      expect(
        queryClient.getQueryState(
          getChatQueryKey({ path: { chatId: "history-meta-fresh" } }),
        )?.isInvalidated,
      ).toBe(false);
    } finally {
      stopDetail();
      stopMessages();
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

describe("accepted Chat snapshot ownership", () => {
  it.each(["ordinary", "member"])(
    "%s 首个接纳快照直接提交原详情，不额外读取",
    (kind) => {
      const queryClient = new QueryClient();
      const id = kind + "-chat";
      const snapshot = {
        id,
        wakerId: "waker",
        latestSequence: 4,
        activeExecutionId: "execution",
      };
      expect(commitAcceptedChat(queryClient, snapshot)).toBe(true);
      expect(
        queryClient.getQueryData(getChatQueryKey({ path: { chatId: id } })),
      ).toEqual({ chat: snapshot });
      expect(
        queryClient.getQueryCache().findAll({ fetchStatus: "fetching" }),
      ).toHaveLength(0);
      expect(commitAcceptedChat(queryClient, { id, wakerId: "waker" })).toBe(
        false,
      );
      queryClient.clear();
    },
  );
  it.each([10, 8])(
    "已提交终态序号10不能被相同或更早回执%s恢复为运行中",
    (sequence) => {
      const queryClient = new QueryClient();
      const key = getChatQueryKey({ path: { chatId: "chat" } });
      const terminal = {
        chat: {
          id: "chat",
          latestSequence: 10,
          lastEndedExecutionId: "execution",
          lastEndedExecutionStatus: "completed",
        },
      };
      queryClient.setQueryData(key, terminal);
      queryClient.setQueryData(chatMessagesKey("chat"), {
        pages: [
          {
            latestSequence: 10,
            resources: [
              { id: "execution", role: "assistant", status: "completed" },
            ],
          },
        ],
        pageParams: [undefined],
      });
      expect(
        commitAcceptedChat(queryClient, {
          id: "chat",
          latestSequence: sequence,
          activeExecutionId: "execution",
        }),
      ).toBe(true);
      expect(queryClient.getQueryData(key)).toEqual(terminal);
      queryClient.clear();
    },
  );
});
