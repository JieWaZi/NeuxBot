import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  Link,
  MemoryRouter,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  afterAll,
  afterEach,
  beforeAll,
  expect,
  it,
  onTestFinished,
  vi,
} from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import type { GlobalEventObserver } from "@/shared/api/global-events";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgePage } from "./knowledge-page";
import { graphEngine } from "./knowledge-force-test-double";
import { saveOrganizePreferences } from "./knowledge-organize-preferences";

vi.mock("react-force-graph-2d", () => import("./knowledge-force-test-double"));
const { listeners } = vi.hoisted(() => ({
  listeners: new Set<GlobalEventObserver>(),
}));
vi.mock("@/shared/api/global-events", () => ({
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    listeners.add(observer);
    return () => listeners.delete(observer);
  },
}));
const server = setupServer(
  http.get(
    /\/knowledge\/(?:connections\/[^/]+\/)?libraries\/[^/]+\/(?:pages|page-directory)$/,
    () => HttpResponse.json({ items: [], total: 0 }),
  ),
  http.get(
    /\/knowledge\/(?:connections\/[^/]+\/)?libraries\/[^/]+\/themes$/,
    () => HttpResponse.json([]),
  ),
);
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => {
  server.resetHandlers();
  saveOrganizePreferences(library.ref, {
    selection: undefined,
    requirements: undefined,
  });
});
afterAll(() => server.close());
const library = {
  id: "kb",
  name: "通用资料库",
  kind: "general",
  ref: { backend: "local", connectionId: "local", knowledgeBaseId: "kb" },
  version: 1,
  structureVersion: 1,
  policyVersion: 1,
};
function acceptedReceipt(body: Record<string, unknown>) {
  return {
    requestId: body.requestId,
    libraryId: "kb",
    resourceId: "job",
    jobId: "job",
    version: 1,
    state: "accepted",
  };
}
const firstSource = {
  id: "source-1",
  sourceId: "raw-1",
  filename: "原始资料.md",
  state: "pending",
  size: 2,
  createdAt: 1,
};
function Location() {
  const value = useLocation();
  return (
    <>
      <Link to="/knowledge">测试切换到列表</Link>
      <output aria-label="当前路由">
        {value.pathname}
        {value.search}
      </output>
    </>
  );
}
function show(initial = "/knowledge/kb?view=raw") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[initial]}>
          <Location />
          <Routes>
            <Route path="/knowledge/:libraryId?" element={<KnowledgePage />} />
          </Routes>
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

it("进入知识库默认展示知识目录，原件保留明确入口", async () => {
  fixtures();
  show("/knowledge/kb");
  await vi.dynamicImportSettled();
  await screen.findByRole("heading", { name: "通用资料库" }, { timeout: 5000 });
  expect(
    screen.queryByRole("button", { name: "返回知识库列表" }),
  ).not.toBeInTheDocument();
  await screen.findByRole("radio", { name: "原件 1" });
  expect(await screen.findByRole("radio", { name: "知识 0" })).toBeChecked();
  expect(screen.getByRole("radio", { name: "原件 1" })).not.toBeChecked();
  expect(await screen.findByRole("treeitem", { name: "索引" })).toBeVisible();
  expect(
    screen.queryByRole("tree", { name: "原件列表" }),
  ).not.toBeInTheDocument();
});

it.each(["knowledge", "summaries"])(
  "停留在 %s 时上传和后台事件自动更新原件数量",
  async (tab) => {
    fixtures();
    let sourceTotal = 1;
    let organized = false;
    const summary = {
      id: "new-summary",
      slug: "summary/new",
      title: "新增摘要",
      pageType: "summary",
      content: "来源整理后的摘要正文。",
    };
    server.use(
      http.get(
        "http://ally.test/api/v1/knowledge/libraries/kb/page-directory",
        () =>
          HttpResponse.json({
            items: organized
              ? [
                  summary,
                  { id: "new-topic", title: "新增知识", pageType: "concept" },
                ]
              : [],
            total: organized ? 2 : 0,
          }),
      ),
      http.get(
        "http://ally.test/api/v1/knowledge/libraries/kb/pages/new-summary",
        () => HttpResponse.json(summary),
      ),
      http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
        HttpResponse.json({
          items: Array.from({ length: sourceTotal }, (_, index) => ({
            ...firstSource,
            id: `source-${index + 1}`,
          })),
          total: sourceTotal,
        }),
      ),
      http.post(
        "http://ally.test/api/v1/knowledge/libraries/kb/sources",
        () => {
          sourceTotal++;
          return HttpResponse.json({ resourceId: "source-2", state: "saved" });
        },
      ),
    );
    const user = userEvent.setup();
    const initial = `/knowledge/kb?view=wiki&wikiTab=${tab}`;
    show(initial);
    await vi.dynamicImportSettled();
    await screen.findByRole("radio", { name: "原件 1" });
    await user.click(screen.getByRole("button", { name: "上传资料" }));
    const file = new File(["ab"], "新增原件.md", { type: "text/markdown" });
    Object.defineProperty(file, "arrayBuffer", {
      value: async () => new Uint8Array([97, 98]).buffer,
    });
    fireEvent.change(
      await screen.findByLabelText<HTMLInputElement>("选择文件"),
      {
        target: { files: [file] },
      },
    );
    await user.click(screen.getByRole("button", { name: "上传文档" }));
    await within(screen.getByRole("dialog")).findByText("待整理");
    await user.click(screen.getByText("关闭", { selector: "button" }));
    await screen.findByRole("radio", { name: "原件 2" });
    sourceTotal = 3;
    organized = true;
    act(() => {
      for (const listener of listeners)
        listener.onEvent({
          protocolVersion: 1,
          epoch: "epoch",
          connectionId: "connection",
          deliverySequence: 1,
          topic: "knowledge",
          type: "knowledge.changed",
          resource: { kind: "knowledge_library", id: "kb", parentId: "local" },
          eventId: "new-source",
          transient: false,
          payload: { jobId: "job" },
        });
    });
    await screen.findByRole("radio", { name: "原件 3" });
    await screen.findByRole("radio", { name: "知识 1" });
    await screen.findByRole("radio", { name: "摘要 1" });
    expect(screen.getByLabelText("当前路由")).toHaveTextContent(initial);
    expect(screen.getByRole("radio", { name: "原件 3" })).not.toBeChecked();
  },
);
function fixtures() {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/wiki/index", () =>
      HttpResponse.json({ intro: "# Wiki Index", version: 1, groups: [] }),
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/source-purges",
      () => HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      () =>
        HttpResponse.json({
          libraryId: "kb",
          version: 0,
          wakerId: "",
          runtimeId: "",
          accountId: "",
          model: "",
          reasoning: "",
        }),
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async ({ request }) =>
        HttpResponse.json({
          ...((await request.json()) as object),
          libraryId: "kb",
          version: 1,
        }),
    ),
    http.get("http://ally.test/api/v1/memory/settings", () =>
      HttpResponse.json({}),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb", () =>
      HttpResponse.json(library),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({ items: [firstSource], total: 1 }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs/job", () =>
      HttpResponse.json({
        job: { id: "job", libraryId: "kb", state: "processing" },
        session: { id: "session", chatId: "" },
      }),
    ),
    http.get("http://ally.test/api/v1/runtimes", () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            models: [
              { id: "model", name: "Model", permissionLevels: ["default"] },
            ],
            defaultModel: "model",
          },
        ],
      }),
    ),
    http.get("http://ally.test/api/v1/ai-accounts", () =>
      HttpResponse.json({ resources: [] }),
    ),
  );
}

