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
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
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
import { ChatQueueProvider, WorkbenchProvider } from "@/modules/chat";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { client } from "@/shared/api/generated/client.gen";
import { WorkBoardPage } from "./work-board-page";
import type { GlobalEventObserver } from "@/shared/api/global-events";
// 业务集成测试保留真实头像外壳与名称；形象绘制及异步资源生命周期由 companion 专属测试验证。
vi.mock("@/shared/companion/companion-avatar", () => ({
  CompanionAvatar: () => null,
}));
const eventObservers = vi.hoisted(() => new Map<GlobalEventObserver, string>());
const readyEpoch = vi.hoisted(() => ({
  current: undefined as string | undefined,
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  getReadyEventEpoch: () => readyEpoch.current,
  subscribeGlobalEvents: (
    topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    eventObservers.set(observer, topic);
    return () => eventObservers.delete(observer);
  },
}));
function publishWorkBoardChange(type = "changed", epoch = "epoch") {
  const topic = "work-board";
  act(() => {
    for (const [observer, subscribedTopic] of eventObservers)
      if (subscribedTopic === topic)
        observer.onEvent({
          protocolVersion: 1,
          epoch,
          connectionId: "connection",
          deliverySequence: 1,
          topic,
          type: `${topic}.${type}`,
          resource: { kind: "global" },
          eventId: "event",
          payload: {},
        });
  });
}

const origin = "http://work-board.test";
function boardResponse(value: Record<string, unknown>) {
  if ("attentionCount" in value) {
    const {
      actionWorkCount,
      attentionCount,
      resultCount,
      attention,
      results,
      ...counts
    } = value;
    return HttpResponse.json({
      complete: true,
      epoch: "epoch",
      workCount: 1,
      runningCount: 0,
      endedCount: 1,
      waitingWorkCount: actionWorkCount ?? 0,
      pendingRequestCount: attentionCount,
      awaitingViewWorkCount: resultCount,
      actionPreview: attention,
      awaitingViewPreview: results,
      ...counts,
    });
  }
  if ("resources" in value && !("totalSize" in value))
    return HttpResponse.json({
      totalSize: value.hasMore
        ? 21
        : Array.isArray(value.resources)
          ? value.resources.length
          : 0,
      complete: true,
      epoch: "epoch",
      ...value,
    });
  return HttpResponse.json({ complete: true, epoch: "epoch", ...value });
}
beforeEach(() => {
  readyEpoch.current = undefined;
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
  http.get(`${origin}/api/v1/wakers/:id/projects`, () =>
    HttpResponse.json({ resources: [], totalSize: 0 }),
  ),
  http.get(`${origin}/api/v1/wakers/:id/recent-project`, () =>
    HttpResponse.json({}),
  ),
  http.get(`${origin}/api/v1/wakers/:id/skills`, () => HttpResponse.json([])),
  http.get(`${origin}/api/v1/wakers/:id/connectors`, () =>
    HttpResponse.json([]),
  ),
  http.get(`${origin}/api/v1/wakers/:id/plugin-references`, () =>
    HttpResponse.json([]),
  ),
  http.get(`${origin}/api/v1/runtimes`, () =>
    HttpResponse.json({ resources: [] }),
  ),
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
  awaitingView: true,
  unreadMessageCount: 1,
  firstUnreadMessageID: "c".repeat(32),
};

function mount(initial = "/work-board", controls?: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <ChatQueueProvider>
          <WorkbenchProvider>
            {controls}
            <WorkBoardPage />
          </WorkbenchProvider>
        </ChatQueueProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return queryClient;
}

it("关注区保留准确统计，列表标题过滤不改变关注区；查看结果链接不提前提交已读", async () => {
  const summaries = vi.fn();
  const lists = vi.fn();
  const focusReads = vi.fn();
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
      const params = new URL(request.url).searchParams;
      if (["50", "100"].includes(params.get("limit") ?? "")) {
        focusReads(params.get("mode"));
        const isResults = params.get("mode") === "awaiting_view";
        return boardResponse({
          resources: [],
          attention: [],
          awaitingView: isResults ? [entry] : [],
          totalSize: isResults ? 9 : 7,
          hasMore: false,
        });
      }
      lists(params.get("query"));
      return boardResponse({
        resources: [entry],
        attention: [],
        hasMore: false,
      });
    }),
    http.post(`${origin}/api/v1/chats/:id/reading`, () => {
      viewed();
      return boardResponse({ success: true });
    }),
  );
  mount();
  const user = userEvent.setup();
  expect(await screen.findByText("最近已完成")).toBeInTheDocument();
  const action = screen.getByRole("region", { name: "需要处理" });
  expect(within(action).getByRole("heading")).toHaveTextContent("7");
  const results = screen.getByRole("region", { name: "待我查看" });
  const result = await within(results).findByRole("link", { name: "查看结果" });
  expect(within(results).getByRole("heading")).toHaveTextContent("9");
  expect(result).toHaveAttribute(
    "href",
    `/chats/${entry.chatId}?wakerId=${entry.wakerId}&message=${entry.firstUnreadMessageID}`,
  );
  await user.type(
    screen.getByRole("searchbox", { name: "搜索任务标题" }),
    "归档",
  );
  await waitFor(() => expect(lists).toHaveBeenLastCalledWith("归档"));
  expect(summaries).toHaveBeenCalledTimes(1);
  expect(focusReads).not.toHaveBeenCalled();
  expect(viewed).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
  publishWorkBoardChange();
  await waitFor(() => expect(summaries).toHaveBeenCalledTimes(2));
  expect(focusReads).not.toHaveBeenCalled();
});

