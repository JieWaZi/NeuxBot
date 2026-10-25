import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  configure,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { ThemeProvider } from "@/shared/theme";
import { client } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { KnowledgePage } from "./knowledge-page";

vi.mock("@/shared/ui/text-diff-view", () => ({
  TextDiffView: ({
    beforeContent,
    afterContent,
  }: {
    beforeContent: string;
    afterContent: string;
  }) => (
    <div>
      <pre>{beforeContent}</pre>
      <pre>{afterContent}</pre>
    </div>
  ),
}));
vi.mock("@/shared/api/use-query-events", () => ({ useQueryEvents: vi.fn() }));
const server = setupServer();
const root = "http://ally.test/api/v1/knowledge/libraries/kb";
const pages = [
  { id: "index", slug: "index", title: "索引", pageType: "index", version: 1 },
  {
    id: "one",
    slug: "concept/one",
    title: "第一页知识",
    themeId: "theme",
    pageType: "concept",
    version: 3,
    updatedAt: 1792479600000,
  },
];
beforeAll(() => {
  configure({ asyncUtilTimeout: 5000 });
  window.localStorage.setItem("ally.ui.theme", "light");
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => {
  server.resetHandlers();
  const hook = renderHook(useErrorNotices);
  act(() => {
    for (const notice of hook.result.current) dismissErrorNotice(notice.id);
  });
  hook.unmount();
});
async function expectGlobalReadError() {
  const notices = renderHook(useErrorNotices);
  await waitFor(() => expect(notices.result.current.length).toBeGreaterThan(0));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  notices.unmount();
}
afterAll(() => {
  configure({ asyncUtilTimeout: 1000 });
  server.close();
});
function Location() {
  const location = useLocation();
  return <output aria-label="当前路由">{location.search}</output>;
}
function show(query = "") {
  server.use(
    http.get(`${root}/source-purges`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(root, () =>
      HttpResponse.json({
        id: "kb",
        name: "Wiki 资料库",
        description: "知识库布局说明",
        kind: "general",
        ref: { backend: "local", connectionId: "local", knowledgeBaseId: "kb" },
        structureVersion: 8,
        version: 1,
      }),
    ),
    http.get(`${root}/sources`, () =>
      HttpResponse.json({
        items: [{ id: "source", filename: "原始资料.md", state: "pending" }],
        total: 1,
      }),
    ),
    http.get(`${root}/jobs`, () => HttpResponse.json({ items: [], total: 0 })),
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({ items: pages, total: 2 }),
    ),
    http.get(`${root}/pages`, () =>
      HttpResponse.json({ items: pages, total: 2 }),
    ),
    http.get(`${root}/wiki/index`, () =>
      HttpResponse.json({
        intro: "# Wiki Index\n\n[阅读第一页](concept/one.md)",
        version: 1,
        groups: [
          {
            type: "concept",
            total: 1,
            items: [{ slug: "concept/one", title: "第一页知识" }],
          },
        ],
      }),
    ),
    http.get(`${root}/themes`, () =>
      HttpResponse.json({
        items: [
          { id: "theme", name: "主题目录" },
          { id: "empty", name: "空目录" },
        ],
      }),
    ),
    http.get(`${root}/pages/:pageId`, ({ params }) =>
      HttpResponse.json(
        params.pageId === "index"
          ? {
              ...pages[0],
              content: "# Wiki Index\n\n[阅读第一页](concept/one.md)",
            }
          : { ...pages[1], content: "# 页面正文\n\n真实的知识正文。" },
      ),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider>
          <MemoryRouter initialEntries={[`/knowledge/kb${query}`]}>
            <Location />
            <Routes>
              <Route path="/knowledge/:libraryId" element={<KnowledgePage />} />
            </Routes>
          </MemoryRouter>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}
it("统一目录在原件与知识间切换，保留筛选且不再展示 Dock", async () => {
  const user = userEvent.setup();
  show("?view=raw&sourceQuery=原始");
  expect(await screen.findByText("原始资料.md")).toBeVisible();
  expect(screen.getByRole("heading", { name: "Wiki 资料库" })).toBeVisible();
  expect(
    document.querySelector('[data-slot="detail-dock-overlay"]'),
  ).toBeNull();
  expect(screen.getByRole("button", { name: "图谱" })).toBeVisible();
  await user.click(await screen.findByRole("radio", { name: /^知识/ }));
  expect(
    await screen.findByRole(
      "heading",
      { name: "Wiki Index" },
      { timeout: 5_000 },
    ),
  ).toBeVisible();
  await user.click(await screen.findByRole("link", { name: "阅读第一页" }));
  expect(
    await screen.findByRole("heading", { name: "页面正文" }),
  ).toBeVisible();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("page=one");
  await user.click(screen.getByRole("radio", { name: /^原件/ }));
  expect(await screen.findByText("原始资料.md")).toBeVisible();
  expect(screen.getByRole("searchbox", { name: "搜索原件…" })).toHaveValue(
    "原始",
  );
});
it("原件只在左侧列出图标与截断文件名，点击后右侧展示正文与操作", async () => {
  show("?view=raw");
  server.use(
    http.get(`${root}/sources/source`, () =>
      HttpResponse.json({
        id: "source",
        filename: "原始资料.md",
        description: "源文件摘要",
      }),
    ),
    http.get(`${root}/sources/source/raw`, () =>
      HttpResponse.json({
        filename: "原始资料.md",
        bytes: btoa("# Source content"),
      }),
    ),
  );
  const item = await screen.findByRole("treeitem", { name: "原始资料.md" });
  expect(item.closest("aside")).not.toBeNull();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(within(item).getByText("原始资料.md")).toHaveClass("truncate");
  await userEvent.click(item);
  const article = await screen.findByRole("article", { name: "原始资料.md" });
  expect(
    await within(article).findByRole("heading", { name: "Source content" }),
  ).toBeVisible();
  expect(within(article).getByRole("button", { name: "下载" })).toBeVisible();
  expect(
    within(article).getByRole("button", { name: "重建知识" }),
  ).toBeVisible();
  expect(within(article).getByRole("button", { name: "删除" })).toBeVisible();
});
it.each(["raw", "summaries"] as const)(
  "索引链接等待目录时切换 %s，迟到结果不会夺回选择",
  async (target) => {
    let directoryReads = 0;
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    show("?view=wiki");
    server.use(
      http.get(`${root}/page-directory`, async () => {
        directoryReads++;
        await pending;
        return HttpResponse.json({ items: pages, total: 2 });
      }),
    );
    try {
      await screen.findByRole("heading", { name: "Wiki Index" });
      await waitFor(() => expect(directoryReads).toBe(1));
      await userEvent.click(screen.getByRole("link", { name: "阅读第一页" }));
      await userEvent.click(
        screen.getByRole("radio", {
          name: target === "raw" ? /^原件/ : /^摘要/,
        }),
      );
      await act(async () => {
        release();
        await new Promise((resolve) => setTimeout(resolve, 100));
      });
      if (target === "raw")
        await screen.findByRole("treeitem", { name: "原始资料.md" });
      await waitFor(() =>
        expect(screen.getByRole("radio", { name: "知识 2" })).toBeVisible(),
      );
      expect(screen.getByLabelText("当前路由")).toHaveTextContent(
        target === "raw" ? "view=raw" : "wikiTab=summaries",
      );
      expect(
        screen.getByRole("radio", {
          name: target === "raw" ? /^原件/ : /^摘要/,
        }),
      ).toBeChecked();
      expect(
        screen.queryByRole("heading", { name: "页面正文" }),
      ).not.toBeInTheDocument();
    } finally {
      release();
    }
  },
);
it("摘要 Tab 不显示索引，知识 Tab 将索引列为第一个文件", async () => {
  show("?view=wiki&wikiTab=summaries");
  await screen.findByText("还没有摘要");
  expect(screen.getByRole("radio", { name: /^摘要/ })).toBeChecked();
  expect(
    screen.queryByRole("treeitem", { name: "索引" }),
  ).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("radio", { name: /^知识/ }));
  expect(screen.getAllByRole("treeitem")[0]).toHaveAccessibleName("索引");
  await userEvent.click(screen.getByRole("treeitem", { name: "索引" }));
  expect(screen.getByRole("radio", { name: /^知识/ })).toBeChecked();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent(
    "wikiTab=knowledge",
  );
});
it("Wiki 目录读取全部分页，摘要与空目录均可见", async () => {
  show("?view=wiki");
  server.use(
    http.get(`${root}/page-directory`, ({ request }) => {
      const offset = new URL(request.url).searchParams.get("offset");
      return HttpResponse.json(
        offset === "0"
          ? { items: pages, total: 3 }
          : {
              items: [
                {
                  id: "last",
                  slug: "summary/last",
                  title: "最后一页摘要",
                  pageType: "summary",
                  themeId: "theme",
                },
              ],
              total: 3,
            },
      );
    }),
  );
  expect(
    await screen.findByRole("heading", { name: "Wiki Index" }),
  ).toBeVisible();
  expect(screen.getByRole("treeitem", { name: "空目录" })).toBeVisible();
  await userEvent.click(screen.getByRole("radio", { name: "摘要 1" }));
  expect(
    await screen.findByRole("treeitem", { name: "最后一页摘要" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("treeitem", { name: "主题目录" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("treeitem", { name: "空目录" }),
  ).not.toBeInTheDocument();
});
it("目录读取失败保留壳并全局报告，重新读取后恢复", async () => {
  const queryClient = show("?view=wiki");
  server.use(
    http.get(
      `${root}/page-directory`,
      () => new HttpResponse(null, { status: 500 }),
    ),
  );
  const sidebar = await screen.findByRole("complementary", {
    name: "Wiki 目录",
  });
  await waitFor(() =>
    expect(within(sidebar).queryByRole("status")).not.toBeInTheDocument(),
  );
  await expectGlobalReadError();
  server.use(
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({ items: pages, total: pages.length }),
    ),
  );
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await within(sidebar).findByRole("treeitem", { name: "第一页知识" });
  expect(within(sidebar).queryByRole("alert")).not.toBeInTheDocument();
});
it("切换正文时不继续展示旧页内容，缺失页面提供返回索引", async () => {
  show("?view=wiki&page=missing");
  server.use(
    http.get(
      `${root}/pages/missing`,
      () => new HttpResponse(null, { status: 404 }),
    ),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "返回索引" })).toBeVisible(),
  );
  expect(
    screen.queryByRole("heading", { name: "页面正文" }),
  ).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "返回索引" }));
  expect(
    await screen.findByRole("heading", { name: "Wiki Index" }),
  ).toBeVisible();
});
it("新建子目录携带真实目录版本，失败后保留草稿和请求身份", async () => {
  show("?view=wiki");
  const user = userEvent.setup();
  await screen.findByRole("heading", { name: "Wiki Index" });
  await user.click(screen.getByRole("treeitem", { name: "主题目录" }));
  await user.click(screen.getByRole("button", { name: "新建目录" }));
  const requests: {
    requestId: string;
    structureVersion: number;
    theme: { parentId: string; name: string };
  }[] = [];
  server.use(
    http.post(`${root}/themes`, async ({ request }) => {
      requests.push((await request.json()) as (typeof requests)[number]);
      return new HttpResponse(null, { status: 504 });
    }),
  );
  await user.type(
    await screen.findByRole("textbox", { name: "目录名称" }),
    "新子目录",
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "创建" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "目录名称" })).toHaveValue(
    "新子目录",
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[0]).toMatchObject({
    structureVersion: 8,
    theme: { parentId: "theme", name: "新子目录" },
  });
  expect(requests[1]?.requestId).toBe(requests[0]?.requestId);
});
it("编辑以页面版本保存正文，失败保留草稿", async () => {
  show("?view=wiki&page=one");
  await screen.findByRole("heading", { name: "页面正文" });
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "编辑页面" }));
  const requests: unknown[] = [];
  server.use(
    http.put(`${root}/pages/one`, async ({ request }) => {
      requests.push(await request.json());
      return new HttpResponse(null, { status: 409 });
    }),
  );
  const content = screen.getByRole("textbox", { name: "正文" });
  expect(
    screen.queryByRole("textbox", { name: "页面地址" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "所属目录" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("combobox", { name: "页面类型" }),
  ).not.toBeInTheDocument();
  await user.clear(content);
  await user.type(content, "人工更新正文");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(content).toHaveValue("人工更新正文");
  expect(requests[0]).toMatchObject({
    expectedVersion: 3,
    structureVersion: 8,
    page: {
      slug: "concept/one",
      title: "第一页知识",
      content: "人工更新正文",
      themeId: "theme",
    },
  });
});
it("没有持久索引时从真实页面目录提供索引，不误报空库", async () => {
  show("?view=wiki");
  server.use(
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({ items: [pages[1]], total: 1 }),
    ),
  );
  expect(await screen.findByRole("link", { name: "第一页知识" })).toBeVisible();
  expect(screen.queryByText("还没有 Wiki 页面")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: "第一页知识" }));
  expect(
    await screen.findByRole("heading", { name: "页面正文" }),
  ).toBeVisible();
});
it("Wiki 直链读取原件计数但不启动整理任务查询，搜索保留匹配页面的祖先目录", async () => {
  show("?view=wiki");
  let sources = 0;
  let jobs = 0;
  server.use(
    http.get(`${root}/sources`, () => {
      sources++;
      return HttpResponse.json({ items: [], total: 0 });
    }),
    http.get(`${root}/jobs`, () => {
      jobs++;
      return HttpResponse.json({ items: [], total: 0 });
    }),
  );
  await screen.findByRole("heading", { name: "Wiki Index" });
  await userEvent.type(
    screen.getByRole("searchbox", { name: "搜索 Wiki 页面…" }),
    "第一页",
  );
  expect(screen.getByRole("treeitem", { name: "主题目录" })).toBeVisible();
  expect(screen.getByRole("treeitem", { name: "第一页知识" })).toBeVisible();
  expect(
    screen.queryByRole("treeitem", { name: "空目录" }),
  ).not.toBeInTheDocument();
  expect(sources).toBe(1);
  expect(jobs).toBe(0);
});
it("创建页面成功后定位后端返回的真实页面", async () => {
  show("?view=wiki");
  await screen.findByRole("heading", { name: "Wiki Index" });
  const user = userEvent.setup();
  let body: unknown;
  server.use(
    http.post(`${root}/pages`, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({ resourceId: "created", state: "saved" });
    }),
    http.get(`${root}/pages/created`, () =>
      HttpResponse.json({
        id: "created",
        title: "人工知识",
        slug: "concept/manual",
        pageType: "concept",
        version: 1,
        content: "# 新页面正文",
      }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "新建页面" }));
  await user.type(
    await screen.findByRole("textbox", { name: "页面标题" }),
    "人工知识",
  );
  await user.type(
    screen.getByRole("textbox", { name: "页面地址" }),
    "concept/manual",
  );
  await user.type(
    screen.getByRole("textbox", { name: "正文" }),
    "# 新页面正文",
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  expect(
    await screen.findByRole("heading", { name: "新页面正文" }),
  ).toBeVisible();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("page=created");
  expect(body).toMatchObject({
    structureVersion: 8,
    page: {
      title: "人工知识",
      slug: "concept/manual",
      content: "# 新页面正文",
      themeId: "",
      pageType: "concept",
      status: "published",
    },
  });
});
it("折叠父目录隐藏子目录及页面，键盘展开后恢复且计数包含后代", async () => {
  show("?view=wiki");
  server.use(
    http.get(`${root}/themes`, () =>
      HttpResponse.json({
        items: [
          { id: "parent", name: "父目录" },
          { id: "theme", name: "主题目录", parentId: "parent" },
        ],
      }),
    ),
  );
  await screen.findByRole("heading", { name: "Wiki Index" });
  const parent = screen.getByRole("treeitem", { name: "父目录" });
  await userEvent.click(parent);
  expect(
    screen.queryByRole("treeitem", { name: "主题目录" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("treeitem", { name: "第一页知识" }),
  ).not.toBeInTheDocument();
  await userEvent.keyboard("{ArrowRight}");
  expect(within(parent).getByText("1")).toBeVisible();
  expect(screen.getByRole("treeitem", { name: "第一页知识" })).toBeVisible();
});
it("仅普通正文中的 Wiki 链接可导航，内联与围栏代码保持原文", async () => {
  show("?view=wiki&page=one");
  server.use(
    http.get(`${root}/pages/one`, () =>
      HttpResponse.json({
        ...pages[1],
        content:
          "# 代码保真\n\n`[[concept/one]]`\n\n```text\n[[concept/one]]\n```\n\n[[concept/one|普通知识链接]]",
      }),
    ),
  );
  await screen.findByRole("heading", { name: "代码保真" });
  expect(
    await screen.findByRole("link", { name: "普通知识链接" }),
  ).toBeVisible();
  const code = Array.from(document.querySelectorAll("article code"), (node) =>
    node.textContent?.trim(),
  );
  expect(code).toEqual(["[[concept/one]]", "[[concept/one]]"]);
});

it("选择知识页面时只有页面选中，所属目录仅作为新建位置", async () => {
  show("?view=wiki");
  const item = await screen.findByRole("treeitem", { name: "第一页知识" });
  await userEvent.click(item);
  await screen.findByRole("heading", { name: "页面正文" });
  expect(screen.getAllByRole("treeitem", { selected: true })).toEqual([item]);
});

it("连续切换已读 Wiki 页面时正文和表格只属于当前选择，不累加旧页", async () => {
  const user = userEvent.setup();
  show("?view=wiki&page=one");
  const first = {
    id: "one",
    slug: "concept/one",
    title: "地址调度对象规格限制",
    pageType: "concept",
    version: 3,
    summary: "每类对象均有独立规模限制。",
    content:
      "# 地址调度对象规格限制\n\n## 对象上限\n\n| 对象 | 上限 |\n| --- | --- |\n| 地址池 | 20 |",
  };
  const second = {
    id: "two",
    slug: "concept/two",
    title: "网络冲突检查",
    pageType: "concept",
    version: 1,
    content:
      "# 网络冲突检查正文\n\n## 检查范围\n\n| 范围 | 行为 |\n| --- | --- |\n| 网段 | 检查重叠 |",
  };
  server.use(
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({ items: [pages[0], first, second], total: 3 }),
    ),
    http.get(`${root}/pages/:pageId`, ({ params }) =>
      HttpResponse.json(params.pageId === "two" ? second : first),
    ),
  );
  await screen.findByRole("heading", {
    name: "地址调度对象规格限制",
    level: 2,
  });
  for (const current of [second, first, second, first]) {
    await user.click(screen.getByRole("treeitem", { name: current.title }));
    const article = await screen.findByRole("article", {
      name: current.title,
    });
    if (current.id === "two")
      await within(article).findByRole("heading", {
        name: "网络冲突检查正文",
        level: 1,
      });
    else
      expect(
        within(article).getAllByRole("heading", { name: current.title }),
      ).toHaveLength(1);
    expect(
      within(article).queryAllByRole("heading", { level: 1 }),
    ).toHaveLength(current.id === "two" ? 1 : 0);
    expect(within(article).getAllByRole("table")).toHaveLength(1);
    expect(screen.getAllByRole("treeitem", { selected: true })).toEqual([
      screen.getByRole("treeitem", { name: current.title }),
    ]);
  }
  expect(screen.getAllByText(first.summary)).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "编辑页面" }));
  expect(screen.getByRole("textbox", { name: "正文" })).toHaveValue(
    first.content,
  );
});

