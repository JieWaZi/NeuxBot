import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { delay, http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { WorkBoardPage } from "./work-board-page";
import { NotificationNavigationProvider } from "@/modules/notification";
import { notificationHref } from "@/app/notifications/notification-navigation";
import type { NotificationItem } from "@/modules/notification";
import type { GlobalEventObserver } from "@/shared/api/global-events";
// 业务集成测试保留真实头像外壳与名称；形象绘制及异步资源生命周期由 companion 专属测试验证。
vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: () => null,
}));
const eventObservers = vi.hoisted(() => new Set<GlobalEventObserver>());
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    eventObservers.add(observer);
    return () => eventObservers.delete(observer);
  },
}));
function publishWorkBoardChange() {
  act(() => {
    for (const observer of eventObservers)
      observer.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "work-board",
        type: "work-board.changed",
        resource: { kind: "global" },
        eventId: "event",
        payload: {},
      });
  });
}

const origin = "http://work-board.test";
let summaryFixture: Record<string, unknown> = {};
let revision = 0;
function fixtureNotice(
  value: Record<string, unknown>,
  bucket: "action" | "unread",
): NotificationItem {
  const string = (key: string) =>
    typeof value[key] === "string" ? value[key] : "";
  const result =
    value.result && typeof value.result === "object" ? value.result : {};
  const version =
    "latestVersion" in result && typeof result.latestVersion === "number"
      ? result.latestVersion
      : 1;
  const reference =
    "reference" in result &&
    result.reference &&
    typeof result.reference === "object"
      ? result.reference
      : {};
  const source = string("source");
  const kind = string("kind");
  const owner = string("ownerKind");
  const noticeKind =
    bucket === "unread"
      ? "result"
      : kind === "review"
        ? "review"
        : kind === "approval"
          ? "approval"
          : "input";
  return {
    id: string("id") || `result:${owner}:${string("ownerId")}:${version}`,
    kind: noticeKind,
    bucket,
    source:
      source === "team" ||
      source === "workflow" ||
      source === "automatic" ||
      source === "im"
        ? source
        : "chat",
    title: string("title"),
    description: string("workTitle"),
    createdAt: typeof value.createdAt === "number" ? value.createdAt : 1000,
    version,
    executor: {
      id: string("executorId") || string("wakerId"),
      kind: "waker",
      name: string("actorName") || string("executorName"),
    },
    target: {
      kind: noticeKind,
      chatId: string("chatId"),
      wakerId: string("wakerId"),
      channelId: string("channelId"),
      threadId: owner === "team_thread" ? string("ownerId") : undefined,
      ownerKind:
        owner === "team_thread" || owner === "workflow_run" ? owner : "chat",
      ownerId: string("ownerId"),
      requestId: string("id"),
      taskId: string("taskId"),
      definitionId: string("workflowId"),
      runId: string("runId"),
      messageId:
        "messageId" in reference && typeof reference.messageId === "string"
          ? reference.messageId
          : undefined,
    },
    dismissed: false,
    failed: value.lastStatus === "failed",
  };
}
function fixtureNotices(value: unknown, bucket: "action" | "unread") {
  return Array.isArray(value)
    ? value.flatMap((item: unknown) =>
        item && typeof item === "object"
          ? [fixtureNotice(Object.fromEntries(Object.entries(item)), bucket)]
          : [],
      )
    : [];
}