it("知识库只保留标题与同行操作，搜索仅在左侧原件目录", async () => {
  fixtures();
  const job = {
    id: "job",
    libraryId: "kb",
    state: "completed",
    sourceVersionIds: ["source-1"],
  };
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.json({ items: [job], total: 1 }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs/job", () =>
      HttpResponse.json({ job }),
    ),
  );
  show();
  await screen.findByText("原始资料.md", {}, { timeout: 5000 });
  expect(screen.getByRole("heading", { name: "通用资料库" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "返回列表" }),
  ).not.toBeInTheDocument();
  const actions = screen.getByRole("group", { name: "知识库操作" });
  expect(
    within(actions)
      .getAllByRole("button")
      .map((action) => action.getAttribute("aria-label")),
  ).toEqual(["图谱", "设置", "整理文档", "上传资料"]);
  expect(
    within(actions).getByRole("button", { name: "设置" }),
  ).toHaveTextContent("");
  expect(
    within(actions).getByRole("button", { name: "整理文档" }),
  ).toHaveTextContent("");
  expect(
    screen.getByRole("heading", { name: "通用资料库" }).closest("header"),
  ).toContainElement(actions);
  expect(
    screen.getByRole("searchbox", { name: "搜索原件…" }).closest("aside"),
  ).not.toBeNull();
  expect(
    screen.queryByRole("combobox", { name: "资料状态" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "排序" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("整理完成")).not.toBeInTheDocument();
  expect(screen.queryByText(/本次 .*份文档/)).not.toBeInTheDocument();
  expect(screen.queryByText("查看整理对话")).not.toBeInTheDocument();
  expect(screen.queryByText("1 份资料")).not.toBeInTheDocument();
  expect(screen.queryByText("整理本页待处理文档")).not.toBeInTheDocument();
  const sourceItem = screen.getByRole("treeitem", { name: "原始资料.md" });
  expect(sourceItem.closest("aside")).not.toBeNull();
  expect(sourceItem).toHaveAttribute("title", "原始资料.md");
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByRole("columnheader")).not.toBeInTheDocument();
});

it("空文档页不以固定最小高度撑开内容", async () => {
  fixtures();
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
  );
  show();
  const empty = await screen.findByText("还没有原始资料");
  expect(empty.parentElement?.parentElement).toHaveClass("min-h-0");
  expect(empty.parentElement?.parentElement).not.toHaveClass("min-h-64");
});

it.each(["relations", "history", "source", "project", "chat"])(
  "旧 %s 入口回到知识目录并规范化 URL",
  async (view) => {
    fixtures();
    show(`/knowledge/kb?view=${view}&page=old&source=old&themeId=old`);
    expect(await screen.findByRole("radio", { name: "知识 0" })).toBeChecked();
    await waitFor(() =>
      expect(screen.getByLabelText("当前路由")).toHaveTextContent(
        "/knowledge/kb",
      ),
    );
    expect(screen.getByLabelText("当前路由").textContent).not.toContain(
      "view=",
    );
    expect(
      screen.queryByRole("button", { name: /历史|详情|归档/ }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Wiki 已保存")).not.toBeInTheDocument();
  },
);

it("顶部图谱保留直链，读取完整分页关系并直接展示正文", async () => {
  // jsdom 不执行布局；图谱外部资源须等实际容器尺寸可用后创建。
  const width = vi
    .spyOn(HTMLElement.prototype, "clientWidth", "get")
    .mockReturnValue(1000);
  const height = vi
    .spyOn(HTMLElement.prototype, "clientHeight", "get")
    .mockReturnValue(600);
  onTestFinished(() => {
    width.mockRestore();
    height.mockRestore();
  });
  fixtures();
  const offsets: string[] = [];
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/pages",
      ({ request }) => {
        const offset = new URL(request.url).searchParams.get("offset") ?? "0";
        offsets.push(offset);
        return HttpResponse.json({
          items:
            offset === "0"
              ? [
                  {
                    id: "first",
                    slug: "concept/first",
                    title: "起点知识",
                    pageType: "concept",
                    outLinks: ["concept/last"],
                  },
                ]
              : [
                  {
                    id: "last",
                    slug: "concept/last",
                    title: "末页知识",
                    pageType: "entity",
                    outLinks: [],
                  },
                ],
          total: 2,
        });
      },
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/themes", () =>
      HttpResponse.json({ items: [] }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/last", () =>
      HttpResponse.json({
        id: "last",
        slug: "concept/last",
        title: "末页知识",
        content: "真实 Wiki 正文",
      }),
    ),
  );
  const user = userEvent.setup();
  show("/knowledge/kb?view=graph");
  const canvas = await screen.findByRole(
    "group",
    { name: "知识关系图" },
    { timeout: 5000 },
  );
  expect(canvas.closest('[data-slot="resource-page-surface"]')).not.toBeNull();
  expect(
    screen.queryByRole("complementary", { name: "Wiki 目录" }),
  ).not.toBeInTheDocument();
  expect(
    document.querySelector('[data-slot="detail-dock-overlay"]'),
  ).toBeNull();
  expect(screen.getByRole("button", { name: "文档" })).toBeVisible();
  expect(offsets).toEqual(["0", "1"]);
  expect(canvas.querySelector("canvas")).toBeInTheDocument();
  expect(graphEngine.props?.graphData?.links).toHaveLength(1);
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("view=graph");
  expect(screen.getByRole("button", { name: "上传资料" })).toBeVisible();
  await user.click(within(canvas).getByRole("button", { name: "末页知识" }));
  expect(await screen.findByText("真实 Wiki 正文")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "打开 Wiki 页面" }),
  ).not.toBeInTheDocument();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("view=graph");
});

