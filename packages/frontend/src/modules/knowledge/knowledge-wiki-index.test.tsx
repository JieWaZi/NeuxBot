import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import {
  dismissErrorNotices,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { remarkWikiLinks } from "./knowledge-wiki-markdown";
import { KnowledgeWikiView } from "./knowledge-wiki-view";

const server = setupServer();
const root = "http://ally.test/api/v1/knowledge/libraries";
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => {
  server.resetHandlers();
  const notices = renderHook(useErrorNotices);
  act(() =>
    dismissErrorNotices(notices.result.current.map((notice) => notice.id)),
  );
  notices.unmount();
});
afterAll(() => server.close());

it.each(["ready", "switch", "failure"] as const)(
  "索引链接等待同库完整metadata，结果=%s",
  async (outcome) => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const failed = outcome === "failure";
    let completed = false;
    server.use(
      http.get(`${root}/first/page-directory`, async () => {
        await gate;
        completed = true;
        return failed
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json({
              items: [
                {
                  id: "one",
                  slug: "concept/one",
                  title: "旧知识",
                  pageType: "concept",
                },
              ],
              total: 1,
            });
      }),
      http.get(`${root}/:libraryId/themes`, () =>
        HttpResponse.json({ items: [] }),
      ),
      http.get(`${root}/:libraryId/wiki/index`, ({ params }) =>
        HttpResponse.json({
          intro: params.libraryId === "first" ? "# 原库" : "# 新库",
          groups:
            params.libraryId === "first"
              ? [
                  {
                    type: "concept",
                    total: 1,
                    items: [{ slug: "concept/one", title: "旧知识" }],
                  },
                ]
              : [],
        }),
      ),
      http.get(`${root}/second/page-directory`, () =>
        HttpResponse.json({ items: [], total: 0 }),
      ),
    );
    const notices = renderHook(useErrorNotices);
    const onPage = vi.fn();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const ui = (id: string) => (
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <KnowledgeWikiView
            onMaintenance={() => {}}
            resource={{
              backend: "local",
              connectionId: "local",
              knowledgeBaseId: id,
            }}
            pageId=""
            onPage={onPage}
            writable={false}
            structureVersion={1}
          />
        </TooltipProvider>
      </QueryClientProvider>
    );
    const view = render(ui("first"));
    await userEvent.click(
      await screen.findByRole("link", { name: "旧知识" }, { timeout: 5000 }),
    );
    expect(
      screen.queryByText("未找到链接指向的 Wiki 页面"),
    ).not.toBeInTheDocument();
    if (outcome === "switch") view.rerender(ui("second"));
    release();
    await waitFor(() => expect(completed).toBe(true));
    if (outcome === "ready")
      await waitFor(() =>
        expect(onPage).toHaveBeenCalledExactlyOnceWith("one", "knowledge"),
      );
    if (outcome === "switch") {
      await screen.findByRole("heading", { level: 1, name: "Wiki Index" });
      await waitFor(() => expect(queryClient.isFetching()).toBe(0));
      expect(onPage).not.toHaveBeenCalled();
    }
    if (outcome === "failure") {
      expect(onPage).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(
          notices.result.current.some((notice) => notice.scope === "global"),
        ).toBe(true),
      );
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "重试" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByText("未找到链接指向的 Wiki 页面"),
      ).not.toBeInTheDocument();
    }
  },
);

it("索引生成的Markdown继续复用链接净化，正文HTML不执行", async () => {
  server.use(
    http.get(`${root}/first/page-directory`, () =>
      HttpResponse.json({ items: [], total: 0 }),
    ),
    http.get(`${root}/first/themes`, () => HttpResponse.json({ items: [] })),
    http.get(`${root}/first/wiki/index`, () =>
      HttpResponse.json({
        intro: "# 安全索引",
        groups: [
          {
            type: "concept",
            total: 1,
            items: [
              {
                slug: "concept/safe",
                title: "正常知识",
                summary:
                  '<img src="https://example.com/raw.png" onerror="alert(1)"><script>alert(1)</script> [危险链接](javascript:alert(1))',
              },
            ],
          },
        ],
      }),
    ),
  );
  const view = render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <TooltipProvider>
        <KnowledgeWikiView
          onMaintenance={() => {}}
          resource={{
            backend: "local",
            connectionId: "local",
            knowledgeBaseId: "first",
          }}
          pageId=""
          onPage={vi.fn()}
          writable={false}
          structureVersion={1}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("link", { name: "正常知识" }, { timeout: 5000 }),
  ).toHaveAttribute("href", "./concept/safe.md");
  expect(
    view.container.querySelector('img,script,iframe,a[href^="javascript:"]'),
  ).toBeNull();
});

