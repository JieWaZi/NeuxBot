import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import { client } from "@/shared/api/generated/client.gen";
import {
  dismissErrorNotices,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { KnowledgeGraphView } from "./knowledge-graph-view";
import { knowledgeKeys } from "./knowledge-queries";
import { localGraphPagesOptions } from "./knowledge-graph-queries";
import { graphEngine } from "./knowledge-force-test-double";

vi.mock("react-force-graph-2d", () => import("./knowledge-force-test-double"));

const server = setupServer();
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://ally.test" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);
vi.stubGlobal("PointerEvent", MouseEvent);
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000);
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(600);
});
afterEach(() => {
  vi.restoreAllMocks();
  const notices = renderHook(useErrorNotices);
  act(() =>
    dismissErrorNotices(notices.result.current.map((notice) => notice.id)),
  );
  notices.unmount();
});
const resource = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
function show(
  backend = "local",
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  }),
) {
  const onCenter = vi.fn();
  function GraphHarness() {
    const [center, setCenter] = useState("");
    return (
      <KnowledgeGraphView
        resource={{
          ...resource,
          backend,
          connectionId: backend === "local" ? "local" : "remote",
        }}
        center={center}
        onCenter={(slug) => {
          setCenter(slug);
          onCenter(slug);
        }}
      />
    );
  }
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <GraphHarness />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, queryClient, onCenter };
}

it("进入图谱时重新核对已过期的目录缓存", async () => {
  const cached = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  cached.setQueryData(localGraphPagesOptions(resource).queryKey, [
    { id: "old", slug: "old", title: "旧缓存知识", pageType: "concept" },
  ]);
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          {
            id: "new",
            slug: "new",
            title: "刷新后的知识",
            pageType: "concept",
          },
        ],
        total: 1,
      }),
    ),
  );
  show("local", cached);
  const refresh = cached
    .getQueryCache()
    .find({ queryKey: localGraphPagesOptions(resource).queryKey });
  expect(refresh?.state.fetchStatus).toBe("fetching");
  await act(async () => {
    await refresh?.promise;
  });
  expect(refresh?.state.data).toMatchObject([{ id: "new", slug: "new" }]);
  expect(
    await screen.findByRole("button", { name: "刷新后的知识" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "旧缓存知识" }),
  ).not.toBeInTheDocument();
});

