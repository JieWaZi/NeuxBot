import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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
vi.mock("../api/connector-queries", () => ({
  getWakerConnectorOptions: () => ({
    queryKey: ["connector-detail"],
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

function renderPage() {
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
      <MemoryRouter initialEntries={["/wakers/owner/connectors/connector-1"]}>
        <Routes>
          <Route
            path="/wakers/:wakerId/connectors/:connectorId"
            element={<ConnectorDetailPage />}
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

function connectionTestResults() {
  const section = screen.getByRole("region", { name: "连接测试" });
  return within(section);
}

it("公开连接器保留配置、工具说明与定向测试、编辑和启停操作", async () => {
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole("heading", { name: /研究服务.*全局/ });
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
  expect(screen.getByText("当前不支持")).toBeVisible();
  const results = connectionTestResults();
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

it("删除仍需确认全局影响，确认后回到当前伙伴列表", async () => {
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole("heading", { name: /研究服务.*全局/ });
  await user.click(screen.getByRole("button", { name: "删除连接器" }));
  const dialog = screen.getByRole("dialog", { name: "删除连接器" });
  expect(within(dialog).getByText(/从 3 个伙伴中移除/)).toBeVisible();
  expect(remove).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
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
  await screen.findByRole("heading", { name: /工作流.*内置/ });
  expect(screen.getByText("由系统按场景启用")).toBeVisible();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "删除连接器" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "编辑" }),
  ).not.toBeInTheDocument();
  expect(screen.getByText("http://127.0.0.1:8432/workflow")).toBeVisible();
  expect(screen.getByRole("heading", { name: "连接配置" })).toBeVisible();
  expect(screen.getByText("10000 ms")).toBeVisible();
  expect(screen.getByText("没有配置项")).toBeVisible();
  const results = connectionTestResults();
  expect(results.getByText("状态")).toBeVisible();
  expect(results.getByText("测试时间")).toBeVisible();
  expect(results.getByText("尚未测试")).toBeVisible();
  expect(results.getByText("耗时")).toBeVisible();
  expect(results.getByText("—")).toBeVisible();
  expect(screen.getByRole("button", { name: "测试连接" })).toBeEnabled();
  expect(screen.getByText("还没有工具信息")).toBeVisible();
  expect(screen.getByText("暂无兼容信息")).toBeVisible();
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
      timeoutMs: 30000,
      environment: [{ id: "key", name: "ACCESS_KEY", configured: false }],
    },
    enabled: true,
  };
  renderPage();
  await screen.findByRole("heading", { name: /凭据服务.*内置/ });
  expect(screen.getByRole("heading", { name: "连接配置" })).toBeVisible();
  expect(screen.getByText("ACCESS_KEY")).toBeVisible();
  expect(screen.getByText("未配置")).toBeVisible();
  expect(screen.getByText("/internal/ally-managed-command")).toBeVisible();
  expect(screen.getByText("30000 ms")).toBeVisible();
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
    await screen.findByRole("heading", { name: /研究服务.*全局/ }),
  ).toBeVisible();
});

it("冷加载在内容区显示状态，保留页头且禁用数据操作", () => {
  state.pending = true;
  renderPage();
  expect(screen.getByRole("heading", { name: "连接器" })).toBeVisible();
  expect(screen.getByRole("status")).toBeVisible();
  expect(screen.getByRole("button", { name: "编辑" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "删除连接器" })).toBeDisabled();
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