it.each([
  "# 旧模型标题\n\n原有范围。",
  "旧模型标题\n=====\n\n原有范围。",
  "原有范围。",
  "",
])(
  "旧索引和无标题导言固定一级标题，刷新继续更新概述与目录：%s",
  async (intro) => {
    let updated = false;
    server.use(
      http.get(`${root}/first/wiki/index`, () =>
        HttpResponse.json({
          intro: updated ? "# 新模型标题\n\n新增文档范围。" : intro,
          groups: [
            {
              type: "concept",
              total: 1,
              items: [
                {
                  slug: updated ? "concept/new" : "concept/old",
                  title: updated ? "新增知识" : "原有知识",
                },
              ],
            },
          ],
        }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { KnowledgeWikiStructuredIndex } =
      await import("./knowledge-wiki-index");
    render(
      <QueryClientProvider client={queryClient}>
        <KnowledgeWikiStructuredIndex
          resource={{
            backend: "local",
            connectionId: "local",
            knowledgeBaseId: "first",
          }}
          active
          onLink={vi.fn()}
          remarkPlugins={[remarkWikiLinks]}
        />
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole("heading", { level: 1, name: "Wiki Index" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: "旧模型标题" }),
    ).not.toBeInTheDocument();
    if (intro) expect(screen.getByText("原有范围。")).toBeVisible();
    expect(screen.getByRole("link", { name: "原有知识" })).toBeVisible();
    updated = true;
    await queryClient.invalidateQueries();
    expect(await screen.findByText("新增文档范围。")).toBeVisible();
    expect(
      screen.getAllByRole("heading", { level: 1, name: "Wiki Index" }),
    ).toHaveLength(1);
    expect(
      screen.queryByRole("heading", { name: "新模型标题" }),
    ).not.toBeInTheDocument();
    expect(await screen.findByRole("link", { name: "新增知识" })).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "原有知识" }),
    ).not.toBeInTheDocument();
  },
);

it.each(["", "index"])(
  "远端索引使用完整分页目录和唯一导言，入口=%s",
  async (pageId) => {
    const remote =
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
    let pageRequests = 0;
    const rows = [
      {
        id: "index",
        slug: "index",
        pageType: "index",
        title: "索引",
        summary: "重复摘要",
        version: 2,
      },
      ...Array.from({ length: 101 }, (_, i) => ({
        id: `page-${i}`,
        slug: `concept/${i}`,
        title: `远端知识${i}`,
        pageType: "concept",
        categoryPath: ["远端目录"],
      })),
      {
        id: "summary",
        slug: "summary",
        pageType: "summary",
        title: "原件摘要",
      },
    ];
    server.use(
      http.get(`${remote}/page-directory`, ({ request }) => {
        pageRequests++;
        const query = new URL(request.url).searchParams;
        const offset = Number(query.get("offset") || 0);
        return HttpResponse.json({
          items: rows.slice(offset, offset + 100),
          total: rows.length,
        });
      }),
      http.get(`${remote}/themes`, () => HttpResponse.json({ items: [] })),
      http.get(`${remote}/pages/index`, () =>
        HttpResponse.json({
          ...rows[0],
          content: "# Wiki Index\n\n远端知识库范围。",
        }),
      ),
    );
    const onPage = vi.fn();
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <TooltipProvider>
          <KnowledgeWikiView
            onMaintenance={() => {}}
            resource={{
              backend: "integration",
              connectionId: "remote",
              knowledgeBaseId: "kb",
            }}
            pageId={pageId}
            onPage={onPage}
            writable={false}
            structureVersion={1}
          />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByRole(
        "heading",
        { level: 1, name: "Wiki Index" },
        { timeout: 5000 },
      ),
    ).toBeVisible();
    expect(screen.getAllByText("远端知识库范围。")).toHaveLength(1);
    expect(screen.queryByText("重复摘要")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "概念 (101)" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "摘要 (1)" })).toBeVisible();
    expect(screen.getByRole("link", { name: "原件摘要" })).toBeVisible();
    expect(
      screen.queryByRole("link", { name: "远端知识50" }),
    ).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
    expect(screen.getByRole("link", { name: "远端知识99" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "加载更多" }));
    await userEvent.click(screen.getByRole("link", { name: "远端知识100" }));
    expect(onPage).toHaveBeenCalledWith("page-100", "knowledge");
    expect(pageRequests).toBe(2);
    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["摘要 (1)", "概念 (101)"]);
  },
);