it("并排关注区复用执行者头像和精确链接，省去重复名称及审批标签", async () => {
  const requestId = "approval-request";
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 1,
        resultCount: 2,
        attention: [
          {
            id: "approval",
            kind: "approval",
            ownerKind: "chat",
            ownerId: entry.chatId,
            chatId: entry.chatId,
            wakerId: entry.wakerId,
            requestId,
            title: "资料伙伴",
            workTitle: "资料伙伴",
            actorName: "资料伙伴",
          },
        ],
        results: [
          { ...entry, title: "资料伙伴" },
          { ...entry, ownerId: "unnamed", title: "其他工作", executorName: "" },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/wakers`, () =>
      boardResponse({
        resources: [
          { id: entry.wakerId, name: "资料伙伴", avatar: "/companion" },
        ],
        totalSize: 1,
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ resources: [], totalSize: 0 }),
    ),
  );
  mount();
  const list = await screen.findByRole("region", { name: "需要处理" });
  await within(list).findByText("资料伙伴");
  const action = within(list);
  expect(action.getByRole("list")).toHaveClass("gap-2");
  expect(
    action.getByText("资料伙伴").closest("li")?.firstElementChild,
  ).toHaveClass("bg-surface-raised", "rounded-md");
  expect(action.getAllByText("资料伙伴")).toHaveLength(1);
  expect(action.getByLabelText("资料伙伴")).toBeInTheDocument();
  expect(action.queryByText("审批", { exact: true })).toBeNull();
  expect(action.getByRole("link", { name: "去处理" })).toHaveAttribute(
    "href",
    expect.stringContaining(requestId),
  );
  expect(action.getByRole("link", { name: "去处理" })).not.toHaveClass(
    "bg-button-fill",
  );

  const results = within(screen.getByRole("region", { name: "待我查看" }));
  expect(results.getByRole("link", { name: "资料伙伴" })).toBeInTheDocument();
  expect(results.getAllByText("资料伙伴")).toHaveLength(1);
  expect(results.getAllByLabelText("资料伙伴").length).toBeGreaterThan(0);
  expect(results.queryByText("执行者信息暂不可用")).toBeNull();
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
    within(screen.getByRole("region", { name: "需要处理" })).getByRole(
      "heading",
    ),
  ).toHaveTextContent("1");
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
        params.get("mode") === "action"
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
  mount("/work-board?mode=action&source=automatic");
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
        mode: "action",
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

it("旧交付链接打开全部任务，保留来源、日期和标题筛选并从第一页开始", async () => {
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
      const filters = new URL(request.url).searchParams;
      queries(Object.fromEntries(filters));
      return boardResponse({
        resources: [entry],
        attention: [],
        hasMore: true,
        nextCursor: "anchor",
      });
    }),
  );
  mount(
    "/work-board?view=deliveries&page=2&cursor=old&source=automatic&query=归档&from=2026-10-01&to=2026-10-12",
  );
  const user = userEvent.setup();
  await screen.findByText("最近已完成");
  expect(summaryQueries).toHaveBeenLastCalledWith({});
  expect(screen.getByRole("heading", { name: "全部任务" })).toBeVisible();
  expect(screen.queryByRole("radio", { name: "最近交付" })).toBeNull();
  expect(queries.mock.lastCall?.[0]).toMatchObject({
    source: "automatic",
    query: "归档",
  });
  expect(queries.mock.lastCall?.[0].states).toBeUndefined();
  expect(queries.mock.lastCall?.[0].cursor).toBeUndefined();
  expect(queries.mock.lastCall?.[0].from).toBeDefined();
  expect(await screen.findByRole("table", { name: "工作记录" })).toBeVisible();
  expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(queries.mock.lastCall?.[0].cursor).toBe("anchor"));
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
  expect(
    within(screen.getByRole("region", { name: "需要处理" })).getByRole(
      "heading",
    ),
  ).toHaveTextContent("0");
  expect(
    within(screen.getByRole("region", { name: "待我查看" })).getByRole(
      "heading",
    ),
  ).toHaveTextContent("0");
  expect(screen.getByText("没有待查看结果")).toBeInTheDocument();
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
    screen.getByRole("columnheader", { name: "任务" }),
  ).toBeInTheDocument();
  await waitFor(() => expect(lists).toHaveBeenLastCalledWith(""));
});

it.each(["/work-board", "/work-board?mode=awaiting_view"])(
  "%s 同时展示待处理空态和待查看失败结果",
  async (route) => {
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
    mount(route);
    expect(await screen.findByText("没有待处理事项")).toBeInTheDocument();
    const region = screen.getByRole("region", { name: "待我查看" });
    expect(within(region).getByText("最近失败")).toBeInTheDocument();
    expect(
      within(region).getByRole("link", { name: "查看结果" }),
    ).toHaveAttribute(
      "href",
      `/chats/${entry.chatId}?wakerId=${entry.wakerId}&message=${entry.firstUnreadMessageID}`,
    );
  },
);

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
  mount("/work-board?mode=awaiting_view&from=2026-10-01&to=2026-10-12");
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
  expect(
    within(screen.getByRole("dialog")).queryByRole("button", {
      name: "全部执行者",
    }),
  ).not.toBeInTheDocument();
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
  await within(screen.getByRole("table")).findByText("整理资料");
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
          awaitingView: reduced
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
      `/work-board?mode=awaiting_view&page=${page}&cursor=old-anchor&source=automatic`,
    );
    const table = within(screen.getByRole("table"));
    await table.findByText("整理资料");
    reduced = true;
    publishWorkBoardChange();
    await table.findByText("仍未查看的结果", {}, { timeout: 7000 });
    expect(requests.mock.lastCall?.[0]).toMatchObject({
      page: target,
      mode: "awaiting_view",
      source: "automatic",
      limit: "10",
    });
    expect(requests.mock.lastCall?.[0].cursor).toBeUndefined();
    expect(
      screen.getByRole("button", { name: `第 ${target} 页` }),
    ).toHaveAttribute("aria-current", "page");
  },
);

it("另一窗口确认阅读后，成果事件同步看板统计和当前列表", async () => {
  let viewed = false;
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        workCount: viewed ? 2 : 1,
        attentionCount: 0,
        resultCount: viewed ? 0 : 1,
        attention: [],
        results: viewed ? [] : [entry],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({
        resources: [{ ...entry, title: viewed ? "已阅读的工作" : "整理资料" }],
        totalSize: 1,
        hasMore: false,
      }),
    ),
  );
  mount();
  await within(screen.getByRole("table")).findByText("整理资料");
  const results = within(screen.getByRole("region", { name: "待我查看" }));
  expect(results.getByRole("heading")).toHaveTextContent("1");
  viewed = true;
  publishWorkBoardChange();
  await waitFor(() =>
    expect(results.getByText("没有待查看结果")).toBeInTheDocument(),
  );
  expect(await screen.findByText("已阅读的工作")).toBeInTheDocument();
});

it("关注列表等待在途深页，再用一次有界头部读取对账并沿新游标继续", async () => {
  const reads: string[] = [];
  let releasePage = () => {};
  const delayedPage = new Promise<void>((resolve) => {
    releasePage = resolve;
  });
  const row = (id: string, title: string) => ({
    ...entry,
    ownerId: id,
    chatId: id,
    title,
  });
  const pages = {
    head: { row: row("1", "原关注首屏"), nextCursor: "next" },
    next: { row: row("2", "第二页旧关注"), nextCursor: "third" },
    third: { row: row("3", "在途旧关注"), nextCursor: "old-tail" },
    "old-tail": { row: row("4", "无须重读的旧页"), nextCursor: "" },
    fresh: { row: row("1", "已更新的关注头部"), nextCursor: "fresh-tail" },
    "fresh-tail": { row: row("5", "从新游标加载"), nextCursor: "" },
  };
  let changed = false;
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({ attentionCount: 0, resultCount: 200, results: [] }),
    ),
    http.get(`${origin}/api/v1/work-board`, async ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("mode") !== "awaiting_view")
        return boardResponse({ resources: [] });
      const cursor = params.get("cursor");
      const page =
        pages[(cursor ?? (changed ? "fresh" : "head")) as keyof typeof pages];
      reads.push(`${cursor ?? "head"}:${params.get("limit")}`);
      if (cursor === "third") await delayedPage;
      return boardResponse({
        totalSize: 200,
        awaitingView: [page.row],
        nextCursor: page.nextCursor,
      });
    }),
  );
  mount();
  try {
    const focus = await screen.findByRole("region", { name: "待我查看" });
    await userEvent.click(
      await within(focus).findByRole("button", { name: "更多" }),
    );
    await within(focus).findByText("原关注首屏");
    fireEvent.scroll(focus);
    await within(focus).findByText("第二页旧关注");
    fireEvent.scroll(focus);
    await waitFor(() => expect(reads).toContain("third:50"));
    focus.scrollTop = 72;
    changed = true;
    publishWorkBoardChange();
    await act(() => delay(300));
    expect(reads.map((item) => item.split(":")[0])).toEqual([
      "head",
      "next",
      "third",
    ]);
    releasePage();
    await within(focus).findByText("已更新的关注头部");
    expect(reads.map((item) => item.split(":")[0])).toEqual([
      "head",
      "next",
      "third",
      "head",
    ]);
    expect(reads.at(-1)).toBe("head:100");
    expect(focus.scrollTop).toBe(72);
    fireEvent.scroll(focus);
    await within(focus).findByText("从新游标加载");
    expect(reads.at(-1)).toBe("fresh-tail:50");
    expect(within(focus).queryByText("在途旧关注")).toBeNull();
  } finally {
    releasePage();
  }
});

it("关注头部重连拒绝旧 epoch，临时不完整保留旧窗口并自动补读", async () => {
  const modes: string[] = [];
  let reconnecting = false;
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 0,
        resultCount: 200,
        results: [],
        epoch: reconnecting ? "next-epoch" : "epoch",
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("mode") !== "awaiting_view")
        return boardResponse({
          resources: [],
          epoch: reconnecting ? "next-epoch" : "epoch",
        });
      const cursor = params.get("cursor");
      if (!reconnecting)
        return boardResponse({
          totalSize: 200,
          awaitingView: [
            {
              ...entry,
              ownerId: cursor ? "tail" : "head",
              title: cursor ? "保留的旧尾页" : "保留的旧头部",
            },
          ],
          nextCursor: cursor ? undefined : "tail",
        });
      const index = modes.length;
      modes.push(cursor ?? "head");
      return boardResponse({
        totalSize: 200,
        epoch: index === 0 ? "epoch" : "next-epoch",
        complete: index !== 1,
        incompleteReason: index === 1 ? "recovering" : undefined,
        awaitingView: [
          {
            ...entry,
            title:
              index === 0
                ? "过期响应"
                : index === 1
                  ? "临时不完整响应"
                  : "新进程有效头部",
          },
        ],
      });
    }),
  );
  mount();
  const focus = await screen.findByRole("region", { name: "待我查看" });
  await userEvent.click(
    await within(focus).findByRole("button", { name: "更多" }),
  );
  await within(focus).findByText("保留的旧头部");
  fireEvent.scroll(focus);
  await within(focus).findByText("保留的旧尾页");
  reconnecting = true;
  readyEpoch.current = "next-epoch";
  publishWorkBoardChange("ready", readyEpoch.current);
  await waitFor(() => expect(modes).toEqual(["head", "head"]), {
    timeout: 2500,
  });
  expect(within(focus).getByText("保留的旧尾页")).toBeVisible();
  expect(within(focus).queryByText("过期响应")).toBeNull();
  expect(within(focus).queryByText("临时不完整响应")).toBeNull();
  await within(focus).findByText("新进程有效头部", {}, { timeout: 3500 });
  expect(modes).toEqual(["head", "head", "head"]);
});

it("关注栏后台读取失败时保留旧页，握手后重新对账", async () => {
  const reads: Array<string | null> = [];
  let recovering = false;
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({ attentionCount: 0, resultCount: 150, results: [] }),
    ),
    http.get(`${origin}/api/v1/work-board`, ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("mode") !== "awaiting_view")
        return boardResponse({ resources: [] });
      const cursor = params.get("cursor");
      reads.push(cursor);
      if (recovering) return new HttpResponse(null, { status: 503 });
      return boardResponse({
        totalSize: 150,
        awaitingView: [
          {
            ...entry,
            ownerId: cursor ? "tail" : "head",
            title: cursor ? "原关注尾页" : "原关注头部",
          },
        ],
        nextCursor: cursor ? undefined : "tail",
      });
    }),
  );
  mount();
  const focus = await screen.findByRole("region", { name: "待我查看" });
  await userEvent.click(
    await within(focus).findByRole("button", { name: "更多" }),
  );
  await within(focus).findByText("原关注头部");
  fireEvent.scroll(focus);
  await within(focus).findByText("原关注尾页");
  recovering = true;
  publishWorkBoardChange();
  await within(focus).findByRole("alert");
  expect(within(focus).getByText("原关注尾页")).toBeVisible();
  await act(() => delay(100));
  expect(reads).toEqual([null, "tail", null]);
  recovering = false;
  publishWorkBoardChange("ready");
  await waitFor(() => expect(reads).toEqual([null, "tail", null, null]));
  await waitFor(() =>
    expect(within(focus).queryByText("原关注尾页")).toBeNull(),
  );
});

it("深处浏览保留窗口和滚动，头部请求在途也等返回后再替换", async () => {
  let releaseHead = () => {};
  const delayedHead = new Promise<void>((resolve) => {
    releaseHead = resolve;
  });
  let resultCount = 110;
  let headReads = 0;
  let changed = false;
  const row = (index: number, title = `关注项 ${index}`) => ({
    ...entry,
    ownerId: String(index).padStart(32, "0"),
    chatId: String(index).padStart(32, "0"),
    title,
  });
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({ attentionCount: 0, resultCount, results: [] }),
    ),
    http.get(`${origin}/api/v1/work-board`, async ({ request }) => {
      const params = new URL(request.url).searchParams;
      if (params.get("mode") !== "awaiting_view")
        return boardResponse({ resources: [] });
      const cursor = params.get("cursor");
      if (changed && !cursor) {
        if (++headReads === 1) await delayedHead;
        return boardResponse({
          totalSize: resultCount,
          awaitingView: [row(1, "新关注头部")],
        });
      }
      const start =
        cursor === "page4"
          ? 106
          : cursor === "page3"
            ? 101
            : cursor === "page2"
              ? 51
              : 1;
      return boardResponse({
        totalSize: resultCount,
        awaitingView: Array.from({ length: start >= 101 ? 5 : 50 }, (_, i) =>
          row(start + i),
        ),
        nextCursor:
          start === 1
            ? "page2"
            : start === 51
              ? "page3"
              : start === 101
                ? "page4"
                : undefined,
      });
    }),
  );
  mount();
  try {
    const focus = await screen.findByRole("region", { name: "待我查看" });
    await userEvent.click(
      await within(focus).findByRole("button", { name: "更多" }),
    );
    await within(focus).findByText("关注项 50");
    fireEvent.scroll(focus);
    await within(focus).findByText("关注项 100");
    fireEvent.scroll(focus);
    const beyondHead = (await within(focus).findByText("关注项 101")).closest(
      "li",
    );
    if (!beyondHead) throw new Error("Missing deep work board entry");
    vi.spyOn(beyondHead, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, focus.scrollTop > 0 ? -1 : 100, 100, 24),
    );
    changed = true;
    publishWorkBoardChange();
    await waitFor(() => expect(headReads).toBe(1));
    focus.scrollTop = 800;
    // 模拟滚动位置先变化，原生 scroll 通知在响应之后才送达。
    releaseHead();
    await waitFor(() => expect(focus).toHaveAttribute("aria-busy", "false"));
    expect(within(focus).getByText("关注项 101")).toBeVisible();
    expect(focus.scrollTop).toBe(800);
    fireEvent.scroll(focus);
    await within(focus).findByText("关注项 110");

    resultCount = 111;
    publishWorkBoardChange();
    await waitFor(
      () => expect(within(focus).getByRole("heading")).toHaveTextContent("111"),
      { timeout: 7000 },
    );
    await act(() => delay(300));
    expect(headReads).toBe(1);
    expect(within(focus).getByText("关注项 101")).toBeVisible();

    focus.scrollTop = 0;
    fireEvent.scroll(focus);
    await within(focus).findByText("新关注头部");
    expect(headReads).toBe(2);
  } finally {
    releaseHead();
  }
});

it("批量确认冻结当前筛选和阅读边界，部分失败后重试相同批次", async () => {
  const filters: string[] = [];
  const submitted: unknown[][] = [];
  const target = (index: number) => ({
    work: { kind: "chat", id: `chat-${index}` },
    chatId: `chat-${index}`,
    throughMessageID: `message-${index}`,
    observedUnreadGeneration: 2,
  });
  let available = Array.from({ length: 129 }, (_, index) => target(index));
  server.use(
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({
        attentionCount: 1,
        resultCount: 129,
        attention: [{ id: "request", kind: "approval", title: "仍需审批" }],
        results: [entry],
      }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ totalSize: 1, resources: [entry] }),
    ),
    http.get(`${origin}/api/v1/work-board/view-targets`, ({ request }) => {
      filters.push(new URL(request.url).searchParams.get("source") ?? "");
      return HttpResponse.json({
        targets: available,
        targetCount: available.length,
        epoch: "epoch",
      });
    }),
    http.post(
      `${origin}/api/v1/work-board/mark-viewed`,
      async ({ request }) => {
        const data = (await request.json()) as { targets: unknown[] };
        submitted.push(data.targets);
        if (submitted.length === 2)
          return new HttpResponse(null, { status: 503 });
        return HttpResponse.json({
          applied: submitted.length === 1 ? 127 : 0,
          skippedConflict: 1,
          skippedDeleted: 0,
        });
      },
    ),
  );
  mount("/work-board?source=automatic");
  const user = userEvent.setup();
  expect(await screen.findByText("仍需审批")).toBeInTheDocument();
  await user.click(
    await screen.findByRole("button", { name: "全部标为已查看" }),
  );
  const dialog = await screen.findByRole("dialog");
  expect(dialog).toHaveTextContent("129");
  available = [target(200)];
  expect(submitted).toHaveLength(0);
  await user.click(
    within(dialog).getByRole("button", { name: "全部标为已查看" }),
  );
  await within(dialog).findByRole("alert");
  expect(dialog).toHaveTextContent("剩余 1 项");
  await user.click(within(dialog).getByRole("button", { name: "重试" }));
  await waitFor(() => expect(dialog).toHaveTextContent("剩余 0 项"));
  expect(submitted.map((batch) => batch.length)).toEqual([128, 1, 1]);
  expect(submitted[2]).toEqual([target(128)]);
  expect(filters).toEqual(["automatic"]);
  expect(dialog).toHaveTextContent("状态变化 2 项");
  await user.click(within(dialog).getByText("关闭"));
  expect(screen.getByText("仍需审批")).toBeInTheDocument();
});

it("看板发送伙伴首条消息失败后保留草稿，以同一请求重试并打开接纳的对话", async () => {
  const bodies: unknown[] = [];
  const wakerId = "f".repeat(32);
  const chatId = "e".repeat(32);
  const read = vi.fn();
  const partnerSearches: string[] = [];
  let releaseFirstResponse = () => {};
  const firstResponse = new Promise<void>((resolve) => {
    releaseFirstResponse = resolve;
  });
  let model = "sonnet";
  const partner = () => ({
    id: wakerId,
    name: "设计伙伴",
    defaultModel: {
      runtimeId: "claude",
      accountId: "configured",
      model,
      reasoning: "low",
    },
  });
  server.use(
    http.get(`${origin}/api/v1/wakers`, ({ request }) => {
      const search = new URL(request.url).searchParams.get("query") ?? "";
      partnerSearches.push(search);
      return boardResponse({
        resources: search
          ? [partner()]
          : [{ id: entry.wakerId, name: "默认伙伴", isDefault: true }],
        totalSize: search ? 1 : 101,
      });
    }),
    http.get(`${origin}/api/v1/wakers/:wakerId`, ({ params }) =>
      HttpResponse.json(
        params.wakerId === wakerId
          ? partner()
          : { id: entry.wakerId, name: "默认伙伴", isDefault: true },
      ),
    ),
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [
          { id: "codex", enabled: true, isDefault: true },
          {
            id: "claude",
            enabled: true,
            defaultPermissionLevel: "auto",
            models: [
              {
                id: "sonnet",
                permissionLevels: ["default", "auto", "full-access"],
              },
              {
                id: "opus",
                permissionLevels: ["default", "auto", "full-access"],
              },
            ],
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/work-board/summary`, () =>
      boardResponse({ attentionCount: 0, resultCount: 0 }),
    ),
    http.get(`${origin}/api/v1/work-board`, () =>
      boardResponse({ resources: [] }),
    ),
    http.get(`${origin}/api/v1/ai-accounts`, () => {
      read();
      return HttpResponse.json({ resources: [] });
    }),
    http.get(`${origin}/api/v1/runtimes/:runtimeId/models`, () => {
      read();
      return HttpResponse.json({ models: [] });
    }),
    http.post(`${origin}/api/v1/chats`, async ({ request }) => {
      bodies.push(await request.json());
      if (bodies.length === 1) {
        await firstResponse;
        return new HttpResponse(null, { status: 503 });
      }
      if (bodies.length === 2) return new HttpResponse(null, { status: 400 });
      return HttpResponse.json({
        chat: { id: chatId, wakerId, latestSequence: 1 },
      });
    }),
  );
  function CurrentRoute() {
    const { pathname, search } = useLocation();
    return <output aria-label="当前路由">{pathname + search}</output>;
  }
  const queryClient = mount("/work-board", <CurrentRoute />);
  const user = userEvent.setup();
  const form = within(
    screen.getByRole("region", { name: /^和\s*伙伴\s*一起创造$/ }),
  );
  const input = form.getByRole("textbox");
  expect(input).toHaveAttribute("contenteditable", "true");
  expect(form.getByRole("button", { name: "添加附件" })).toBeInTheDocument();
  expect(form.getByRole("button", { name: "发送消息" })).toBeDisabled();
  const picker = form.getByRole("button", { name: "选择伙伴" });
  const cliPicker = form.getByTestId("composer-model-control");
  expect(picker.parentElement).toBe(cliPicker.parentElement);
  expect(picker.compareDocumentPosition(cliPicker)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  await waitFor(() => expect(picker).toBeEnabled());
  expect(partnerSearches).toEqual([""]);
  fireEvent.keyDown(picker, { key: "ArrowDown" });
  await user.type(
    await screen.findByRole("searchbox", { name: "搜索伙伴" }),
    "设计伙伴",
  );
  await user.click(await screen.findByRole("button", { name: "设计伙伴" }));
  expect(form.getByTestId("composer-permission-control")).toHaveTextContent(
    "完全访问",
  );
  // 该账号的模型不在本机账号快照里，仍允许显式收紧权限。
  const permission = form.getByTestId("composer-permission-control");
  await user.click(permission);
  await user.click(
    await screen.findByRole("menuitemradio", { name: /^默认权限/ }),
  );
  expect(permission).toHaveTextContent("默认权限");
  await user.click(permission);
  await user.click(
    await screen.findByRole("menuitemradio", { name: /^完全访问/ }),
  );
  await user.type(input, "整理设计稿");
  const send = await form.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  fireEvent.keyDown(input, { key: "Enter", isComposing: true });
  expect(bodies).toHaveLength(0);
  await user.click(await form.findByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(bodies).toHaveLength(1));
  expect(form.getByRole("button", { name: "发送消息" })).toBeDisabled();
  expect(form.queryByRole("button", { name: "停止执行" })).toBeNull();
  expect(picker).toBeDisabled();
  releaseFirstResponse();
  await form.findByRole("button", { name: "重试原发送" });
  await waitFor(() => expect(input).toHaveTextContent("整理设计稿"));
  expect(picker).toBeDisabled();
  expect(cliPicker).toBeDisabled();
  const lockedPermission = form.queryByTestId("composer-permission-control");
  if (lockedPermission) expect(lockedPermission).toBeDisabled();
  expect(form.getByRole("button", { name: "发送消息" })).toBeDisabled();
  fireEvent.keyDown(input, { key: "Enter" });
  expect(bodies).toHaveLength(1);
  model = "opus";
  await act(() =>
    queryClient.invalidateQueries({
      queryKey: getWakerOptions({ path: { wakerId } }).queryKey,
    }),
  );
  await user.click(await form.findByRole("button", { name: "重试原发送" }));
  await waitFor(() => expect(bodies).toHaveLength(2));
  await waitFor(() =>
    expect(form.getByRole("button", { name: "重试原发送" })).toBeEnabled(),
  );
  await user.click(form.getByRole("button", { name: "重试原发送" }));
  await waitFor(() =>
    expect(screen.getByLabelText("当前路由")).toHaveTextContent(
      `/chats/${chatId}?wakerId=${wakerId}`,
    ),
  );
  expect(bodies).toHaveLength(3);
  expect(bodies[0]).toMatchObject({
    clientRequestId: expect.any(String),
    wakerId,
    runtimeId: "claude",
    accountId: "configured",
    model: "sonnet",
    reasoning: "low",
    permissionLevel: "full-access",
    text: "整理设计稿",
  });
  expect(bodies[1]).toEqual(bodies[0]);
  expect(bodies[2]).toEqual(bodies[0]);
  expect(read).not.toHaveBeenCalled();
});
