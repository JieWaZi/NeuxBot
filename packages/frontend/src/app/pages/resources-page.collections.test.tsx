import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import type * as ResourceSdk from "@/shared/api/generated/sdk.gen";
import { listPlugins } from "@/shared/api/generated/sdk.gen";
import { ResourcesPage } from "./resources-page";

const state = vi.hoisted(() => ({
  failSecond: false,
  installedMode: false,
  failNextPage: false,
  pluginTotal: 21,
  skillTotal: 13,
  connectorTotal: 13,
  failRefresh: false,
  independentEmpty: false,
  bundledEmpty: false,
  failIndependent: false,
  failBundled: false,
  removeSkill: vi.fn<(input: unknown) => void>(),
  removeConnector: vi.fn<(input: unknown) => void>(),
  removePlugin: vi.fn<(input: { path: { pluginId: string } }) => void>(),
  intersections: new Map<Element, () => void>(),
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => {
  const sdk = await original<typeof ResourceSdk>();
  type PageRequest = { query: { pageNum: number; pageSize: number } };
  const plugin = (id: string, componentCount = 0) => ({
    plugin: { id, name: id },
    installations: [{ wakerId: "w1", versionId: "v1" }],
    versions: [
      {
        id: "v1",
        package: {
          manifest: { name: id },
          skills: Array.from({ length: componentCount }, (_, i) => ({
            key: `skill${i}`,
            name: `插件技能${i}`,
            description: "说明",
          })),
          servers: Array.from({ length: componentCount }, (_, i) => ({
            key: `mcp${i}`,
            command: "example",
          })),
        },
      },
    ],
  });
  return {
    ...sdk,
    browsePlugins: vi.fn(async ({ query }: PageRequest) => {
      if (state.failRefresh) throw new Error("refresh failed");
      if (state.failNextPage && query.pageNum === 2)
        throw new Error("load failed");
      const total = state.installedMode ? 2 : state.pluginTotal;
      return {
        data: {
          entries: state.installedMode
            ? ["插件A", "插件B"].map((name) => ({
                sourceKey: `catalog:${name}`,
                pluginId: name,
                versionId: "v1",
                name,
                installedCount: 1,
                category: "coding",
              }))
            : Array.from(
                {
                  length: Math.min(
                    query.pageSize,
                    total - (query.pageNum - 1) * query.pageSize,
                  ),
                },
                (_, i) => {
                  const index = i + (query.pageNum - 1) * query.pageSize;
                  return {
                    sourceKey: `catalog:${index}`,
                    catalogKey: `catalog${index}`,
                    name: `目录${index}`,
                    installedCount: 0,
                    category: index % 2 === 0 ? "coding" : "documents",
                  };
                },
              ),
          totalSize: total,
          installedTotalSize: state.installedMode ? 2 : 0,
          hasMore: query.pageNum * query.pageSize < total,
          pageNum: query.pageNum,
          pageSize: query.pageSize,
        },
      };
    }),
    getPlugin: vi.fn(async ({ path }: { path: { pluginId: string } }) => ({
      data: plugin(path.pluginId),
    })),
    listPlugins: vi.fn(async ({ query }: PageRequest) => {
      if (state.failBundled) throw new Error("plugin read failed");
      if (state.failRefresh) throw new Error("refresh failed");
      return {
        data: {
          resources:
            query.pageSize === 100
              ? [plugin("组合插件", state.bundledEmpty ? 0 : 28)]
              : [plugin("插件A"), plugin("插件B")],
          totalSize: query.pageSize === 100 ? 1 : 2,
        },
      };
    }),
    listLibrarySkills: vi.fn(async ({ query }: PageRequest) => {
      if (state.failIndependent || (state.failNextPage && query.pageNum === 2))
        throw new Error("skill read failed");
      if (state.failRefresh) throw new Error("refresh failed");
      return {
        data: {
          resources: state.independentEmpty
            ? []
            : Array.from(
                {
                  length: Math.min(
                    query.pageSize,
                    state.skillTotal - (query.pageNum - 1) * query.pageSize,
                  ),
                },
                (_, i) => {
                  const index = i + (query.pageNum - 1) * query.pageSize;
                  return {
                    skill: {
                      id: `skill${index}`,
                      name: `独立技能${index}`,
                      source:
                        index === 0 ? "builtin" : index % 2 ? "git" : "zip",
                      categories: index < 29 ? ["编程"] : [],
                      builtIn: index === 0,
                    },
                    affectedWakerCount: index === 1 ? 2 : 0,
                  };
                },
              ),
          totalSize: state.independentEmpty ? 0 : state.skillTotal,
        },
      };
    }),
    listLibraryConnectors: vi.fn(async ({ query }: PageRequest) => {
      if (state.failIndependent || (state.failNextPage && query.pageNum === 2))
        throw new Error("connector read failed");
      if (state.failRefresh) throw new Error("refresh failed");
      return {
        data: {
          resources: state.independentEmpty
            ? []
            : Array.from(
                {
                  length: Math.min(
                    query.pageSize,
                    state.connectorTotal - (query.pageNum - 1) * query.pageSize,
                  ),
                },
                (_, i) => {
                  const index = i + (query.pageNum - 1) * query.pageSize;
                  return {
                    connector: {
                      id: `mcp${index}`,
                      name: `独立连接器${index}`,
                      scope: index === 0 ? "built_in" : "public",
                      transport: "stdio",
                    },
                    compatibility: [],
                    affectedWakerCount: 2,
                  };
                },
              ),
          totalSize: state.independentEmpty ? 0 : state.connectorTotal,
        },
      };
    }),
    deleteLibrarySkill: vi.fn(
      async (options: Parameters<typeof sdk.deleteLibrarySkill>[0]) => {
        state.removeSkill(options);
        return { data: { success: true } };
      },
    ),
    deleteLibraryConnector: vi.fn(
      async (options: Parameters<typeof sdk.deleteLibraryConnector>[0]) => {
        state.removeConnector(options);
        return { data: { success: true } };
      },
    ),
    deletePlugin: vi.fn(
      async (options: Parameters<typeof sdk.deletePlugin>[0]) => {
        state.removePlugin(options);
        if (state.failSecond && options.path.pluginId === "插件B")
          throw new Error("failed");
        return { data: { success: true } };
      },
    ),
  };
});

beforeEach(() => {
  vi.mocked(listPlugins).mockClear();
  state.failSecond = false;
  state.installedMode = false;
  state.failNextPage = false;
  state.pluginTotal = 21;
  state.skillTotal = 13;
  state.connectorTotal = 13;
  state.failRefresh = false;
  state.independentEmpty = false;
  state.bundledEmpty = false;
  state.failIndependent = false;
  state.failBundled = false;
  state.removeSkill.mockClear();
  state.removeConnector.mockClear();
  state.removePlugin.mockClear();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      targets = new Set<Element>();
      constructor(private callback: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.targets.add(target);
        state.intersections.set(target, () =>
          this.callback(
            [{ target, isIntersecting: true } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          ),
        );
      }
      disconnect() {
        for (const target of this.targets) state.intersections.delete(target);
      }
      unobserve(target: Element) {
        state.intersections.delete(target);
      }
    },
  );
});
afterEach(() => {
  cleanup();
  state.intersections.clear();
  vi.unstubAllGlobals();
});
function mount(search: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/resources${search}`]}>
        <ResourcesPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
async function scrollNext() {
  const target = document.querySelector(
    '[data-slot="resource-infinite-scroll"]',
  );
  await waitFor(() =>
    expect(target && state.intersections.has(target)).toBe(true),
  );
  act(() => {
    if (target) state.intersections.get(target)?.();
  });
}

it("技能分类页使用面包屑与分类内搜索，滚动追加且不重复展示插件技能", async () => {
  state.skillTotal = 31;
  mount("?category=skills&source=plugins");
  await screen.findByRole("link", { name: "查看技能 独立技能1" });
  const installed = within(screen.getByRole("region", { name: "我的技能" }));
  const coding = within(screen.getByRole("region", { name: "编程" }));
  const uncategorized = within(screen.getByRole("region", { name: "未分类" }));
  expect(screen.queryByText(/^\d+ 个技能$/)).not.toBeInTheDocument();
  expect(
    installed.getByRole("heading", { name: /^我的技能\s*1$/ }),
  ).toBeVisible();
  expect(
    installed.getByRole("link", { name: "查看技能 独立技能1" }),
  ).toBeVisible();
  expect(installed.getAllByRole("article")).toHaveLength(1);
  expect(
    coding.queryByRole("link", { name: "查看技能 独立技能1" }),
  ).not.toBeInTheDocument();
  expect(coding.getByRole("heading", { name: /^编程\s*27$/ })).toBeVisible();
  expect(
    uncategorized.getByRole("heading", { name: /^未分类\s*2$/ }),
  ).toBeVisible();
  expect(coding.getAllByRole("article")).toHaveLength(6);
  expect(uncategorized.getAllByRole("article")).toHaveLength(2);
  expect(
    screen.queryByText(/插件技能|来自插件|独立安装/),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "查看技能 独立技能0" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "查看编程全部技能" }));
  expect(screen.getByRole("heading", { level: 1, name: "编程" })).toBeVisible();
  expect(screen.getAllByRole("article")).toHaveLength(24);
  const search = screen.getByRole("searchbox", { name: "在编程中搜索" });
  expect(screen.getByRole("navigation", { name: "技能" })).toHaveTextContent(
    "技能编程",
  );
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  await scrollNext();
  expect(screen.getAllByRole("article")).toHaveLength(28);
  expect(screen.queryByText(/^\d+ 个技能$/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "全部分类" }),
  ).not.toBeInTheDocument();
  fireEvent.change(search, { target: { value: "独立技能2" } });
  await screen.findByRole("link", { name: "查看技能 独立技能28" });
  expect(screen.getAllByRole("article")).toHaveLength(10);
  expect(
    screen.queryByRole("link", { name: "查看技能 独立技能29" }),
  ).not.toBeInTheDocument();
  fireEvent.change(search, { target: { value: "" } });
  fireEvent.click(
    within(screen.getByRole("navigation", { name: "技能" })).getByRole("link", {
      name: "技能",
    }),
  );
  expect(screen.getByRole("heading", { level: 1, name: "技能" })).toBeVisible();
  expect(screen.getAllByRole("article")).toHaveLength(9);
  expect(listPlugins).not.toHaveBeenCalled();
});

it.each(["", "&source=plugins", "&source=independent"])(
  "连接器列表忽略旧来源 %s，只读取独立连接器并滚动追加",
  async (source) => {
    state.connectorTotal = 29;
    mount(`?category=connectors${source}`);
    await screen.findByRole("link", { name: "查看连接器 独立连接器23" });
    expect(screen.getAllByRole("article")).toHaveLength(24);
    expect(screen.getByText("29 个连接器")).toBeVisible();
    expect(screen.queryByText("24 个连接器")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "查看连接器 mcp0" }),
    ).not.toBeInTheDocument();
    expect(listPlugins).not.toHaveBeenCalled();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    await scrollNext();
    await screen.findByRole("link", { name: "查看连接器 独立连接器28" });
    expect(screen.getAllByRole("article")).toHaveLength(29);
    expect(screen.getByText("29 个连接器")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "加载更多" }),
    ).not.toBeInTheDocument();
    expect(state.removeConnector).not.toHaveBeenCalled();
  },
);

it("连接器后续页失败保留已读条目，并可重试自动追加", async () => {
  state.connectorTotal = 29;
  state.bundledEmpty = true;
  state.failNextPage = true;
  mount("?category=connectors");
  await screen.findByRole("link", { name: "查看连接器 独立连接器23" });
  expect(screen.getAllByRole("article")).toHaveLength(24);
  await scrollNext();
  await screen.findByRole("alert");
  expect(screen.getAllByRole("article")).toHaveLength(24);
  expect(screen.getByText("29 个连接器")).toBeVisible();
  state.failNextPage = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByRole("link", { name: "查看连接器 独立连接器28" });
  expect(screen.getAllByRole("article")).toHaveLength(29);
});

it("连接器列表不依赖插件查询，独立查询失败时显示错误", async () => {
  state.failBundled = true;
  mount("?category=connectors");
  await screen.findByRole("link", { name: "查看连接器 独立连接器0" });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(listPlugins).not.toHaveBeenCalled();
  cleanup();
  state.failIndependent = true;
  mount("?category=connectors");
  expect(screen.queryByText(/\d+ 个连接器/)).not.toBeInTheDocument();
  await screen.findByRole("alert");
  expect(screen.queryByText(/\d+ 个连接器/)).not.toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "查看连接器 mcp0" }),
  ).not.toBeInTheDocument();
});

it("插件一次展示完整分类，分类数量与实际行数一致", async () => {
  mount("");
  await screen.findByRole("link", { name: "查看插件 目录20" });
  const coding = screen.getByRole("region", { name: "编程" });
  const documents = screen.getByRole("region", { name: "文档" });
  expect(within(coding).getAllByRole("article")).toHaveLength(11);
  expect(within(documents).getAllByRole("article")).toHaveLength(10);
  expect(within(coding).getByRole("heading", { level: 2 })).toHaveTextContent(
    "11",
  );
  expect(
    within(documents).getByRole("heading", { level: 2 }),
  ).toHaveTextContent("10");
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
});

it("已安装插件从详情管理，列表只呈现安装状态", async () => {
  state.installedMode = true;
  mount("");
  const link = await screen.findByRole("link", { name: "查看插件 插件A" });
  expect(link).toHaveAttribute(
    "href",
    `/resources/plugins/${encodeURIComponent("插件A")}`,
  );
  expect(screen.getAllByText("已安装")).toHaveLength(2);
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "删除所选" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("1 个伙伴使用")).not.toBeInTheDocument();
  expect(state.removePlugin).not.toHaveBeenCalled();
});

it("插件滚动查询追加，后续页失败保留已读条目并提供重试", async () => {
  state.pluginTotal = 60;
  state.failNextPage = true;
  mount("");
  await screen.findByRole("link", { name: "查看插件 目录23" });
  expect(screen.getAllByRole("article")).toHaveLength(24);
  expect(
    screen.queryByRole("link", { name: "查看插件 目录24" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
  expect(
    within(screen.getByRole("region", { name: "编程" })).getByRole("heading", {
      level: 2,
    }),
  ).toHaveTextContent(/^编程$/);
  await scrollNext();
  await screen.findByRole("alert");
  expect(screen.getAllByRole("article")).toHaveLength(24);
  state.failNextPage = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByRole("link", { name: "查看插件 目录47" });
  expect(screen.getAllByRole("article")).toHaveLength(48);
  await scrollNext();
  await screen.findByRole("link", { name: "查看插件 目录59" });
  expect(screen.getAllByRole("article")).toHaveLength(60);
  expect(
    screen.queryByRole("button", { name: "加载更多" }),
  ).not.toBeInTheDocument();
});

it.each([false, true])(
  "独立连接器为空时只展示一处空状态（插件为空=%s）",
  async (bundledEmpty) => {
    state.independentEmpty = true;
    state.bundledEmpty = bundledEmpty;
    mount("?category=connectors");
    await screen.findByText("暂无资源");
    expect(screen.getByText("0 个连接器")).toBeVisible();
    expect(screen.getAllByText("暂无资源")).toHaveLength(1);
    expect(
      screen.queryByRole("heading", { name: /独立安装|来自插件/ }),
    ).not.toBeInTheDocument();
    expect(listPlugins).not.toHaveBeenCalled();
  },
);

it("统一插件列表显示标题和整行搜索，不提供目录与我的插件切换", async () => {
  mount("");
  await screen.findByRole("link", { name: "查看插件 目录0" });
  expect(screen.getByRole("heading", { level: 1, name: "插件" })).toBeVisible();
  expect(
    screen.queryByRole("radio", { name: "我的插件" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("radio", { name: "插件库" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("全部插件")).not.toBeInTheDocument();
});

it.each([
  ["", "browsePlugins", "目录0"],
  ["?category=skills&source=independent", "listLibrarySkills", "独立技能1"],
  [
    "?category=connectors&source=independent",
    "listLibraryConnectors",
    "独立连接器0",
  ],
  [
    "?category=connectors&source=plugins",
    "listLibraryConnectors",
    "独立连接器0",
  ],
])(
  "%s 后台刷新失败保留卡片，并在重试成功后清除错误",
  async (search, queryId, name) => {
    const client = mount(search);
    await screen.findByRole("link", { name: new RegExp(`${name}$`) });
    state.failRefresh = true;
    await act(async () => {
      await client.invalidateQueries({
        predicate: (query) =>
          (query.queryKey[0] as { _id?: string } | undefined)?._id === queryId,
      });
    });
    const alert = await screen.findByRole("alert");
    expect(
      within(alert).getByText("刷新失败，当前显示上次加载的内容。"),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: new RegExp(`${name}$`) }),
    ).toBeVisible();
    if (queryId !== "listPluginResources")
      expect(
        screen.queryByRole("button", { name: "加载更多" }),
      ).not.toBeInTheDocument();
    state.failRefresh = false;
    fireEvent.click(within(alert).getByRole("button", { name: "重试" }));
    await waitFor(() =>
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
    );
    expect(
      screen.getByRole("link", { name: new RegExp(`${name}$`) }),
    ).toBeVisible();
    client.clear();
  },
);

it("技能目录只保留 Git/ZIP 技能与安装入口，不展示内部内置技能", async () => {
  mount("?category=skills&skillCategory=编程");
  await screen.findByRole("link", { name: "查看技能 独立技能1" });
  expect(screen.getByRole("searchbox", { name: "在编程中搜索" })).toBeVisible();
  expect(screen.getAllByRole("article")).toHaveLength(12);
  expect(screen.getAllByRole("button", { name: "安装" })).toHaveLength(11);
  expect(screen.getByText("已安装到 2 个伙伴")).toBeVisible();
  expect(screen.getByRole("button", { name: "安装到伙伴" })).toBeVisible();
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "查看技能 独立技能0" }),
  ).not.toBeInTheDocument();
});

it("独立技能为空时只展示一个空态", async () => {
  state.independentEmpty = true;
  state.bundledEmpty = true;
  mount("?category=skills");
  await screen.findByText("暂无资源");
  expect(screen.getAllByText("暂无资源")).toHaveLength(1);
  expect(screen.queryByRole("article")).not.toBeInTheDocument();
  expect(screen.queryByText("0 个技能")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "添加技能" })).toBeVisible();
});

it("技能摘要后续页失败不发布不完整分类，重试后恢复完整概览", async () => {
  state.skillTotal = 131;
  state.failNextPage = true;
  mount("?category=skills");
  const alert = await screen.findByRole("alert");
  expect(screen.queryByRole("article")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "编程" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "未分类" }),
  ).not.toBeInTheDocument();
  state.failNextPage = false;
  fireEvent.click(within(alert).getByRole("button", { name: "重试" }));
  const uncategorized = within(
    await screen.findByRole("region", { name: "未分类" }),
  );
  expect(
    uncategorized.getByRole("heading", { name: /^未分类\s*102$/ }),
  ).toBeVisible();
  expect(uncategorized.getAllByRole("article")).toHaveLength(6);
  expect(screen.getAllByRole("article")).toHaveLength(13);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
