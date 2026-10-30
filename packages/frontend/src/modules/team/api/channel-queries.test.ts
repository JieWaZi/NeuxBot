import {
  InfiniteQueryObserver,
  QueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import {
  commitTeamTimelineEvents,
  commitTeamTimelineProjectionEvent,
  reconcileTeamTimelineQuery,
  teamQueryKeys,
  teamTimelineInfiniteOptions,
} from "./channel-queries";

import { getChannelThreadQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import type {
  ListChannelTimelineResponse,
  GithubComJieWaZiNeuxBotServerApisChannelsChannelThreadDetail as ChannelThreadDetail,
} from "@/shared/api/generated/types.gen";

const server = setupServer();
beforeAll(() => {
  client.setConfig({ baseUrl: "http://127.0.0.1:19877" });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

describe("team timeline query ownership", () => {
  it("真实线程导航key失效不重读已加载的时间线页", async () => {
    const reads: string[] = [];
    server.use(
      http.get(
        "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
        ({ request }) => {
          const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
          reads.push(cursor);
          return HttpResponse.json({
            resources: [
              {
                chatId: "lead",
                coordinator: true,
                message: { id: cursor || "latest", role: "assistant" },
              },
            ],
            hasMore: !cursor,
            nextCursor: cursor ? "" : "older",
            leadChatLatestSequence: cursor ? undefined : 7,
          });
        },
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const observer = new InfiniteQueryObserver(
      queryClient,
      teamTimelineInfiniteOptions("channel", "thread"),
    );
    const stop = observer.subscribe(() => {});
    try {
      await observer.refetch();
      await observer.fetchNextPage();
      expect(reads).toEqual(["", "older"]);
      await queryClient.invalidateQueries({
        queryKey: teamQueryKeys.threads("channel"),
      });
      expect(reads).toEqual(["", "older"]);
      expect(observer.getCurrentResult().data?.pages).toHaveLength(2);
    } finally {
      stop();
      queryClient.clear();
      server.resetHandlers();
    }
  });
});

it("主Chat连续终态只提交原时间线，保留旧页、待投递输入与成员报告并收敛准确活动", () => {
  const queryClient = new QueryClient();
  const key = teamQueryKeys.timelinePages("channel", "thread");
  const old = {
    resources: [
      {
        chatId: "lead",
        coordinator: true,
        message: {
          id: "older",
          role: "assistant" as const,
          status: "running" as const,
        },
      },
    ],
  };
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 7,
          resources: [
            { chatId: "member", message: { id: "pending", role: "user" } },
            {
              chatId: "lead",
              coordinator: true,
              directDelivered: true,
              message: { id: "report", role: "user", actor: "team_member" },
            },
          ],
        },
        old,
      ],
      pageParams: ["", "older"],
    },
  );
  const detailKey = getChannelThreadQueryKey({
    path: { channelId: "channel", threadId: "thread" },
  });
  queryClient.setQueryData<ChannelThreadDetail>(detailKey, {
    leadActiveExecutionId: "current",
  });
  expect(
    commitTeamTimelineEvents(
      queryClient,
      "channel",
      "thread",
      "lead",
      "lead-waker",
      [
        {
          sequence: 8,
          message: {
            id: "older",
            role: "assistant",
            status: "completed",
            executionId: "current",
          },
        },
        {
          sequence: 9,
          type: "execution.completed",
          message: {
            id: "current",
            role: "assistant",
            status: "completed",
            executionId: "current",
          },
        },
      ],
    ),
  ).toBe(9);
  const data =
    queryClient.getQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
      key,
    );
  expect(data?.pages[0]?.resources?.map((item) => item.message?.id)).toEqual([
    "pending",
    "report",
    "current",
  ]);
  expect(data?.pages[0]?.resources?.[1]?.directDelivered).toBe(true);
  expect(data?.pages[1]?.resources?.[0]?.message?.status).toBe("completed");
  expect(
    queryClient.getQueryData<ChannelThreadDetail>(detailKey)
      ?.leadActiveExecutionId,
  ).toBeUndefined();
  expect(
    commitTeamTimelineEvents(
      queryClient,
      "channel",
      "thread",
      "lead",
      "lead-waker",
      [{ sequence: 11, message: { id: "gap", role: "assistant" } }],
    ),
  ).toBeUndefined();
  expect(queryClient.getQueryData(key)).toBe(data);
  expect(
    queryClient.getQueryData(["chats", "messages", "lead"]),
  ).toBeUndefined();
  queryClient.clear();
});

it("领域和Chat同时恢复共用一个HTTP头部，保留翻过旧页且迟到响应不回退主Chat", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  const older = {
    resources: [
      {
        chatId: "lead",
        coordinator: true,
        message: { id: "old", role: "assistant" as const },
      },
    ],
  };
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 7,
          hasMore: true,
          nextCursor: "older",
          resources: [],
        },
        older,
      ],
      pageParams: ["", "older"],
    },
  );
  let reads = 0;
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      async () => {
        reads++;
        await gate;
        return HttpResponse.json({
          leadChatLatestSequence: 7,
          hasMore: false,
          resources: [
            { chatId: "member", message: { id: "new-pending", role: "user" } },
          ],
        });
      },
    ),
  );
  try {
    const first = reconcileTeamTimelineQuery(queryClient, "channel", "thread");
    const second = reconcileTeamTimelineQuery(queryClient, "channel", "thread");
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(reads).toBe(1);
    expect(
      commitTeamTimelineEvents(
        queryClient,
        "channel",
        "thread",
        "lead",
        "waker",
        [
          {
            sequence: 8,
            message: {
              id: "live-terminal",
              role: "assistant",
              status: "completed",
            },
          },
        ],
      ),
    ).toBe(8);
    release();
    expect(await Promise.all([first, second])).toEqual([8, 8]);
    expect(reads).toBe(1);
    const data =
      queryClient.getQueryData<
        InfiniteData<ListChannelTimelineResponse, string>
      >(key);
    expect(data?.pages).toHaveLength(2);
    expect(data?.pages[1]).toEqual(older);
    expect(data?.pages[0]?.resources?.map((item) => item.message?.id)).toEqual([
      "new-pending",
      "live-terminal",
    ]);
  } finally {
    release();
    queryClient.clear();
    server.resetHandlers();
  }
});