it("上传后只显示待整理，点击整理才接纳一次冻结来源任务", async () => {
  fixtures();
  let uploaded = false;
  const organized: Record<string, unknown>[] = [];
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({
        items: uploaded
          ? [
              firstSource,
              { ...firstSource, id: "source-2", filename: "新资料.md" },
            ]
          : [firstSource],
        total: uploaded ? 2 : 1,
      }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      async ({ request }) => {
        const body = await request.json();
        expect(body).toMatchObject({ filename: "新资料.md", bytes: "YWI=" });
        uploaded = true;
        return HttpResponse.json({
          resourceId: "source-2",
          state: "committed",
        });
      },
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        organized.push(body);
        return HttpResponse.json({
          receipt: acceptedReceipt(body),
        });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  const upload = await screen.findByRole("button", { name: "上传资料" });
  await waitFor(() => expect(upload).toBeEnabled());
  await user.click(upload);
  const file = new File(["ab"], "新资料.md", { type: "text/markdown" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new Uint8Array([97, 98]).buffer,
  });
  fireEvent.change(await screen.findByLabelText<HTMLInputElement>("选择文件"), {
    target: { files: [file] },
  });
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(
    await within(screen.getByRole("dialog")).findByText("待整理"),
  ).toBeVisible();
  expect(organized).toHaveLength(0);
  await user.click(screen.getByText("关闭", { selector: "button" }));
  const organize = screen.getByRole("button", { name: "整理文档" });
  await waitFor(() => expect(organize).toBeEnabled());
  await user.click(organize);
  await vi.dynamicImportSettled();
  const start = await screen.findByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.dblClick(start);
  await waitFor(() => expect(organized).toHaveLength(1));
  expect(organized[0]).toMatchObject({
    sourceVersionIds: ["source-1", "source-2"],
    runtimeId: "codex",
    model: "model",
  });
});

it("默认整理全库，不受列表翻页与表格选中范围限制", async () => {
  fixtures();
  const organized: unknown[] = [];
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) =>
        HttpResponse.json({
          items:
            new URL(request.url).searchParams.get("offset") === "50"
              ? [{ ...firstSource, id: "source-51", filename: "下一页.md" }]
              : [
                  firstSource,
                  ...Array.from({ length: 49 }, (_, index) => ({
                    ...firstSource,
                    id: `processed-${index}`,
                    filename: `已整理-${index}.md`,
                    state: "processed",
                  })),
                ],
          total: 51,
        }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        organized.push(await request.json());
        return HttpResponse.json({ receipt: { jobId: "job" } });
      },
    ),
  );
  const user = userEvent.setup();
  show("/knowledge/kb?view=raw");
  await screen.findByRole("treeitem", { name: "原始资料.md" });
  fireEvent.scroll(screen.getByRole("tree", { name: "原件列表" }));
  await screen.findByText("下一页.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  const start = await screen.findByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() => expect(organized).toHaveLength(1));
  expect(organized[0]).toMatchObject({
    sourceVersionIds: ["source-1", "source-51"],
  });
});

it.each(["network", "partial500"])(
  "新建%s未知结果关闭重开后沿用请求身份，且不会自动上传或整理",
  async (failure) => {
    fixtures();
    const requests: Record<string, unknown>[] = [];
    server.use(
      http.get("http://ally.test/api/v1/knowledge/connections/libraries", () =>
        HttpResponse.json({ items: [], total: 0 }),
      ),
      http.post(
        "http://ally.test/api/v1/knowledge/libraries",
        async ({ request }) => {
          requests.push((await request.json()) as Record<string, unknown>);
          return requests.length === 1
            ? failure === "network"
              ? HttpResponse.error()
              : HttpResponse.json(
                  {
                    message: "默认配置保存待补齐",
                    code: "KnowledgeDefaultsPersistenceFailed",
                  },
                  { status: 500 },
                )
            : HttpResponse.json({ resourceId: "kb", libraryId: "kb" });
        },
      ),
    );
    const user = userEvent.setup();
    show("/knowledge");
    await user.click(screen.getByRole("button", { name: "新建知识库" }));
    await user.type(
      await screen.findByRole("textbox", { name: "名称" }),
      "通用资料库",
    );
    await user.click(screen.getByRole("button", { name: "创建知识库" }));
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled(),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));
    await user.click(screen.getByRole("button", { name: "新建知识库" }));
    expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "创建知识库" }));
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[1]).toEqual(requests[0]);
    expect(requests[0]).not.toHaveProperty("kind");
    expect(requests[0]).toMatchObject({
      executionDefaults: { runtimeId: "codex", model: "model" },
    });
    expect(requests[1]).toMatchObject({ name: "通用资料库" });
    expect(await screen.findByRole("radio", { name: "知识 0" })).toBeChecked();
  },
);

