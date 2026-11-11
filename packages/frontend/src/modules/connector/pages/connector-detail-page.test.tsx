import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import {
  Link,
  MemoryRouter,
  Route,
  Routes,
  useLocation,
} from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";

import "@/shared/i18n";

import type { ConnectorRecord, ConnectorView } from "../model/types";
import { ConnectorDetailPage } from "./connector-detail-page";

const { state, remove, testConnection, setEnabled } = vi.hoisted(() => {
  const state: {
    view: ConnectorView;
    error: Error | null;
    pending: boolean;
  } = { view: {}, error: null, pending: false };
  type Request = { path: { connectorId: string; wakerId: string } };
  return {
    state,
    remove: vi.fn<(request: Request) => Promise<{ success: boolean }>>(),
    testConnection:
      vi.fn<(request: Request) => Promise<{ success: boolean }>>(),
    setEnabled:
      vi.fn<
        (
          request: Request & { body: { enabled: boolean } },
        ) => Promise<{ success: boolean }>
      >(),
  };
});

vi.mock("@/shared/workspace/app-scene-workspace", () => ({
  useRouteSceneActive: () => true,
}));
vi.mock("@/shared/api/use-query-events", () => ({
  useQueryEvents: () => undefined,
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("../components/connector-form-dialog", () => ({
  ConnectorFormDialog: ({
    open,
    connector,
  }: {
    open: boolean;
    connector: ConnectorRecord;
  }) =>
    open ? (
      <div role="dialog" aria-label="编辑连接器">
        {connector.name}
      </div>
    ) : null,
}));
vi.mock(
  "@/shared/api/generated/@tanstack/react-query.gen",
  async (importOriginal) => ({
    ...(await importOriginal<object>()),
    getWakerOptions: () => ({
      queryKey: ["current-partner"],
      queryFn: async () => ({ id: "owner", name: "研究伙伴" }),
    }),
  }),
);
vi.mock("../api/connector-queries", () => ({
  getWakerConnectorOptions: (options: {
    path: { connectorId: string; wakerId: string };
  }) => ({
    queryKey: ["connector-detail", options.path],
    queryFn: async () => {
      if (state.pending) return new Promise<ConnectorView>(() => {});
      if (state.error) throw state.error;
      return state.view;
    },
  }),
  setWakerConnectorEnabledMutation: () => ({ mutationFn: setEnabled }),
  testWakerConnectorMutation: () => ({ mutationFn: testConnection }),
  deleteWakerConnectorMutation: () => ({ mutationFn: remove }),
  invalidateVisibleConnectorQueries: async () => undefined,
}));

beforeEach(() => {
  state.error = null;
  state.pending = false;
  state.view = {
    connector: {
      id: "connector-1",
      name: "研究服务",
      description: "检索资料",
      scope: "public",
      transport: "stdio",
      command: "/Applications/research-server/bin/start-service",
      arguments: ["--workspace", "/Users/example/project files"],
      timeoutMs: 12000,
      environment: [{ id: "token", name: "API_TOKEN", configured: true }],
      testStatus: "succeeded",
      testedAt: 1791792000000,
      testDurationMs: 54,
      testToolCount: 1,
      testTools: [
        { name: "search_documents", description: "查找工作范围内的文档。" },
      ],
    },
    enabled: true,
    affectedWakerCount: 3,
    compatibility: [
      { runtimeId: "codex", supported: true },
      { runtimeId: "claude", supported: false },
    ],
  };
  for (const mutation of [remove, testConnection, setEnabled]) {
    mutation.mockReset();
    mutation.mockResolvedValue({ success: true });
  }
});

function CurrentLocation() {
  const location = useLocation();
  return (
    <span data-testid="current-location">
      {location.pathname}
      {location.search}
    </span>
  );
}

function renderPage(
  entry:
    | string
    | {
        pathname: string;
        state: { resourceBack?: string };
      } = "/wakers/owner/connectors/connector-1",
) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false, gcTime: 0 },
            mutations: { retry: false },
          },
        })
      }
    >
      <MemoryRouter initialEntries={[entry]}>
        <CurrentLocation />
        <Link to="/wakers/owner/connectors/connector-2">另一个连接器</Link>
        <Routes>
          <Route
            path="/wakers/:wakerId/connectors/:connectorId"
            element={<ConnectorDetailPage />}
          />
          <Route
            path="/resources/connectors/:connectorId"
            element={<ConnectorDetailPage />}
          />
          <Route path="/resources" element={<p>全局连接器列表</p>} />
          <Route
            path="/wakers/:wakerId/connectors"
            element={<p>直接连接器列表</p>}
          />
          <Route
            path="/wakers/:wakerId/resources"
            element={<p>连接器列表</p>}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function connectionTestResults() {
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "连接测试详情" }));
  const section = screen.getByRole("region", { name: "连接测试" });
  return within(section);
}