function boardResponse(value: Record<string, unknown>) {
  if ("attentionCount" in value) {
    summaryFixture = value;
    return HttpResponse.json({
      workCount: 1,
      runningCount: 0,
      endedCount: 1,
      actionWorkCount: 0,
      ...value,
    });
  }
  if ("resources" in value && !("totalSize" in value))
    return HttpResponse.json({
      totalSize: value.hasMore
        ? 21
        : Array.isArray(value.resources)
          ? value.resources.length
          : 0,
      ...value,
    });
  return HttpResponse.json(value);
}
beforeEach(() => {
  summaryFixture = {};
  revision = 0;
  client.setConfig({ baseUrl: origin });
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
    })),
  );
});
const server = setupServer(
  http.get(`${origin}/api/v1/notifications/snapshot`, async () => {
    await delay(20);
    const actionCount = Number(summaryFixture.attentionCount ?? 0),
      unreadResultCount = Number(summaryFixture.resultCount ?? 0);
    return HttpResponse.json({
      epoch: "a".repeat(32),
      revision: ++revision,
      synced: true,
      actionCount,
      unreadResultCount,
      unreadFailureCount: 0,
      totalCount: actionCount + unreadResultCount,
      actionPreview: fixtureNotices(summaryFixture.attention, "action").slice(
        0,
        5,
      ),
      unreadPreview: fixtureNotices(summaryFixture.results, "unread").slice(
        0,
        5,
      ),
      generatedAt: 1000,
    });
  }),
  http.get(`${origin}/api/v1/notifications`, async ({ request }) => {
    const incoming = new URL(request.url).searchParams;
    const bucket = incoming.get("bucket") === "action" ? "action" : "unread";
    const params = new URLSearchParams({
      mode: bucket === "action" ? "attention" : "results",
      limit: "50",
    });
    const cursor = incoming.get("cursor");
    if (cursor) params.set("cursor", cursor);
    const data: Record<string, unknown> = await (
      await fetch(`${origin}/api/v1/work-board?${params}`)
    ).json();
    const resources = fixtureNotices(
      data[bucket === "action" ? "attention" : "resources"],
      bucket,
    );
    return HttpResponse.json({
      resources,
      totalSize: data.totalSize ?? resources.length,
      pageNum: 1,
      pageSize: 50,
      hasMore: data.hasMore ?? false,
      nextCursor: data.nextCursor ?? "",
      epoch: "a".repeat(32),
      revision,
    });
  }),
  http.get(`${origin}/api/v1/wakers`, () =>
    boardResponse({ resources: [], totalSize: 0 }),
  ),
  http.get(`${origin}/api/v1/channels`, () =>
    boardResponse({ resources: [], totalSize: 0 }),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});
const entry = {
  ownerKind: "chat",
  ownerId: "a".repeat(32),
  chatId: "a".repeat(32),
  wakerId: "b".repeat(32),
  title: "整理资料",
  executorName: "资料伙伴",
  source: "automatic",
  activity: "idle",
  lastStatus: "completed",
  updatedAt: 1000,
  approvalCount: 0,
  inputCount: 0,
  reviewCount: 0,
  result: {
    ownerKind: "chat",
    ownerId: "a".repeat(32),
    latestVersion: 12,
    viewedVersion: 0,
    availableAt: 900,
    reference: {
      chatId: "a".repeat(32),
      messageId: "c".repeat(32),
      executionId: "c".repeat(32),
    },
  },
};

function mount(initial = "/work-board") {
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <MemoryRouter initialEntries={[initial]}>
        <NotificationNavigationProvider resolveHref={notificationHref}>
          <WorkBoardPage />
        </NotificationNavigationProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

it("关注区保留准确统计，列表标题过滤不改变关注区；查看结果链接不提前提交已读", async () => {
  const summaries = vi.fn();
  const lists = vi.fn();
  const viewed = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () => {
      summaries();
      return boardResponse({
        attentionCount: 7,
        resultCount: 9,
        attention: [],
        results: [entry],
      });
    }),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      lists(new URL(request.url).searchParams.get("query"));
      return boardResponse({
        resources: [entry],
        attention: [],
        hasMore: false,
      });
    }),
    http.post(`${origin}/api/v1/work-board/results/:kind/:id/view`, () => {
      viewed();
      return boardResponse({ success: true });
    }),
  );
  mount();
  const user = userEvent.setup();
  expect(await screen.findByText("最近已完成")).toBeInTheDocument();
  expect(
    within(screen.getByRole("banner")).getByText("最近 30 天"),
  ).toBeInTheDocument();
  await user.click(await screen.findByRole("radio", { name: "待我查看 9" }));
  const results = screen.getByRole("region", { name: "待我查看" });
  expect(screen.getByRole("radio", { name: "待我查看 9" })).toBeChecked();
  const result = within(results).getByRole("link", { name: "查看结果" });
  expect(result).toHaveAttribute(
    "href",
    `/chats/${entry.chatId}?wakerId=${entry.wakerId}&resultMessage=${entry.result.reference.messageId}`,
  );
  await user.type(
    screen.getByRole("searchbox", { name: "搜索工作标题" }),
    "归档",
  );
  await waitFor(() => expect(lists).toHaveBeenLastCalledWith("归档"));
  expect(summaries).toHaveBeenCalledTimes(1);
  expect(viewed).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
});