it("后台运行允许上传和提交其他文件，不请求任务详情", async () => {
  fixtures();
  let detailReads = 0;
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.json({
        items: [{ id: "job", libraryId: "kb", state: "processing" }],
        total: 1,
        activeJob: { id: "job", libraryId: "kb", state: "processing" },
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs/job", () => {
      detailReads++;
      return HttpResponse.json({
        job: { id: "job", libraryId: "kb", state: "processing" },
      });
    }),
  );
  show();
  await screen.findByRole("button", { name: "设置" });
  await vi.dynamicImportSettled();
  await screen.findByText("原始资料.md", {}, { timeout: 4000 });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled(),
  );
  expect(screen.queryByText("正在整理")).not.toBeInTheDocument();
  expect(detailReads).toBe(0);
});

it("详情工具栏不再提供返回列表的页头按钮", async () => {
  fixtures();
  show();
  await screen.findByRole("button", { name: "设置" });
  expect(
    screen.queryByRole("button", { name: "返回列表" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "通用资料库" }),
  ).not.toBeInTheDocument();
});

it("整理默认选中待处理和失败文件，不混入已整理原件", async () => {
  fixtures();
  const requests: Array<{ sourceVersionIds: string[] }> = [];
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({
        items: [
          firstSource,
          {
            ...firstSource,
            id: "failed",
            filename: "失败原件.md",
            state: "failed",
          },
          {
            ...firstSource,
            id: "processed",
            filename: "已整理原件.md",
            state: "processed",
          },
        ],
        total: 3,
      }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        requests.push((await request.json()) as { sourceVersionIds: string[] });
        return HttpResponse.json({
          receipt: { jobId: "job", state: "accepted" },
        });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("失败原件.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  expect(await screen.findByText("已选 2 个文件")).toBeVisible();
  const start = screen.getByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]?.sourceVersionIds).toEqual(["source-1", "failed"]);
});

it.each(["failed", "cancelled"] as const)(
  "没有pending时默认整理%s原件，终态旧任务不占槽且提交新任务身份",
  async (state) => {
    fixtures();
    const requests: Array<{ requestId: string; sourceVersionIds: string[] }> =
      [];
    server.use(
      http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
        HttpResponse.json({
          items: [
            { ...firstSource, id: "retry-source", state },
            {
              ...firstSource,
              id: "processed",
              filename: "已整理原件.md",
              state: "processed",
            },
          ],
          total: 2,
        }),
      ),
      http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
        HttpResponse.json({
          items: [
            {
              id: "old-job",
              libraryId: "kb",
              requestId: "old-request",
              state,
              attempt: 2,
            },
          ],
          total: 1,
        }),
      ),
      http.post(
        "http://ally.test/api/v1/knowledge/libraries/kb/organize",
        async ({ request }) => {
          requests.push(
            (await request.json()) as {
              requestId: string;
              sourceVersionIds: string[];
            },
          );
          return HttpResponse.json({
            receipt: { jobId: "job", state: "accepted" },
          });
        },
      ),
    );
    const user = userEvent.setup();
    show();
    await screen.findByText("已整理原件.md");
    const organize = screen.getByRole("button", { name: "整理文档" });
    await waitFor(() => expect(organize).toBeEnabled());
    await user.click(organize);
    expect(await screen.findByText("已选 1 个文件")).toBeVisible();
    const start = screen.getByRole("button", { name: "开始整理" });
    await waitFor(() => expect(start).toBeEnabled());
    await user.click(start);
    await waitFor(() => expect(requests).toHaveLength(1));
    expect(requests[0]?.sourceVersionIds).toEqual(["retry-source"]);
    expect(requests[0]?.requestId).toBeTruthy();
    expect(requests[0]?.requestId).not.toBe("old-request");
  },
);

it("真实运行中的任务不阻止整理其他失败来源", async () => {
  fixtures();
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({
        items: [{ ...firstSource, state: "failed" }],
        total: 1,
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.json({
        items: [{ id: "active-job", libraryId: "kb", state: "processing" }],
        activeJob: { id: "active-job", libraryId: "kb", state: "processing" },
        total: 1,
      }),
    ),
  );
  show();
  await screen.findByText("原始资料.md");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled(),
  );
});

it("整理可用性读取失败只走通知并保持操作禁用", async () => {
  fixtures();
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.error(),
    ),
  );
  show();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "整理文档" })).toBeDisabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("查看整理对话")).not.toBeInTheDocument();
});

it("确认整理取得真实任务回执后关闭弹窗，尚无Execution时也不显示失败", async () => {
  fixtures();
  const user = userEvent.setup();
  let finish: (() => void) | undefined;
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
        return HttpResponse.json({
          receipt: {
            requestId: body.requestId,
            libraryId: "kb",
            resourceId: "job",
            jobId: "job",
            version: 1,
            state: "accepted",
          },
          noop: false,
          session: { chatId: "" },
        });
      },
    ),
  );
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("dialog")).toBeVisible();
  expect(screen.queryByText("正在连接整理对话…")).not.toBeInTheDocument();
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]).toMatchObject({
    message: "整理指定资料，连续完成全部阶段。",
  });
  finish?.();
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
  expect(requests).toHaveLength(1);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
it("离开详情后迟到的接纳不会在列表页弹出聊天", async () => {
  fixtures();
  const user = userEvent.setup();
  let finish: (() => void) | undefined;
  server.use(
    http.get("http://ally.test/api/v1/knowledge/connections/libraries", () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
        return HttpResponse.json({ receipt: acceptedReceipt(body) });
      },
    ),
  );
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("link", { name: "测试切换到列表" }));
  finish?.();
  await waitFor(() =>
    expect(screen.getByLabelText("当前路由").textContent).toBe("/knowledge"),
  );
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
});

it("后台接纳只使用知识任务回执且不挂载聊天", async () => {
  fixtures();
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) =>
        HttpResponse.json({
          receipt: acceptedReceipt(
            (await request.json()) as Record<string, unknown>,
          ),
        }),
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  const start = await screen.findByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
});