it("公开连接器保留配置、工具说明与定向测试、编辑和启停操作", async () => {
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole("heading", { name: "研究服务" });
  expect(
    screen.getByText("/Applications/research-server/bin/start-service"),
  ).toBeVisible();
  expect(
    screen.getByText("--workspace /Users/example/project files"),
  ).toBeVisible();
  expect(screen.getByText("API_TOKEN")).toBeVisible();
  expect(screen.getByText("已配置")).toBeVisible();
  expect(
    screen.getByRole("heading", { name: "search_documents" }),
  ).toBeVisible();
  expect(screen.getByText("查找工作范围内的文档。")).toBeVisible();
  expect(screen.queryByText("当前不支持")).not.toBeInTheDocument();
  expect(screen.queryByText("Codex")).not.toBeInTheDocument();
  const results = await connectionTestResults();
  expect(results.getByText("状态")).toBeVisible();
  expect(results.getByText("测试时间")).toBeVisible();
  expect(results.getByText("耗时")).toBeVisible();
  expect(results.getByText("测试通过")).toBeVisible();
  expect(results.getByText("54 ms")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "测试连接" }));
  await waitFor(() =>
    expect(testConnection.mock.calls[0]?.[0]).toMatchObject({
      path: { connectorId: "connector-1", wakerId: "owner" },
    }),
  );
  await user.click(screen.getByRole("switch", { name: "停用 研究服务" }));
  await waitFor(() =>
    expect(setEnabled.mock.calls[0]?.[0]).toMatchObject({
      path: { connectorId: "connector-1", wakerId: "owner" },
      body: { enabled: false },
    }),
  );
  await user.click(screen.getByRole("button", { name: "编辑" }));
  expect(screen.getByRole("dialog", { name: "编辑连接器" })).toHaveTextContent(
    "研究服务",
  );
});

it("伙伴移除共享连接器只确认当前关系影响，确认后回到伙伴列表", async () => {
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole("heading", { name: "研究服务" });
  await user.click(screen.getByRole("button", { name: "移除连接器" }));
  const dialog = screen.getByRole("dialog", { name: "移除连接器" });
  expect(within(dialog).getByText(/当前伙伴移除/)).toBeVisible();
  expect(within(dialog).queryByText(/从 3 个伙伴中移除/)).toBeNull();
  expect(remove).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "确认移除" }));
  await waitFor(() =>
    expect(remove.mock.calls[0]?.[0]).toMatchObject({
      path: { connectorId: "connector-1", wakerId: "owner" },
    }),
  );
  expect(await screen.findByText("连接器列表")).toBeVisible();
});

it("系统按场景启用的内置连接器保留完整配置与未测试字段，隐藏启停和删除", async () => {
  state.view = {
    connector: {
      id: "connector-1",
      name: "工作流",
      scope: "built_in",
      transport: "sse",
      activationPolicy: "workflow_context",
      testStatus: "untested",
      url: "http://127.0.0.1:8432/workflow",
    },
    enabled: true,
  };
  renderPage();
  await screen.findByRole("heading", { name: "工作流" });
  expect(screen.getByText("由系统按场景启用")).toBeVisible();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "删除连接器" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "编辑" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("http://127.0.0.1:8432/workflow")).toBeVisible();
  expect(screen.getByRole("heading", { name: "连接器信息" })).toBeVisible();
  expect(screen.getByText("10 秒")).toBeVisible();
  expect(screen.getByText("没有配置项")).toBeVisible();
  const results = await connectionTestResults();
  expect(results.getByText("状态")).toBeVisible();
  expect(results.getByText("测试时间")).toBeVisible();
  expect(results.getByText("尚未测试")).toBeVisible();
  expect(results.getByText("耗时")).toBeVisible();
  expect(results.getByText("—")).toBeVisible();
  expect(screen.getByRole("button", { name: "测试连接" })).toBeEnabled();
  expect(screen.getByText("还没有工具信息")).toBeVisible();
  expect(screen.queryByText("暂无兼容信息")).not.toBeInTheDocument();
  expect(screen.queryByText("0 个工具")).not.toBeInTheDocument();
});