it("空图谱读取失败进入全局通知，内容不伪装空库也不展示重试按钮", async () => {
  const notices = renderHook(useErrorNotices);
  let release = () => {};
  let attempt = 0;
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/libraries/kb/pages",
      async () => {
        attempt++;
        if (attempt === 1) {
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return HttpResponse.error();
        }
        return HttpResponse.json({ items: [], total: 0 });
      },
    ),
  );
  const { queryClient } = show();
  expect(
    await screen.findByRole("status", { name: "正在读取内容…" }),
  ).toHaveAttribute("aria-busy", "true");
  expect(screen.queryByText("还没有知识图谱")).not.toBeInTheDocument();
  await waitFor(() => expect(attempt).toBe(1));
  release();
  await waitFor(() =>
    expect(
      screen.queryByRole("status", { name: "正在读取内容…" }),
    ).not.toBeInTheDocument(),
  );
  await waitFor(() =>
    expect(
      notices.result.current.some((notice) => notice.scope === "global"),
    ).toBe(true),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByText("还没有知识图谱")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  await queryClient.invalidateQueries({ queryKey: ["knowledge"] });
  await screen.findByText("还没有知识图谱");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("键盘选择真实孤立节点，刷新失败保留图谱且不显示无关系空态", async () => {
  let fail = false;
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/id", () =>
      HttpResponse.json({
        id: "id",
        slug: "isolated",
        title: "孤立知识",
        content: "# 孤立知识\n\n孤立页面正文",
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      fail
        ? HttpResponse.error()
        : HttpResponse.json({
            items: [
              {
                id: "id",
                slug: "isolated",
                title: "孤立知识",
                pageType: "concept",
              },
            ],
            total: 1,
          }),
    ),
  );
  const { queryClient } = show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  fireEvent.keyDown(within(canvas).getByRole("button", { name: "孤立知识" }), {
    key: "Enter",
  });
  expect(screen.getByText("暂无页面链接")).toBeVisible();
  await screen.findByText("孤立页面正文");
  expect(screen.getAllByRole("heading", { name: "孤立知识" })).toHaveLength(1);
  expect(
    screen.queryByRole("button", { name: "打开 Wiki 页面" }),
  ).not.toBeInTheDocument();
  const before = graphEngine.methods.zoom();
  fireEvent.keyDown(canvas, { key: "+" });
  expect(graphEngine.methods.zoom()).toBeGreaterThan(before);
  const reader = screen.getByRole("region", { name: "节点详情" });
  const presence = reader.parentElement;
  const panel = presence?.parentElement;
  expect(presence).toHaveAttribute("data-motion", "none");
  expect(panel).toHaveAttribute("data-state", "open");
  expect(panel).toHaveClass("transition-[width]", "w-120");
  await userEvent.click(screen.getByRole("button", { name: "关闭节点详情" }));
  expect(presence).toHaveAttribute("data-state", "closed");
  expect(presence).toHaveAttribute("inert");
  expect(panel).toHaveAttribute("data-state", "closed");
  expect(panel).toHaveClass("w-0");
  await waitFor(() => expect(presence).not.toBeInTheDocument());
  fail = true;
  await queryClient.invalidateQueries({ queryKey: ["knowledge"] });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("group", { name: "知识关系图" })).toBeInTheDocument();
});

it.each(["weknora", "integration"])(
  "%s 远端保留截断事实，单击才读取正文，离开页面后不迟到导航",
  async (backend) => {
    let release = () => {};
    let reading = false;
    server.use(
      http.get(
        "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb/graph",
        ({ request }) => {
          const query = new URL(request.url).searchParams;
          expect(query.get("mode")).toBe("overview");
          expect(query.get("limit")).toBe("200");
          return HttpResponse.json({
            nodes: [
              {
                slug: "remote",
                title: "远端知识",
                page_type: "entity",
                link_count: 3,
              },
            ],
            edges: [],
            meta: {
              mode: "overview",
              total: 205,
              returned: 1,
              truncated: true,
            },
          });
        },
      ),
      http.get(
        "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb/page-by-slug",
        async () => {
          reading = true;
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return HttpResponse.json({
            id: "remote-id",
            slug: "remote",
            title: "远端知识",
            content: "远端真实正文",
          });
        },
      ),
    );
    const { unmount, queryClient } = show(backend);
    const canvas = await screen.findByRole("group", { name: "知识关系图" });
    expect(
      screen.getAllByText("远端返回 1 / 205 个页面，可搜索全库页面并展开关联。")
        .length,
    ).toBeGreaterThan(0);
    expect(reading).toBe(false);
    await userEvent.click(
      within(canvas).getByRole("button", { name: "远端知识" }),
    );
    await waitFor(() => expect(reading).toBe(true));
    expect(
      screen.queryByRole("button", { name: "打开 Wiki 页面" }),
    ).not.toBeInTheDocument();
    unmount();
    release();
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  },
);

it("图谱交给 Canvas 引擎并在卸载时暂停动画", async () => {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          { slug: "a", title: "甲", outLinks: ["b"] },
          { slug: "b", title: "乙", outLinks: [] },
        ],
        total: 2,
      }),
    ),
  );
  const { unmount } = show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  expect(canvas.querySelector("canvas")).toBeInTheDocument();
  expect(graphEngine.methods.d3Force).toHaveBeenCalledWith(
    "collision",
    expect.any(Function),
  );
  const pauses = graphEngine.methods.pauseAnimation.mock.calls.length;
  unmount();
  expect(graphEngine.methods.pauseAnimation).toHaveBeenCalledTimes(pauses + 1);
});