it("页面操作属于正文标题，正文展示类型标签和更新时间", async () => {
  show("?view=wiki&page=one");
  await screen.findByRole("heading", { name: "页面正文" });
  const article = screen.getByRole("article", { name: "第一页知识" });
  const header = within(article).getByRole("heading", {
    name: "第一页知识",
  }).parentElement;
  if (!header) throw new Error("缺少正文标题行");
  for (const name of ["编辑页面", "历史版本", "删除页面"])
    expect(within(header).getByRole("button", { name })).toBeVisible();
  expect(within(article).getByText("概念")).toHaveAttribute(
    "data-slot",
    "badge",
  );
  expect(article.querySelector("time")).toHaveAttribute(
    "dateTime",
    new Date(1792479600000).toISOString(),
  );
});

it("历史版本按需读取，选择版本显示保存的历史正文", async () => {
  show("?view=wiki&page=one");
  let reads = 0;
  server.use(
    http.get(`${root}/pages/one/revisions`, () => {
      reads++;
      return HttpResponse.json({
        items: [
          {
            id: "revision",
            pageId: "one",
            version: 2,
            title: "旧页面标题",
            content: "# 旧页面标题\n\n# 历史正文\n\n旧知识内容",
            editSource: "user",
            editedAt: 1792479600000,
          },
        ],
        total: 1,
      });
    }),
  );
  await screen.findByRole("heading", { name: "页面正文" });
  expect(reads).toBe(0);
  await userEvent.click(screen.getByRole("button", { name: "历史版本" }));
  const dialog = await screen.findByRole("dialog", { name: "历史版本" });
  await userEvent.click(
    await within(dialog).findByRole("button", { name: /版本 2/ }),
  );
  await userEvent.click(within(dialog).getByRole("radio", { name: "原文" }));
  expect(await within(dialog).findByLabelText("版本原文")).toHaveTextContent(
    "旧知识内容",
  );
  expect(within(dialog).getByText("人工编辑")).toBeVisible();
  expect(reads).toBeGreaterThanOrEqual(1);
});