it("超过五条时在关注区滚动展示全部事项，并定位团队实际成员", async () => {
  const request = {
    id: "input:chat:request",
    kind: "input",
    ownerKind: "team_thread",
    ownerId: "thread",
    title: "请补充信息",
    workTitle: "团队资料整理",
    actorName: "检索成员",
    wakerId: "member",
    channelId: "team",
    chatId: "member-chat",
    source: "team",
    createdAt: 100,
  };
  const modes = vi.fn();
  const requests = Array.from({ length: 6 }, (_, index) => ({
    ...request,
    id: index === 0 ? request.id : request.id + index,
    title: index === 0 ? request.title : "请补充信息 " + (index + 1),
  }));
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 6,
        resultCount: 0,
        attention: requests.slice(0, 5),
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request: incoming }) => {
      const mode = new URL(incoming.url).searchParams.get("mode");
      modes(mode);
      return boardResponse({
        resources: [],
        attention: mode === "attention" ? requests : [],
        totalSize: mode === "attention" ? requests.length : 0,
        hasMore: false,
      });
    }),
  );
  mount();
  const region = await screen.findByRole("region", { name: "需要操作" });
  await within(region).findByText("请补充信息 6");
  expect(within(region).getAllByRole("listitem")).toHaveLength(6);
  expect(region).toHaveClass("overflow-y-auto", "scrollbar-hidden");
  expect(
    within(region).queryByRole("button", { name: /^查看其余/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "清空" }),
  ).not.toBeInTheDocument();
  await waitFor(() => expect(modes).toHaveBeenLastCalledWith("attention"));
  expect(
    screen.getAllByRole("link", { name: "去处理" }).at(-1),
  ).toHaveAttribute(
    "href",
    "/teams/team/threads/thread?wakerId=member&request=input%3Achat%3Arequest5",
  );
  expect(
    screen.getByRole("button", { name: "选择日期范围" }),
  ).toBeInTheDocument();
});

it.each([5, 6])(
  "关注区两个 Tab 在总数为 %i 时展示全部引用，并保持工作记录过滤不变",
  async (count) => {
    const requests = Array.from({ length: count }, (_, index) => ({
      id: "request-" + index,
      kind: "input",
      ownerKind: "chat",
      ownerId: "chat-" + index,
      chatId: "chat-" + index,
      title: "待办 " + (index + 1),
      workTitle: "资料整理 " + (index + 1),
      actorName: "资料伙伴",
      source: "chat",
      createdAt: 100 + index,
    }));
    const results = Array.from({ length: count }, (_, index) => ({
      ...entry,
      ownerId: "result-" + index,
      title: "成果 " + (index + 1),
    }));
    const modes = vi.fn();
    server.use(
      http.get(`${origin}/api/v1/work-board/summary`, () =>
        boardResponse({
          attentionCount: count,
          resultCount: count,
          attention: requests.slice(0, 5),
          results: results.slice(0, 5),
        }),
      ),
      http.get(`${origin}/api/v1/work-board`, ({ request }) => {
        const mode = new URL(request.url).searchParams.get("mode");
        modes(mode);
        return boardResponse({
          resources: mode === "results" ? results : [],
          attention: mode === "attention" ? requests : [],
          totalSize: mode === "all" ? 0 : count,
          hasMore: false,
        });
      }),
    );
    mount();
    const user = userEvent.setup();
    await screen.findByRole("radio", { name: "需要操作 " + count });
    const attention = screen.getByRole("region", { name: "需要操作" });
    await waitFor(() =>
      expect(within(attention).getAllByRole("listitem")).toHaveLength(count),
    );
    const attentionMore = within(attention).queryByRole("button", {
      name: /^查看其余 /,
    });
    expect(attentionMore).not.toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: "待我查看 " + count }));
    const resultRegion = screen.getByRole("region", { name: "待我查看" });
    await waitFor(() =>
      expect(within(resultRegion).getAllByRole("listitem")).toHaveLength(count),
    );
    const resultMore = within(resultRegion).queryByRole("button", {
      name: /^查看其余 /,
    });
    expect(resultMore).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "选择日期范围" }),
    ).toBeInTheDocument();
    expect(modes).toHaveBeenCalledTimes(count === 5 ? 1 : 3);
  },
);

