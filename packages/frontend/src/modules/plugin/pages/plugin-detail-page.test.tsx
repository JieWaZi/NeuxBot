import {
  render,
  act,
  screen,
  fireEvent,
  cleanup,
  within,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { it, expect, vi, afterEach } from "vitest";
import { i18n } from "@/shared/i18n";
import { PluginDetailPage } from "./plugin-detail-page";

const { togglePlugin, renamePlugin, fixture } = vi.hoisted(() => ({
  fixture: { updateAvailable: false },
  togglePlugin: vi.fn(),
  renamePlugin: vi.fn<(input: unknown) => Promise<{ success: boolean }>>(
    async () => ({ success: true }),
  ),
}));

vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("../api/plugin-queries", () => ({
  cancelPluginAuthorizationMutation: () => ({ mutationFn: vi.fn() }),
  prepareCatalogPluginMutation: () => ({ mutationFn: vi.fn() }),
  getPluginOptions: (options: { query?: { locale?: string } }) => ({
    queryKey: ["test-plugin", options.query?.locale],
    queryFn: async () => ({
      plugin: { id: "p", name: "办公室" },
      resolvedVersionId: "v1",
      ready: true,
      update: {
        available: fixture.updateAvailable,
        catalogKey: "office",
        archiveSHA256: "fixed",
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
    }),
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
afterEach(async () => {
  cleanup();
  vi.clearAllMocks();
  fixture.updateAvailable = false;
  await i18n.changeLanguage("zh-CN");
});
function open(path: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
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

it("shows partner plugin information and keeps enable controls in the page header", async () => {
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
  expect(uninstall).toHaveClass("text-destructive");
  expect(uninstall).not.toHaveTextContent("从伙伴卸载");
  fireEvent.click(uninstall);
  expect(screen.getByRole("dialog")).toHaveTextContent("installation:w1");
});
it("global detail keeps its reading layout and opens shared settings from the edit action", async () => {
  open("/resources/plugins/p?tab=configuration");
  await screen.findByRole("heading", { name: "办公室" });
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  const summary = screen.getByText("整理笔记和办公资料");
  expect(summary).toHaveClass("truncate");
  expect(summary).toHaveAttribute("title", "整理笔记和办公资料");
  expect(
    screen.queryByRole("textbox", { name: "展示名称" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "编辑插件" }));
  const dialog = await screen.findByRole("dialog", { name: "编辑 办公室" });
  expect(within(dialog).getByRole("textbox", { name: "展示名称" })).toHaveValue(
    "办公室",
  );
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
    expect(screen.getByText("将更新全部 1 个伙伴")).toBeVisible();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  },
);
