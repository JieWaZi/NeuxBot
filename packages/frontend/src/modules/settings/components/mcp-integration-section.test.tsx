import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { delay, http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import {
  integrationServiceOptions,
  integrationServiceQueryKey,
} from "@/shared/api/integration-service";
import { MCPIntegrationSection } from "./mcp-integration-section";

const origin = "http://mcp-integration.test";
const endpoint = `${origin}/api/v1/integrations/service`;
const settings = {
  enabled: true,
  available: true,
  publicBaseUrl: "https://example.test/proxy/api/v1",
};
const server = setupServer(
  http.get(endpoint, () => HttpResponse.json(settings)),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
});
afterAll(() => server.close());

function show(onOpenAPI = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MCPIntegrationSection onOpenAPI={onOpenAPI} />
    </QueryClientProvider>,
  );
  return { queryClient, onOpenAPI };
}

it("复用发布状态和已保存地址，复制 URL、占位配置并跳转已有 Key 管理", async () => {
  const user = userEvent.setup();
  const { onOpenAPI } = show();
  const status = await screen.findByRole("status", {
    name: "MCP 发布服务状态",
  });
  expect(within(status).getByText("运行中")).toBeVisible();
  expect(screen.getByText("https://example.test/proxy/mcp")).toBeVisible();
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(screen.getByText(/retrieve \+ ingest/)).toBeVisible();
  expect(screen.getByText(/retrieve \+ organize/)).toBeVisible();
  expect(screen.getByText(/远程客户端必须能访问/)).toBeVisible();
  expect(
    screen.getByText(/关闭浏览器页面或桌面窗口不会停止服务/),
  ).toBeVisible();
  expect(screen.getByText(/真正退出管理该 Core 的原生应用/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "复制 MCP URL" }));
  expect(await navigator.clipboard.readText()).toBe(
    "https://example.test/proxy/mcp",
  );
  expect(screen.getByRole("button", { name: "MCP URL 已复制" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "复制 MCP 配置" }));
  expect(JSON.parse(await navigator.clipboard.readText())).toEqual({
    mcpServers: {
      neuxally: {
        url: "https://example.test/proxy/mcp",
        headers: { "X-API-Key": "<YOUR_API_KEY>" },
      },
    },
  });
  expect(screen.getByRole("button", { name: "MCP 配置已复制" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "前往 API 集成" }));
  expect(onOpenAPI).toHaveBeenCalledOnce();
});

it.each([
  [{ enabled: false, available: false }, "已停用"],
  [{ enabled: true, available: false, errorCode: "bind_failed" }, "不可用"],
  [{ enabled: true }, "状态未知"],
  [{ available: true }, "状态未知"],
])("不将停用、绑定失败或不完整状态冒充运行中：%j", async (state, label) => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({ ...state, publicBaseUrl: settings.publicBaseUrl }),
    ),
  );
  show();
  const status = await screen.findByRole("status", {
    name: "MCP 发布服务状态",
  });
  expect(within(status).getByText(label)).toBeVisible();
  expect(within(status).queryByText("运行中")).not.toBeInTheDocument();
});

it("回环地址提示仅限同机，IPv6 URL 可复制", async () => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        ...settings,
        publicBaseUrl: "http://[::1]:8431/api/v1",
      }),
    ),
  );
  show();
  expect(await screen.findByText("http://[::1]:8431/mcp")).toBeVisible();
  expect(screen.getByText(/当前为回环地址，仅限同机/)).toBeVisible();
});

it("地址为空时不生成伪地址或配置", async () => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({ ...settings, publicBaseUrl: "" }),
    ),
  );
  show();
  expect(await screen.findByText(/请先在 API 集成中保存明确/)).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP URL" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("首次加载保留页面标题和可用导航，内容使用骨架", async () => {
  server.use(
    http.get(endpoint, async () => {
      await delay(100);
      return HttpResponse.json(settings);
    }),
  );
  show();
  expect(screen.getByRole("heading", { name: "MCP 集成" })).toBeVisible();
  expect(screen.getByRole("button", { name: "前往 API 集成" })).toBeEnabled();
  expect(
    screen.getByRole("status", { name: "正在读取 MCP 接入信息" }),
  ).toHaveAttribute("aria-busy", "true");
  expect(
    await screen.findByText("https://example.test/proxy/mcp"),
  ).toBeVisible();
});

it("首次读取失败显示错误并支持就近重试", async () => {
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  const user = userEvent.setup();
  show();
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.queryByText("运行中")).not.toBeInTheDocument();
  server.use(http.get(endpoint, () => HttpResponse.json(settings)));
  await user.click(screen.getByRole("button", { name: "重试" }));
  expect(
    await screen.findByText("https://example.test/proxy/mcp"),
  ).toBeVisible();
});

it("共享查询刷新失败时保留旧地址但将可用状态改为未知", async () => {
  const { queryClient } = show();
  expect(await screen.findByText("运行中")).toBeVisible();
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  await queryClient.invalidateQueries({
    queryKey: integrationServiceQueryKey(),
  });
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByText("https://example.test/proxy/mcp")).toBeVisible();
  expect(screen.getByText("状态未知")).toBeVisible();
  expect(screen.queryByText("运行中")).not.toBeInTheDocument();
});

it("复制失败提供反馈，不显示复制成功", async () => {
  const user = userEvent.setup();
  show();
  const button = await screen.findByRole("button", { name: "复制 MCP 配置" });
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(
    new Error("denied"),
  );
  await user.click(button);
  expect(await screen.findByRole("alert")).toHaveTextContent("复制失败");
  expect(
    screen.queryByRole("button", { name: "MCP 配置已复制" }),
  ).not.toBeInTheDocument();
  await user.click(button);
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("button", { name: "MCP 配置已复制" })).toBeVisible();
});

it("发布设置的共享查询更新后直接使用新保存地址与状态", async () => {
  const user = userEvent.setup();
  const { queryClient } = show();
  await user.click(await screen.findByRole("button", { name: "复制 MCP URL" }));
  expect(screen.getByRole("button", { name: "MCP URL 已复制" })).toBeVisible();
  act(() => {
    queryClient.setQueryData(integrationServiceOptions().queryKey, {
      ...settings,
      enabled: false,
      available: false,
      publicBaseUrl: "https://new.example.test/prefix/api/v1",
    });
  });
  expect(
    await screen.findByText("https://new.example.test/prefix/mcp"),
  ).toBeVisible();
  expect(screen.getByText("已停用")).toBeVisible();
  expect(screen.getByRole("button", { name: "复制 MCP URL" })).toBeVisible();
  expect(
    screen.queryByText("https://example.test/proxy/mcp"),
  ).not.toBeInTheDocument();
});
