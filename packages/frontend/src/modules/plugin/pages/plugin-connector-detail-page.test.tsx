import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { PluginConnectorDetailPage } from "./plugin-connector-detail-page";

const read = vi.hoisted(() => vi.fn());
const probe = vi.hoisted(() => vi.fn());
const readCatalog = vi.hoisted(() => vi.fn());
const parentState = vi.hoisted(() => ({
  error: false,
  value: {
    plugin: { id: "p", name: "GitHub" },
    installations: [] as Array<{
      wakerId: string;
      enabled: boolean;
      versionId: string;
    }>,
    versions: [
      {
        id: "v-old",
        version: "1.2.3",
        package: { manifest: { name: "github" } },
        provenance: {
          sourceCommit: "a".repeat(40),
          archiveSHA256: "b".repeat(64),
        },
      },
    ],
  },
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("../api/plugin-queries", () => ({
  listWakersOptions: () => ({
    queryKey: ["picker-partners"],
    queryFn: async () => ({ resources: [] }),
  }),
  getWakerOptions: () => ({
    queryKey: ["picker-owner"],
    queryFn: async () => ({}),
  }),
  testPluginConnectorMutation: () => ({ mutationFn: probe }),
  getCatalogPluginConnectorOptions: (options: unknown) => ({
    queryKey: ["catalog", options],
    queryFn: () => readCatalog(options),
  }),
  getPluginConnectorOptions: (options: unknown) => ({
    queryKey: ["plugin-connector", options],
    queryFn: () => read(options),
  }),
  getPluginOptions: () => ({
    queryKey: ["parent"],
    queryFn: async () => {
      if (parentState.error) throw new Error("fixture plugin detail failure");
      return parentState.value;
    },
  }),
}));
afterEach(() => {
  cleanup();
  read.mockReset();
  probe.mockReset();
  readCatalog.mockReset();
  parentState.value.installations = [];
  parentState.error = false;
});
function NavigationState() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="destination">
        {JSON.stringify({
          path: location.pathname + location.search,
          state: location.state,
        })}
      </output>
      <button
        data-testid="switch-component"
        onClick={() =>
          navigate("/resources/plugins/p/connectors/other?version=v-old")
        }
      >
        另一个组件
      </button>
    </>
  );
}
function open(path: string) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[path]}>
        <NavigationState />
        <Routes>
          <Route
            path="/resources/plugins/catalog/:catalogKey/connectors/:connectorKey"
            element={<PluginConnectorDetailPage />}
          />
          <Route
            path="/resources/plugins/:pluginId/connectors/:connectorKey"
            element={<PluginConnectorDetailPage />}
          />
          <Route
            path="/wakers/:wakerId/plugins/:pluginId/connectors/:connectorKey"
            element={<PluginConnectorDetailPage />}
          />
          <Route path="/resources/plugins/p" element={<p>所属插件详情</p>} />
          <Route path="/chat" element={<p>连接器聊天草稿</p>} />
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
  expect(screen.getByRole("link", { name: "插件" })).toHaveAttribute(
    "href",
    path.startsWith("/wakers")
      ? "/wakers/w1/resources?category=plugins"
      : "/resources?category=plugins",
  );
  expect(screen.getByRole("navigation", { name: "插件" })).toBeVisible();
  expect(screen.getByRole("heading", { name: "连接器信息" })).toBeVisible();
  expect(
    screen.queryByText("Codex", { selector: "span" }),
  ).not.toBeInTheDocument();
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
  await screen.findByRole("heading", { name: "连接器信息" });
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

it("reads the pinned catalog snapshot and disables probing without a matching local installation", async () => {
  readCatalog.mockResolvedValue({
    connector: { id: "c", name: "github", testStatus: "untested" },
  });
  open(
    `/resources/plugins/catalog/github/connectors/github?sourceCommit=${"a".repeat(40)}&archiveSHA256=${"b".repeat(64)}`,
  );
  await screen.findByRole("heading", { name: "连接器信息" });
  expect(screen.getByRole("button", { name: "测试连接" })).toBeDisabled();
  expect(readCatalog).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { key: "github" },
      query: {
        connectorKey: "github",
        sourceCommit: "a".repeat(40),
        archiveSHA256: "b".repeat(64),
      },
    }),
  );
  expect(read).not.toHaveBeenCalled();
  expect(probe).not.toHaveBeenCalled();
});