it("关闭重开上传弹窗后用原请求身份核对未知回执", async () => {
  fixtures();
  const requests: Array<{ requestId?: string }> = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      async ({ request }) => {
        requests.push((await request.json()) as { requestId?: string });
        return requests.length === 1
          ? HttpResponse.error()
          : HttpResponse.json({ resourceId: "source-2", state: "saved" });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "上传资料" }));
  const file = new File(["ab"], "恢复资料.md", { type: "text/markdown" });
  Object.defineProperty(file, "arrayBuffer", {
    value: async () => new Uint8Array([97, 98]).buffer,
  });
  fireEvent.change(await screen.findByLabelText<HTMLInputElement>("选择文件"), {
    target: { files: [file] },
  });
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(await screen.findByText("上传失败")).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByText("关闭", { selector: "button" }));
  await user.click(screen.getByRole("button", { name: "上传资料" }));
  expect(screen.getByText("恢复资料.md")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "重试失败文档" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]?.requestId).toBe(requests[0]?.requestId);
  expect(
    await within(screen.getByRole("dialog")).findByText("待整理"),
  ).toBeVisible();
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

it("整理从库配置初始化，临时修改直接执行且不写回库配置", async () => {
  fixtures();
  const user = userEvent.setup();
  const profileRequests: Record<string, unknown>[] = [];
  const organized: Record<string, unknown>[] = [];
  const profile = {
    libraryId: "kb",
    version: 2,
    wakerId: "configured-waker",
    runtimeId: "codex",
    accountId: "system",
    model: "saved-model",
    reasoning: "",
  };
  server.use(
    http.get("http://ally.test/api/v1/runtimes", () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            models: [
              { id: "model", name: "Model", permissionLevels: ["default"] },
              {
                id: "saved-model",
                name: "已存模型",
                permissionLevels: ["default"],
              },
            ],
            defaultModel: "model",
          },
        ],
      }),
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      () => HttpResponse.json(profile),
    ),
    http.put(
      "http://ally.test/api/v1/knowledge/libraries/kb/execution-defaults",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        profileRequests.push(body);
        return HttpResponse.json(profile);
      },
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        organized.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ noop: true });
      },
    ),
  );
  show();
  await screen.findByText("原始资料.md", {}, { timeout: 10000 });
  await user.click(await screen.findByRole("button", { name: "整理文档" }));
  const model = await screen.findByRole(
    "combobox",
    { name: "模型" },
    { timeout: 10000 },
  );
  expect(model).toHaveTextContent("已存模型");
  fireEvent.keyDown(model, { key: "ArrowDown" });
  await user.click(await screen.findByRole("option", { name: "Model" }));
  await user.click(screen.getByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(organized).toHaveLength(1));
  expect(profileRequests).toHaveLength(0);
  expect(organized[0]).toMatchObject({
    sourceVersionIds: ["source-1"],
    model: "model",
  });
  expect(organized[0]).not.toHaveProperty("wakerId");
  expect(organized[0]).not.toHaveProperty("permissionLevel");
});

it("后台接纳200缺完整任务回执时关闭重开保留原请求，不重复用户输入", async () => {
  fixtures();
  const user = userEvent.setup();
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        return HttpResponse.json(
          requests.length === 1
            ? { receipt: { jobId: "job" } }
            : {
                receipt: acceptedReceipt(body),
              },
        );
      },
    ),
  );
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.type(
    await screen.findByRole("textbox", { name: "整理提取要求" }),
    "保留在线和离线适用条件",
  );
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "开始整理" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  const requirements = await screen.findByRole("textbox", {
    name: "整理提取要求",
  });
  expect(requirements).toHaveValue("保留在线和离线适用条件");
  expect(requirements).toBeDisabled();
  expect(screen.getByRole("button", { name: "已选 1 个文件" })).toBeDisabled();
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
});

it("文档初次加载显示内容骨架，后台刷新仍保留已有文档与筛选", async () => {
  fixtures();
  let release!: () => void;
  let pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      async () => {
        await pending;
        return HttpResponse.json({ items: [firstSource], total: 1 });
      },
    ),
  );
  const queryClient = show();
  try {
    const loading = await screen.findByRole("status", {
      name: "正在读取内容…",
    });
    expect(loading).toHaveAttribute("aria-busy", "true");
    expect(loading.querySelector("[aria-hidden=true]")).toBeInTheDocument();
    expect(screen.getByRole("searchbox", { name: "搜索原件…" })).toBeEnabled();
    expect(screen.queryByText("还没有原始资料")).not.toBeInTheDocument();
    release();
    await screen.findByText("原始资料.md");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled(),
    );
    pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    void queryClient.invalidateQueries({
      predicate: (query) => query.queryKey.includes("sources"),
    });
    await waitFor(() => expect(queryClient.isFetching()).toBeGreaterThan(0));
    expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled();
    expect(screen.getByText("原始资料.md")).toBeVisible();
    expect(
      screen.queryByRole("status", { name: "正在读取内容…" }),
    ).not.toBeInTheDocument();
  } finally {
    release();
  }
});

it("后台接纳中关闭重开配置复用同一个待定请求，不取消任务或重复发送", async () => {
  fixtures();
  const user = userEvent.setup();
  let finish: (() => void) | undefined;
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
        return HttpResponse.json({
          receipt: acceptedReceipt(body),
        });
      },
    ),
  );
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  expect(requests).toHaveLength(1);
  finish?.();
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(requests).toHaveLength(1);
  expect(
    screen.queryByRole("region", { name: "悬浮对话" }),
  ).not.toBeInTheDocument();
});