it("关注列表与空状态保持同一内容区域，标题与 Tab 位于区域外", async () => {
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 1,
        resultCount: 0,
        attention: [
          {
            id: "input:chat:request",
            kind: "input",
            ownerKind: "chat",
            ownerId: "chat",
            chatId: "chat",
            title: "你喜欢哪种水果？",
            actorName: "私人助理",
            source: "chat",
            createdAt: 100,
          },
        ],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ resources: [], attention: [], hasMore: false }),
    ),
  );
  mount();
  const user = userEvent.setup();
  await screen.findByText("你喜欢哪种水果？");
  const surface = screen.getByRole("region", { name: "需要操作" });
  expect(within(surface).getAllByRole("listitem")).toHaveLength(1);
  expect(surface).not.toContainElement(
    screen.getByRole("heading", { name: "需要关注" }),
  );
  expect(surface).not.toContainElement(
    screen.getByRole("radio", { name: "需要操作 1" }),
  );
  await user.click(screen.getByRole("radio", { name: "待我查看 0" }));
  expect(screen.getByRole("region", { name: "待我查看" })).toBe(surface);
  expect(within(surface).getByText("没有待查看结果")).toBeInTheDocument();
});

it("关注区滚动到底后继续读取下一页，不显示更多入口", async () => {
  const requests = Array.from({ length: 51 }, (_, index) => ({
    id: `request-${index}`,
    kind: "input",
    ownerKind: "chat",
    ownerId: `chat-${index}`,
    chatId: `chat-${index}`,
    title: `待办 ${index + 1}`,
    actorName: "资料伙伴",
  }));
  const cursors = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 51,
        resultCount: 0,
        attention: requests.slice(0, 5),
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      const query = new URL(request.url).searchParams;
      if (query.get("mode") !== "attention")
        return boardResponse({
          resources: [],
          attention: [],
          totalSize: 0,
          hasMore: false,
        });
      cursors(query.get("cursor"));
      return boardResponse({
        resources: [],
        attention: query.get("cursor")
          ? requests.slice(50)
          : requests.slice(0, 50),
        totalSize: 51,
        hasMore: !query.get("cursor"),
        nextCursor: query.get("cursor") ? undefined : "tail",
      });
    }),
  );
  mount();
  const region = await screen.findByRole("region", { name: "需要操作" });
  await within(region).findByText("待办 50");
  Object.defineProperties(region, {
    clientHeight: { configurable: true, value: 336 },
    scrollHeight: { configurable: true, value: 3000 },
    scrollTop: { configurable: true, writable: true, value: 2700 },
  });
  fireEvent.scroll(region);
  expect(await within(region).findByText("待办 51")).toBeInTheDocument();
  expect(cursors).toHaveBeenLastCalledWith("tail");
  expect(
    within(region).queryByRole("button", { name: /^查看其余/ }),
  ).not.toBeInTheDocument();
});

it("清空覆盖全部待查看结果，保留需要操作与工作记录筛选", async () => {
  const user = userEvent.setup();
  const clear = vi.fn();
  const viewed = vi.fn();
  let cleared = false;
  const attention = [
    {
      id: "approval-one",
      kind: "approval",
      ownerKind: "chat",
      ownerId: "chat-one",
      title: "需要许可",
      actorName: "资料伙伴",
    },
  ];
  const results = Array.from({ length: 7 }, (_, index) => ({
    ...entry,
    ownerId: `result-${index}`,
  }));
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 1,
        resultCount: cleared ? 0 : 7,
        attention,
        results: cleared ? [] : results.slice(0, 5),
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({
        resources: [entry],
        attention: [],
        totalSize: 1,
        hasMore: false,
      }),
    ),
    http.post(`${origin}/api/v1/notifications/unread/clear`, async () => {
      clear();
      await delay(50);
      cleared = true;
      return HttpResponse.json({ message: "" });
    }),
    http.post(
      `${origin}/api/v1/work-board/results/:ownerKind/:ownerId/view`,
      () => {
        viewed();
        return HttpResponse.json({ message: "" });
      },
    ),
  );
  mount("/work-board?source=automatic");
  const clearButton = await screen.findByRole("button", { name: "清空" });
  const actionTab = screen.getByRole("radio", { name: "需要操作 1" });
  expect(clearButton.compareDocumentPosition(actionTab)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  await user.click(clearButton);
  expect(await screen.findByRole("button", { name: "清空中…" })).toBeDisabled();
  await screen.findByRole("radio", { name: "待我查看 0" });
  expect(clear).toHaveBeenCalledOnce();
  expect(viewed).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "清空" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("需要许可")).toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "来源" })).toHaveTextContent(
    "自动任务",
  );
});