it("内置连接器保留完整配置、凭据状态与编辑入口", async () => {
  const user = userEvent.setup();
  state.view = {
    connector: {
      id: "connector-1",
      name: "凭据服务",
      scope: "built_in",
      transport: "stdio",
      command: "/internal/ally-managed-command",
      timeoutMs: 12345,
      environment: [{ id: "key", name: "ACCESS_KEY", configured: false }],
    },
    enabled: true,
  };
  renderPage();
  await screen.findByRole("heading", { name: "凭据服务" });
  expect(screen.getByRole("heading", { name: "连接器信息" })).toBeVisible();
  expect(screen.getByText("ACCESS_KEY")).toBeVisible();
  expect(screen.getByText("未配置")).toBeVisible();
  expect(screen.getByText("/internal/ally-managed-command")).toBeVisible();
  expect(screen.getByText("12.345 秒")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "编辑" }));
  expect(screen.getByRole("dialog", { name: "编辑连接器" })).toHaveTextContent(
    "凭据服务",
  );
});

it("请求等待和失败时保留页头，错误可重试且不会被当作未找到", async () => {
  const user = userEvent.setup();
  state.error = new Error("request failed");
  renderPage();
  expect(screen.getByRole("heading", { name: "连接器" })).toBeVisible();
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByText("没有找到这个连接器")).not.toBeInTheDocument();
  state.error = null;
  await user.click(screen.getByRole("button", { name: "重试" }));
  expect(
    await screen.findByRole("heading", { name: "研究服务" }),
  ).toBeVisible();
});

it("冷加载在内容区显示状态，保留页头且禁用数据操作", () => {
  state.pending = true;
  renderPage();
  expect(screen.getByRole("heading", { name: "连接器" })).toBeVisible();
  expect(screen.getByRole("status")).toBeVisible();
  expect(screen.getByRole("button", { name: "编辑" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "移除连接器" }),
  ).not.toBeInTheDocument();
});

it("keeps tool names and descriptions on one line with complete hover text", async () => {
  const name = "very_long_tool_name_".repeat(8);
  const description =
    "A complete tool description that should be available on hover. ".repeat(
      12,
    );
  state.view.connector = {
    ...state.view.connector,
    testTools: [{ name, description }],
  };
  renderPage();
  const heading = await screen.findByRole("heading", { name });
  const text = screen.getByText(description.trim());
  expect(heading).toHaveClass("truncate");
  expect(heading).toHaveAttribute("title", name);
  expect(text).toHaveClass("truncate");
  expect(text).toHaveAttribute("title", description);
  expect(text.className).not.toContain("max-w-");
});

it("global deletion confirms the actual affected count and does not treat context-free enabled as disabled", async () => {
  const user = userEvent.setup();
  state.view.enabled = false;
  renderPage("/resources/connectors/connector-1");
  await screen.findByRole("heading", { name: "研究服务" });
  expect(screen.queryByText("已停用")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "删除连接器" }));
  const dialog = screen.getByRole("dialog", { name: "删除连接器" });
  expect(within(dialog).getByText(/从 3 个伙伴中移除/)).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
  await waitFor(() =>
    expect(remove).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectorId: "connector-1", wakerId: "" },
        query: { confirmed: true, expectedAffectedCount: 3 },
      }),
      expect.anything(),
    ),
  );
});

it("deletes a partner-owned connector without describing unrelated partner impact", async () => {
  const user = userEvent.setup();
  state.view.connector = {
    ...state.view.connector,
    scope: "private",
    ownerWakerId: "owner",
  };
  renderPage();
  await screen.findByRole("heading", { name: "研究服务" });
  await user.click(screen.getByRole("button", { name: "删除连接器" }));
  const dialog = screen.getByRole("dialog", { name: "删除连接器" });
  expect(within(dialog).getByText(/当前伙伴自有连接器/)).toBeVisible();
  expect(within(dialog).queryByText(/从 3 个伙伴中移除/)).toBeNull();
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
  await waitFor(() =>
    expect(remove).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectorId: "connector-1", wakerId: "owner" },
        query: undefined,
      }),
      expect.anything(),
    ),
  );
});