it("领域对账只重读含临时输入的旧窗口，保留其他已加载历史和仍未投递的输入", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 7,
          hasMore: true,
          nextCursor: "mutable",
          resources: [],
        },
        {
          hasMore: true,
          nextCursor: "immutable",
          resources: [
            {
              chatId: "member",
              message: {
                id: "channel-task-follow-up:delivered",
                role: "user",
                createdAt: 2,
              },
            },
          ],
        },
        {
          hasMore: false,
          resources: [
            {
              chatId: "lead",
              coordinator: true,
              message: { id: "oldest", role: "assistant" },
            },
          ],
        },
      ],
      pageParams: ["", "mutable", "immutable"],
    },
  );
  const reads: string[] = [];
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
        reads.push(cursor);
        return HttpResponse.json(
          cursor
            ? {
                hasMore: true,
                nextCursor: "immutable",
                resources: [
                  {
                    chatId: "member",
                    message: {
                      id: "channel-task-follow-up:still-pending",
                      role: "user",
                      createdAt: 1,
                    },
                  },
                ],
              }
            : {
                hasMore: true,
                nextCursor: "mutable",
                leadChatLatestSequence: 7,
                resources: [],
              },
        );
      },
    ),
  );
  try {
    await reconcileTeamTimelineQuery(queryClient, "channel", "thread");
    expect(reads).toEqual(["", "mutable"]);
    expect(
      queryClient
        .getQueryData<InfiniteData<ListChannelTimelineResponse, string>>(key)
        ?.pages.flatMap((page) => page.resources ?? [])
        .map((item) => item.message?.id),
    ).toEqual(["channel-task-follow-up:still-pending", "oldest"]);
  } finally {
    queryClient.clear();
    server.resetHandlers();
  }
});