it("清空失败保留待查看数据和操作入口", async () => {
  const user = userEvent.setup();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 1,
        attention: [],
        results: [entry],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({
        resources: [],
        attention: [],
        totalSize: 0,
        hasMore: false,
      }),
    ),
    http.post(
      `${origin}/api/v1/notifications/unread/clear`,
      () => new HttpResponse(null, { status: 500 }),
    ),
  );
  mount();
  await user.click(await screen.findByRole("button", { name: "清空" }));
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "清空" })).toBeEnabled();
  await user.click(screen.getByRole("radio", { name: "待我查看 1" }));
  expect(
    within(screen.getByRole("region", { name: "待我查看" })).getByRole("link", {
      name: "整理资料",
    }),
  ).toBeInTheDocument();
});

it("来源筛选只约束工作记录，上方全局待办与统计保持不变", async () => {
  const scopes = vi.fn();
  const lists = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, ({ request }) => {
      scopes(new URL(request.url).searchParams.get("source"));
      return boardResponse({
        workCount: 12,
        actionWorkCount: 1,
        attentionCount: 1,
        resultCount: 0,
        attention: [
          {
            id: "global-input",
            kind: "input",
            ownerKind: "chat",
            ownerId: "global-chat",
            title: "全局待补充信息",
            actorName: "私人助理",
          },
        ],
        results: [],
      });
    }),
    http.get(`${origin}/api/v1/work-board`, async ({ request }) => {
      const query = new URL(request.url).searchParams;
      lists(query.get("source"), query.get("cursor"));
      // 请求开始不代表响应已渲染，保留真实的异步间隔验证按钮就绪状态。
      await delay(50);
      return boardResponse({
        totalSize: 21,
        resources: [entry],
        attention: [],
        hasMore: !query.has("cursor"),
        nextCursor: "next",
      });
    }),
  );
  mount();
  const user = userEvent.setup();
  await screen.findByText("最近已完成");
  fireEvent.keyDown(screen.getByRole("combobox", { name: "来源" }), {
    key: "ArrowDown",
  });
  await user.click(await screen.findByRole("option", { name: "自动任务" }));
  await waitFor(() =>
    expect(lists).toHaveBeenLastCalledWith("automatic", null),
  );
  expect(scopes).toHaveBeenCalledTimes(1);
  expect(scopes).toHaveBeenLastCalledWith(null);
  expect(screen.getByText("全局待补充信息")).toBeInTheDocument();
  expect(
    within(screen.getByRole("region", { name: "工作概览" })).getByRole(
      "button",
      { name: "任务总数" },
    ),
  ).toHaveTextContent("12");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() =>
    expect(lists).toHaveBeenLastCalledWith("automatic", "next"),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "上一页" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "上一页" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "第 1 页" })).toHaveAttribute(
      "aria-current",
      "page",
    ),
  );
  expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "来源" })).toHaveTextContent(
    "自动任务",
  );
});

it("需要操作使用统一任务状态下拉框，按待审核类型筛选并可切回普通状态", async () => {
  const queries = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        workCount: 3,
        attentionCount: 3,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      queries(Object.fromEntries(params));
      const kinds = (params.get("states") || "approval,input,review").split(
        ",",
      );
      const attention =
        params.get("mode") === "attention"
          ? ["approval", "input", "review"]
              .filter((kind) => kinds.includes(kind))
              .map((kind) => ({
                id: kind,
                kind,
                title: `待办-${kind}`,
                workTitle: "审批工作",
                ownerKind: "chat",
                ownerId: "chat",
                createdAt: 1,
              }))
          : [];
      return boardResponse({
        resources: [],
        attention,
        totalSize: attention.length,
        hasMore: false,
      });
    }),
  );
  mount("/work-board?mode=attention&source=automatic");
  const user = userEvent.setup();
  const records = screen.getByRole("region", { name: "工作记录" });
  await within(records).findByText("待办-approval");
  expect(
    screen.queryByRole("button", { name: "需要操作 · 恢复默认" }),
  ).not.toBeInTheDocument();
  await user.click(
    within(records).getByRole("button", { name: "任务状态 (3)" }),
  );
  expect(screen.getByRole("checkbox", { name: "待审核" })).toBeChecked();
  await user.click(screen.getByRole("checkbox", { name: "待补充" }));
  await user.click(screen.getByRole("checkbox", { name: "待验收" }));
  await waitFor(() =>
    expect(queries).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: "attention",
        states: "approval",
        source: "automatic",
      }),
    ),
  );
  await waitFor(() =>
    expect(within(records).queryByText("待办-input")).not.toBeInTheDocument(),
  );
  expect(within(records).getByText("待办-approval")).toBeVisible();
  await user.click(screen.getByRole("checkbox", { name: "最近已完成" }));
  await waitFor(() =>
    expect(queries).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: "all",
        states: "completed",
        source: "automatic",
      }),
    ),
  );
  expect(screen.getByRole("checkbox", { name: "待审核" })).not.toBeChecked();
  expect(screen.getByRole("checkbox", { name: "最近已完成" })).toBeChecked();
});