it("按实际画布尺寸布局后仍保留概览分组名称与聚焦入口", async () => {
  const width = vi
    .spyOn(HTMLElement.prototype, "clientWidth", "get")
    .mockReturnValue(1200);
  const height = vi
    .spyOn(HTMLElement.prototype, "clientHeight", "get")
    .mockReturnValue(700);
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          {
            slug: "a",
            title: "主题中心",
            pageType: "entity",
            outLinks: ["b", "c", "d", "e"],
          },
          ...["b", "c", "d", "e"].map((slug) => ({
            slug,
            title: `知识${slug}`,
            outLinks: [],
          })),
        ],
        total: 5,
      }),
    ),
  );
  try {
    show();
    const caption = await screen.findByRole("button", {
      name: "主题中心 · 聚焦这组页面",
    });
    await userEvent.click(caption);
    expect(
      await screen.findByRole("button", { name: "返回全图 · Esc" }),
    ).toBeVisible();
  } finally {
    width.mockRestore();
    height.mockRestore();
  }
});

it("远端搜索区分请求中、全局失败通知与真实空结果，重新读取恢复", async () => {
  const notices = renderHook(useErrorNotices);
  let release = () => {};
  let attempt = 0;
  server.use(
    http.get(
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb/graph",
      () =>
        HttpResponse.json({
          nodes: [{ slug: "a", title: "已有页面", link_count: 0 }],
          edges: [],
          meta: { mode: "overview", total: 1, returned: 1 },
        }),
    ),
    http.get(
      "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb/pages",
      async () => {
        if (++attempt === 1) {
          await new Promise<void>((resolve) => {
            release = resolve;
          });
          return HttpResponse.error();
        }
        return HttpResponse.json({
          items: [{ id: "found", slug: "found", title: "找到的页面" }],
          total: 1,
        });
      },
    ),
  );
  const { queryClient } = show("weknora");
  await screen.findByRole("group", { name: "知识关系图" });
  await userEvent.type(
    screen.getByRole("searchbox", { name: "搜索图谱页面…" }),
    "知识",
  );
  expect(await screen.findByText("正在搜索页面…")).toBeVisible();
  expect(screen.queryByText("没有匹配的页面")).not.toBeInTheDocument();
  release();
  await waitFor(() =>
    expect(screen.queryByText("正在搜索页面…")).not.toBeInTheDocument(),
  );
  await waitFor(() =>
    expect(
      notices.result.current.some((notice) => notice.scope === "global"),
    ).toBe(true),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByText("没有匹配的页面")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  await queryClient.invalidateQueries({ queryKey: ["knowledge"] });
  await screen.findByText("找到的页面");
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("图谱接入第三方拖动结束事件并固定真实引擎坐标", async () => {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          { slug: "a", title: "拖动节点", outLinks: ["b"] },
          { slug: "b", title: "邻居" },
        ],
        total: 2,
      }),
    ),
  );
  show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  expect(canvas.querySelector("canvas")).toBeInTheDocument();
  const node = graphEngine.props?.graphData?.nodes.find(
    (entry) => entry.slug === "a",
  );
  if (!node) throw new Error("缺少引擎节点");
  node.x = 120;
  node.y = 100;
  act(() => graphEngine.props?.onNodeDragEnd?.(node, { x: 20, y: 0 }));
  expect(node).toMatchObject({
    x: 120,
    y: 100,
    fx: 120,
    fy: 100,
    pinned: true,
  });
});