it("已有任务排队时仍可上传并仅提交其他待处理文件", async () => {
  fixtures();
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/jobs", () =>
      HttpResponse.json({
        items: [{ id: "newer-completed", libraryId: "kb", state: "completed" }],
        total: 2,
        activeJob: {
          id: "old-retry",
          libraryId: "kb",
          state: "queued",
          attempt: 2,
        },
      }),
    ),
  );
  const requests: Record<string, unknown>[] = [];
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({
        items: [
          firstSource,
          {
            ...firstSource,
            id: "failed-but-reserved",
            filename: "失败但本轮未结束.md",
            state: "failed",
            organizeBlocked: true,
          },
          {
            ...firstSource,
            id: "queued-source",
            filename: "排队中.md",
            state: "queued",
          },
          {
            ...firstSource,
            id: "processing-source",
            filename: "整理中.md",
            state: "processing",
          },
        ],
        total: 4,
      }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        return HttpResponse.json({ receipt: acceptedReceipt(body) });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByRole("button", { name: "设置" });
  await vi.dynamicImportSettled();
  await screen.findByText("原始资料.md", {}, { timeout: 4000 });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled(),
  );
  expect(screen.getByRole("button", { name: "上传资料" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  const dialog = await screen.findByRole("dialog");
  await vi.dynamicImportSettled();
  await waitFor(() => expect(dialog).toHaveTextContent("已选 1 个文件"));
  await user.click(screen.getByRole("button", { name: "开始整理" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]?.sourceVersionIds).toEqual([firstSource.id]);
});

it("整理全库待处理文件，可搜索取消选择并提交仅本次的提取要求", async () => {
  fixtures();
  const requests: Record<string, unknown>[] = [];
  const firstPage = Array.from({ length: 50 }, (_, index) => ({
    ...firstSource,
    id: `source-${index}`,
    filename: `资料-${index}.md`,
  }));
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) =>
        HttpResponse.json({
          items:
            new URL(request.url).searchParams.get("offset") === "50"
              ? [
                  {
                    ...firstSource,
                    id: "failed-next-page",
                    filename: "失败资料.md",
                    state: "failed",
                  },
                ]
              : firstPage,
          total: 51,
        }),
    ),
    http.post(
      "http://ally.test/api/v1/knowledge/libraries/kb/organize",
      async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        requests.push(body);
        return HttpResponse.json({
          receipt: acceptedReceipt(body),
        });
      },
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("资料-0.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(
    await screen.findByRole("button", { name: "已选 51 个文件" }),
  );
  await user.type(
    screen.getByRole("searchbox", { name: "搜索文件…" }),
    "失败资料",
  );
  const failed = screen.getByRole("checkbox", { name: "失败资料.md" });
  expect(failed).toBeChecked();
  await user.click(failed);
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "已选 50 个文件" })).toBeVisible();
  await user.type(
    screen.getByRole("textbox", { name: "整理提取要求" }),
    "重点提取部署步骤和限制条件",
  );
  expect(
    screen.queryByText("账号决定可用模型；模型决定推理选项和支持的执行权限。"),
  ).not.toBeInTheDocument();
  const start = screen.getByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(requests[0]?.sourceVersionIds).toEqual(
    firstPage.map((source) => source.id),
  );
  expect(requests[0]?.message).toContain("重点提取部署步骤和限制条件");
});

it("上传资料从点击到表单就绪保持同一个弹窗", async () => {
  fixtures();
  show();
  await screen.findByText("原始资料.md");
  fireEvent.click(screen.getByRole("button", { name: "上传资料" }));
  const shell = screen.getByRole("dialog", { name: "上传资料" });
  await screen.findByRole("button", { name: "选择文件" });
  expect(screen.getByRole("dialog", { name: "上传资料" })).toBe(shell);
});

it.each(["unknown", "separate"])(
  "远端模式为 %s 时不再展示检测提示条与重新检测按钮",
  async (mode) => {
    const remote =
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
    server.use(
      http.get(`${remote}/source-purges`, () =>
        HttpResponse.json({ items: [], total: 0 }),
      ),
      http.get(remote, () =>
        HttpResponse.json({
          ...library,
          ref: {
            backend: "integration",
            connectionId: "remote",
            knowledgeBaseId: "kb",
          },
        }),
      ),
      http.get(`${remote}/sources`, () =>
        HttpResponse.json({ items: [firstSource], total: 1 }),
      ),
      http.get(`${remote}/jobs`, () =>
        HttpResponse.json({ items: [], total: 0 }),
      ),
      http.get(`${remote}/capabilities`, () =>
        HttpResponse.json({
          serviceKind: mode === "unknown" ? "unknown" : "neuxally",
          uploadMode: mode,
          connectionRevision: 2,
          operations: { read: true, rawOnly: mode === "separate" },
          details: {
            read: {
              supported: true,
              permission: "allowed",
              condition: "ready",
            },
            rawOnly: {
              supported: mode === "separate",
              permission: mode === "separate" ? "allowed" : "unknown",
              condition: "ready",
            },
          },
        }),
      ),
    );
    show("/knowledge/kb?view=raw&backend=integration&connection=remote");
    await screen.findByText("原始资料.md", {}, { timeout: 5000 });
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "上传资料" })
          .hasAttribute("disabled"),
      ).toBe(mode === "unknown"),
    );
    expect(
      screen.queryByRole("button", { name: "重新检测" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("上传仅保存资料，整理需单独启动。"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("服务类型或处理模式尚未确认，当前仅可读取。"),
    ).not.toBeInTheDocument();
  },
);

it("清空整理文件后不可提交，重新勾选才能开始", async () => {
  fixtures();
  const user = userEvent.setup();
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(
    await screen.findByRole("button", { name: "已选 1 个文件" }),
  );
  await user.click(screen.getByRole("button", { name: "清空选择" }));
  await user.keyboard("{Escape}");
  expect(screen.getByRole("button", { name: "开始整理" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "已选 0 个文件" }));
  await user.click(screen.getByRole("checkbox", { name: "原始资料.md" }));
  await user.keyboard("{Escape}");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "开始整理" })).toBeEnabled(),
  );
});

it("当前页全部已整理时仍可选择其他页待整理文件", async () => {
  fixtures();
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) =>
        HttpResponse.json({
          items:
            new URL(request.url).searchParams.get("offset") === "50"
              ? [
                  {
                    ...firstSource,
                    id: "next-pending",
                    filename: "待整理资料.md",
                  },
                ]
              : Array.from({ length: 50 }, (_, index) => ({
                  ...firstSource,
                  id: `processed-${index}`,
                  filename: `已整理-${index}.md`,
                  state: "processed",
                })),
          total: 51,
        }),
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("已整理-0.md");
  const organize = screen.getByRole("button", { name: "整理文档" });
  await waitFor(() => expect(organize).toBeEnabled());
  await user.click(organize);
  await user.click(
    await screen.findByRole("button", { name: "已选 1 个文件" }),
  );
  expect(screen.getByRole("checkbox", { name: "待整理资料.md" })).toBeChecked();
});