it("概览卡片切换筛选，再次点击保持选中，通过总数清除状态并保留来源和日期", async () => {
  const queries = vi.fn();
  const summaryQueries = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, ({ request }) => {
      summaryQueries(Object.fromEntries(new URL(request.url).searchParams));
      return boardResponse({
        workCount: 12,
        runningCount: 3,
        endedCount: 5,
        actionWorkCount: 2,
        attentionCount: 4,
        resultCount: 1,
        attention: [],
        results: [],
      });
    }),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      queries(Object.fromEntries(new URL(request.url).searchParams));
      return boardResponse({
        resources: [entry],
        attention: [],
        hasMore: true,
        nextCursor: "anchor",
      });
    }),
  );
  mount(
    "/work-board?source=automatic&query=归档&from=2026-10-01&to=2026-10-12",
  );
  const user = userEvent.setup();
  await screen.findByText("最近已完成");
  const overview = screen.getByRole("region", { name: "工作概览" });
  expect(summaryQueries).toHaveBeenLastCalledWith({});
  const header = screen.getByRole("banner");
  expect(
    within(header).getByText("汇总已有工作，处理待办，查看结果。"),
  ).toBeInTheDocument();
  expect(within(header).getByText("最近 30 天")).toBeInTheDocument();
  expect(
    within(overview).queryByText("2026-10-01 – 2026-10-12"),
  ).not.toBeInTheDocument();
  expect(
    within(overview).getByRole("button", { name: "任务总数" }),
  ).toHaveTextContent("12");
  expect(
    within(overview).getByRole("button", { name: "需要操作" }),
  ).toHaveTextContent("2");
  expect(screen.getByRole("heading", { name: "工作记录" })).toBeInTheDocument();
  expect(screen.getByRole("region", { name: "工作记录" })).toContainElement(
    screen.getByRole("combobox", { name: "来源" }),
  );
  await user.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(queries.mock.lastCall?.[0].cursor).toBe("anchor"));
  await user.click(
    within(overview).getByRole("button", { name: "已结束任务" }),
  );
  await waitFor(() =>
    expect(queries.mock.lastCall?.[0]).toMatchObject({
      states: "completed,failed,canceled",
      source: "automatic",
      query: "",
    }),
  );
  expect(queries.mock.lastCall?.[0].cursor).toBeUndefined();
  expect(queries.mock.lastCall?.[0].from).toBeDefined();
  expect(screen.getByRole("table", { name: "工作记录" })).toBeInTheDocument();
  await user.click(
    within(overview).getByRole("button", { name: "已结束任务" }),
  );
  expect(queries.mock.lastCall?.[0].states).toBe("completed,failed,canceled");
  expect(
    within(overview).getByRole("button", { name: "已结束任务" }),
  ).toHaveAttribute("aria-pressed", "true");
  await user.click(within(overview).getByRole("button", { name: "任务总数" }));
  await waitFor(() =>
    expect(queries.mock.lastCall?.[0].states).toBeUndefined(),
  );
  expect(
    within(overview).getByRole("button", { name: "任务总数" }),
  ).toHaveAttribute("aria-pressed", "true");
  await user.click(screen.getByRole("button", { name: "任务状态" }));
  await user.click(screen.getByRole("checkbox", { name: "执行中" }));
  await user.click(screen.getByRole("checkbox", { name: "最近失败" }));
  await waitFor(() =>
    expect(queries.mock.lastCall?.[0].states).toBe("running,failed"),
  );
  expect(screen.getByRole("checkbox", { name: "执行中" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "最近失败" })).toBeChecked();
});