it("历史分页失败保留已读版本并禁用追加，重新读取后可恢复", async () => {
  const queryClient = show("?view=wiki&page=one");
  let fail = true;
  const offsets: string[] = [];
  server.use(
    http.get(`${root}/pages/one/revisions`, ({ request }) => {
      const offset = new URL(request.url).searchParams.get("offset") ?? "";
      offsets.push(offset);
      if (offset === "1" && fail)
        return new HttpResponse(null, { status: 500 });
      return HttpResponse.json({
        items:
          offset === "0"
            ? [
                {
                  id: "r3",
                  pageId: "one",
                  version: 3,
                  title: "当前",
                  content: "当前快照",
                },
              ]
            : [
                {
                  id: "r2",
                  pageId: "one",
                  version: 2,
                  title: "历史",
                  content: "第二版快照",
                },
              ],
        total: 2,
      });
    }),
  );
  await screen.findByRole("heading", { name: "页面正文" });
  await userEvent.click(screen.getByRole("button", { name: "历史版本" }));
  const dialog = screen.getByRole("dialog", { name: "历史版本" });
  const more = await within(dialog).findByRole("button", { name: "加载更多" });
  await userEvent.click(more);
  await waitFor(() => expect(offsets).toContain("1"));
  await waitFor(() => expect(more).toBeDisabled());
  await expectGlobalReadError();
  expect(within(dialog).getByRole("button", { name: /版本 3/ })).toBeVisible();
  fail = false;
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await waitFor(() => expect(more).toBeEnabled());
  await userEvent.click(
    within(dialog).getByRole("button", { name: "加载更多" }),
  );
  await userEvent.click(
    await within(dialog).findByRole("button", { name: /版本 2/ }),
  );
  await userEvent.click(within(dialog).getByRole("radio", { name: "原文" }));
  expect(await within(dialog).findByLabelText("版本原文")).toHaveTextContent(
    "第二版快照",
  );
  expect(
    within(dialog).queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  expect(offsets).toContain("1");
});

it("远端历史携带页面地址读取快照，写操作入口保留并检查能力", async () => {
  show("?view=wiki&page=one&backend=weknora&connection=remote");
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  const slugs: (string | null)[] = [];
  server.use(
    http.get(remote, () =>
      HttpResponse.json({
        id: "kb",
        name: "远端资料库",
        ref: {
          backend: "weknora",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        },
      }),
    ),
    http.get(`${remote}/page-directory`, () =>
      HttpResponse.json({ items: pages, total: 2 }),
    ),
    http.get(`${remote}/themes`, () => HttpResponse.json({ items: [] })),
    http.get(`${remote}/pages/one`, ({ request }) =>
      new URL(request.url).searchParams.get("slug") === "concept/one"
        ? HttpResponse.json({ ...pages[1], content: "远端当前正文" })
        : new HttpResponse(null, { status: 400 }),
    ),
    http.get(`${remote}/pages/one/revisions`, ({ request }) => {
      const slug = new URL(request.url).searchParams.get("slug");
      slugs.push(slug);
      return slug === "concept/one"
        ? HttpResponse.json({
            items: [
              { id: "r1", pageId: "one", version: 1, contentLoaded: false },
            ],
            total: 1,
          })
        : new HttpResponse(null, { status: 400 });
    }),
    http.get(
      `${remote}/pages/one/revisions/2`,
      () => new HttpResponse(null, { status: 404 }),
    ),
    http.get(`${remote}/pages/one/revisions/1`, ({ request }) => {
      const slug = new URL(request.url).searchParams.get("slug");
      slugs.push(slug);
      return slug === "concept/one"
        ? HttpResponse.json({
            id: "r1",
            pageId: "one",
            version: 1,
            content: "远端历史快照",
          })
        : new HttpResponse(null, { status: 400 });
    }),
  );
  await screen.findByText("远端当前正文");
  expect(screen.getByRole("button", { name: "编辑页面" })).toBeVisible();
  expect(screen.getByRole("button", { name: "删除页面" })).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "历史版本" }));
  await userEvent.click(await screen.findByRole("button", { name: /版本 1/ }));
  expect(await screen.findByText("远端历史快照")).toBeVisible();
  expect(slugs).toEqual(["concept/one", "concept/one"]);
});