it("普通取消后重新打开会读取新增文件，不复用旧来源草稿", async () => {
  fixtures();
  let items = [firstSource];
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/sources", () =>
      HttpResponse.json({ items, total: items.length }),
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await screen.findByRole("button", { name: "已选 1 个文件" });
  await user.click(screen.getByRole("button", { name: "取消" }));
  items = [
    firstSource,
    { ...firstSource, id: "new-upload", filename: "新增资料.md" },
  ];
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(
    await screen.findByRole("button", { name: "已选 2 个文件" }),
  );
  expect(screen.getByRole("checkbox", { name: "新增资料.md" })).toBeChecked();
});

it("取消后重新挂载页面仍恢复本库的模型和整理要求，清空要求也会记住", async () => {
  fixtures();
  server.use(
    http.get("http://ally.test/api/v1/runtimes", () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            models: [
              { id: "model", name: "Model", permissionLevels: ["default"] },
              { id: "other", name: "Other", permissionLevels: ["default"] },
            ],
            defaultModel: "model",
          },
        ],
      }),
    ),
  );
  const user = userEvent.setup();
  show();
  await screen.findByText("原始资料.md", {}, { timeout: 10000 });
  await user.click(await screen.findByRole("button", { name: "整理文档" }));
  fireEvent.keyDown(
    await screen.findByRole("combobox", { name: "模型" }, { timeout: 10000 }),
    { key: "ArrowDown" },
  );
  await user.click(await screen.findByRole("option", { name: "Other" }));
  await user.type(
    screen.getByRole("textbox", { name: "整理提取要求" }),
    "保留部署限制",
  );
  await user.click(screen.getByRole("button", { name: "取消" }));
  cleanup();
  show();
  await screen.findByText("原始资料.md", {}, { timeout: 10000 });
  await user.click(await screen.findByRole("button", { name: "整理文档" }));
  expect(
    await screen.findByRole("combobox", { name: "模型" }, { timeout: 10000 }),
  ).toHaveTextContent("Other");
  expect(screen.getByRole("textbox", { name: "整理提取要求" })).toHaveValue(
    "保留部署限制",
  );
  await user.clear(screen.getByRole("textbox", { name: "整理提取要求" }));
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  expect(
    await screen.findByRole("textbox", { name: "整理提取要求" }),
  ).toHaveValue("");
});

it("原件向下滚动追加真实分页，不再展示分页控件", async () => {
  fixtures();
  const offsets: number[] = [];
  const all = Array.from({ length: 21 }, (_, index) => ({
    ...firstSource,
    id: `page-source-${index + 1}`,
    filename: `分页资料-${index + 1}.md`,
  }));
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) => {
        const params = new URL(request.url).searchParams;
        const offset = Number(params.get("offset"));
        offsets.push(offset);
        return HttpResponse.json({
          items: all.slice(offset, offset + Number(params.get("limit"))),
          total: all.length,
        });
      },
    ),
  );
  show();
  await screen.findByText("分页资料-1.md");
  expect(screen.getByText("分页资料-10.md")).toBeVisible();
  expect(screen.queryByText("分页资料-11.md")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "下一页" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "每页数量" }),
  ).not.toBeInTheDocument();
  fireEvent.scroll(screen.getByRole("tree", { name: "原件列表" }));
  await screen.findByText("分页资料-11.md");
  expect(screen.getByText("分页资料-1.md")).toBeVisible();
  fireEvent.scroll(screen.getByRole("tree", { name: "原件列表" }));
  await screen.findByText("分页资料-21.md");
  expect(screen.getByText("分页资料-1.md")).toBeVisible();
  expect(offsets).toEqual([0, 10, 20]);
});

it("侧栏原件搜索使用后端匹配总数并重置累计目录", async () => {
  fixtures();
  const all = Array.from({ length: 44 }, (_, index) => ({
    ...firstSource,
    id: `filtered-source-${index + 1}`,
    filename: `${index < 10 ? "完成" : "排队"}-${index + 1}.md`,
    state: index < 10 ? "processed" : "queued",
    createdAt: index + 1,
  }));
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) => {
        const params = new URL(request.url).searchParams;
        const offset = Number(params.get("offset"));
        const limit = Number(params.get("limit"));
        const state = params.get("state") || "all";
        const query = params.get("query") || "";
        const filtered = all.filter(
          (source) =>
            (state === "all" || source.state === state) &&
            source.filename.includes(query),
        );
        if (params.get("sort") !== "oldest") filtered.reverse();
        return HttpResponse.json({
          items: filtered.slice(offset, offset + limit),
          total: filtered.length,
        });
      },
    ),
  );
  const user = userEvent.setup();
  show("/knowledge/kb?view=raw&sourceQuery=排队");
  await vi.dynamicImportSettled();
  await screen.findByText("排队-44.md", {}, { timeout: 5000 });
  expect(screen.getByRole("radio", { name: "原件 34" })).toBeVisible();
  expect(screen.queryByText("排队-21.md")).not.toBeInTheDocument();
  await user.clear(screen.getByRole("searchbox", { name: "搜索原件…" }));
  await user.type(
    screen.getByRole("searchbox", { name: "搜索原件…" }),
    "排队-1",
  );
  await screen.findByRole("radio", { name: "原件 9" });
  expect(screen.getByText("排队-19.md")).toBeVisible();
  expect(screen.queryByText("排队-44.md")).not.toBeInTheDocument();
  await user.clear(screen.getByRole("searchbox", { name: "搜索原件…" }));
  await user.type(
    screen.getByRole("searchbox", { name: "搜索原件…" }),
    "无匹配",
  );
  await screen.findByRole("radio", { name: "原件 0" });
  expect(screen.queryByText("排队-19.md")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "资料状态" }),
  ).not.toBeInTheDocument();
});

