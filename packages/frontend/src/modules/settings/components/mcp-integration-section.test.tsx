import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
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
const mcpEndpoint = `${origin}/api/v1/integrations/mcp`;
const defaults = {
  enabled: false,
  mcpEnabled: false,
  available: false,
  publicBaseUrl: "https://example.test/proxy/api/v1",
};
let saved = { ...defaults };
const server = setupServer(
  http.get(endpoint, () => HttpResponse.json(saved)),
  http.put(mcpEndpoint, async ({ request }) => {
    const body: unknown = await request.json();
    expect(body).toEqual({ enabled: !saved.mcpEnabled });
    saved = { ...saved, mcpEnabled: !saved.mcpEnabled, available: true };
    return HttpResponse.json({});
  }),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  saved = { ...defaults };
});
afterEach(() => {
  server.resetHandlers();
  vi.restoreAllMocks();
});
afterAll(() => server.close());
function show() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <MCPIntegrationSection />
    </QueryClientProvider>,
  );
  return queryClient;
}

const mcpSwitch = () => screen.getByRole("switch", { name: "启用 MCP" });
const waitForReady = () => waitFor(() => expect(mcpSwitch()).toBeEnabled());

it.each([false, true])(
  "API 未开启时禁用 MCP 并保留偏好：%s",
  async (preference) => {
    saved = { ...defaults, mcpEnabled: preference };
    const user = userEvent.setup();
    show();
    expect(await screen.findByText("请先开启 API 服务。")).toBeVisible();
    expect(mcpSwitch()).toBeDisabled();
    expect(mcpSwitch()).toHaveAttribute("aria-checked", String(preference));
    await user.click(mcpSwitch());
    expect(saved.mcpEnabled).toBe(preference);
    expect(
      screen.queryByRole("button", { name: "复制 MCP 配置" }),
    ).not.toBeInTheDocument();
  },
);

it("API 已开启时使用 Switch 开启 MCP，仅保留配置和占位提示", async () => {
  saved = { ...defaults, enabled: true, available: true };
  const user = userEvent.setup();
  const queryClient = show();
  await waitForReady();
  expect(mcpSwitch()).not.toBeChecked();
  await user.click(mcpSwitch());
  await waitFor(() => expect(mcpSwitch()).toBeChecked());
  expect(screen.getByRole("button", { name: "复制 MCP 配置" })).toBeVisible();
  expect(
    screen.getByText("将 <YOUR_API_KEY> 替换为你的 API Key。"),
  ).toBeVisible();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({ enabled: true, mcpEnabled: true });
  expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "开启 MCP" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("MCP URL")).not.toBeInTheDocument();
});