it("单击在右侧读取真实正文，正文链接只切换阅读，展开邻居才改变子图", async () => {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          { id: "a", slug: "a", title: "甲知识", outLinks: ["b"] },
          { id: "b", slug: "b", title: "乙知识" },
        ],
        total: 2,
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/a", () =>
      HttpResponse.json({
        id: "a",
        slug: "a",
        title: "甲知识",
        version: 3,
        content: "甲的真实正文 [[b|乙知识入口]]",
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/b", () =>
      HttpResponse.json({
        id: "b",
        slug: "b",
        title: "乙知识",
        version: 2,
        content: "乙的真实正文",
      }),
    ),
  );
  const { onCenter } = show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  await userEvent.click(within(canvas).getByRole("button", { name: "甲知识" }));
  const reader = await screen.findByRole("region", { name: "节点详情" });
  expect(await within(reader).findByText(/甲的真实正文/)).toBeVisible();
  expect(within(reader).getByText("v3")).toBeVisible();
  expect(onCenter).not.toHaveBeenCalled();
  await userEvent.click(
    within(reader).getByRole("link", { name: "乙知识入口" }),
  );
  expect(await screen.findByText("乙的真实正文")).toBeVisible();
  expect(onCenter).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "展开邻居" }));
  expect(onCenter).toHaveBeenCalledWith("b");
});

it("双击只聚焦一层邻域，Shift 单击和扩张边缘保留当前上下文", async () => {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          { slug: "a", title: "甲", outLinks: ["b"] },
          { slug: "b", title: "乙", outLinks: ["c"] },
          { slug: "c", title: "丙", outLinks: ["d"] },
          { slug: "d", title: "丁" },
        ],
        total: 4,
      }),
    ),
  );
  const { onCenter } = show();
  const user = userEvent.setup();
  let canvas = await screen.findByRole("group", { name: "知识关系图" });
  await user.dblClick(within(canvas).getByRole("button", { name: "甲" }));
  await waitFor(() => expect(onCenter).toHaveBeenCalledWith("a"));
  canvas = screen.getByRole("group", { name: "知识关系图" });
  expect(
    within(canvas).queryByRole("button", { name: "丙" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "节点详情" }),
  ).not.toBeInTheDocument();
  await user.keyboard("{Shift>}");
  await user.click(within(canvas).getByRole("button", { name: "乙" }));
  await user.keyboard("{/Shift}");
  expect(
    await within(canvas).findByRole("button", { name: "丙" }),
  ).toBeInTheDocument();
  expect(
    within(canvas).getByRole("button", { name: "甲" }),
  ).toBeInTheDocument();
  expect(
    within(canvas).queryByRole("button", { name: "丁" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "扩张边缘（1）" }));
  expect(
    await within(canvas).findByRole("button", { name: "丁" }),
  ).toBeInTheDocument();
  expect(onCenter).toHaveBeenCalledTimes(1);
});

