import {
  act,
  render,
  screen,
  fireEvent,
  cleanup,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ResourcePageSearchField } from "@/shared/ui/resource-page";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { it, expect, vi, afterEach, beforeEach } from "vitest";
import "@/shared/i18n";
import type * as PluginQueries from "../api/plugin-queries";
import {
  getPluginOptions,
  listWakerPluginsQueryKey,
} from "../api/plugin-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type * as ResourceSdk from "@/shared/api/generated/sdk.gen";
import {
  browsePlugins,
  getPlugin,
  getCatalogPlugin,
  listPlugins,
} from "@/shared/api/generated/sdk.gen";
import type { PluginDetail, PluginCatalogEntry } from "../model/plugin-view";
import type { PluginBrowseEntry } from "../model/plugin-browse";
import { ApiError } from "@/shared/api/errors";
import { PluginResourceCollection } from "./plugin-resource-collection";
type CatalogReply = {
  data: PluginCatalogEntry;
  request: Request;
  response: Response;
};
const fixture = vi.hoisted(() => ({
  refresh: vi.fn(),
  fetchedAt: 0,
  sourceState: "available",
  unknownTotal: false,
  entries: [] as PluginBrowseEntry[],
  partnerEntries: [] as PluginDetail[],
  available: true,
  failDetail: false,
  detailGate: undefined as Promise<void> | undefined,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", async (original) => ({
  ...(await original<typeof PluginQueries>()),
  refreshPluginCatalogMutation: () => ({ mutationFn: fixture.refresh }),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof ResourceSdk>()),
  listPlugins: vi.fn(),
  listWakerPlugins: vi.fn(async () => ({
    data: {
      resources: fixture.partnerEntries,
      totalSize: fixture.partnerEntries.length,
    },
  })),
  browsePlugins: vi.fn(async () => ({
    data: {
      entries: fixture.entries,
      totalSize: fixture.unknownTotal ? undefined : fixture.entries.length,
      installedTotalSize: fixture.entries.filter(
        (entry) => (entry.installedCount ?? 0) > 0,
      ).length,
      hasMore: false,
      pageNum: 1,
      pageSize: 9,
      catalogSource: {
        state: fixture.sourceState,
        fetchedAt: fixture.fetchedAt,
      },
    },
  })),
  getPlugin: vi.fn(async (options: { path: { pluginId: string } }) => {
    if (fixture.detailGate) await fixture.detailGate;
    if (fixture.failDetail) throw new ApiError("详情读取失败");
    return {
      data: {
        plugin: { id: options.path.pluginId, name: "实际安装插件" },
        resolvedVersionId: "v1",
        update: fixture.entries.find(
          (entry) => entry.pluginId === options.path.pluginId,
        )?.update,
        account: { available: fixture.available, checkedAt: 123 },
        installations: [{ wakerId: "w1", versionId: "v1" }],
        versions: [
          {
            id: "v1",
            package: {
              manifest: { name: "office" },
              integration: {
                authentication: {
                  strategy: "plugin-command",
                  protocol: "neuxbot-auth/1",
                },
              },
            },
          },
        ],
      },
    };
  }),
  getCatalogPlugin: vi.fn(async () => ({
    data: {
      key: "new",
      sourceKey: "catalog:new",
      package: { manifest: { name: "新插件" } },
    },
  })),
}));
vi.mock("../components/plugin-install-dialog", () => ({
  PluginInstallDialog: ({
    detail,
    catalog,
    authorizeOnly,
    wakerId,
    onClose,
    catalogRead,
    preview,
  }: {
    detail?: PluginDetail;
    catalog?: PluginCatalogEntry;
    authorizeOnly?: boolean;
    wakerId?: string;
    onClose: () => void;
    preview?: PluginBrowseEntry;
    catalogRead?: { pending: boolean; error?: unknown; onRetry: () => void };
  }) => (
    <div role="dialog">
      {detail?.plugin?.id}:{catalog?.key}:{wakerId}:
      {authorizeOnly ? "reauthorize" : "install"}
      {preview?.description}
      {catalogRead?.pending && <span role="status" aria-busy="true" />}
      <button onClick={onClose}>关闭</button>
    </div>
  ),
}));
vi.mock("../components/plugin-update-dialog", () => ({
  PluginUpdateDialog: ({
    detail,
    wakerId,
  }: {
    detail: PluginDetail;
    wakerId?: string;
  }) => (
    <div role="dialog" data-version={detail.update?.version}>
      update:{detail.plugin?.id}:{wakerId}
    </div>
  ),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  fixture.fetchedAt = 0;
  fixture.sourceState = "available";
  fixture.unknownTotal = false;
  fixture.available = true;
  fixture.failDetail = false;
  fixture.detailGate = undefined;
  fixture.partnerEntries = [];
  fixture.entries = [
    {
      sourceKey: "catalog:office",
      pluginId: "existing",
      versionId: "v1",
      catalogKey: "office",
      name: "飞书",
      description: "实际安装内容",
      installedCount: 1,
    },
    {
      sourceKey: "catalog:new",
      catalogKey: "new",
      name: "新插件",
      installedCount: 0,
    },
  ];
  fixture.refresh.mockResolvedValue({ success: true });
});
function showCatalog(
  withSearch = false,
  targetPartnerId?: string,
  wakerId?: string,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PluginResourceCollection
          search=""
          targetPartnerId={targetPartnerId}
          wakerId={wakerId}
          searchField={
            withSearch ? (
              <ResourcePageSearchField
                aria-label="搜索插件"
                placeholder="搜索插件…"
              />
            ) : undefined
          }
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
it("renders installed and catalog summaries without scanning management resources", async () => {
  showCatalog();
  await screen.findByText("飞书");
  expect(screen.getByText("新插件")).toBeVisible();
  expect(screen.queryByText("2 项资源")).toBeNull();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("radiogroup")).toBeNull();
  expect(listPlugins).not.toHaveBeenCalled();
  expect(getPlugin).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "查看插件 飞书" })).toHaveAttribute(
    "href",
    "/resources/plugins/existing",
  );
});
it("keeps a delisted installed row without persistent catalog warnings", async () => {
  fixture.sourceState = "unavailable";
  fixture.unknownTotal = true;
  fixture.entries = fixture.entries
    .slice(0, 1)
    .map((entry) => ({ ...entry, catalogKey: undefined }));
  showCatalog();
  await screen.findByText("飞书");
  expect(screen.queryByText("已显示 1 项资源")).toBeNull();
  expect(
    screen.getByRole("heading", { level: 2, name: "其他" }),
  ).toHaveTextContent(/^其他$/);
  expect(
    screen.queryByText(
      "暂时无法获取插件目录，当前无法确认是否有更新。已安装插件仍可使用。",
    ),
  ).not.toBeInTheDocument();
});
it("opens a catalog install lazily and preserves the target partner", async () => {
  showCatalog(false, "w2");
  fireEvent.click(await screen.findByRole("button", { name: "安装 新插件" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent(":new:w2:install"),
  );
  expect(getCatalogPlugin).toHaveBeenCalledOnce();
  expect(getPlugin).not.toHaveBeenCalled();
});
it("opens a local uninstalled plugin and installs from its saved identity without catalog preparation", async () => {
  fixture.entries = [
    {
      sourceKey: "local-skills:local-plugin",
      pluginId: "local-plugin",
      versionId: "local-version",
      name: "研发组合",
      installedCount: 0,
    },
  ];
  showCatalog(false, "w2");
  expect(
    await screen.findByRole("link", { name: "查看插件 研发组合" }),
  ).toHaveAttribute("href", "/resources/plugins/local-plugin");
  fireEvent.click(screen.getByRole("button", { name: "安装 研发组合" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "local-plugin::w2:install",
    ),
  );
  expect(getPlugin).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "local-plugin" },
      query: expect.objectContaining({ versionId: "local-version" }),
    }),
  );
  expect(getCatalogPlugin).not.toHaveBeenCalled();
});
it("reads the installed snapshot only when reauthorizing", async () => {
  fixture.available = false;
  fixture.entries[0] = { ...fixture.entries[0], requiresAuthorization: true };
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "授权 飞书" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent(
      "existing:::reauthorize",
    ),
  );
  expect(getPlugin).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "existing" },
      query: expect.objectContaining({ versionId: "v1", locale: "zh-CN" }),
    }),
  );
});
it("reads an installed update lazily", async () => {
  fixture.entries[0] = { ...fixture.entries[0], update: { available: true } };
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "更新 飞书" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent("update:existing"),
  );
});
it("opens the installed update from a partner row in that partner's scope", async () => {
  fixture.partnerEntries = [
    {
      plugin: { id: "existing", name: "飞书" },
      installations: [{ wakerId: "w1", versionId: "v1", enabled: false }],
      update: { available: true, wakerIds: ["w1"] },
    },
  ];
  showCatalog(false, undefined, "w1");
  fireEvent.click(await screen.findByRole("button", { name: "更新 飞书" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent("update:existing:w1"),
  );
  expect(getPlugin).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "existing" },
      query: expect.objectContaining({ wakerId: "w1" }),
    }),
  );
});
it("loads a fresh update target when cached detail predates the catalog sync", async () => {
  let finishDetail = () => {};
  fixture.detailGate = new Promise<void>((resolve) => {
    finishDetail = resolve;
  });
  fixture.entries[0] = {
    ...fixture.entries[0],
    update: { available: true, version: "0.2.0" },
  };
  const client = showCatalog();
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId: "existing" },
      query: { locale: "zh-CN" },
    }),
  );
  client.setQueryData(options.queryKey, {
    plugin: { id: "existing" },
    update: { available: false },
  });
  fireEvent.click(await screen.findByRole("button", { name: "更新 飞书" }));
  try {
    await waitFor(() => expect(getPlugin).toHaveBeenCalledOnce());
    expect(screen.getByRole("dialog", { name: "更新 飞书" })).toBeVisible();
    expect(screen.queryByText(/update:existing/)).toBeNull();
  } finally {
    finishDetail();
  }
  expect(await screen.findByText(/update:existing/)).toHaveAttribute(
    "data-version",
    "0.2.0",
  );
  fixture.failDetail = true;
  await act(async () => {
    await expect(
      client.fetchQuery({ ...options, staleTime: 0 }),
    ).rejects.toThrow("详情读取失败");
  });
  expect(screen.getByText(/update:existing/)).toBeVisible();
});
it("reads the current catalog target even while the same management detail is already loading", async () => {
  fixture.entries[0] = {
    ...fixture.entries[0],
    update: { available: true, version: "0.3.0" },
  };
  const client = showCatalog();
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId: "existing" },
      query: { locale: "zh-CN" },
    }),
  );
  let settle!: (value: PluginDetail) => void;
  const management = client.fetchQuery({
    ...options,
    queryFn: () =>
      new Promise<PluginDetail>((resolve) => {
        settle = resolve;
      }),
  });
  try {
    fireEvent.click(await screen.findByRole("button", { name: "更新 飞书" }));
    expect(await screen.findByText(/update:existing/)).toHaveAttribute(
      "data-version",
      "0.3.0",
    );
  } finally {
    settle({ update: { available: true, version: "0.2.0" } });
    await management;
  }
});
it("renders the same partner plugin once when adjacent pages overlap", async () => {
  const installed = {
    plugin: { id: "existing", name: "飞书" },
    installations: [{ wakerId: "w1", versionId: "v1" }],
  };
  fixture.partnerEntries = [installed];
  const client = showCatalog(false, undefined, "w1");
  await screen.findByRole("heading", { name: "飞书" });
  act(() => {
    client.setQueryData(
      [
        ...listWakerPluginsQueryKey(
          withLocalErrorSurface({
            path: { wakerId: "w1" },
            query: { query: "", pageSize: 9 },
          }),
        ),
        "infinite",
      ],
      {
        pages: [
          { items: [installed], total: 3 },
          {
            items: [
              installed,
              { plugin: { id: "second", name: "第二页插件" } },
            ],
            total: 3,
          },
        ],
        pageParams: [1, 2],
      },
    );
  });
  await screen.findByRole("heading", { name: "第二页插件" });
  expect(screen.getAllByRole("heading", { name: "飞书" })).toHaveLength(1);
});
it("keeps a failed action read inside the dialog and retries there", async () => {
  fixture.failDetail = true;
  fixture.entries[0] = { ...fixture.entries[0], update: { available: true } };
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "更新 飞书" }));
  const dialog = await screen.findByRole("dialog");
  expect(await within(dialog).findByRole("alert")).toHaveTextContent(
    "详情读取失败",
  );
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  fixture.failDetail = false;
  fireEvent.click(within(dialog).getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent("update:existing"),
  );
  expect(screen.queryByRole("alert")).toBeNull();
});
it("opens a modal immediately while catalog details are loading", async () => {
  let complete!: (value: CatalogReply) => void;
  vi.mocked(getCatalogPlugin).mockImplementationOnce(
    () =>
      new Promise<CatalogReply>((resolve) => {
        complete = resolve;
      }),
  );
  fixture.entries[1] = { ...fixture.entries[1], description: "目录已知简介" };
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "安装 新插件" }));
  const dialog = await screen.findByRole("dialog");
  expect(
    within(dialog).getByText("目录已知简介", { exact: false }),
  ).toBeVisible();
  expect(within(dialog).getByRole("status")).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(screen.queryByText("正在处理…")).toBeNull();
  await act(async () =>
    complete({
      data: { key: "new", package: { manifest: { name: "新插件" } } },
      request: new Request("http://localhost/api/v1/plugins/catalog/new"),
      response: new Response(),
    }),
  );
  await waitFor(() =>
    expect(screen.getByRole("dialog")).toHaveTextContent(":new::install"),
  );
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
});
it("does not reopen a dismissed modal when its details arrive", async () => {
  let complete!: (value: CatalogReply) => void;
  vi.mocked(getCatalogPlugin).mockImplementationOnce(
    () =>
      new Promise<CatalogReply>((resolve) => {
        complete = resolve;
      }),
  );
  showCatalog();
  fireEvent.click(await screen.findByRole("button", { name: "安装 新插件" }));
  fireEvent.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "关闭",
    }),
  );
  await act(async () =>
    complete({
      data: { key: "new" },
      request: new Request("http://localhost/api/v1/plugins/catalog/new"),
      response: new Response(),
    }),
  );
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("button", { name: "安装 新插件" })).toBeEnabled();
});
it("groups the complete list by standard category and sends unknown categories to other", async () => {
  fixture.entries = [
    {
      sourceKey: "source:automation",
      catalogKey: "automation",
      name: "自动项目",
      category: "automation",
    },
    {
      sourceKey: "source:code",
      catalogKey: "code",
      name: "代码项目",
      category: "coding",
    },
    {
      sourceKey: "source:unknown",
      catalogKey: "unknown",
      name: "未知项目",
      category: "unlisted",
    },
    { sourceKey: "source:missing", catalogKey: "missing", name: "缺失分类" },
  ];
  showCatalog();
  await screen.findByText("代码项目");
  const groups = screen.getAllByRole("region");
  expect(
    groups.map((group) => group.getAttribute("aria-labelledby")),
  ).toHaveLength(3);
  expect(screen.getByRole("region", { name: "编程" })).toHaveTextContent(
    "代码项目",
  );
  expect(screen.getByRole("region", { name: "自动化" })).toHaveTextContent(
    "自动项目",
  );
  expect(screen.getByRole("region", { name: "其他" })).toHaveTextContent(
    "未知项目",
  );
  expect(screen.getByRole("region", { name: "其他" })).toHaveTextContent(
    "缺失分类",
  );
  const headings = screen.getAllByRole("heading", { level: 2 });
  expect(headings.map((heading) => heading.textContent)).toEqual([
    "编程1",
    "自动化1",
    "其他2",
  ]);
  expect(screen.queryByRole("button", { name: "加载更多" })).toBeNull();
});
it("waits for background events before refreshing browse summaries", async () => {
  const client = showCatalog();
  await screen.findByText("飞书");
  fixture.entries = [
    {
      sourceKey: "catalog:new",
      catalogKey: "new",
      name: "更新后的插件",
      installedCount: 0,
    },
  ];
  fireEvent.click(await screen.findByRole("button", { name: "检查更新" }));
  await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "检查更新" })).toBeEnabled(),
  );
  expect(screen.getByText("飞书")).toBeVisible();
  expect(screen.queryByText("更新后的插件")).not.toBeInTheDocument();
  expect(browsePlugins).toHaveBeenCalledTimes(1);
  client.clear();
});
it("keeps installed summaries after a failed manual catalog check", async () => {
  fixture.sourceState = "stale";
  fixture.refresh.mockRejectedValueOnce(new ApiError("目录暂不可用"));
  showCatalog();
  await screen.findByText("飞书");
  fireEvent.click(await screen.findByRole("button", { name: "检查更新" }));
  await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "检查更新" })).toBeEnabled(),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.queryByText("目录暂不可用")).not.toBeInTheDocument();
  expect(
    screen.queryByText(
      "目录更新失败，继续显示上次缓存的插件。已安装插件仍可使用。",
    ),
  ).not.toBeInTheDocument();
  expect(screen.getByText("飞书")).toBeVisible();
  expect(fixture.refresh.mock.calls[0]?.[0]).not.toHaveProperty(
    "headers.X-NeuxAlly-Error-Surface",
  );
});
it("refreshes directly after search without showing the last check time on hover", async () => {
  fixture.fetchedAt = Date.UTC(2026, 9, 19, 0, 0, 0);
  const user = userEvent.setup();
  showCatalog(true);
  await screen.findByRole("button", { name: "安装 新插件" });
  const refresh = screen.getByRole("button", { name: "检查更新" });
  const search = screen.getByRole("searchbox", { name: "搜索插件" });
  expect(
    search.compareDocumentPosition(refresh) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(refresh.textContent).toBe("");
  await user.hover(refresh);
  expect(screen.queryByText(/上次成功检查：/)).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(refresh);
  await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
});
it.each(["{Enter}", " "])(
  "refreshes the catalog directly with the keyboard (%s)",
  async (key) => {
    const user = userEvent.setup();
    showCatalog(true);
    await screen.findByRole("button", { name: "安装 新插件" });
    const refresh = screen.getByRole("button", { name: "检查更新" });
    act(() => refresh.focus());
    expect(fixture.refresh).not.toHaveBeenCalled();
    await user.keyboard(key);
    await waitFor(() => expect(fixture.refresh).toHaveBeenCalledOnce());
    expect(refresh).toHaveFocus();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  },
);