it("删除先确认，失败可重试且使用相同身份和原始版本，成功后返回索引", async () => {
  show("?view=wiki&page=one");
  const user = userEvent.setup();
  const requests: { requestId: string }[] = [];
  let fail = true;
  server.use(
    http.put(`${root}/pages/one`, async ({ request }) => {
      requests.push((await request.json()) as { requestId: string });
      return fail
        ? new HttpResponse(null, { status: 504 })
        : HttpResponse.json({ resourceId: "one", state: "saved" });
    }),
  );
  await screen.findByRole("heading", { name: "页面正文" });
  await user.click(screen.getByRole("button", { name: "删除页面" }));
  let dialog = screen.getByRole("dialog", { name: "删除页面" });
  await user.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(requests).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "删除页面" }));
  dialog = screen.getByRole("dialog", { name: "删除页面" });
  await user.click(within(dialog).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  await waitFor(() =>
    expect(within(dialog).getByRole("button", { name: "删除" })).toBeEnabled(),
  );
  expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  fail = false;
  await user.click(within(dialog).getByRole("button", { name: "删除" }));
  await screen.findByRole("heading", { name: "Wiki Index" });
  expect(requests).toHaveLength(2);
  expect(requests[0]).toMatchObject({
    expectedVersion: 3,
    structureVersion: 8,
    page: {
      id: "one",
      status: "archived",
      content: "# 页面正文\n\n真实的知识正文。",
    },
  });
  expect(requests[1]?.requestId).toBe(requests[0]?.requestId);
  expect(screen.getByLabelText("当前路由")).not.toHaveTextContent("page=one");
});