it("真实50条头部滚动后保留被挤出正文和旧游标，继续上翻不产生缺口", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  const message = (sequence: number) => ({
    chatId: "lead",
    coordinator: true,
    message: {
      id: `message-${sequence}`,
      role: "assistant" as const,
      createdAt: sequence,
      revision: sequence,
    },
  });
  const range = (from: number, to: number) =>
    Array.from({ length: to - from + 1 }, (_, index) => message(from + index));
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 150,
          resources: range(101, 150),
          hasMore: true,
          nextCursor: "before-101",
        },
        { resources: range(51, 100), hasMore: true, nextCursor: "before-51" },
      ],
      pageParams: ["", "before-101"],
    },
  );
  let latest = 151;
  const reads: string[] = [];
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
        reads.push(cursor);
        if (cursor === "before-51")
          return HttpResponse.json({ resources: range(1, 50), hasMore: false });
        return HttpResponse.json({
          leadChatLatestSequence: latest,
          resources: range(latest - 49, latest),
          hasMore: true,
          nextCursor: `before-${latest - 49}`,
        });
      },
    ),
  );
  try {
    for (; latest <= 155; latest++) {
      expect(
        commitTeamTimelineEvents(
          queryClient,
          "channel",
          "thread",
          "lead",
          "waker",
          [{ sequence: latest, message: message(latest).message }],
        ),
      ).toBe(latest);
      await reconcileTeamTimelineQuery(queryClient, "channel", "thread");
      const data =
        queryClient.getQueryData<
          InfiniteData<ListChannelTimelineResponse, string>
        >(key);
      expect(
        new Set(
          data?.pages
            .flatMap((page) => page.resources ?? [])
            .map((item) => item.message?.id),
        ),
      ).toEqual(new Set(range(51, latest).map((item) => item.message.id)));
      expect(data?.pages[0]?.nextCursor).toBe("before-101");
    }
    const observer = new InfiniteQueryObserver(
      queryClient,
      teamTimelineInfiniteOptions("channel", "thread"),
    );
    await observer.fetchNextPage();
    const data =
      queryClient.getQueryData<
        InfiniteData<ListChannelTimelineResponse, string>
      >(key);
    expect(
      new Set(
        data?.pages
          .flatMap((page) => page.resources ?? [])
          .map((item) => item.message?.id),
      ),
    ).toEqual(new Set(range(1, 155).map((item) => item.message.id)));
    expect(reads).toEqual(["", "", "", "", "", "before-51"]);
  } finally {
    queryClient.clear();
    server.resetHandlers();
  }
});

it("旧GET读取后才发生的成员补充变化由同一Query owner补读，命令完成时已覆盖新事实", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [{ leadChatLatestSequence: 7, resources: [], hasMore: false }],
      pageParams: [""],
    },
  );
  let started = () => {},
    release = () => {};
  const oldStarted = new Promise<void>((resolve) => {
    started = resolve;
  });
  const oldGate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let reads = 0;
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      async () => {
        const current = ++reads;
        if (current === 1) {
          started();
          await oldGate;
        }
        return HttpResponse.json({
          leadChatLatestSequence: 7,
          resources: [
            {
              chatId: "member",
              message: {
                id:
                  current === 1
                    ? "channel-task-follow-up:already-delivered"
                    : "channel-task-follow-up:new-input",
                role: "user",
                createdAt: current,
              },
            },
          ],
          hasMore: false,
        });
      },
    ),
  );
  try {
    const beforeChange = reconcileTeamTimelineQuery(
      queryClient,
      "channel",
      "thread",
    );
    await oldStarted;
    const afterChange = reconcileTeamTimelineQuery(
      queryClient,
      "channel",
      "thread",
    );
    release();
    expect(await Promise.all([beforeChange, afterChange])).toEqual([7, 7]);
    expect(reads).toBe(2);
    expect(
      queryClient
        .getQueryData<InfiniteData<ListChannelTimelineResponse, string>>(key)
        ?.pages[0]?.resources?.map((item) => item.message?.id),
    ).toEqual(["channel-task-follow-up:new-input"]);
  } finally {
    release();
    queryClient.clear();
    server.resetHandlers();
  }
});