it.each(["no-index", "empty", "incomplete"])(
  "第三方远端无索引时按真实目录降级，状态=%s",
  async (state) => {
    const notices = renderHook(useErrorNotices);
    const remote =
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
    server.use(
      http.get(`${remote}/page-directory`, () =>
        HttpResponse.json({
          items:
            state === "no-index"
              ? [
                  {
                    id: "one",
                    slug: "concept/one",
                    pageType: "concept",
                    title: "公开知识",
                  },
                ]
              : [],
          total: state === "incomplete" ? 100 : state === "no-index" ? 1 : 0,
        }),
      ),
    );
    const { KnowledgeWikiRemoteIndex } =
      await import("./knowledge-wiki-remote-index");
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <KnowledgeWikiRemoteIndex
          resource={{
            backend: "weknora",
            connectionId: "remote",
            knowledgeBaseId: "kb",
          }}
          active
          onLink={vi.fn()}
          remarkPlugins={[remarkWikiLinks]}
        />
      </QueryClientProvider>,
    );
    if (state === "incomplete") {
      await waitFor(() =>
        expect(
          notices.result.current.some((notice) => notice.scope === "global"),
        ).toBe(true),
      );
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "重试" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("heading", { level: 1 }),
      ).not.toBeInTheDocument();
    } else {
      expect(
        await screen.findByRole("heading", { level: 1, name: "Wiki Index" }),
      ).toBeVisible();
      if (state === "no-index")
        expect(screen.getByRole("link", { name: "公开知识" })).toBeVisible();
      else expect(screen.queryByRole("link")).not.toBeInTheDocument();
    }
  },
);

it("远端交错分类按目录与业务顺序分组，后来的A类页面不会落进B类", async () => {
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  server.use(
    http.get(`${remote}/page-directory`, () =>
      HttpResponse.json({
        items: [
          {
            id: "a2",
            slug: "a-two",
            title: "A后二",
            pageType: "concept",
            categoryPath: ["A目录"],
            sortOrder: 20,
          },
          {
            id: "b",
            slug: "b",
            title: "B唯一",
            pageType: "concept",
            categoryPath: ["B目录"],
            sortOrder: 1,
          },
          {
            id: "a1",
            slug: "a-one",
            title: "A先一",
            pageType: "concept",
            categoryPath: ["A目录"],
            sortOrder: 10,
          },
        ],
        total: 3,
      }),
    ),
  );
  const { KnowledgeWikiRemoteIndex } =
    await import("./knowledge-wiki-remote-index");
  const { container } = render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <KnowledgeWikiRemoteIndex
        resource={{
          backend: "integration",
          connectionId: "remote",
          knowledgeBaseId: "kb",
        }}
        active
        onLink={vi.fn()}
        remarkPlugins={[remarkWikiLinks]}
      />
    </QueryClientProvider>,
  );
  await screen.findByRole("link", { name: "A先一" });
  expect(container.textContent).toMatch(/A目录.*A先一.*A后二.*B目录.*B唯一/s);
});
