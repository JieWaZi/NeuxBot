import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { expect, it, vi, beforeAll, afterAll } from "vitest";
import { SettingsScene } from "./settings-scene";
import { settingsSectionOf } from "../settings-navigation";
import "@/shared/i18n";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
const origin = "http://integrations-navigation.test";
const server = setupServer(
  http.get(`${origin}/api/v1/integrations/service`, () =>
    HttpResponse.json({ enabled: false, mcpEnabled: false, available: false }),
  ),
  http.get(`${origin}/api/v1/integrations/keys`, () =>
    HttpResponse.json({ resources: [], totalSize: 0 }),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterAll(() => server.close());

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ mode: "system", setMode: vi.fn() }),
}));

function Harness() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <SettingsScene
        section={settingsSectionOf("integrations")}
        onClose={() => undefined}
        imIntegrationContent={<p>IM 平台内容</p>}
      />
      <output aria-label="地址">
        {location.pathname}
        {location.search}
      </output>
      <button onClick={() => navigate(-1)}>浏览器后退</button>
    </>
  );
}
it("集成总览同时展示三块，服务二级、面包屑及浏览器后退同步", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={["/settings/integrations?keep=value"]}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("navigation", { name: "设置 Scene" }),
  ).toHaveTextContent("集成");
  expect(await screen.findByText("IM 平台内容")).toBeVisible();
  for (const title of ["IM", "API", "MCP"])
    expect(screen.getByRole("heading", { name: title })).toBeVisible();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "MCP 服务" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "API 服务" }));
  expect(screen.getByRole("status", { name: "地址" })).toHaveTextContent(
    "detail=service",
  );
  expect(
    screen.queryByRole("button", { name: "返回集成" }),
  ).toBeInTheDocument();
  await screen.findByRole(
    "button",
    { name: "编辑连接配置" },
    { timeout: 10000 },
  );
  expect(screen.getAllByText("已停用")).toHaveLength(1);
  expect(
    screen.getByRole("navigation", { name: "设置 Scene" }),
  ).toHaveTextContent("集成API 服务");
  await user.click(screen.getByRole("link", { name: "集成" }));
  expect(screen.getByRole("status", { name: "地址" })).not.toHaveTextContent(
    "detail=",
  );
  expect(screen.getByRole("button", { name: "MCP 服务" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "MCP 服务" }));
  expect(
    screen.getByRole("navigation", { name: "设置 Scene" }),
  ).toHaveTextContent("集成MCP 服务");
  await user.click(screen.getByRole("link", { name: "集成" }));
  expect(await screen.findByRole("button", { name: "API 服务" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "浏览器后退" }));
  expect(
    await screen.findByRole("heading", { name: "MCP 服务" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "返回集成" }),
  ).toBeInTheDocument();
});

it.each([
  ["api", "API 服务"],
  ["mcp", "MCP 服务"],
])("%s 二级标题的悬停返回保留分页条件并恢复三个分组", async (tab, title) => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter
        initialEntries={[
          `/settings/integrations?tab=${tab}&detail=service&keep=value&keysPage=2`,
        ]}
      >
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await user.hover(await screen.findByRole("heading", { name: title }));
  await user.click(screen.getByRole("button", { name: "返回集成" }));
  expect(screen.getByRole("status", { name: "地址" })).toHaveTextContent(
    "/settings/integrations?keep=value&keysPage=2",
  );
  for (const name of ["IM", "API", "MCP"])
    expect(screen.getByRole("heading", { name })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "返回集成" }),
  ).not.toBeInTheDocument();
});

it.each([
  ["weixin", "微信"],
  ["feishu", "飞书"],
  ["wecom", "企业微信"],
])("平台 %s 深链在读取前同步面包屑", async (platform, name) => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter
        initialEntries={[`/settings/integrations?tab=im&platform=${platform}`]}
      >
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("navigation", { name: "设置 Scene" }),
  ).toHaveTextContent(`集成${name}`);
  expect(screen.getByRole("link", { name: "集成" })).toHaveAttribute(
    "href",
    "/settings/integrations",
  );
  expect(
    screen.queryByRole("heading", { name: "API" }),
  ).not.toBeInTheDocument();
});