it("Wiki 目录和正文分别显示骨架，加载后替换为真实内容", async () => {
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  show("?view=wiki");
  server.use(
    http.get(`${root}/page-directory`, async () => {
      await pending;
      return HttpResponse.json({ items: pages, total: 2 });
    }),
  );
  try {
    const directory = await screen.findByRole("complementary", {
      name: "Wiki 目录",
    });
    const article = screen.getByRole("article");
    expect(within(directory).getByRole("status")).toHaveAttribute(
      "aria-busy",
      "true",
    );
    expect(within(article).getByRole("status")).toHaveAttribute(
      "aria-busy",
      "true",
    );
    expect(screen.queryByText("还没有 Wiki 页面")).not.toBeInTheDocument();
    release();
    expect(
      await screen.findByRole("heading", { name: "Wiki Index" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("status", { name: "正在读取内容…" }),
    ).not.toBeInTheDocument();
  } finally {
    release();
  }
});

it("Wiki 分割线支持键盘和鼠标调整目录宽度", async () => {
  show("?view=wiki");
  await screen.findByRole("heading", { name: "Wiki Index" });
  const handle = screen.getByRole("separator", { name: "调整 Wiki 目录宽度" });
  expect(handle).toHaveAttribute("aria-valuenow", "288");
  fireEvent.keyDown(handle, { key: "ArrowRight" });
  expect(handle).toHaveAttribute("aria-valuenow", "296");
  fireEvent.mouseDown(handle, { clientX: 296, button: 0 });
  fireEvent.mouseMove(document, { clientX: 360 });
  fireEvent.mouseUp(document, { clientX: 360 });
  expect(handle).toHaveAttribute("aria-valuenow", "360");
  fireEvent.keyDown(handle, { key: "Home" });
  expect(handle).toHaveAttribute("aria-valuenow", "224");
  fireEvent.keyDown(handle, { key: "End" });
  expect(handle).toHaveAttribute("aria-valuenow", "480");
});

it.each([false, true])(
  "增量后结构化索引保留旧知识和新增摘要且导言只渲染一次（持久索引=%s）",
  async (persisted) => {
    show("?view=wiki");
    const intro = "# DNS 拨测知识库\n\n拨测范围与使用条件。";
    let indexReads = 0;
    const old = {
      id: "old",
      slug: "concept/old",
      title: "原有拨测知识",
      pageType: "concept",
    };
    const added = {
      id: "new",
      slug: "summary/new",
      title: "新增资料摘要",
      pageType: "summary",
    };
    server.use(
      http.get(`${root}/page-directory`, () =>
        HttpResponse.json({
          items: persisted ? [pages[0], old, added] : [old, added],
          total: persisted ? 3 : 2,
        }),
      ),
      http.get(`${root}/pages/index`, () =>
        HttpResponse.json({
          ...pages[0],
          title: "Wiki Index",
          summary: intro,
          content: intro,
        }),
      ),
      http.get(`${root}/wiki/index`, () => {
        indexReads++;
        return HttpResponse.json({
          intro: persisted ? intro : "",
          version: persisted ? 1 : 0,
          groups: [
            {
              type: "concept",
              total: 1,
              items: [{ slug: old.slug, title: old.title }],
            },
            {
              type: "summary",
              total: 1,
              items: [{ slug: added.slug, title: added.title }],
            },
          ],
        });
      }),
    );
    expect(
      await screen.findByRole("link", { name: "原有拨测知识" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "新增资料摘要" })).toBeVisible();
    expect(indexReads).toBe(1);
    if (persisted) {
      expect(
        screen.queryByRole("heading", { name: "DNS 拨测知识库" }),
      ).not.toBeInTheDocument();
      expect(screen.getByText("拨测范围与使用条件。")).toBeVisible();
      expect(screen.queryByText(intro)).not.toBeInTheDocument();
      expect(
        screen.getAllByRole("heading", { level: 1, name: "Wiki Index" }),
      ).toHaveLength(1);
    }
  },
);

it("索引后页失败保留已读条目并禁用追加，重新读取后可导航", async () => {
  const queryClient = show("?view=wiki");
  let fail = true;
  const cursors: string[] = [];
  server.use(
    http.get(`${root}/page-directory`, () =>
      HttpResponse.json({
        items: [
          ...pages,
          {
            id: "last",
            slug: "concept/last",
            title: "后页知识",
            pageType: "concept",
          },
        ],
        total: 3,
      }),
    ),
    http.get(`${root}/pages/last`, () =>
      HttpResponse.json({
        id: "last",
        slug: "concept/last",
        pageType: "concept",
        content: "# 后页知识正文",
      }),
    ),
    http.get(`${root}/wiki/index`, ({ request }) => {
      const q = new URL(request.url).searchParams;
      if (!q.get("pageTypes"))
        return HttpResponse.json({
          intro: "# 全库索引",
          groups: [
            {
              type: "concept",
              total: 2,
              items: [{ slug: "concept/one", title: "第一页知识" }],
              nextCursor: "1",
            },
          ],
        });
      expect(q.get("pageTypes")).toBe("concept");
      cursors.push(q.get("cursor") ?? "");
      return fail
        ? new HttpResponse(null, { status: 500 })
        : HttpResponse.json({
            groups: [
              {
                type: "concept",
                total: 2,
                items: [{ slug: "concept/last", title: "后页知识" }],
              },
            ],
          });
    }),
  );
  await screen.findByRole("heading", { level: 1, name: "Wiki Index" });
  await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
  const group = screen.getByRole("region", { name: "概念" });
  await waitFor(() =>
    expect(within(group).queryByText("正在读取内容…")).not.toBeInTheDocument(),
  );
  await expectGlobalReadError();
  expect(within(group).getByRole("link", { name: "第一页知识" })).toBeVisible();
  await waitFor(() =>
    expect(
      within(group).getByRole("button", { name: "加载更多" }),
    ).toBeDisabled(),
  );
  fail = false;
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await waitFor(() =>
    expect(
      within(screen.getByRole("region", { name: "概念" })).getByRole("button", {
        name: "加载更多",
      }),
    ).toBeEnabled(),
  );
  const restoredGroup = screen.getByRole("region", { name: "概念" });
  await userEvent.click(
    within(restoredGroup).getByRole("button", { name: "加载更多" }),
  );
  const last = await within(restoredGroup).findByRole("link", {
    name: "后页知识",
  });
  expect(cursors).toEqual(["1", "1"]);
  expect(
    within(restoredGroup).queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  await userEvent.click(last);
  expect(
    await screen.findByRole("heading", { name: "后页知识正文" }),
  ).toBeVisible();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("page=last");
});

it("远端索引维持原读取边界，不请求同标识本地索引", async () => {
  show("?view=wiki&backend=weknora&connection=remote");
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  let localIndexReads = 0;
  server.use(
    http.get(`${root}/wiki/index`, () => {
      localIndexReads++;
      return HttpResponse.json({ groups: [] });
    }),
    http.get(remote, () =>
      HttpResponse.json({
        id: "kb",
        name: "远端知识库",
        ref: {
          backend: "weknora",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        },
      }),
    ),
    http.get(`${remote}/page-directory`, () =>
      HttpResponse.json({ items: pages, total: 2 }),
    ),
    http.get(`${remote}/themes`, () => HttpResponse.json({ items: [] })),
    http.get(`${remote}/pages/index`, () =>
      HttpResponse.json({ ...pages[0], content: "# 远端索引正文" }),
    ),
  );
  expect(
    await screen.findByRole("heading", { level: 1, name: "Wiki Index" }),
  ).toBeVisible();
  expect(localIndexReads).toBe(0);
});

it("索引正文沿上游Markdown分类层级、链接与同行摘要，不画文件行", async () => {
  show("?view=wiki");
  server.use(
    http.get(`${root}/wiki/index`, () =>
      HttpResponse.json({
        intro: "# DNS 拨测知识库",
        groups: [
          {
            type: "concept",
            total: 3,
            items: [
              {
                slug: "concept/one",
                title: "DNS 拨测",
                summary: "探测不同地区的解析表现",
                categoryPath: ["网络", "DNS"],
              },
              {
                slug: "concept/cache",
                title: "缓存策略",
                summary: "说明TTL与缓存更新条件",
                categoryPath: ["网络", "DNS"],
              },
              {
                slug: "concept/http",
                title: "HTTP 拨测",
                categoryPath: ["网络", "HTTP"],
              },
            ],
          },
        ],
      }),
    ),
  );
  const group = await screen.findByRole(
    "region",
    { name: "概念" },
    { timeout: 5000 },
  );
  expect(
    within(group).getByRole("heading", { level: 2, name: "概念 (3)" }),
  ).toBeVisible();
  const link = within(group).getByRole("link", { name: "DNS 拨测" });
  expect(link.closest("p")).toHaveTextContent(
    "DNS 拨测 — 探测不同地区的解析表现",
  );
  expect(
    [...group.querySelectorAll('[data-streamdown="strong"]')].map(
      (node) => node.textContent,
    ),
  ).toEqual(["网络", "DNS", "HTTP"]);
  expect(group.querySelectorAll("br").length).toBeGreaterThanOrEqual(5);
  expect(group.querySelector("svg,li")).toBeNull();
  expect(
    within(group).queryByRole("button", { name: "阅读：DNS 拨测" }),
  ).not.toBeInTheDocument();
  await screen.findByRole(
    "treeitem",
    { name: "第一页知识" },
    { timeout: 5000 },
  );
  await userEvent.click(link);
  expect(
    await screen.findByRole("heading", { name: "页面正文" }),
  ).toBeVisible();
  expect(screen.getByLabelText("当前路由")).toHaveTextContent("page=one");
});

it("Wiki 保留统一库页头，编辑操作跟随可截断的正文标题", async () => {
  show("?view=wiki&page=one");
  const title = await screen.findByRole("heading", { name: "第一页知识" });
  expect(title).toHaveClass("truncate", "min-w-0");
  const row = title.parentElement;
  expect(row).toContainElement(
    screen.getByRole("button", { name: "编辑页面" }),
  );
  expect(row).toContainElement(
    screen.getByRole("button", { name: "历史版本" }),
  );
  expect(screen.getByRole("heading", { name: "Wiki 资料库" })).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "Wiki" }),
  ).not.toBeInTheDocument();
});

