import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, Link, RouterProvider } from "react-router-dom";
import { SettingsNavigationGuardProvider } from "@/modules/settings";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import {
  integrationServiceQueryKey,
  integrationServiceOptions,
} from "@/shared/api/integration-service";
import { SettingsPageFooterHost } from "./settings-page-footer";
import { APIServiceSettings } from "./api-service-settings";

const origin = "http://api-service-settings.test";
const settings = {
  enabled: true,
  mcpEnabled: true,
  available: true,
  ip: "127.0.0.1",
  port: 8431,
  publicBaseUrl: "https://example.test/api/v1",
};
const endpoint = `${origin}/api/v1/integrations/service`;
const server = setupServer(
  http.get(endpoint, () => HttpResponse.json(settings)),
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
      <SettingsPageFooterHost>
        <main>
          <APIServiceSettings />
        </main>
      </SettingsPageFooterHost>
    </QueryClientProvider>,
  );
  return { ...view, queryClient };
}
it("连接配置先只读，编辑原Card且保存条由pane承载，取消恢复已保存值", async () => {
  const user = userEvent.setup();
  show();
  await screen.findByText(settings.publicBaseUrl);
  expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
  const card = screen
    .getByText(settings.publicBaseUrl)
    .closest('[data-slot="settings-card"]');
  expect(card).not.toBeNull();
  await user.click(screen.getByRole("button", { name: "编辑连接配置" }));
  expect(within(card as HTMLElement).getByLabelText("端口")).toHaveValue(8431);
  const save = screen.getByRole("button", { name: "保存" });
  expect(save).toBeDisabled();
  const footer = save.closest("footer");
  expect(footer).not.toBeNull();
  expect(footer?.closest("main")).toBeNull();
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
  expect(screen.queryByRole("contentinfo")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "编辑连接配置" }));
  expect(screen.getByLabelText("端口")).toHaveValue(8431);
});
it("复制只用已保存地址，保存整体草稿且保留MCP偏好", async () => {
  let saved = settings;
  server.use(
    http.get(endpoint, () => HttpResponse.json(saved)),
    http.put(endpoint, async ({ request }) => {
      expect(await request.json()).toEqual({
        enabled: false,
        ip: "0.0.0.0",
        port: 18431,
        publicBaseUrl: "https://new.test/api/v1",
      });
      saved = {
        ...settings,
        enabled: false,
        ip: "0.0.0.0",
        port: 18431,
        publicBaseUrl: "https://new.test/api/v1",
      };
      return HttpResponse.json({});
    }),
  );
  const user = userEvent.setup();
  const { queryClient } = show();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  for (const [label, value] of [
    ["监听 IP", "0.0.0.0"],
    ["端口", "18431"],
    ["客户端访问地址", "https://new.test/api/v1"],
  ] as const) {
    const input = screen.getByLabelText(label);
    await user.clear(input);
    await user.type(input, value);
  }
  await user.click(screen.getByRole("switch", { name: "启用 API 服务" }));
  await user.click(screen.getByRole("button", { name: "复制访问地址" }));
  expect(await navigator.clipboard.readText()).toBe(settings.publicBaseUrl);
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument(),
  );
  expect(screen.getByText("https://new.test/api/v1")).toBeVisible();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({ enabled: false, mcpEnabled: true });
});
it("后台读取失败保留草稿，写入成功读回失败不允许重复保存", async () => {
  const user = userEvent.setup();
  const { queryClient } = show();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  server.use(
    http.get(endpoint, () => HttpResponse.json({}, { status: 503 })),
    http.put(endpoint, () => HttpResponse.json({})),
  );
  await act(() =>
    queryClient.invalidateQueries({ queryKey: integrationServiceQueryKey() }),
  );
  expect(screen.getByLabelText("端口")).toHaveValue(18431);
  expect(await screen.findByRole("alert")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(
    await screen.findByText("配置已保存，但读取最新状态失败。请重试读取。"),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
});

it("API真实dirty草稿的父导航与后退保护一致，读取分页仍保留草稿", async () => {
  const router = createMemoryRouter(
    [
      {
        path: "*",
        element: (
          <SettingsNavigationGuardProvider>
            <APIServiceSettings />
            <Link to="/settings/integrations?tab=api&detail=service&keysPage=2">
              下一页密钥
            </Link>
            <Link to="/settings/integrations">集成</Link>
          </SettingsNavigationGuardProvider>
        ),
      },
    ],
    {
      initialEntries: [
        "/settings/integrations",
        "/settings/integrations?tab=api&detail=service",
      ],
      initialIndex: 1,
    },
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  await user.click(screen.getByRole("link", { name: "下一页密钥" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(router.state.location.search).toContain("keysPage=2");
  await user.click(screen.getByRole("link", { name: "集成" }));
  await user.click(await screen.findByRole("button", { name: "继续编辑" }));
  expect(screen.getByLabelText("端口")).toHaveValue(18431);
  expect(router.state.location.search).toContain("tab=api");
  await act(() => router.navigate(-2));
  await user.click(await screen.findByRole("button", { name: "放弃更改" }));
  await waitFor(() =>
    expect(router.state.location.search).not.toContain("keysPage"),
  );
});

it("成功写入的读回重试期间继续输入，读回成功不能关闭新草稿", async () => {
  server.use(http.put(endpoint, () => HttpResponse.json({})));
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("配置已保存，但读取最新状态失败。请重试读取。");
  let release: () => void = () => undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let reading = false;
  server.use(
    http.get(endpoint, async () => {
      reading = true;
      await gate;
      return HttpResponse.json({ ...settings, port: 18431 });
    }),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() => expect(reading).toBe(true));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "28431");
  release();
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(screen.getByLabelText("端口")).toHaveValue(28431);
  expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
});

it("写成功读回失败时不能复制旧URL或从旧值重开，取消保留已提交配置待核对", async () => {
  server.use(http.put(endpoint, () => HttpResponse.json({})));
  const user = userEvent.setup();
  show();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  await user.clear(screen.getByLabelText("客户端访问地址"));
  await user.type(
    screen.getByLabelText("客户端访问地址"),
    "https://new.test/api/v1",
  );
  await user.click(screen.getByRole("switch", { name: "启用 API 服务" }));
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("配置已保存，但读取最新状态失败。请重试读取。");
  expect(
    screen.queryByRole("button", { name: "复制访问地址" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.getByText("18431")).toBeVisible();
  expect(screen.getByText("关闭")).toBeVisible();
  expect(screen.queryByText("8431")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "编辑连接配置" })).toBeDisabled();
  expect(
    screen.queryByRole("button", { name: "复制访问地址" }),
  ).not.toBeInTheDocument();
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        ...settings,
        port: 18431,
        publicBaseUrl: "https://canonical.test/api/v1",
      }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "编辑连接配置" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "复制访问地址" }));
  expect(await navigator.clipboard.readText()).toBe(
    "https://canonical.test/api/v1",
  );
});

it("写成功读回持续失败时重挂载仍保留接受字段、未知URL且禁用编辑，GET恢复才解锁", async () => {
  server.use(http.put(endpoint, () => HttpResponse.json({})));
  const user = userEvent.setup();
  const { queryClient, unmount } = show();
  await user.click(await screen.findByRole("button", { name: "编辑连接配置" }));
  await user.clear(screen.getByLabelText("端口"));
  await user.type(screen.getByLabelText("端口"), "18431");
  await user.clear(screen.getByLabelText("客户端访问地址"));
  await user.type(
    screen.getByLabelText("客户端访问地址"),
    "https://new.test/api/v1",
  );
  server.use(http.get(endpoint, () => HttpResponse.json({}, { status: 503 })));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("配置已保存，但读取最新状态失败。请重试读取。");
  unmount();
  render(
    <QueryClientProvider client={queryClient}>
      <APIServiceSettings />
    </QueryClientProvider>,
  );
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByText("18431")).toBeVisible();
  expect(screen.getByText("地址尚未核对，请重试读取。")).toBeVisible();
  expect(screen.queryByText("8431")).not.toBeInTheDocument();
  expect(
    screen.queryByText("https://example.test/api/v1"),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("https://new.test/api/v1")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "复制访问地址" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "编辑连接配置" })).toBeDisabled();
  expect(
    queryClient.getQueryData(integrationServiceOptions().queryKey),
  ).toMatchObject({
    port: 18431,
    mcpEnabled: true,
    publicBaseUrl: undefined,
    available: undefined,
  });
  server.use(
    http.get(endpoint, () =>
      HttpResponse.json({
        ...settings,
        port: 18431,
        publicBaseUrl: "https://canonical.test/api/v1",
      }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "编辑连接配置" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "复制访问地址" }));
  expect(await navigator.clipboard.readText()).toBe(
    "https://canonical.test/api/v1",
  );
});