it("复制 HTTP 配置包含完整代理地址与 API Key 占位符", async () => {
  saved = { ...defaults, enabled: true, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  show();
  await user.click(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  );
  expect(JSON.parse(await navigator.clipboard.readText())).toEqual({
    mcpServers: {
      neuxbot: {
        url: "https://example.test/proxy/mcp",
        headers: { "X-API-Key": "<YOUR_API_KEY>" },
      },
    },
  });
  expect(screen.getByRole("button", { name: "MCP 配置已复制" })).toBeVisible();
});

it("API 关闭时隐藏 MCP 配置，再开启 API 时恢复原偏好", async () => {
  saved = { ...defaults, enabled: true, mcpEnabled: true, available: true };
  const queryClient = show();
  expect(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  ).toBeVisible();
  act(() =>
    queryClient.setQueryData(integrationServiceOptions().queryKey, {
      ...saved,
      enabled: false,
      available: false,
    }),
  );
  await waitFor(() => expect(mcpSwitch()).toBeDisabled());
  expect(mcpSwitch()).toBeChecked();
  expect(screen.getByText("请先开启 API 服务。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  act(() =>
    queryClient.setQueryData(integrationServiceOptions().queryKey, saved),
  );
  await waitFor(() => expect(mcpSwitch()).toBeEnabled());
  expect(mcpSwitch()).toBeChecked();
  expect(screen.getByRole("button", { name: "复制 MCP 配置" })).toBeVisible();
});

it("关闭 MCP 后移除配置，API 保持启用", async () => {
  saved = { ...defaults, enabled: true, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  const queryClient = show();
  await waitForReady();
  await user.click(mcpSwitch());
  await waitFor(() => expect(mcpSwitch()).not.toBeChecked());
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({ enabled: true, mcpEnabled: false });
});

it("启停成功后等待 service 查询确认，期间 Switch 禁用且不假切换", async () => {
  saved = { ...defaults, enabled: true, available: true };
  const refetchStarted = vi.fn();
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.get(endpoint, async () => {
      if (saved.mcpEnabled) {
        refetchStarted();
        await gate;
      }
      return HttpResponse.json(saved);
    }),
  );
  const user = userEvent.setup();
  show();
  await waitForReady();
  await user.click(mcpSwitch());
  await waitFor(() => expect(refetchStarted).toHaveBeenCalledOnce());
  expect(mcpSwitch()).toBeDisabled();
  expect(mcpSwitch()).not.toBeChecked();
  expect(mcpSwitch()).toHaveAttribute("aria-busy", "true");
  expect(screen.getByText("正在开启…")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  release();
  await waitFor(() => expect(mcpSwitch()).toBeChecked());
  expect(mcpSwitch()).toBeEnabled();
});

it.each([false, true])("启停失败保留真实设置：原偏好 %s", async (enabled) => {
  saved = { ...defaults, enabled: true, mcpEnabled: enabled, available: true };
  server.use(
    http.put(mcpEndpoint, () => HttpResponse.json({}, { status: 503 })),
  );
  const user = userEvent.setup();
  show();
  await waitForReady();
  await user.click(mcpSwitch());
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(mcpSwitch()).toBeEnabled();
  expect(mcpSwitch()).toHaveAttribute("aria-checked", String(enabled));
});

it("缺失 MCP 状态时不把 API 可用解释为 MCP 开启", async () => {
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        enabled: true,
        available: true,
        publicBaseUrl: defaults.publicBaseUrl,
      }),
    ),
  );
  show();
  expect(await screen.findByText("状态未知")).toBeVisible();
  expect(mcpSwitch()).toBeDisabled();
  expect(mcpSwitch()).not.toBeChecked();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("首次加载保留 MCP 标签与禁用 Switch，内容使用骨架", async () => {
  saved = { ...defaults, enabled: true, available: true };
  server.use(
    http.get(endpoint, async () => {
      await delay(100);
      return HttpResponse.json(saved);
    }),
  );
  show();
  expect(screen.getByRole("heading", { name: "MCP" })).toBeVisible();
  expect(mcpSwitch()).toBeDisabled();
  expect(
    screen.getByRole("status", { name: "正在读取 MCP 接入信息" }),
  ).toHaveAttribute("aria-busy", "true");
  await waitForReady();
});

it("状态读取失败由 API 服务区处理，MCP 禁用且不重复错误和重试入口", async () => {
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  const queryClient = show();
  expect(await screen.findByText("状态未知")).toBeVisible();
  expect(mcpSwitch()).toBeDisabled();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({ ...defaults, enabled: true, available: true }),
    ),
  );
  await queryClient.invalidateQueries({
    queryKey: integrationServiceQueryKey(),
  });
  await waitForReady();
});

it("启停回执成功但刷新失败时显示未知，不假装开启", async () => {
  saved = { ...defaults, enabled: true, available: true };
  server.use(
    http.get(endpoint, () =>
      saved.mcpEnabled
        ? HttpResponse.json({}, { status: 503 })
        : HttpResponse.json(saved),
    ),
  );
  const user = userEvent.setup();
  show();
  await waitForReady();
  await user.click(mcpSwitch());
  expect(await screen.findByText("状态未知")).toBeVisible();
  expect(mcpSwitch()).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("API 与 MCP 均开启但没有地址时不生成伪配置", async () => {
  saved = {
    ...defaults,
    enabled: true,
    mcpEnabled: true,
    available: true,
    publicBaseUrl: "",
  };
  show();
  expect(await screen.findByText("尚无可用的客户端访问地址。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("复制失败有反馈，重试成功后清除", async () => {
  saved = { ...defaults, enabled: true, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  show();
  const button = await screen.findByRole("button", { name: "复制 MCP 配置" });
  vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(
    new Error("denied"),
  );
  await user.click(button);
  expect(await screen.findByRole("alert")).toHaveTextContent("复制失败");
  await user.click(button);
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("button", { name: "MCP 配置已复制" })).toBeVisible();
});