it("第三方命中检测选择的节点用于读取正文，不再自行扫描全部节点", async () => {
  server.use(
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages", () =>
      HttpResponse.json({
        items: [
          { id: "a", slug: "a", title: "目标节点", outLinks: ["b"] },
          { id: "b", slug: "b", title: "遮挡节点" },
        ],
        total: 2,
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/a", () =>
      HttpResponse.json({
        id: "a",
        title: "目标节点",
        content: "正确选择目标",
      }),
    ),
    http.get("http://ally.test/api/v1/knowledge/libraries/kb/pages/b", () =>
      HttpResponse.json({
        id: "b",
        title: "遮挡节点",
        content: "错误选择遮挡",
      }),
    ),
  );
  show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  expect(canvas.querySelector("canvas")).toBeInTheDocument();
  const node = graphEngine.props?.graphData?.nodes.find(
    (entry) => entry.slug === "a",
  );
  if (!node) throw new Error("缺少引擎节点");
  act(() =>
    graphEngine.props?.onNodeClick?.(
      node,
      new MouseEvent("click", { detail: 1 }),
    ),
  );
  expect(await screen.findByText("正确选择目标")).toBeVisible();
  expect(screen.queryByText("错误选择遮挡")).not.toBeInTheDocument();
});

it("图谱索引展示结构化目录，目录链接在图谱内读取正文", async () => {
  const root = "http://ally.test/api/v1/knowledge/libraries/kb";
  server.use(
    http.get(`${root}/pages`, () =>
      HttpResponse.json({
        items: [
          {
            id: "index",
            slug: "index",
            title: "Wiki Index",
            pageType: "index",
          },
          {
            id: "dns",
            slug: "concept/dns",
            title: "DNS 检测",
            pageType: "concept",
          },
        ],
        total: 2,
      }),
    ),
    http.get(`${root}/pages/index`, () =>
      HttpResponse.json({
        id: "index",
        slug: "index",
        title: "Wiki Index",
        pageType: "index",
        content: "# 知识库简介",
      }),
    ),
    http.get(`${root}/pages/dns`, () =>
      HttpResponse.json({
        id: "dns",
        slug: "concept/dns",
        title: "DNS 检测",
        content: "检测详细内容",
      }),
    ),
    http.get(`${root}/wiki/index`, () =>
      HttpResponse.json({
        intro: "# 知识库简介\n\n图谱索引概述。",
        groups: [
          {
            type: "concept",
            total: 1,
            items: [
              {
                slug: "concept/dns",
                title: "DNS 检测",
                summary: "检测步骤摘要",
              },
            ],
          },
        ],
      }),
    ),
  );
  show();
  const canvas = await screen.findByRole("group", { name: "知识关系图" });
  await userEvent.click(
    within(canvas).getByRole("button", { name: "Wiki Index" }),
  );
  expect(await screen.findByText(/检测步骤摘要/)).toBeVisible();
  expect(screen.getAllByRole("heading", { name: "Wiki Index" })).toHaveLength(
    1,
  );
  expect(screen.getByText("图谱索引概述。")).toBeVisible();
  expect(screen.getByRole("heading", { name: "索引", level: 3 })).toBeVisible();
  await userEvent.click(screen.getByRole("link", { name: "DNS 检测" }));
  expect(await screen.findByText("检测详细内容")).toBeVisible();
});

it("自有远端图谱使用远端完整分页与真实链接，不请求本机或官方图谱", async () => {
  const remote =
    "http://ally.test/api/v1/knowledge/connections/remote/libraries/kb";
  server.use(
    http.get(`${remote}/pages`, ({ request }) => {
      const offset = Number(new URL(request.url).searchParams.get("offset"));
      return HttpResponse.json({
        items:
          offset === 0
            ? [
                {
                  id: "a",
                  slug: "a",
                  title: "远端甲",
                  pageType: "concept",
                  outLinks: ["b"],
                },
              ]
            : [
                {
                  id: "b",
                  slug: "b",
                  title: "远端乙",
                  pageType: "entity",
                  inLinks: ["a"],
                },
              ],
        total: 2,
      });
    }),
  );
  const cache = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wikiKey = [
    ...knowledgeKeys.resource({
      backend: "custom",
      connectionId: "remote",
      knowledgeBaseId: "kb",
    }),
    "wiki",
    "pages",
  ];
  const lightweightDirectory = [
    { id: "a", slug: "a", title: "轻量目录标题", pageType: "concept" },
  ];
  cache.setQueryData(wikiKey, lightweightDirectory);
  show("custom", cache);
  const graph = await screen.findByRole("group", { name: "知识关系图" });
  expect(within(graph).getByRole("button", { name: "远端甲" })).toBeVisible();
  expect(within(graph).getByRole("button", { name: "远端乙" })).toBeVisible();
  expect(cache.getQueryData(wikiKey)).toEqual(lightweightDirectory);
  expect(graphEngine.props?.graphData?.links).toHaveLength(1);
  expect(graphEngine.props?.graphData?.links[0]).toMatchObject({
    source: { slug: "a" },
    target: { slug: "b" },
  });
});
