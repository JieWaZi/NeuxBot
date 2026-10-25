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
import { ResourcesPage } from "./resources-page";

const state = vi.hoisted(() => ({
  failSecond: false,
  failNextPage: false,
  failRefresh: false,
  independentEmpty: false,
  bundledEmpty: false,
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
    listPluginCatalog: vi.fn(async ({ query }: PageRequest) => {
      if (state.failRefresh) throw new Error("refresh failed");
      if (state.failNextPage && query.pageNum === 2)
        throw new Error("load failed");
      return {
        data: {
          entries: Array.from(
            {
              length: Math.min(
                query.pageSize,
                21 - (query.pageNum - 1) * query.pageSize,
              ),
            },
            (_, i) => ({
              key: `catalog${i + (query.pageNum - 1) * query.pageSize}`,
              package: {
                manifest: {
                  name: `目录${i + (query.pageNum - 1) * query.pageSize}`,
                },
              },
            }),
          ),
          totalSize: 21,
        },
      };
    }),
    listPlugins: vi.fn(async ({ query }: PageRequest) => {
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
      if (state.failRefresh) throw new Error("refresh failed");
      return {
        data: {
          resources: state.independentEmpty
            ? []
            : Array.from(
                {
                  length: Math.min(
                    query.pageSize,
                    13 - (query.pageNum - 1) * query.pageSize,
                  ),
                },
                (_, i) => {
                  const index = i + (query.pageNum - 1) * query.pageSize;
                  return {
                    skill: {
                      id: `skill${index}`,
                      name: `独立技能${index}`,
                      scope: index === 0 ? "built_in" : "public",
                      builtIn: index === 0,
                    },
                    affectedWakerCount: 2,
                  };
                },
              ),
          totalSize: state.independentEmpty ? 0 : 13,
        },
      };
    }),
    listLibraryConnectors: vi.fn(async ({ query }: PageRequest) => {
      if (state.failRefresh) throw new Error("refresh failed");
      return {
        data: {
          resources: state.independentEmpty
            ? []
            : Array.from(
                {
                  length: Math.min(
                    query.pageSize,
                    13 - (query.pageNum - 1) * query.pageSize,
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
          totalSize: state.independentEmpty ? 0 : 13,
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
  state.failSecond = false;
  state.failNextPage = false;
  state.failRefresh = false;
  state.independentEmpty = false;
  state.bundledEmpty = false;
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
  const button = await screen.findByRole("button", { name: "加载更多" });
  const target = button.parentElement;
  expect(target && state.intersections.has(target)).toBe(true);
  act(() => {
    if (target) state.intersections.get(target)?.();
  });
}

it("keeps plugin-derived skill menu labels concise and links to the parent plugin", async () => {
  mount("?category=skills&source=plugins");
  await screen.findByText("28 项资源");
  expect(
    screen.getByRole("link", { name: "查看技能 插件技能0" }),
  ).toHaveAttribute(
    "href",
    "/resources/plugins/组合插件/skills/skill0?version=v1",
  );
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "插件技能0更多操作" }),
    { button: 0, ctrlKey: false },
  );
  expect(screen.getByRole("menuitem", { name: "查看详情" })).toHaveAttribute(
    "href",
    "/resources/plugins/组合插件/skills/skill0?version=v1",
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "查看插件" })).toHaveAttribute(
    "href",
    "/resources/plugins/组合插件",
  );
});

it.each(["skills", "connectors"] as const)(
  "%s 概览独立安装在前，各显示九项，分组箭头切换来源后滚动追加",
  async (category) => {
    mount(`?category=${category}`);
    await screen.findByText("41 项资源");
    const bundled = screen.getByRole("region", { name: "来自插件" });
    const independent = screen.getByRole("region", { name: "独立安装" });
    expect(within(bundled).getAllByRole("article")).toHaveLength(9);
    expect(within(independent).getAllByRole("article")).toHaveLength(9);
    expect(
      independent.compareDocumentPosition(bundled) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    fireEvent.click(
      within(bundled).getByRole("button", { name: "来自插件 28" }),
    );
    expect(
      screen.queryByRole("region", { name: "独立安装" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("28 项资源")).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: /来自插件/ }),
    ).not.toBeInTheDocument();
    await scrollNext();
    await waitFor(() =>
      expect(
        within(screen.getByRole("region", { name: "来自插件" })).getAllByRole(
          "article",
        ),
      ).toHaveLength(18),
    );
    await scrollNext();
    expect(
      within(screen.getByRole("region", { name: "来自插件" })).getAllByRole(
        "article",
      ),
    ).toHaveLength(27);
    await scrollNext();
    expect(
      within(screen.getByRole("region", { name: "来自插件" })).getAllByRole(
        "article",
      ),
    ).toHaveLength(28);
    expect(
      screen.queryByRole("button", { name: "加载更多" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "下一页" }),
    ).not.toBeInTheDocument();
  },
);

it.each([
  ["skills", "技能", "独立技能", state.removeSkill, "skillId"],
  ["connectors", "连接器", "独立连接器", state.removeConnector, "connectorId"],
] as const)(
  "%s 追加后可批量删除独立资源，保护内置资源并提交影响数量",
  async (category, label, prefix, remove, idKey) => {
    mount(`?category=${category}&source=independent`);
    await screen.findByText("13 项资源");
    expect(
      screen.queryByRole("heading", { name: /独立安装/ }),
    ).not.toBeInTheDocument();
    expect(screen.getAllByRole("article")).toHaveLength(9);
    await scrollNext();
    await waitFor(() =>
      expect(screen.getAllByRole("article")).toHaveLength(13),
    );
    expect(
      screen.queryByRole("checkbox", { name: `选择${label} ${prefix}0` }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择${label} ${prefix}1` }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择${label} ${prefix}11` }),
    );
    fireEvent.click(screen.getByRole("button", { name: "删除所选" }));
    expect(remove).not.toHaveBeenCalled();
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: category === "skills" ? "卸载所选" : "确认删除",
      }),
    );
    await waitFor(() => expect(remove).toHaveBeenCalledTimes(2));
    expect(remove.mock.calls[0]?.[0]).toMatchObject({
      path: { [idKey]: category === "skills" ? "skill1" : "mcp1" },
      query: { confirmed: true, expectedAffectedCount: 2 },
    });
  },
);

