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

it("默认关闭，直接开启 MCP 不需要开启 API，页面只保留配置和占位提示", async () => {
  const user = userEvent.setup();
  const queryClient = show();
  expect(await screen.findByText("MCP 尚未开启。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "开启 MCP" }));
  expect(await screen.findByRole("button", { name: "关闭 MCP" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "复制 MCP 配置" })).toBeVisible();
  expect(
    screen.getByText("将 <YOUR_API_KEY> 替换为你的 API Key。"),
  ).toBeVisible();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({ enabled: false, mcpEnabled: true });
  expect(
    screen.queryByRole("button", { name: "前往 API 集成" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "复制 MCP URL" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("MCP URL")).not.toBeInTheDocument();
  expect(screen.queryByText("知识库权限")).not.toBeInTheDocument();
  expect(screen.queryByText("连接与运行边界")).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
});

it("API 开启而 MCP 关闭时依然显示开启按钮", async () => {
  saved = { ...defaults, enabled: true, available: true };
  show();
  expect(await screen.findByRole("button", { name: "开启 MCP" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("复制 HTTP 配置包含完整代理地址与 API Key 占位符", async () => {
  saved = { ...defaults, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  show();
  await user.click(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  );
  expect(JSON.parse(await navigator.clipboard.readText())).toEqual({
    mcpServers: {
      neuxally: {
        url: "https://example.test/proxy/mcp",
        headers: { "X-API-Key": "<YOUR_API_KEY>" },
      },
    },
  });
  expect(screen.getByRole("button", { name: "MCP 配置已复制" })).toBeVisible();
});

it("关闭 MCP 后移除配置，API 仍保持启用", async () => {
  saved = { ...defaults, enabled: true, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  const queryClient = show();
  await user.click(await screen.findByRole("button", { name: "关闭 MCP" }));
  expect(await screen.findByRole("button", { name: "开启 MCP" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({ enabled: true, mcpEnabled: false, available: true });
});

it("开启请求进行中禁用重复操作，未确认前不展示配置", async () => {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.put(mcpEndpoint, async () => {
      await gate;
      saved = { ...saved, mcpEnabled: true, available: true };
      return HttpResponse.json({});
    }),
  );
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "开启 MCP" }));
  expect(screen.getByRole("button", { name: "正在开启…" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  release();
  expect(await screen.findByRole("button", { name: "关闭 MCP" })).toBeEnabled();
});

it("启停成功后仍等待 service 查询确认真实状态", async () => {
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
  await user.click(await screen.findByRole("button", { name: "开启 MCP" }));
  await waitFor(() => expect(refetchStarted).toHaveBeenCalledOnce());
  expect(screen.getByRole("button", { name: "正在开启…" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
  release();
  expect(await screen.findByRole("button", { name: "关闭 MCP" })).toBeEnabled();
});

it.each([false, true])(
  "启停失败保留真实设置，不假切换：原开启状态 %s",
  async (enabled) => {
    saved = { ...defaults, mcpEnabled: enabled, available: enabled };
    server.use(
      http.put(mcpEndpoint, () => HttpResponse.json({}, { status: 503 })),
    );
    const user = userEvent.setup();
    show();
    await user.click(
      await screen.findByRole("button", {
        name: enabled ? "关闭 MCP" : "开启 MCP",
      }),
    );
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(
      screen.getByRole("button", { name: enabled ? "关闭 MCP" : "开启 MCP" }),
    ).toBeEnabled();
    expect(
      Boolean(screen.queryByRole("button", { name: "复制 MCP 配置" })),
    ).toBe(enabled);
  },
);

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
  expect(await screen.findByText("状态未知", { selector: "p" })).toBeVisible();
  expect(screen.getByRole("button", { name: "状态未知" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "关闭 MCP" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("MCP 已保存开启但监听失败时明确显示不可用", async () => {
  saved = { ...defaults, mcpEnabled: true, available: false };
  show();
  expect(await screen.findByText("MCP 服务暂不可用。")).toBeVisible();
  expect(screen.queryByText("运行中")).not.toBeInTheDocument();
});

it("首次加载保留标题与禁用的操作，内容显示骨架", async () => {
  server.use(
    http.get(endpoint, async () => {
      await delay(100);
      return HttpResponse.json(saved);
    }),
  );
  show();
  expect(screen.getByRole("heading", { name: "MCP 集成" })).toBeVisible();
  expect(screen.getByRole("button", { name: "状态未知" })).toBeDisabled();
  expect(
    screen.getByRole("status", { name: "正在读取 MCP 接入信息" }),
  ).toHaveAttribute("aria-busy", "true");
  expect(await screen.findByRole("button", { name: "开启 MCP" })).toBeEnabled();
});

it("首次读取失败支持重试，不能提交未知状态", async () => {
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  const user = userEvent.setup();
  show();
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("button", { name: "状态未知" })).toBeDisabled();
  server.use(http.get(endpoint, () => HttpResponse.json(saved)));
  await user.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByRole("button", { name: "开启 MCP" })).toBeEnabled();
});

it("启停成功但状态刷新失败时显示未知，重试读取后恢复", async () => {
  server.use(
    http.get(endpoint, () =>
      saved.mcpEnabled
        ? HttpResponse.json({}, { status: 503 })
        : HttpResponse.json(saved),
    ),
  );
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "开启 MCP" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("button", { name: "状态未知" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "关闭 MCP" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("MCP 尚未开启。")).not.toBeInTheDocument();
  server.use(http.get(endpoint, () => HttpResponse.json(saved)));
  await user.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByRole("button", { name: "关闭 MCP" })).toBeEnabled();
});

it("后台查询失败时保留配置并将操作改为未知，防止误切换", async () => {
  saved = { ...defaults, mcpEnabled: true, available: true };
  const queryClient = show();
  expect(await screen.findByRole("button", { name: "关闭 MCP" })).toBeEnabled();
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  await queryClient.invalidateQueries({
    queryKey: integrationServiceQueryKey(),
  });
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByRole("button", { name: "状态未知" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "复制 MCP 配置" })).toBeVisible();
});

it("开启但未有明确访问地址时不生成伪配置", async () => {
  saved = { ...defaults, mcpEnabled: true, available: true, publicBaseUrl: "" };
  show();
  expect(await screen.findByText("尚无可用的客户端访问地址。")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "复制 MCP 配置" }),
  ).not.toBeInTheDocument();
});

it("复制失败有反馈，重试成功后清除", async () => {
  saved = { ...defaults, mcpEnabled: true, available: true };
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

it("共享查询中保存地址更新后使用新配置并清除旧复制反馈", async () => {
  saved = { ...defaults, mcpEnabled: true, available: true };
  const user = userEvent.setup();
  const queryClient = show();
  await user.click(
    await screen.findByRole("button", { name: "复制 MCP 配置" }),
  );
  act(() => {
    queryClient.setQueryData(integrationServiceOptions().queryKey, {
      ...saved,
      publicBaseUrl: "https://new.example.test/prefix/api/v1",
    });
  });
  await waitFor(() =>
    expect(screen.getByLabelText("HTTP MCP 配置")).toHaveTextContent(
      "https://new.example.test/prefix/mcp",
    ),
  );
  expect(screen.getByRole("button", { name: "复制 MCP 配置" })).toBeVisible();
});