it("目录行菜单可新建子目录，固定所操作的父目录", async () => {
  const user = userEvent.setup();
  show("?view=wiki&page=one");
  await user.click(
    await screen.findByRole("button", { name: "主题目录的操作" }),
  );
  expect(screen.getByRole("menuitem", { name: "重命名目录" })).toBeVisible();
  expect(screen.getByRole("menuitem", { name: "删除目录" })).toBeVisible();
  await user.click(screen.getByRole("menuitem", { name: "新建子目录" }));
  const writes: unknown[] = [];
  server.use(
    http.post(`${root}/themes`, async ({ request }) => {
      writes.push(await request.json());
      return HttpResponse.json({ resourceId: "child", state: "saved" });
    }),
  );
  await user.type(
    await screen.findByRole("textbox", { name: "目录名称" }),
    "新子目录",
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    structureVersion: 8,
    theme: { parentId: "theme", name: "新子目录" },
  });
});

it("目录重命名保留父级、排序和失败草稿，删除空目录使用版本保护", async () => {
  const user = userEvent.setup();
  show("?view=wiki&page=one");
  await user.click(await screen.findByRole("button", { name: "空目录的操作" }));
  await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
  const field = await screen.findByRole("textbox", { name: "目录名称" });
  await user.clear(field);
  await user.type(field, "改名目录");
  const writes: Array<{ requestId: string; delete?: boolean }> = [];
  server.use(
    http.put(`${root}/themes/empty`, async ({ request }) => {
      const body = (await request.json()) as {
        requestId: string;
        delete?: boolean;
      };
      writes.push(body);
      return writes.length === 1
        ? HttpResponse.json(
            { message: "目录版本冲突", code: "KnowledgeConflict" },
            { status: 409 },
          )
        : HttpResponse.json({ resourceId: "empty", state: "saved" });
    }),
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(field).toHaveValue("改名目录");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(writes[1]).toMatchObject({
    structureVersion: 8,
    theme: { name: "改名目录" },
  });
  await user.click(screen.getByRole("button", { name: "空目录的操作" }));
  await user.click(screen.getByRole("menuitem", { name: "删除目录" }));
  expect(await screen.findByRole("dialog")).toHaveTextContent("仅可删除空目录");
  await user.click(screen.getByRole("button", { name: "删除" }));
  await waitFor(() => expect(writes).toHaveLength(3));
  expect(writes[2]).toMatchObject({ delete: true, structureVersion: 8 });
});

it("官方远端目录操作入口保留，点击明确提示能力未支持且不写本地", async () => {
  const user = userEvent.setup();
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  show("?view=wiki&page=one&backend=weknora&connection=remote");
  server.use(
    http.get(remote, () =>
      HttpResponse.json({
        id: "kb",
        name: "远端库",
        kind: "official",
        ref: {
          backend: "weknora",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        },
      }),
    ),
    http.get(`${remote}/page-directory`, () =>
      HttpResponse.json({ items: pages, total: pages.length }),
    ),
    http.get(`${remote}/themes`, () =>
      HttpResponse.json({
        items: [{ id: "theme", name: "主题目录", hasChildren: false }],
      }),
    ),
    http.get(`${remote}/pages/one`, () =>
      HttpResponse.json({ ...pages[1], content: "远端正文" }),
    ),
  );
  await user.click(
    await screen.findByRole("button", { name: "主题目录的操作" }),
  );
  await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "编辑页面" }));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("目录写入结果未知时关闭重开仍保留原命令身份和草稿", async () => {
  const user = userEvent.setup();
  const writes: Array<{ requestId: string }> = [];
  show("?view=wiki&page=one");
  server.use(
    http.put(`${root}/themes/empty`, async ({ request }) => {
      writes.push((await request.json()) as { requestId: string });
      return writes.length === 1
        ? HttpResponse.json(
            { code: "KnowledgeRemoteOutcomeUnknown" },
            { status: 504 },
          )
        : HttpResponse.json({ resourceId: "empty", state: "saved" });
    }),
  );
  await user.click(await screen.findByRole("button", { name: "空目录的操作" }));
  await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
  const field = await screen.findByRole("textbox", { name: "目录名称" });
  await user.clear(field);
  await user.type(field, "待确认名称");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(screen.getByRole("button", { name: "空目录的操作" }));
  await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
  expect(await screen.findByRole("textbox", { name: "目录名称" })).toHaveValue(
    "待确认名称",
  );
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]?.requestId).toBe(writes[0]?.requestId);
});

