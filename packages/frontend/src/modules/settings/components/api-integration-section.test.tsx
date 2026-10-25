import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { delay, http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import { integrationServiceQueryKey } from "@/shared/api/integration-service";
import { APIIntegrationSection } from "./api-integration-section";
const origin = "http://integration-scene.test";
const server = setupServer(
  http.get(`${origin}/api/v1/integrations/service`, () =>
    HttpResponse.json({
      enabled: true,
      mcpEnabled: false,
      available: true,
      ip: "127.0.0.1",
      port: 8431,
      publicBaseUrl: "https://example.test/proxy/api/v1",
    }),
  ),
  http.get(`${origin}/api/v1/integrations/keys`, () =>
    HttpResponse.json({ resources: [], totalSize: 0 }),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
function show() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <APIIntegrationSection />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, queryClient };
}
it("服务状态和客户端地址清晰展示，不再提供技术文档区", async () => {
  show();
  expect(screen.getByRole("heading", { name: "API 集成" })).toBeVisible();
  expect(screen.getByRole("button", { name: "创建 API Key" })).toBeVisible();
  const overview = await screen.findByRole("status", {
    name: "API 服务状态",
  });
  expect(within(overview).queryByText("API 服务可用")).not.toBeInTheDocument();
  expect(within(overview).getByText("运行中")).toBeVisible();
  expect(
    within(overview).getByText("https://example.test/proxy/api/v1"),
  ).toBeVisible();
  expect(
    within(overview).getByRole("button", { name: "复制访问地址" }),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "连接设置" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  expect(screen.queryByLabelText("端口")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "API 文档" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("查看接入信息")).not.toBeInTheDocument();
  expect(await screen.findByText("尚未创建 API Key。")).toBeVisible();
});
it("监听切换失败保留有效地址与草稿，并可就近重试", async () => {
  server.use(
    http.put(`${origin}/api/v1/integrations/service`, () =>
      HttpResponse.json({ message: "occupied" }, { status: 503 }),
    ),
  );
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "连接设置" }));
  const port = await screen.findByLabelText("端口");
  await user.clear(port);
  await user.type(port, "18431");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(port).toHaveValue(18431);
  await user.click(screen.getByRole("button", { name: "连接设置" }));
  await user.click(screen.getByRole("button", { name: "连接设置" }));
  expect(screen.getByLabelText("端口")).toHaveValue(18431);
  const overview = screen.getByRole("status", { name: "API 服务状态" });
  expect(
    within(overview).getByText("https://example.test/proxy/api/v1"),
  ).toBeVisible();
});

it("密钥只提供修改和删除，删除后列表移除该密钥", async () => {
  let deleted = false;
  server.use(
    http.get(`${origin}/api/v1/integrations/keys`, () =>
      HttpResponse.json({
        resources: deleted
          ? []
          : [
              {
                id: "key-a",
                name: "团队检索",
                displayPrefix: "sk-test",
                enabled: true,
                revision: 3,
                grants: [
                  {
                    resourceKind: "knowledge",
                    resourceIds: ["kb-a"],
                    capabilities: ["retrieve"],
                  },
                ],
              },
            ],
        totalSize: deleted ? 0 : 1,
      }),
    ),
    http.get(`${origin}/api/v1/integrations/resources`, () =>
      HttpResponse.json({
        resources: [{ id: "kb-a", name: "团队知识库" }],
        totalSize: 1,
      }),
    ),
    http.delete(
      `${origin}/api/v1/integrations/keys/key-a`,
      async ({ request }) => {
        expect(await request.json()).toEqual({ expectedRevision: 3 });
        deleted = true;
        return HttpResponse.json({});
      },
    ),
  );
  const user = userEvent.setup();
  show();
  expect(await screen.findByText("团队检索")).toBeVisible();
  expect(screen.queryByText("团队知识库")).not.toBeInTheDocument();
  expect(screen.getByRole("table", { name: "访问密钥" })).toBeVisible();
  expect(screen.getByRole("button", { name: "修改" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "启用" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "撤销" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "轮换" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "删除" }));
  await user.click(await screen.findByRole("button", { name: "确认删除" }));
  expect(await screen.findByText("尚未创建 API Key。")).toBeVisible();
});

it("API 集成同时展示 MCP Switch 和密钥列表，共享服务请求", async () => {
  const read = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/integrations/service`, () => {
      read();
      return HttpResponse.json({
        enabled: true,
        mcpEnabled: false,
        available: true,
      });
    }),
  );
  show();
  expect(await screen.findByText("尚未创建 API Key。")).toBeVisible();
  await waitFor(() =>
    expect(screen.getByRole("switch", { name: "启用 MCP" })).toBeEnabled(),
  );
  expect(screen.getByRole("heading", { name: "MCP" })).toBeVisible();
  expect(read).toHaveBeenCalledOnce();
  expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
});

it("共享服务失败时只显示一次错误和重试，密钥区域仍正常显示", async () => {
  server.use(
    http.get(`${origin}/api/v1/integrations/service`, () =>
      HttpResponse.json({}, { status: 503 }),
    ),
  );
  show();
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(await screen.findByText("尚未创建 API Key。")).toBeVisible();
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getAllByRole("button", { name: "重试" })).toHaveLength(1);
  expect(screen.getByRole("switch", { name: "启用 MCP" })).toBeDisabled();
});

it("API 保存仅刷新共享设置，保留 MCP 偏好，关闭再开启父开关恢复配置", async () => {
  let apiEnabled = true;
  const settings = () => ({
    enabled: apiEnabled,
    mcpEnabled: true,
    available: apiEnabled,
    ip: "127.0.0.1",
    port: 8431,
    publicBaseUrl: "https://example.test/proxy/api/v1",
  });
  let refreshing = false;
  server.use(
    http.get(`${origin}/api/v1/integrations/service`, async () => {
      if (refreshing) await delay(50);
      return HttpResponse.json(settings());
    }),
    http.put(`${origin}/api/v1/integrations/service`, async ({ request }) => {
      const body = await request.json();
      expect(body).not.toHaveProperty("mcpEnabled");
      apiEnabled = !apiEnabled;
      refreshing = true;
      return HttpResponse.json({});
    }),
  );
  const user = userEvent.setup();
  const { queryClient } = show();
  expect(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "连接设置" }));
  await user.click(screen.getByRole("switch", { name: "启用 API 服务" }));
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByText("请先开启 API 服务。")).toBeVisible();
  expect(screen.getByRole("switch", { name: "启用 MCP" })).toBeDisabled();
  expect(screen.getByRole("switch", { name: "启用 MCP" })).toBeChecked();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存" })).toBeEnabled(),
  );
  expect(
    queryClient
      .getQueryCache()
      .findAll({ queryKey: integrationServiceQueryKey() }),
  ).toHaveLength(1);
  await user.click(screen.getByRole("switch", { name: "启用 API 服务" }));
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  ).toBeVisible();
  expect(screen.getByRole("switch", { name: "启用 MCP" })).toBeChecked();
});
