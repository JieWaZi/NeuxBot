import {
  render,
  act,
  screen,
  fireEvent,
  cleanup,
  within,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { it, expect, vi, afterEach, beforeAll } from "vitest";
import { i18n } from "@/shared/i18n";
import { PluginDetailPage } from "./plugin-detail-page";

const { togglePlugin, renamePlugin, fixture } = vi.hoisted(() => ({
  fixture: {
    updateAvailable: false,
    updateWakerIds: ["w1"],
    rollbackAvailable: false,
    unsupported: false,
    readError: false,
    hold: false,
    request: vi.fn(),
  },
  togglePlugin: vi.fn(),
  renamePlugin: vi.fn<(input: unknown) => Promise<{ success: boolean }>>(
    async () => ({ success: true }),
  ),
}));

vi.mock("../components/plugin-presentation", () => ({
  PluginPresentation: () => null,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", () => ({
  getPluginConfigurationOptions: () => ({
    queryKey: [{ _id: "getPluginConfiguration" }],
    queryFn: async () => ({
      versionId: "v1",
      fields: [],
      values: {},
      configuredSecrets: [],
      configurationRevision: 0,
      accountGeneration: 0,
      ready: true,
    }),
  }),
  cancelPluginAuthorizationMutation: () => ({ mutationFn: vi.fn() }),
  prepareCatalogPluginMutation: () => ({ mutationFn: vi.fn() }),
  updatePluginInstallationsMutation: () => ({ mutationFn: vi.fn() }),
  getPluginOptions: (options: {
    query?: { locale?: string; wakerId?: string; versionId?: string };
  }) => ({
    queryKey: ["test-plugin", options.query?.locale],
    queryFn: async () => {
      fixture.request(options);
      if (fixture.readError) throw new Error("详情暂不可用");
      if (fixture.hold) await new Promise(() => undefined);
      return {
        plugin: { id: "p", name: "办公室" },
        resolvedVersionId: "v1",
        packageStatus: fixture.unsupported ? "unsupported" : "available",
        packageErrorCode: fixture.unsupported
          ? "PluginPackageUnsupported"
          : undefined,
        update: {
          available: fixture.updateAvailable,
          wakerIds: fixture.updateWakerIds,
          catalogKey: "office",
          archiveSHA256: "fixed",
        },
        rollback: {
          available: fixture.rollbackAvailable,
          versionId: "previous",
          version: "0.9.0",
        },
        installations: [{ wakerId: "w1", versionId: "v1", enabled: true }],
        partnerNames: { w1: "工作伙伴" },
        versions: [
          {
            id: "v1",
            version: "1.0",
            package: {
              manifest: {
                name: "office",
                version: "1.0",
                author: { name: "作者" },
                license: "MIT",
              },
              presentation: {
                shortDescription: "整理笔记和办公资料",
                localizedDescriptions: {
                  en: "Office tasks",
                  "zh-CN": "整理笔记和办公资料",
                },
                developerName: "作者",
              },
              upstream: { author: "作者", tag: "1.0" },
              readme:
                options.query?.locale === "en"
                  ? "English business guide"
                  : "按任务整理笔记，协助处理办公资料。",
              skills: [{ key: "notes", name: "笔记" }],
            },
          },
        ],
      };
    },
  }),
  setWakerPluginEnabledMutation: () => ({ mutationFn: togglePlugin }),
  updatePluginMutation: () => ({ mutationFn: renamePlugin }),
  updatePluginConfigurationMutation: () => ({ mutationFn: vi.fn() }),
  invalidatePluginQueries: async () => undefined,
}));
vi.mock("../components/plugin-removal-dialog", () => ({
  PluginRemovalDialog: ({
    mode,
    wakerId,
  }: {
    mode: string;
    wakerId: string;
  }) => (
    <div role="dialog">
      {mode}:{wakerId}
    </div>
  ),
}));
// 页面行为用例先准备实际正文模块，避免将开发环境模块编译时间当作读取失败。
beforeAll(async () => {
  await import("../components/plugin-overview");
});

afterEach(async () => {
  cleanup();
  vi.clearAllMocks();
  fixture.updateAvailable = false;
  fixture.updateWakerIds = ["w1"];
  fixture.rollbackAvailable = false;
  fixture.unsupported = false;
  fixture.readError = false;
  fixture.hold = false;
  await i18n.changeLanguage("zh-CN");
});
function CurrentRoute() {
  const location = useLocation();
  return (
    <p data-testid="current-route">
      {location.pathname}
      {location.search}
    </p>
  );
}
function open(path: string, resourceBack?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter
        initialEntries={[
          resourceBack ? { pathname: path, state: { resourceBack } } : path,
        ]}
      >
        <Routes>
          <Route path="resources" element={<CurrentRoute />} />
          <Route
            path="resources/plugins/:pluginId"
            element={<PluginDetailPage />}
          />
          <Route
            path="wakers/:wakerId/plugins/:pluginId"
            element={<PluginDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

it.each(["/resources/plugins/p", "/wakers/w1/plugins/p"])(
  "%s 不提供上传、导入或添加本地版本操作",
  async (path) => {
    open(path);
    await screen.findByRole("heading", { name: "办公室" });
    expect(
      screen.queryByRole("button", {
        name: /上传|导入|添加新版本|选择文件夹|选择 ZIP/,
      }),
    ).not.toBeInTheDocument();
    if (path.startsWith("/resources"))
      expect(screen.getByRole("button", { name: "安装到伙伴" })).toBeEnabled();
  },
);

it("keeps partner enable controls in the header and uninstall in the final section", async () => {
  open("/wakers/w1/plugins/p?tab=configuration");
  await screen.findByRole("heading", { name: "办公室" });
  expect(
    screen.queryByRole("button", { name: "安装到伙伴" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: /^已安装/ }),
  ).not.toBeInTheDocument();
  expect(
    await screen.findByRole("heading", { name: "插件信息" }),
  ).toBeVisible();
  expect(screen.getByText("MIT")).toBeVisible();
  expect(screen.getByText("1.0")).toBeVisible();
  const header = screen
    .getByRole("heading", { name: "办公室" })
    .closest("header");
  expect(header).not.toBeNull();
  if (!header) throw new Error("Plugin header is missing");
  const toggle = within(header).getByRole("switch", { name: "关闭插件" });
  expect(toggle).toBeChecked();
  fireEvent.click(toggle);
  await waitFor(() => expect(togglePlugin).toHaveBeenCalled());
  expect(togglePlugin.mock.calls[0]?.[0]).toMatchObject({
    path: { wakerId: "w1", pluginId: "p" },
    body: { enabled: false },
  });
  expect(screen.getByRole("button", { name: "查看技能 笔记" })).toBeVisible();
  const uninstall = screen.getByRole("button", { name: "从伙伴卸载" });
  expect(uninstall).toHaveTextContent("从伙伴卸载");
  expect(header).not.toContainElement(uninstall);
  expect(uninstall.closest("section")).toContainElement(
    screen.getByRole("heading", { name: "危险操作" }),
  );
  expect(
    screen.queryByRole("button", { name: "编辑插件" }),
  ).not.toBeInTheDocument();
  fireEvent.click(uninstall);
  expect(screen.getByRole("dialog")).toHaveTextContent("installation:w1");
});
it("global detail keeps its reading layout and opens shared settings from the edit action", async () => {
  open("/resources/plugins/p?tab=configuration");
  await screen.findByRole("heading", { name: "办公室" });
  expect(screen.getByRole("link", { name: "插件" })).toHaveAttribute(
    "href",
    "/resources",
  );
  expect(
    screen.queryByRole("link", { name: "资源库" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  const summary = screen.getByText("整理笔记和办公资料");
  expect(summary).toHaveClass("truncate");
  expect(summary).toHaveAttribute("title", "整理笔记和办公资料");
  expect(
    screen.queryByRole("textbox", { name: "展示名称" }),
  ).not.toBeInTheDocument();
  const edit = await screen.findByRole("button", { name: "编辑插件" });
  expect(
    screen.getByRole("heading", { name: "办公室" }).closest("header"),
  ).toContainElement(edit);
  expect(screen.queryByRole("heading", { name: "展示名称" })).toBeNull();
  fireEvent.click(edit);
  const dialog = await screen.findByRole("dialog", { name: "编辑 办公室" });
  expect(
    await within(dialog).findByRole("textbox", { name: "展示名称" }),
  ).toHaveValue("办公室");
  expect(within(dialog).getByRole("button", { name: "保存" })).toBeDisabled();
  expect(
    within(dialog).queryByRole("button", { name: "保存名称" }),
  ).not.toBeInTheDocument();
  fireEvent.change(within(dialog).getByRole("textbox", { name: "展示名称" }), {
    target: { value: "资料助手" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(renamePlugin.mock.calls[0]?.[0]).toMatchObject({
      path: { pluginId: "p" },
      query: { wakerId: "" },
      body: { name: "资料助手" },
    }),
  );
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("reloads installed documentation by locale while preserving a user resource name", async () => {
  open("/resources/plugins/p");
  expect(
    await screen.findByText("按任务整理笔记，协助处理办公资料。"),
  ).toBeVisible();
  await act(() => i18n.changeLanguage("en-US"));
  expect(await screen.findByText("English business guide")).toBeVisible();
  expect(screen.getByRole("heading", { name: "办公室" })).toBeVisible();
  expect(screen.getByText("Office tasks")).toBeVisible();
});

it.each(["/resources/plugins/p", "/wakers/w1/plugins/p"])(
  "%s exposes the update entry while retaining installed information",
  async (path) => {
    fixture.updateAvailable = true;
    open(path);
    await screen.findByRole("heading", { name: "办公室" });
    expect(screen.getByText("1.0")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "可更新" }));
    expect(
      await screen.findByRole("dialog", { name: "更新 办公室" }),
    ).toBeVisible();
    expect(await screen.findByText("将更新全部 1 个伙伴")).toBeVisible();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  },
);

it("mounts the detail shell and back action while its content is still loading", () => {
  fixture.hold = true;
  open("/resources/plugins/p");
  expect(screen.getByRole("heading", { name: "插件" })).toBeVisible();
  expect(
    screen.getByRole("button", { name: "返回资源库" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true");
  expect(
    screen.queryByRole("button", { name: "删除资源" }),
  ).not.toBeInTheDocument();
});

it("keeps the partner detail current when another partner has an update", async () => {
  fixture.updateAvailable = true;
  fixture.updateWakerIds = ["w2"];
  open("/wakers/w1/plugins/p");
  await screen.findByRole("heading", { name: "办公室" });
  expect(screen.queryByRole("button", { name: "可更新" })).toBeNull();
});

it("offers the Core-approved rollback in partner detail for an unsupported current package", async () => {
  fixture.rollbackAvailable = true;
  fixture.unsupported = true;
  open("/wakers/w1/plugins/p");
  fireEvent.click(await screen.findByRole("button", { name: "回滚到 0.9.0" }));
  expect(await screen.findByText("将回滚全部 1 个伙伴")).toBeVisible();
  expect(fixture.request).toHaveBeenLastCalledWith(
    expect.objectContaining({
      query: expect.objectContaining({ wakerId: "w1" }),
    }),
  );
});

it("keeps installed content visible after a failed background read and retries in place", async () => {
  const client = open("/resources/plugins/p");
  await screen.findByRole("heading", { name: "办公室" });
  fixture.readError = true;
  await act(() => client.invalidateQueries());
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByText("按任务整理笔记，协助处理办公资料。")).toBeVisible();
  fixture.readError = false;
  fireEvent.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
});

it("reads partner scope without accepting a global version override", async () => {
  open("/wakers/w1/plugins/p?version=other-version");
  await screen.findByRole("heading", { name: "办公室" });
  expect(fixture.request).toHaveBeenCalledWith(
    expect.objectContaining({
      query: expect.objectContaining({ wakerId: "w1", versionId: undefined }),
    }),
  );
  expect(
    screen.queryByRole("heading", { name: "管理" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "删除资源" }),
  ).not.toBeInTheDocument();
});

it.each([undefined, "/resources?query=office"])(
  "returns to the unified resource list preserving the supplied search: %s",
  async (resourceBack) => {
    open("/resources/plugins/p", resourceBack);
    await screen.findByRole("heading", { name: "办公室" });
    fireEvent.click(screen.getByRole("button", { name: "返回资源库" }));
    expect(screen.getByTestId("current-route")).toHaveTextContent(
      resourceBack ?? "/resources",
    );
  },
);

it("opens global deletion from the final section with the original resource scope", async () => {
  open("/resources/plugins/p");
  await screen.findByRole("heading", { name: "办公室" });
  const remove = screen.getByRole("button", { name: "删除资源" });
  expect(screen.getAllByRole("heading", { level: 2 }).at(-1)).toHaveTextContent(
    "危险操作",
  );
  fireEvent.click(remove);
  expect(screen.getByRole("dialog")).toHaveTextContent("resource:");
});

it.each(["/resources/plugins/p", "/wakers/w1/plugins/p"])(
  "%s exposes an unsupported snapshot as unavailable while preserving removal",
  async (path) => {
    fixture.unsupported = true;
    fixture.updateAvailable = true;
    open(path);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "当前插件包不受支持，请从插件目录重新安装。",
    );
    expect(screen.getByText("不可用")).toBeVisible();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: /安装到伙伴|可更新|编辑插件|查看技能|查看连接器/,
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "插件信息" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("按任务整理笔记，协助处理办公资料。"),
    ).not.toBeInTheDocument();
    const remove = screen.getByRole("button", {
      name: path.startsWith("/resources") ? "删除资源" : "从伙伴卸载",
    });
    fireEvent.click(remove);
    expect(screen.getByRole("dialog")).toHaveTextContent(
      path.startsWith("/resources") ? "resource:" : "installation:w1",
    );
  },
);