it("零数据概览展示0，关注区和筛选无匹配使用不同空状态，清除筛选可恢复", async () => {
  const lists = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        workCount: 0,
        runningCount: 0,
        endedCount: 0,
        actionWorkCount: 0,
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      lists(new URL(request.url).searchParams.get("query"));
      return boardResponse({
        resources: [],
        attention: [],
        hasMore: false,
      });
    }),
  );
  mount("/work-board?query=missing");
  const user = userEvent.setup();
  await screen.findByText("没有匹配的工作");
  expect(await screen.findByText("没有待处理事项")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "任务总数" })).toHaveTextContent(
    "0",
  );
  expect(
    screen.getByRole("button", { name: "任务总数" }),
  ).not.toHaveTextContent("—");
  await user.click(screen.getByRole("radio", { name: "待我查看 0" }));
  expect(screen.getByText("没有待查看结果")).toBeInTheDocument();
  expect(
    screen.getByText("新成果和失败结果会显示在这里，查看后自动移出。"),
  ).toBeInTheDocument();
  await user.click(
    within(screen.getByRole("region", { name: "工作记录" })).getByRole(
      "button",
      { name: "清除筛选" },
    ),
  );
  await screen.findByText("还没有近期工作记录");
  expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
  expect(
    screen.getByRole("columnheader", { name: "工作" }),
  ).toBeInTheDocument();
  await waitFor(() => expect(lists).toHaveBeenLastCalledWith(""));
});

it("失败成果在待查看预览明确标识，并链接原结果而不提前确认已读", async () => {
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 1,
        attention: [],
        results: [{ ...entry, lastStatus: "failed" }],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ resources: [], attention: [], hasMore: false }),
    ),
  );
  mount();
  const user = userEvent.setup();
  await user.click(await screen.findByRole("radio", { name: "待我查看 1" }));
  const region = screen.getByRole("region", { name: "待我查看" });
  expect(within(region).getByText("最近失败")).toBeInTheDocument();
  expect(
    within(region).getByRole("link", { name: "查看结果" }),
  ).toHaveAttribute(
    "href",
    `/chats/${entry.chatId}?wakerId=${entry.wakerId}&resultMessage=${entry.result.reference.messageId}`,
  );
});

it("分页使用当前筛选的真实总数，切换每页条数回到首屏并保留范围", async () => {
  const requests = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      requests(Object.fromEntries(new URL(request.url).searchParams));
      return boardResponse({
        resources: [entry],
        attention: [],
        totalSize: 37,
        hasMore: true,
        nextCursor: "next",
      });
    }),
  );
  mount("/work-board?source=automatic");
  const user = userEvent.setup();
  await screen.findByText("共 37 项");
  expect(requests.mock.lastCall?.[0].limit).toBe("10");
  await user.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(requests.mock.lastCall?.[0].cursor).toBe("next"));
  await user.click(screen.getByRole("button", { name: "每页条数" }));
  await user.click(screen.getByRole("menuitemradio", { name: "20 条/页" }));
  await waitFor(() =>
    expect(requests.mock.lastCall?.[0]).toMatchObject({
      limit: "20",
      source: "automatic",
    }),
  );
  expect(requests.mock.lastCall?.[0].cursor).toBeUndefined();
  expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "第 1 页" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

it("总数字段缺失时显示读取失败，不能把已有记录误报为共0项", async () => {
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      HttpResponse.json({
        workCount: 1,
        runningCount: 0,
        endedCount: 1,
        actionWorkCount: 0,
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      HttpResponse.json({
        resources: [entry],
        attention: [],
        hasMore: true,
        nextCursor: "next",
      }),
    ),
  );
  mount();
  await screen.findAllByText("读取失败");
  expect(screen.queryByText("共 0 项")).not.toBeInTheDocument();
});

it("完整页码可以直接跳到第3页，返回首屏或相邻页仍保留来源和条数", async () => {
  const requests = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      const query = new URL(request.url).searchParams;
      requests(Object.fromEntries(query));
      return boardResponse({
        resources: [
          {
            ...entry,
            title: `工作第${query.get("page") || (query.has("cursor") ? "2" : "1")}页`,
          },
        ],
        attention: [],
        totalSize: 23,
        hasMore: query.get("page") !== "3",
        nextCursor: "next",
      });
    }),
  );
  mount("/work-board?source=automatic");
  const user = userEvent.setup();
  await screen.findByText("共 23 项");
  expect(screen.getByRole("button", { name: "第 2 页" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "第 3 页" }));
  await waitFor(() =>
    expect(requests.mock.lastCall?.[0]).toMatchObject({
      page: "3",
      source: "automatic",
      limit: "10",
    }),
  );
  expect(requests.mock.lastCall?.[0].cursor).toBeUndefined();
  await screen.findByText("工作第3页");
  await user.click(screen.getByRole("button", { name: "第 1 页" }));
  await screen.findByText("工作第1页");
  expect(screen.getByRole("button", { name: "第 1 页" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

it("切换待查看模式后仍能清除保留的日期筛选", async () => {
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ resources: [], attention: [], hasMore: false }),
    ),
  );
  mount("/work-board?mode=results&from=2026-10-01&to=2026-10-12");
  const records = screen.getByRole("region", { name: "工作记录" });
  await userEvent
    .setup()
    .click(within(records).getByRole("button", { name: "清除筛选" }));
  await waitFor(() =>
    expect(
      within(records).queryByRole("button", { name: "清除筛选" }),
    ).not.toBeInTheDocument(),
  );
});