it.each([
  [
    "/wakers/owner/connectors?query=research",
    "/wakers/owner/connectors?query=research",
  ],
  ["https://example.com", "/wakers/owner/resources?category=connectors"],
])(
  "returns only to an allowed list from %s",
  async (resourceBack, expected) => {
    const user = userEvent.setup();
    renderPage({
      pathname: "/wakers/owner/connectors/connector-1",
      state: { resourceBack },
    });
    await screen.findByRole("heading", { name: "研究服务" });
    await user.click(screen.getByRole("button", { name: "返回列表" }));
    expect(screen.getByTestId("current-location")).toHaveTextContent(expected);
  },
);

it("shows the current probe failure before an old saved failure", async () => {
  const user = userEvent.setup();
  state.view.connector = { ...state.view.connector, testError: "旧连接错误" };
  testConnection.mockRejectedValueOnce(new Error("network failed"));
  renderPage();
  await screen.findByRole("heading", { name: "研究服务" });
  await user.click(screen.getByRole("button", { name: "测试连接" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(
      "连接测试未完成，请稍后重试。",
    ),
  );
  expect(screen.queryByText("旧连接错误")).toBeNull();
});

it.each(["pending", "error"])(
  "keeps old probe and toggle %s with their connector identity",
  async (mode) => {
    const user = userEvent.setup();
    if (mode === "pending") {
      testConnection.mockImplementationOnce(() => new Promise(() => {}));
      setEnabled.mockImplementationOnce(() => new Promise(() => {}));
    } else {
      testConnection.mockRejectedValueOnce(new Error("probe failed"));
      setEnabled.mockRejectedValueOnce(new Error("toggle failed"));
    }
    renderPage();
    await screen.findByRole("heading", { name: "研究服务" });
    await user.click(screen.getByRole("button", { name: "测试连接" }));
    await user.click(screen.getByRole("switch", { name: "停用 研究服务" }));
    if (mode === "error")
      await waitFor(() => expect(screen.getAllByRole("alert")).toHaveLength(2));
    state.view.connector = {
      ...state.view.connector,
      id: "connector-2",
      name: "新连接器",
    };
    await user.click(screen.getByRole("link", { name: "另一个连接器" }));
    await screen.findByRole("heading", { name: "新连接器" });
    expect(screen.getByRole("button", { name: "测试连接" })).toBeEnabled();
    expect(screen.getByRole("switch", { name: "停用 新连接器" })).toBeEnabled();
    expect(screen.queryByRole("alert")).toBeNull();
  },
);

it("places the product TryCard before partner controls, tools and information with relationship removal in the partner row", async () => {
  renderPage();
  const title = await screen.findByRole("heading", { name: "研究服务" });
  const header = title.closest("header");
  expect(header).toContainElement(screen.getByRole("button", { name: "编辑" }));
  expect(header).not.toContainElement(
    screen.getByRole("button", { name: "移除连接器" }),
  );
  expect(
    screen
      .getAllByRole("heading", { level: 2 })
      .map((item) => item.textContent),
  ).toEqual(["当前伙伴", "工具列表", "连接器信息"]);
  const prompt = screen.getByRole("button", { name: "帮我使用这个连接器。" });
  const partner = screen.getByRole("heading", { name: "当前伙伴" });
  expect(
    prompt.compareDocumentPosition(partner) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(screen.queryByText("测试时间")).not.toBeInTheDocument();
  expect(screen.queryByText("耗时")).not.toBeInTheDocument();
  const details = screen.getByRole("button", { name: "连接测试详情" });
  details.focus();
  await userEvent.setup().keyboard("{Enter}");
  expect(screen.getByRole("region", { name: "连接测试" })).toBeVisible();
});

it("keeps system-managed connectors visible with an honestly disabled TryCard", async () => {
  state.view.connector = {
    ...state.view.connector,
    scope: "built_in",
    activationPolicy: "team_context",
  };
  renderPage();
  await screen.findByRole("heading", { name: "研究服务" });
  const prompt = screen.getByRole("button", { name: "帮我使用这个连接器。" });
  expect(prompt).toBeDisabled();
  expect(prompt).toHaveAttribute(
    "title",
    "此连接器由系统按场景使用，不能在普通聊天中试用。",
  );
});