it("新建时更改所属目录后结果未知，独立核对入口继续原命令", async () => {
  const user = userEvent.setup();
  const writes: Array<{ requestId: string }> = [];
  show("?view=wiki&page=one");
  await user.click(await screen.findByRole("button", { name: "新建页面" }));
  await user.type(
    screen.getByRole("textbox", { name: "页面标题" }),
    "待确认新页",
  );
  await user.type(
    screen.getByRole("textbox", { name: "页面地址" }),
    "concept/pending",
  );
  screen.getByRole("combobox", { name: "所属目录" }).focus();
  await user.keyboard("{Enter}{End}{Enter}");
  server.use(
    http.post(`${root}/pages`, async ({ request }) => {
      writes.push((await request.json()) as { requestId: string });
      return writes.length === 1
        ? new HttpResponse(null, { status: 504 })
        : HttpResponse.json({ resourceId: "one", state: "saved" });
    }),
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "创建" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(
    screen.getByRole("button", { name: "核对未确认操作：待确认新页" }),
  );
  expect(await screen.findByRole("textbox", { name: "页面地址" })).toHaveValue(
    "concept/pending",
  );
  await user.click(screen.getByRole("button", { name: "创建" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]?.requestId).toBe(writes[0]?.requestId);
});

it("未确认目录命令明确拒绝后可修改草稿并使用新的请求身份", async () => {
  const { toApiError } = await import("@/shared/api/errors");
  const interceptor = client.interceptors.error.use((error, response) =>
    toApiError(error, response?.status),
  );
  try {
    const user = userEvent.setup();
    const writes: Array<{ requestId: string }> = [];
    show("?view=wiki&page=one");
    server.use(
      http.put(`${root}/themes/empty`, async ({ request }) => {
        writes.push((await request.json()) as { requestId: string });
        if (writes.length === 1) return new HttpResponse(null, { status: 504 });
        if (writes.length === 2)
          return HttpResponse.json(
            { message: "明确拒绝", code: "KnowledgeConflict" },
            { status: 409 },
          );
        return HttpResponse.json({ resourceId: "empty", state: "saved" });
      }),
    );
    await user.click(
      await screen.findByRole("button", { name: "空目录的操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "重命名目录" }));
    const first = screen.getByRole("textbox", { name: "目录名称" });
    await user.clear(first);
    await user.type(first, "原请求名称");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));
    await user.click(
      screen.getByRole("button", { name: "核对未确认操作：原请求名称" }),
    );
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(screen.getByRole("textbox", { name: "目录名称" })).toBeEnabled(),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    const next = screen.getByRole("textbox", { name: "目录名称" });
    await user.clear(next);
    await user.type(next, "新请求名称");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(writes).toHaveLength(3));
    expect(writes[1]?.requestId).toBe(writes[0]?.requestId);
    expect(writes[2]?.requestId).not.toBe(writes[1]?.requestId);
  } finally {
    client.interceptors.error.eject(interceptor);
  }
});

it("恢复结果未知保留独立核对入口，切换视图后重试刷新正文且不更换请求", async () => {
  show("?view=wiki&page=one");
  const user = userEvent.setup();
  const requests: unknown[] = [];
  let fail = true;
  let restored = false;
  server.use(
    http.get(`${root}/pages/one/revisions`, () =>
      HttpResponse.json({
        items: [
          {
            id: "r2",
            pageId: "one",
            version: 2,
            title: "历史标题",
            content: "复原正文",
            editSource: "user",
          },
        ],
        total: 1,
      }),
    ),
    http.post(`${root}/pages/one/restore`, async ({ request }) => {
      requests.push(await request.json());
      restored = true;
      return fail
        ? new HttpResponse(null, { status: 504 })
        : HttpResponse.json({ resourceId: "one", version: 4, state: "saved" });
    }),
    http.get(`${root}/pages/one`, () =>
      HttpResponse.json(
        restored
          ? { ...pages[1], title: "历史标题", content: "复原正文", version: 4 }
          : { ...pages[1], content: "当前正文" },
      ),
    ),
  );
  await screen.findByText("当前正文");
  await user.click(screen.getByRole("button", { name: "历史版本" }));
  await user.click(await screen.findByRole("button", { name: /版本 2/ }));
  await user.click(await screen.findByRole("button", { name: "恢复此版本" }));
  await user.click(screen.getByRole("button", { name: "确认恢复" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "确认恢复" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(
    within(screen.getByRole("dialog", { name: "历史版本" })).getByRole(
      "button",
      { name: "关闭" },
    ),
  );
  await user.click(screen.getByRole("radio", { name: /^原件/ }));
  await screen.findByText("原始资料.md");
  await user.click(screen.getByRole("radio", { name: /^知识/ }));
  await user.click(
    await screen.findByRole("button", { name: "核对未确认操作：第一页知识" }),
  );
  fail = false;
  await user.click(await screen.findByRole("button", { name: "确认恢复" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  await waitFor(() => expect(document.body.textContent).toContain("复原正文"));
  expect(screen.getByText("版本 4")).toBeVisible();
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
  expect(
    screen.queryByRole("button", { name: /核对未确认操作/ }),
  ).not.toBeInTheDocument();
});

it("Wiki 正文初次读取失败全局报告，恢复后清除错误", async () => {
  let failed = true;
  const queryClient = show("?view=wiki&page=one");
  server.use(
    http.get(`${root}/pages/one`, () =>
      failed
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({
            id: "one",
            slug: "concept/one",
            title: "恢复正文",
            content: "重试后正文",
          }),
    ),
  );
  await expectGlobalReadError();
  failed = false;
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await screen.findByText("重试后正文");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("索引初次读取失败全局报告，重新读取后恢复", async () => {
  let failed = true;
  const queryClient = show("?view=wiki");
  server.use(
    http.get(`${root}/wiki/index`, () =>
      failed
        ? new HttpResponse(null, { status: 503 })
        : HttpResponse.json({ intro: "索引恢复正文", groups: [] }),
    ),
  );
  await expectGlobalReadError();
  failed = false;
  await act(async () => {
    await queryClient.refetchQueries();
  });
  await screen.findByText("索引恢复正文");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("轻量目录先展示，点击后才请求所选正文", async () => {
  let detailReads = 0;
  let directoryReads = 0;
  const queryClient = show();
  server.use(
    http.get(`${root}/page-directory`, ({ request }) => {
      directoryReads++;
      expect(new URL(request.url).searchParams.get("limit")).toBe("1000");
      return HttpResponse.json({ items: pages, total: pages.length });
    }),
    http.get(`${root}/pages/one`, () => {
      detailReads++;
      return HttpResponse.json({
        ...pages[1],
        content: "点击后读取的独立正文",
      });
    }),
  );
  try {
    await screen.findByRole("treeitem", { name: "第一页知识" });
    expect(directoryReads).toBe(1);
    expect(detailReads).toBe(0);
    await userEvent.click(screen.getByRole("treeitem", { name: "第一页知识" }));
    await screen.findByText("点击后读取的独立正文");
    expect(detailReads).toBe(1);
  } finally {
    queryClient.clear();
  }
});