it("执行者仅筛选工作记录，清除后恢复全部执行者", async () => {
  const requests = vi.fn();
  const summaries = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, ({ request }) => {
      summaries(Object.fromEntries(new URL(request.url).searchParams));
      return boardResponse({
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      });
    }),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      requests(new URL(request.url).searchParams.get("executorId"));
      return boardResponse({ resources: [], attention: [], hasMore: false });
    }),
    http.get(`${origin}/api/v1/work-board/executors`, () =>
      HttpResponse.json({
        resources: [{ id: "b".repeat(32), name: "资料伙伴", kind: "waker" }],
      }),
    ),
  );
  mount();
  const user = userEvent.setup();
  const records = screen.getByRole("region", { name: "工作记录" });
  expect(
    within(records).queryByRole("button", { name: "清除筛选" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "全部执行者" }));
  await user.click(await screen.findByRole("button", { name: "资料伙伴" }));
  await waitFor(() =>
    expect(requests).toHaveBeenLastCalledWith("b".repeat(32)),
  );
  expect(summaries).toHaveBeenCalledTimes(1);
  expect(summaries).toHaveBeenLastCalledWith({});
  const clear = within(records).getByRole("button", { name: "清除筛选" });
  await user.click(clear);
  expect(requests).toHaveBeenCalledWith(null);
  expect(
    screen.getByRole("button", { name: "全部执行者" }),
  ).toBeInTheDocument();
  expect(
    within(records).queryByRole("button", { name: "清除筛选" }),
  ).not.toBeInTheDocument();
});

it("数字跳页后由资源变化刷新当前页，保留筛选和页码", async () => {
  const requests = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 0,
        attention: [],
        results: [],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      requests(Object.fromEntries(new URL(request.url).searchParams));
      return boardResponse({
        resources: [entry],
        attention: [],
        totalSize: 23,
        hasMore: false,
      });
    }),
  );
  mount("/work-board?page=3&source=automatic");
  await screen.findByText("整理资料");
  const initialRequests = requests.mock.calls.length;
  publishWorkBoardChange();
  await waitFor(
    () => expect(requests.mock.calls.length).toBeGreaterThan(initialRequests),
    { timeout: 7000 },
  );
  expect(requests.mock.lastCall?.[0]).toMatchObject({
    page: "3",
    source: "automatic",
  });
});

it.each([
  { page: 2, total: 10, target: "1" },
  { page: 3, total: 11, target: "2" },
])(
  "总数缩减到$total后将第$page页与旧游标一起纠正",
  async ({ page, total, target }) => {
    let reduced = false;
    const requests = vi.fn();
    server.use(
      http.get(`${origin}/api/v1/work-board/summary`, () =>
        boardResponse({
          attentionCount: 0,
          resultCount: 0,
          attention: [],
          results: [],
        }),
      ),
      http.get(`${origin}/api/v1/work-board`, ({ request }) => {
        const query = new URL(request.url).searchParams;
        requests(Object.fromEntries(query));
        const corrected =
          reduced && query.get("page") === target && !query.has("cursor");
        return boardResponse({
          resources: reduced
            ? corrected
              ? [{ ...entry, title: "仍未查看的结果" }]
              : []
            : [entry],
          attention: [],
          totalSize: reduced ? total : (page - 1) * 10 + 1,
          hasMore: false,
        });
      }),
    );
    mount(
      `/work-board?mode=results&page=${page}&cursor=old-anchor&source=automatic`,
    );
    await screen.findByText("整理资料");
    reduced = true;
    publishWorkBoardChange();
    await screen.findByText("仍未查看的结果");
    expect(requests.mock.lastCall?.[0]).toMatchObject({
      page: target,
      mode: "results",
      source: "automatic",
      limit: "10",
    });
    expect(requests.mock.lastCall?.[0].cursor).toBeUndefined();
    expect(
      screen.getByRole("button", { name: `第 ${target} 页` }),
    ).toHaveAttribute("aria-current", "page");
  },
);