it("恢复命令登记后真实上翻旧页不能吞掉头部pending对账", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  const head = {
    chatId: "lead",
    coordinator: true,
    message: { id: "head", role: "assistant" as const, createdAt: 18 },
  };
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 7,
          resources: [
            head,
            {
              chatId: "member",
              message: {
                id: "channel-task-follow-up:delivered",
                role: "user",
                createdAt: 20,
              },
            },
          ],
          hasMore: true,
          nextCursor: "older",
        },
      ],
      pageParams: [""],
    },
  );
  let releasePage = () => {},
    releaseHead = () => {};
  const pageGate = new Promise<void>((resolve) => {
    releasePage = resolve;
  });
  const headGate = new Promise<void>((resolve) => {
    releaseHead = resolve;
  });
  const reads: string[] = [];
  let pageSignal: AbortSignal | undefined;
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      async ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
        reads.push(cursor);
        if (cursor) {
          pageSignal = request.signal;
          await pageGate;
          return HttpResponse.json({
            resources: [
              {
                chatId: "lead",
                coordinator: true,
                message: { id: "old", role: "assistant", createdAt: 1 },
              },
            ],
            hasMore: false,
          });
        }
        await headGate;
        return HttpResponse.json({
          leadChatLatestSequence: 7,
          resources: [
            head,
            {
              chatId: "member",
              message: {
                id: "channel-task-follow-up:new-input",
                role: "user",
                createdAt: 21,
              },
            },
          ],
          hasMore: true,
          nextCursor: "older",
        });
      },
    ),
  );
  const observer = new InfiniteQueryObserver(
    queryClient,
    teamTimelineInfiniteOptions("channel", "thread"),
  );
  let completed = false;
  const command = reconcileTeamTimelineQuery(
    queryClient,
    "channel",
    "thread",
    "pending",
  ).then((sequence) => {
    completed = true;
    return sequence;
  });
  const pagination = observer.fetchNextPage();
  try {
    await waitFor(() => expect(reads).toEqual(["older"]));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(pageSignal?.aborted).toBe(false);
    expect(completed).toBe(false);
    releasePage();
    expect((await pagination).data?.pages[1]?.resources?.[0]?.message?.id).toBe(
      "old",
    );
    await waitFor(() => expect(reads).toEqual(["older", ""]));
    expect(completed).toBe(false);
    releaseHead();
    expect(await command).toBe(7);
    expect(
      queryClient
        .getQueryData<InfiniteData<ListChannelTimelineResponse, string>>(key)
        ?.pages.flatMap((page) => page.resources ?? [])
        .map((item) => item.message?.id),
    ).toEqual(["head", "channel-task-follow-up:new-input", "old"]);
    expect(reads).toEqual(["older", ""]);
  } finally {
    releasePage();
    releaseHead();
    await Promise.allSettled([pagination, command]);
    queryClient.clear();
    server.resetHandlers();
  }
});

it.each(["recovery", "pending"] as const)(
  "同批先%s不会丢恢复范围，旧report自然变化精准更新且task不重读报告页",
  async (firstScope) => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const key = teamQueryKeys.timelinePages("channel", "thread");
    queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
      key,
      {
        pages: [
          {
            leadChatLatestSequence: 7,
            resources: [],
            hasMore: true,
            nextCursor: "report-window",
          },
          {
            resources: [
              {
                chatId: "lead",
                coordinator: true,
                directDelivered: false,
                message: {
                  id: "report",
                  role: "user",
                  actor: "team_member",
                  createdAt: 10,
                },
              },
            ],
            hasMore: true,
            nextCursor: "immutable",
          },
          {
            resources: [
              {
                chatId: "lead",
                coordinator: true,
                message: { id: "old", role: "assistant", createdAt: 1 },
              },
            ],
            hasMore: false,
          },
        ],
        pageParams: ["", "report-window", "immutable"],
      },
    );
    const reads: string[] = [];
    server.use(
      http.get(
        "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
        ({ request }) => {
          const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
          reads.push(cursor);
          return HttpResponse.json(
            cursor
              ? {
                  resources: [
                    {
                      chatId: "lead",
                      coordinator: true,
                      directDelivered: true,
                      message: {
                        id: "report",
                        role: "user",
                        actor: "team_member",
                        createdAt: 10,
                      },
                    },
                  ],
                  hasMore: true,
                  nextCursor: "immutable",
                }
              : {
                  leadChatLatestSequence: 7,
                  resources: [],
                  hasMore: true,
                  nextCursor: "report-window",
                },
          );
        },
      ),
    );
    try {
      await Promise.all([
        reconcileTeamTimelineQuery(
          queryClient,
          "channel",
          "thread",
          firstScope,
        ),
        reconcileTeamTimelineQuery(
          queryClient,
          "channel",
          "thread",
          firstScope === "recovery" ? "pending" : "recovery",
        ),
      ]);
      expect(reads).toEqual(["", "report-window"]);
      expect(
        queryClient.getQueryData<
          InfiniteData<ListChannelTimelineResponse, string>
        >(key)?.pages[1]?.resources?.[0]?.directDelivered,
      ).toBe(true);
      const beforeDelivery =
        queryClient.getQueryData<
          InfiniteData<ListChannelTimelineResponse, string>
        >(key);
      queryClient.removeQueries({ queryKey: key, exact: true });
      queryClient.setQueryData<
        InfiniteData<ListChannelTimelineResponse, string>
      >(
        key,
        beforeDelivery
          ? {
              ...beforeDelivery,
              pages: beforeDelivery.pages.map((page, index) =>
                index === 1
                  ? {
                      ...page,
                      resources: page.resources?.map((item) => ({
                        ...item,
                        directDelivered: false,
                      })),
                    }
                  : page,
              ),
            }
          : beforeDelivery,
      );
      reads.length = 0;
      await reconcileTeamTimelineQuery(
        queryClient,
        "channel",
        "thread",
        "pending",
      );
      expect(reads).toEqual([""]);
      expect(
        queryClient.getQueryData<
          InfiniteData<ListChannelTimelineResponse, string>
        >(key)?.pages[1]?.resources?.[0]?.directDelivered,
      ).toBe(false);
      commitTeamTimelineProjectionEvent(queryClient, "channel", "thread", {
        type: "team.member.direct_delivered",
        payload: { notificationId: "report" },
      });
      expect(
        queryClient.getQueryData<
          InfiniteData<ListChannelTimelineResponse, string>
        >(key)?.pages[1]?.resources?.[0]?.directDelivered,
      ).toBe(true);
    } finally {
      queryClient.clear();
      server.resetHandlers();
    }
  },
);