it("状态更新减少原件总数时重新核对累计目录", async () => {
  fixtures();
  let all = Array.from({ length: 21 }, (_, index) => ({
    ...firstSource,
    id: `shrinking-${index + 1}`,
    filename: `进度资料-${index + 1}.md`,
  }));
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/sources",
      ({ request }) => {
        const query = new URL(request.url).searchParams;
        const offset = Number(query.get("offset"));
        const limit = Number(query.get("limit"));
        return HttpResponse.json({
          items: all.slice(offset, offset + limit),
          total: all.length,
        });
      },
    ),
  );
  const client = show("/knowledge/kb?view=raw");
  await vi.dynamicImportSettled();
  await screen.findByText("进度资料-1.md", {}, { timeout: 5000 });
  fireEvent.scroll(screen.getByRole("tree", { name: "原件列表" }));
  await screen.findByText("进度资料-11.md");
  fireEvent.scroll(screen.getByRole("tree", { name: "原件列表" }));
  await screen.findByText("进度资料-21.md");
  all = all.slice(0, 5);
  await client.invalidateQueries({ queryKey: ["knowledge"] });
  await screen.findByText("进度资料-1.md");
  expect(screen.getByRole("radio", { name: "原件 5" })).toBeVisible();
  expect(screen.queryByText("进度资料-21.md")).not.toBeInTheDocument();
});

it.each([
  "capability-denied",
  "capability-error",
  "jobs-error",
  "sources-error",
])("远端整理未知接纳后重开不被 %s 阻断原请求核对", async (failure) => {
  const user = userEvent.setup();
  const root =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  let denied = false;
  const bodies: Record<string, unknown>[] = [];
  server.use(
    http.get(root, () =>
      HttpResponse.json({
        ...library,
        ref: {
          backend: "custom",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        },
      }),
    ),
    http.get(`${root}/sources`, () =>
      denied && failure === "sources-error"
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({ items: [firstSource], total: 1 }),
    ),
    http.get(`${root}/source-purges`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(`${root}/jobs`, () =>
      denied && failure === "jobs-error"
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({
            items: [],
            total: 0,
          }),
    ),
    http.get(`${root}/capabilities`, () =>
      denied && failure === "capability-error"
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({
            operations: {
              organize: !(denied && failure === "capability-denied"),
            },
            profiles: [{ id: "original-profile", name: "原配置" }],
            details: {
              organize: {
                supported: true,
                permission:
                  denied && failure === "capability-denied"
                    ? "denied"
                    : "allowed",
                condition: "ready",
              },
            },
          }),
    ),
    http.get(`${root}/operations/:requestId`, ({ params }) =>
      HttpResponse.json({
        requestId: params.requestId,
        operation: "job.accept",
        ref: {
          backend: "custom",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        },
      }),
    ),
    http.post(`${root}/organize`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      bodies.push(body);
      if (bodies.length === 1) return new HttpResponse(null, { status: 503 });
      return HttpResponse.json({
        requestId: body.requestId,
        jobId: "job",
        state: "accepted",
      });
    }),
  );
  const queryClient = show(
    "/knowledge/kb?view=raw&backend=custom&connection=remote",
  );
  await screen.findByText("原始资料.md", {}, { timeout: 5000 });
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  const start = await screen.findByRole("button", { name: "开始整理" });
  await waitFor(() => expect(start).toBeEnabled());
  await user.click(start);
  await waitFor(() => expect(start).toBeEnabled());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  denied = true;
  await user.click(screen.getByRole("button", { name: "取消" }));
  await queryClient.invalidateQueries({
    queryKey: ["knowledge", "custom", "remote", "kb"],
  });
  expect(screen.getByRole("button", { name: "整理文档" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "整理文档" }));
  await user.click(await screen.findByRole("button", { name: "开始整理" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(bodies).toHaveLength(2);
  expect(bodies[1]).toEqual(bodies[0]);
});

it("目录搜索从当前分页归零，URL 与请求共享筛选并展示服务端结果", async () => {
  const requests: { search: string; offset: number }[] = [];
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/connections/libraries",
      ({ request }) => {
        const params = new URL(request.url).searchParams;
        const search = params.get("search") ?? "";
        const offset = Number(params.get("offset"));
        requests.push({ search, offset });
        return HttpResponse.json(
          search
            ? {
                items: [
                  {
                    ...library,
                    id: "beyond-page",
                    name: "第 61 个知识库",
                    purpose: "匹配中文描述",
                  },
                ],
                total: 1,
              }
            : { items: [{ ...library, name: "当前分页知识库" }], total: 61 },
        );
      },
    ),
  );
  const user = userEvent.setup();
  const client = show("/knowledge?librariesOffset=50&location=remote");
  await screen.findByRole("heading", { name: "当前分页知识库" });
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索知识库…" }), {
    target: { value: "中文描述" },
  });
  await screen.findByRole("heading", { name: "第 61 个知识库" });
  const url = new URL(
    screen.getByLabelText("当前路由").textContent ?? "",
    "http://ally.test",
  );
  expect(url.searchParams.get("search")).toBe("中文描述");
  expect(url.searchParams.get("librariesOffset")).toBe("0");
  expect(url.searchParams.get("location")).toBe("remote");
  expect(requests).toContainEqual({ search: "中文描述", offset: 0 });
  expect(screen.getByText("1 个知识库")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "下一页" }),
  ).not.toBeInTheDocument();
  expect(
    client.getQueriesData({ queryKey: ["knowledge", "libraries"] }),
  ).toHaveLength(2);
  await user.clear(screen.getByRole("searchbox", { name: "搜索知识库…" }));
  await screen.findByRole("heading", { name: "当前分页知识库" });
  expect(requests).toContainEqual({ search: "", offset: 0 });
});

it("目录搜索直链保留筛选并从服务器分页读取", async () => {
  const requests: URLSearchParams[] = [];
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/connections/libraries",
      ({ request }) => {
        requests.push(new URL(request.url).searchParams);
        return HttpResponse.json({
          items: [{ ...library, name: "直链搜索结果" }],
          total: 65,
        });
      },
    ),
  );
  show("/knowledge?search=中文&librariesOffset=50");
  await screen.findByRole("heading", { name: "直链搜索结果" });
  expect(screen.getByRole("searchbox", { name: "搜索知识库…" })).toHaveValue(
    "中文",
  );
  expect(requests[0]?.get("search")).toBe("中文");
  expect(requests[0]?.get("offset")).toBe("50");
  expect(screen.getByText("65 个知识库")).toBeVisible();
});