it("does not fetch a catalog connector without both snapshot fields", () => {
  open(
    `/resources/plugins/catalog/github/connectors/github?sourceCommit=${"a".repeat(40)}`,
  );
  expect(screen.getByText("没有找到这个连接器")).toBeVisible();
  expect(readCatalog).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
});

it.each([true, false])(
  "probes a catalog connector only when local provenance matches: %s",
  async (matches) => {
    readCatalog.mockResolvedValue({
      connector: { id: "c", name: "github", testStatus: "untested" },
    });
    probe.mockResolvedValue({});
    open(
      `/resources/plugins/catalog/github/connectors/github?sourceCommit=${(matches ? "a" : "c").repeat(40)}&archiveSHA256=${"b".repeat(64)}&pluginId=p&version=v-old`,
    );
    await screen.findByRole("heading", { name: "连接器信息" });
    const button = screen.getByRole("button", { name: "测试连接" });
    if (!matches) {
      expect(button).toBeDisabled();
      expect(probe).not.toHaveBeenCalled();
      expect(screen.queryByText("GitHub")).not.toBeInTheDocument();
      expect(screen.queryByText("1.2.3")).not.toBeInTheDocument();
      expect(screen.getByText("c".repeat(12))).toBeVisible();
      return;
    }
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    await waitFor(() =>
      expect(probe).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { pluginId: "p" },
          query: {
            versionId: "v-old",
            key: "github",
            wakerId: undefined,
            sourceCommit: "a".repeat(40),
            archiveSHA256: "b".repeat(64),
          },
        }),
        expect.anything(),
      ),
    );
  },
);

it("prepares a plugin reference from a fixed MCP only for the actual enabled current installation", async () => {
  parentState.value.installations = [
    { wakerId: "w1", enabled: true, versionId: "v-old" },
  ];
  read.mockResolvedValue({
    connector: { id: "c", name: "github", activationPolicy: "per_execution" },
  });
  open("/wakers/w1/plugins/p/connectors/github?version=v-old");
  await screen.findByRole("heading", { name: "当前伙伴" });
  const arrow = screen.getByRole("button", { name: "帮我使用这个连接器。" });
  await waitFor(() => expect(arrow).toBeEnabled());
  fireEvent.click(arrow);
  expect(
    JSON.parse(screen.getByTestId("destination").textContent ?? "{}").state
      .chatDraftIntent,
  ).toMatchObject({
    pluginId: "p",
    wakerId: "w1",
    text: "帮我使用这个连接器。",
  });
});

it.each(["disabled", "different version", "details unavailable"])(
  "does not offer a current-partner plugin reference when %s",
  async (reason) => {
    parentState.value.installations = [
      {
        wakerId: "w1",
        enabled: reason !== "disabled",
        versionId: reason === "different version" ? "new-version" : "v-old",
      },
    ];
    parentState.error = reason === "details unavailable";
    read.mockResolvedValue({
      connector: { id: "c", name: "github", activationPolicy: "per_execution" },
    });
    open("/wakers/w1/plugins/p/connectors/github?version=v-old");
    await screen.findByRole("heading", { name: "连接器信息" });
    expect(
      screen.getByRole("button", { name: "帮我使用这个连接器。" }),
    ).toBeDisabled();
  },
);

it("closes a retained partner picker when navigating to another MCP in the same plugin version", async () => {
  read.mockResolvedValue({
    connector: { id: "c", name: "github", activationPolicy: "per_execution" },
  });
  open("/resources/plugins/p/connectors/github?version=v-old");
  await screen.findByRole("heading", { name: "连接器信息" });
  const arrow = screen.getByRole("button", { name: "帮我使用这个连接器。" });
  await waitFor(() => expect(arrow).toBeEnabled());
  fireEvent.click(arrow);
  expect(screen.getByRole("dialog", { name: "选择试用伙伴" })).toBeVisible();
  fireEvent.click(screen.getByTestId("switch-component"));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});