it("插件库滚动追加保留已加载卡片", async () => {
  mount("");
  await screen.findByText("21 项资源");
  expect(screen.getAllByRole("article")).toHaveLength(9);
  await scrollNext();
  await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(18));
  expect(screen.getByRole("link", { name: "查看插件 目录0" })).toBeVisible();
});

it("我的插件批量删除保留失败项重试，并携带原安装版本", async () => {
  mount("?view=mine");
  await screen.findByText("2 项资源");
  fireEvent.click(screen.getByRole("checkbox", { name: "选择" }));
  fireEvent.click(screen.getByRole("button", { name: "删除所选" }));
  const dialog = screen.getByRole("dialog", { name: "删除所选插件" });
  expect(state.removePlugin).not.toHaveBeenCalled();
  const targets = within(dialog).getByRole("list", { name: "待删除条目" });
  expect(targets.querySelectorAll("li svg")).toHaveLength(2);
  expect(within(targets).getAllByText("1 个伙伴使用")).toHaveLength(2);
  expect(within(dialog).getByText(/本机授权/)).toBeVisible();
  state.failSecond = true;
  fireEvent.click(within(dialog).getByRole("button", { name: "确认删除" }));
  await within(dialog).findByText("1 项删除失败，已保留失败项，可重试。");
  expect(within(dialog).queryByText("插件A")).not.toBeInTheDocument();
  expect(state.removePlugin.mock.calls[0]?.[0]).toMatchObject({
    body: {
      confirmed: true,
      installations: [
        { wakerId: "w1", expectedVersionId: "v1", versionId: "v1" },
      ],
    },
  });
  state.failSecond = false;
  fireEvent.click(within(dialog).getByRole("button", { name: "重试失败项" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    state.removePlugin.mock.calls.map((call) => call[0].path.pluginId),
  ).toEqual(["插件A", "插件B", "插件B"]);
});

it("追加失败保留已有卡片，停止自动重试并允许重试下一批", async () => {
  mount("");
  await screen.findByText("21 项资源");
  state.failNextPage = true;
  await scrollNext();
  await screen.findByText("加载失败，请重试。");
  expect(screen.getAllByRole("article")).toHaveLength(9);
  expect(state.intersections.size).toBe(0);
  state.failNextPage = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(18));
});

it.each(["skills", "connectors"] as const)(
  "%s 隐藏空来源分组，两组都为空时只展示一处空状态",
  async (category) => {
    state.independentEmpty = true;
    mount(`?category=${category}`);
    await screen.findByText("28 项资源");
    expect(
      screen.queryByRole("region", { name: "独立安装" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("暂无资源")).not.toBeInTheDocument();
    cleanup();
    state.bundledEmpty = true;
    mount(`?category=${category}`);
    await screen.findByText("暂无资源");
    expect(screen.getAllByText("暂无资源")).toHaveLength(1);
    expect(
      screen.queryByRole("heading", { name: /独立安装|来自插件/ }),
    ).not.toBeInTheDocument();
  },
);

it.each(["skills", "connectors"] as const)(
  "%s 筛选为空时不显示来源标题",
  async (category) => {
    state.independentEmpty = true;
    mount(`?category=${category}&source=independent`);
    await screen.findByText("暂无资源");
    expect(
      screen.queryByRole("heading", { name: /独立安装|来自插件/ }),
    ).not.toBeInTheDocument();
  },
);

it("插件切换直接展示文字导航与当前资源计数", async () => {
  mount("");
  await screen.findByText("21 项资源");
  expect(screen.queryByText("全部插件")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("radio", { name: "我的插件" }));
  await screen.findByText("2 项资源");
  expect(screen.getByRole("radio", { name: "我的插件" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  fireEvent.keyDown(screen.getByRole("radio", { name: "我的插件" }), {
    key: "ArrowLeft",
  });
  await screen.findByText("21 项资源");
  expect(screen.getByRole("radio", { name: "插件库" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
});

it.each([
  ["", "listPluginCatalog", "目录0"],
  ["?view=mine", "listPlugins", "插件A"],
  ["?category=skills&source=independent", "listLibrarySkills", "独立技能0"],
  [
    "?category=connectors&source=independent",
    "listLibraryConnectors",
    "独立连接器0",
  ],
  ["?category=skills&source=plugins", "listPluginResources", "插件技能0"],
  ["?category=connectors&source=plugins", "listPluginResources", "mcp0"],
])(
  "%s 后台刷新失败保留卡片，并在重试成功后清除错误",
  async (search, queryId, name) => {
    const client = mount(search);
    await screen.findByRole("link", { name: new RegExp(name) });
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
    expect(screen.getByRole("link", { name: new RegExp(name) })).toBeVisible();
    if (queryId !== "listPluginResources")
      expect(
        screen.queryByRole("button", { name: "加载更多" }),
      ).not.toBeInTheDocument();
    state.failRefresh = false;
    fireEvent.click(within(alert).getByRole("button", { name: "重试" }));
    await waitFor(() =>
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("link", { name: new RegExp(name) })).toBeVisible();
    client.clear();
  },
);
