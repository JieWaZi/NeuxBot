import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginConnectorDetailPage } from "./plugin-connector-detail-page";

const read = vi.hoisted(() => vi.fn());
const probe = vi.hoisted(() => vi.fn());
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("../api/plugin-queries", () => ({
  testPluginConnectorMutation: () => ({ mutationFn: probe }),
  getCatalogPluginConnectorOptions: () => ({
    queryKey: ["catalog"],
    queryFn: async () => ({}),
  }),
  getPluginConnectorOptions: (options: unknown) => ({
    queryKey: ["plugin-connector", options],
    queryFn: () => read(options),
  }),
  getPluginOptions: () => ({
    queryKey: ["parent"],
    queryFn: async () => ({
      plugin: { name: "GitHub" },
      versions: [{ id: "v-old", package: { manifest: { name: "github" } } }],
    }),
  }),
}));
afterEach(() => {
  cleanup();
  read.mockReset();
  probe.mockReset();
});
function open(path: string) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="/resources/plugins/:pluginId/connectors/:connectorKey"
            element={<PluginConnectorDetailPage />}
          />
          <Route
            path="/wakers/:wakerId/plugins/:pluginId/connectors/:connectorKey"
            element={<PluginConnectorDetailPage />}
          />
          <Route path="/resources/plugins/p" element={<p>所属插件详情</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it.each([
  "/resources/plugins/p/connectors/github",
  "/wakers/w1/plugins/p/connectors/github",
])("shows pinned read-only information at %s", async (path) => {
  read.mockResolvedValue({
    connector: {
      id: "c",
      name: "github",
      transport: "streamable_http",
      url: "https://example.com/mcp",
      testTools: [],
      testStatus: "untested",
    },
    compatibility: [{ runtimeId: "codex", supported: true }],
  });
  open(path + "?version=v-old");
  expect(await screen.findByText("https://example.com/mcp")).toBeVisible();
  expect(screen.getByRole("heading", { name: "连接配置" })).toBeVisible();
  expect(screen.getByText("Codex", { selector: "span" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "编辑" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "删除连接器" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(read).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "p" },
      query: {
        versionId: "v-old",
        key: "github",
        wakerId: path.startsWith("/wakers") ? "w1" : undefined,
      },
    }),
  );
});
it("does not offer a view-plugin action", async () => {
  read.mockResolvedValue({ connector: { id: "c", name: "github" } });
  open("/resources/plugins/p/connectors/github?version=v-old");
  await screen.findByRole("heading", { name: "github" });
  expect(
    screen.queryByRole("link", { name: "查看插件" }),
  ).not.toBeInTheDocument();
});

it("does not query an unspecified version", () => {
  open("/resources/plugins/p/connectors/github");
  expect(screen.getByText("没有找到这个连接器")).toBeVisible();
  expect(read).not.toHaveBeenCalled();
});
it("shows a local error when the pinned version is unavailable", async () => {
  read.mockRejectedValue(new Error("固定版本不可用"));
  open("/resources/plugins/p/connectors/github?version=v-old");
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "详情读取失败，请重试。",
  );
});

it("probes the saved version on demand and displays discovered tools", async () => {
  read.mockResolvedValue({
    connector: {
      id: "c",
      name: "github",
      transport: "streamable_http",
      url: "https://example.com/mcp",
      testStatus: "untested",
    },
  });
  probe.mockImplementation(async () => {
    const result = {
      connector: {
        id: "c",
        name: "github",
        transport: "streamable_http",
        url: "https://example.com/mcp",
        testStatus: "succeeded",
        testedAt: Date.now(),
        testDurationMs: 25,
        testToolCount: 1,
        testTools: [{ name: "search_repositories", description: "搜索仓库" }],
      },
    };
    read.mockResolvedValue(result);
    return result;
  });
  open("/resources/plugins/p/connectors/github?version=v-old");
  await screen.findByText("https://example.com/mcp");
  expect(probe).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
  expect(
    await screen.findByRole("heading", { name: "search_repositories" }),
  ).toBeVisible();
  expect(probe).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { pluginId: "p" },
      query: expect.objectContaining({ versionId: "v-old", key: "github" }),
    }),
    expect.anything(),
  );
});

it("reconciles a late probe response with current server state", async () => {
  read.mockResolvedValue({
    connector: { id: "c", name: "github", testStatus: "untested" },
  });
  let finish: ((value: object) => void) | undefined;
  probe.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  open("/resources/plugins/p/connectors/github?version=v-old");
  await screen.findByRole("heading", { name: "连接配置" });
  fireEvent.click(screen.getByRole("button", { name: "测试连接" }));
  await waitFor(() => expect(probe).toHaveBeenCalled());
  // 服务端身份已变化，旧 POST 回包中的工具不能覆盖新的 GET 结果。
  read.mockResolvedValue({
    connector: {
      id: "c",
      name: "github",
      testStatus: "untested",
      testTools: [],
    },
  });
  finish?.({
    connector: {
      id: "c",
      name: "github",
      testStatus: "succeeded",
      testTools: [{ name: "stale_private_tool" }],
    },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "测试连接" })).toBeEnabled(),
  );
  expect(
    screen.queryByRole("heading", { name: "stale_private_tool" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("还没有工具信息")).toBeVisible();
});