it("断线超过50条导致新头部完全越过旧窗口时，只补缺口到原已加载区间", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const key = teamQueryKeys.timelinePages("channel", "thread");
  const range = (from: number, to: number) =>
    Array.from({ length: to - from + 1 }, (_, index) => ({
      chatId: "lead",
      coordinator: true,
      message: {
        id: `message-${from + index}`,
        role: "assistant" as const,
        createdAt: from + index,
        revision: from + index,
      },
    }));
  queryClient.setQueryData<InfiniteData<ListChannelTimelineResponse, string>>(
    key,
    {
      pages: [
        {
          leadChatLatestSequence: 150,
          resources: range(101, 150),
          hasMore: true,
          nextCursor: "before-101",
        },
        { resources: range(51, 100), hasMore: true, nextCursor: "before-51" },
      ],
      pageParams: ["", "before-101"],
    },
  );
  const reads: string[] = [];
  server.use(
    http.get(
      "http://127.0.0.1:19877/api/v1/channels/channel/threads/thread/timeline",
      ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor") ?? "";
        reads.push(cursor);
        const newest = cursor ? Number(cursor.replace("before-", "")) - 1 : 225;
        return HttpResponse.json({
          leadChatLatestSequence: cursor ? undefined : 225,
          resources: range(Math.max(1, newest - 49), newest),
          hasMore: newest > 50,
          nextCursor: newest > 50 ? `before-${newest - 49}` : undefined,
        });
      },
    ),
  );
  try {
    await reconcileTeamTimelineQuery(queryClient, "channel", "thread");
    const data =
      queryClient.getQueryData<
        InfiniteData<ListChannelTimelineResponse, string>
      >(key);
    expect(
      new Set(
        data?.pages
          .flatMap((page) => page.resources ?? [])
          .map((item) => item.message?.id),
      ),
    ).toEqual(new Set(range(51, 225).map((item) => item.message.id)));
    expect(data?.pages[0]?.nextCursor).toBe("before-101");
    expect(reads).toEqual(["", "before-176"]);
    const observer = new InfiniteQueryObserver(
      queryClient,
      teamTimelineInfiniteOptions("channel", "thread"),
    );
    await observer.fetchNextPage();
    expect(
      new Set(
        queryClient
          .getQueryData<InfiniteData<ListChannelTimelineResponse, string>>(key)
          ?.pages.flatMap((page) => page.resources ?? [])
          .map((item) => item.message?.id),
      ),
    ).toEqual(new Set(range(1, 225).map((item) => item.message.id)));
    expect(reads).toEqual(["", "before-176", "before-51"]);
  } finally {
    queryClient.clear();
    server.resetHandlers();
  }
});
