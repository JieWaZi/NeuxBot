import { client } from "@/shared/api/generated/client.gen";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll } from "vitest";
const categoryServer = setupServer(
  http.get("http://127.0.0.1:8080/api/v1/resource-categories", () =>
    HttpResponse.json([{ id: "category", name: "开发" }]),
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: "http://127.0.0.1:8080" });
  categoryServer.listen({ onUnhandledRequest: "error" });
});
afterAll(() => categoryServer.close());
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as GeneratedQueries from "@/shared/api/generated/@tanstack/react-query.gen";
import "@/shared/i18n";
import { ConnectorAddPage } from "./connector-add-page";
import { ConnectorFormDialog } from "../components/connector-form-dialog";
const { create, update } = vi.hoisted(() => ({
  create: vi.fn<(input: unknown) => Promise<{ connector: { id: string } }>>(),
  update: vi.fn<(input: unknown) => Promise<{ connector: { id: string } }>>(),
}));
vi.mock("@/modules/waker", () => ({
  WakerDetailLayout: ({ children }: { children: ReactNode }) => children,
}));
vi.mock(
  "@/shared/api/generated/@tanstack/react-query.gen",
  async (importOriginal) => ({
    ...(await importOriginal<typeof GeneratedQueries>()),
    listResourceCategoriesOptions: () => ({
      queryKey: ["connector-form-categories"],
      queryFn: async () => [],
    }),
  }),
);
vi.mock("../api/connector-queries", () => ({
  createWakerConnectorMutation: () => ({ mutationFn: create }),
  updateWakerConnectorMutation: () => ({ mutationFn: update }),
  invalidateVisibleConnectorQueries: async () => undefined,
}));
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
beforeEach(() => {
  create.mockReset().mockResolvedValue({ connector: { id: "saved" } });
  update.mockReset().mockResolvedValue({ connector: { id: "builtin" } });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
function Location() {
  const location = useLocation();
  return <output>{JSON.stringify(location)}</output>;
}
function mount(entry: string) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route
            path="/resources/connectors/new"
            element={<ConnectorAddPage />}
          />
          <Route
            path="/wakers/:wakerId/connectors/new"
            element={<ConnectorAddPage />}
          />
          <Route
            path="/wakers/:wakerId/resources/connectors/new"
            element={<ConnectorAddPage />}
          />
          <Route path="*" element={<Location />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
it("页面内新建 STDIO，保留参数、敏感字段及安全返回上下文", async () => {
  const user = userEvent.setup();
  mount(
    "/resources/connectors/new?returnTo=" +
      encodeURIComponent(
        "/resources?category=connectors&search=项目&partner=w1",
      ),
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(
    screen.getByRole("heading", { level: 1, name: "添加连接器" }),
  ).toBeVisible();
  expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
  expect(screen.getByRole("radio", { name: "STDIO" })).toBeChecked();
  expect(screen.getByRole("radio", { name: "HTTP" })).not.toBeChecked();
  expect(screen.getByRole("radio", { name: "SSE" })).not.toBeChecked();
  expect(screen.getByRole("navigation", { name: "连接器" })).toBeVisible();
  const transport = screen.getByRole("radiogroup", { name: "传输方式" });
  expect(transport).not.toHaveClass("w-full");
  expect(screen.getAllByRole("radiogroup", { name: "传输方式" })).toHaveLength(
    1,
  );
  const header = screen
    .getByRole("heading", { level: 1, name: "添加连接器" })
    .closest("header");
  const form = header?.closest("form");
  expect(header).toContainElement(transport);
  expect(form?.firstElementChild).toBe(header);
  expect(
    form?.querySelector('[data-slot="connector-form-content"]'),
  ).not.toContainElement(transport);
  expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
  expect(screen.queryByText("启动/测试超时（秒）")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "保存" }),
  ).not.toBeInTheDocument();
  await user.type(screen.getByRole("textbox", { name: /名称/ }), "项目工具");
  await user.type(screen.getByRole("textbox", { name: /命令/ }), "npx");
  await user.type(
    screen.getByRole("textbox", { name: /^参数/ }),
    '-y "project tools"',
  );
  await user.click(screen.getByRole("button", { name: "添加" }));
  await user.type(
    screen.getByRole("textbox", { name: "配置项名称" }),
    "API_TOKEN",
  );
  await user.type(screen.getByLabelText("敏感值"), "secret");
  expect(screen.getByLabelText("敏感值")).toHaveAttribute("type", "password");
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledOnce());
  expect(create.mock.calls[0]?.[0]).toMatchObject({
    path: { wakerId: "" },
    body: {
      name: "项目工具",
      command: "npx",
      arguments: ["-y", "project tools"],
      transport: "stdio",
      timeoutMs: 10000,
      trustConfirmed: true,
      environment: [{ name: "API_TOKEN", value: "secret" }],
    },
  });
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "/resources/connectors/saved",
    ),
  );
  expect(screen.getByRole("status")).toHaveTextContent(
    `category=connectors&search=${encodeURIComponent("项目")}&partner=w1`,
  );
});
it("HTTP 与 SSE 原生字段可保存到当前伙伴，新建不要求模型", async () => {
  const user = userEvent.setup();
  mount(
    "/wakers/w1/resources/connectors/new?returnTo=" +
      encodeURIComponent(
        "/wakers/w1/resources?category=connectors&search=remote",
      ),
  );
  await user.click(screen.getByRole("radio", { name: "SSE" }));
  await user.type(screen.getByRole("textbox", { name: /名称/ }), "远端工具");
  await user.type(
    screen.getByRole("textbox", { name: /服务地址/ }),
    "https://example.com/mcp",
  );
  expect(
    screen.queryByRole("textbox", { name: /命令/ }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/模型/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledOnce());
  expect(create.mock.calls[0]?.[0]).toMatchObject({
    path: { wakerId: "w1" },
    body: {
      transport: "sse",
      command: "",
      url: "https://example.com/mcp",
      timeoutMs: 10000,
    },
  });
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "/wakers/w1/connectors/saved",
    ),
  );
  expect(screen.getByRole("status")).toHaveTextContent(
    "/wakers/w1/resources?category=connectors&search=remote",
  );
});
it("重置清空草稿，恶意 returnTo 不生成外部返回链接", async () => {
  const user = userEvent.setup();
  mount(
    "/resources/connectors/new?returnTo=" +
      encodeURIComponent("//evil.example/resources"),
  );
  expect(screen.getByRole("link", { name: "连接器" })).toHaveAttribute(
    "href",
    "/resources?category=connectors",
  );
  await user.type(screen.getByRole("textbox", { name: /名称/ }), "草稿");
  await user.click(screen.getByRole("button", { name: "重置" }));
  expect(screen.getByRole("textbox", { name: /名称/ })).toHaveValue("");
  expect(screen.queryByText("有未保存的更改")).not.toBeInTheDocument();
  expect(create).not.toHaveBeenCalled();
});
it("修复校验后显示本次 API 失败，保留输入以便重试", async () => {
  const user = userEvent.setup();
  create.mockRejectedValueOnce(new Error("upstream unavailable"));
  mount("/resources/connectors/new");
  await user.type(screen.getByRole("textbox", { name: /命令/ }), "npx");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(screen.getByRole("alert")).toHaveTextContent("请输入连接器名称。");
  await user.type(screen.getByRole("textbox", { name: /名称/ }), "项目工具");
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledOnce());
  expect(screen.getByRole("alert")).not.toHaveTextContent("请输入连接器名称。");
  expect(screen.getByRole("textbox", { name: /名称/ })).toHaveValue("项目工具");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "/resources/connectors/saved",
    ),
  );
});

it("提交中锁定草稿与信任确认，避免同一表单重复保存", async () => {
  const user = userEvent.setup();
  let resolveSave: ((value: { connector: { id: string } }) => void) | undefined;
  create.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveSave = resolve;
      }),
  );
  mount("/resources/connectors/new");
  await user.type(screen.getByRole("textbox", { name: "名称" }), "项目工具");
  await user.type(screen.getByRole("textbox", { name: "命令" }), "npx");
  await user.click(screen.getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(create).toHaveBeenCalledOnce());
  expect(screen.getByRole("textbox", { name: "命令" })).toBeDisabled();
  expect(screen.getByRole("textbox", { name: "参数" })).toBeDisabled();
  expect(screen.getByRole("checkbox")).toBeDisabled();
  for (const radio of screen.getAllByRole("radio"))
    expect(radio).toBeDisabled();
  const saving = screen.getByRole("button", { name: "正在保存…" });
  expect(saving).toHaveAttribute("aria-disabled", "true");
  expect(saving).toHaveAttribute("aria-busy", "true");
  fireEvent.click(saving);
  expect(create).toHaveBeenCalledOnce();
  resolveSave?.({ connector: { id: "saved" } });
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent(
      "/resources/connectors/saved",
    ),
  );
});
it("复用编辑表单保留内置限制与敏感值局部更新", async () => {
  const user = userEvent.setup();
  const saved = vi.fn();
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <MemoryRouter>
        <ConnectorFormDialog
          open
          wakerId="w1"
          onOpenChange={vi.fn()}
          onSaved={saved}
          connector={{
            id: "builtin",
            scope: "built_in",
            name: "内置服务",
            command: "server",
            transport: "stdio",
            environment: [{ id: "token", name: "API_TOKEN", configured: true }],
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const dialog = screen.getByRole("dialog");
  const form = dialog.querySelector('[data-slot="connector-form"]');
  const content = dialog.querySelector('[data-slot="connector-form-content"]');
  const footer = screen.getByLabelText("连接器保存状态");
  expect(form).toHaveClass("min-h-0", "flex-1", "overflow-hidden");
  expect(content).toHaveClass("min-h-0", "flex-1", "overflow-y-auto");
  expect(footer.parentElement).toBe(form);
  expect(form?.lastElementChild).toBe(footer);
  expect(content?.contains(footer)).toBe(false);
  expect(footer).toHaveClass("shrink-0");
  expect(footer).not.toHaveClass("sticky", "fixed");
  expect(screen.getByRole("textbox", { name: "名称" })).toBeDisabled();
  expect(screen.getByRole("textbox", { name: "命令" })).toBeDisabled();
  expect(screen.getByRole("spinbutton")).toBeDisabled();
  expect(screen.getByLabelText("敏感值")).toHaveValue("");
  expect(screen.getByLabelText("敏感值")).toHaveAttribute(
    "placeholder",
    "留空以保留已有值",
  );
  await user.type(screen.getByLabelText("敏感值"), "replacement");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledOnce());
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    path: { connectorId: "builtin", wakerId: "w1" },
    body: {
      environment: [{ id: "token", name: "API_TOKEN", value: "replacement" }],
      trustConfirmed: false,
    },
  });
  expect(create).not.toHaveBeenCalled();
  expect(saved).toHaveBeenCalledWith("builtin");
});

it("顶部传输切换共享同一草稿，并由重置恢复默认状态", async () => {
  const user = userEvent.setup();
  mount("/resources/connectors/new");
  await user.type(screen.getByRole("textbox", { name: "命令" }), "npx");
  const http = screen.getByRole("radio", { name: "HTTP" });
  await user.click(http);
  await user.type(
    screen.getByRole("textbox", { name: "服务地址" }),
    "https://example.com/mcp",
  );
  await user.click(screen.getByRole("radio", { name: "STDIO" }));
  expect(screen.getByRole("textbox", { name: "命令" })).toHaveValue("npx");
  await user.keyboard("{End}");
  expect(screen.getByRole("radio", { name: "SSE" })).toBeChecked();
  expect(screen.getByRole("textbox", { name: "服务地址" })).toHaveValue(
    "https://example.com/mcp",
  );
  await user.click(screen.getByRole("button", { name: "重置" }));
  expect(screen.getByRole("radio", { name: "STDIO" })).toBeChecked();
  expect(screen.getByRole("textbox", { name: "命令" })).toHaveValue("");
  expect(
    screen.queryByRole("button", { name: "保存" }),
  ).not.toBeInTheDocument();
  expect(create).not.toHaveBeenCalled();
});

it("顶部返回保留列表搜索且不会提交未保存草稿", async () => {
  const user = userEvent.setup();
  mount(
    "/resources/connectors/new?returnTo=" +
      encodeURIComponent("/resources?category=connectors&search=remote"),
  );
  await user.type(screen.getByRole("textbox", { name: "名称" }), "草稿服务");
  await user.type(screen.getByRole("textbox", { name: "命令" }), "npx");
  await user.click(screen.getByRole("checkbox"));
  const back = screen.getByRole("button", { name: "返回列表" });
  expect(back).toHaveAttribute("type", "button");
  await user.click(back);
  expect(screen.getByRole("status")).toHaveTextContent("/resources");
  expect(screen.getByRole("status")).toHaveTextContent(
    "category=connectors&search=remote",
  );
  expect(create).not.toHaveBeenCalled();
});

it("编辑使用秒并校验范围，保存时转换毫秒且不重新要求信任", async () => {
  const user = userEvent.setup();
  const client = new QueryClient();
  const editor = (open: boolean) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ConnectorFormDialog
          open={open}
          wakerId="w1"
          onOpenChange={vi.fn()}
          connector={{
            id: "existing",
            name: "远端服务",
            transport: "streamable_http",
            url: "https://example.com/mcp",
            timeoutMs: 30000,
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(editor(true));
  let timeout = screen.getByRole("spinbutton", {
    name: "启动/测试超时（秒）",
  });
  expect(
    within(screen.getByLabelText("连接器保存状态")).queryByRole("status"),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "取消" })).toBeVisible();
  expect(screen.getByRole("button", { name: "保存" })).toBeVisible();
  fireEvent.change(timeout, { target: { value: "45" } });
  expect(screen.getByRole("status")).toHaveTextContent("有未保存的更改");
  view.rerender(editor(false));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  view.rerender(editor(true));
  timeout = screen.getByRole("spinbutton", { name: "启动/测试超时（秒）" });
  expect(
    within(screen.getByLabelText("连接器保存状态")).queryByRole("status"),
  ).not.toBeInTheDocument();
  expect(timeout).toBeEnabled();
  expect(timeout).toHaveValue(30);
  expect(timeout).toHaveAttribute("min", "0.1");
  expect(timeout).toHaveAttribute("max", "300");
  expect(timeout).toHaveAttribute("step", "0.001");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  const form = timeout.closest("form");
  if (!form) throw new Error("编辑超时必须属于连接器表单");
  for (const value of ["", "0.099", "300.001", "0.1001"]) {
    fireEvent.change(timeout, { target: { value } });
    fireEvent.submit(form);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "连接超时必须在 0.1 到 300 秒之间，最多三位小数。",
    );
    expect(update).not.toHaveBeenCalled();
  }
  await user.clear(timeout);
  await user.type(timeout, "4.001");
  expect(timeout).toBeValid();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledOnce());
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    path: { connectorId: "existing", wakerId: "w1" },
    body: { timeoutMs: 4001, trustConfirmed: false },
  });
  expect(create).not.toHaveBeenCalled();
});

it("历史小数秒可直接提交，编辑其他字段不丢失毫秒精度", async () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <ConnectorFormDialog
          open
          wakerId="w1"
          onOpenChange={vi.fn()}
          connector={{
            id: "existing",
            name: "远端服务",
            transport: "streamable_http",
            url: "https://example.com/mcp",
            timeoutMs: 12345,
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  const timeout = screen.getByRole("spinbutton", {
    name: "启动/测试超时（秒）",
  });
  expect(timeout).toHaveValue(12.345);
  expect(timeout).toBeValid();
  await user.type(screen.getByRole("textbox", { name: "名称" }), "更新");
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(update).toHaveBeenCalledOnce());
  expect(update.mock.calls[0]?.[0]).toMatchObject({
    path: { connectorId: "existing", wakerId: "w1" },
    body: { name: "远端服务更新", timeoutMs: 12345, trustConfirmed: false },
  });
});

// jsdom 只验证滚动所有权与结构约束；真实短窗、长表单和遮挡需平台验收。
it.each(["STDIO", "HTTP"])(
  "%s 保存区独立于滚动正文，多个配置项与信任确认不进入footer",
  async (transport) => {
    const user = userEvent.setup();
    const { container } = mount("/resources/connectors/new");
    if (transport === "HTTP")
      await user.click(screen.getByRole("radio", { name: "HTTP" }));
    await user.type(
      screen.getByRole("textbox", { name: "名称" }),
      "长表单工具",
    );
    const surface = container.querySelector(
      '[data-slot="resource-page-surface"]',
    );
    const viewport = surface?.querySelector(
      '[data-slot="resource-page-viewport"]',
    );
    const pageContent = surface?.querySelector(
      '[data-slot="resource-page-content"]',
    );
    const form = surface?.querySelector('[data-slot="connector-form"]');
    const scrollContent = form?.querySelector(
      '[data-slot="connector-form-content"]',
    );
    const footer = screen.getByLabelText("连接器保存状态");
    expect(surface).toHaveAttribute("data-content-layout", "pane");
    expect(viewport).toHaveClass("overflow-hidden");
    expect(viewport).not.toHaveClass("overflow-y-auto", "pb-8");
    expect(pageContent).toHaveClass("flex", "h-full", "min-h-0", "flex-col");
    expect(form).toHaveClass("flex-1", "min-h-0", "overflow-hidden");
    expect(scrollContent).toHaveClass("flex-1", "min-h-0", "overflow-y-auto");
    const footerHost = surface?.querySelector(
      '[data-slot="resource-page-footer-host"]',
    );
    expect(footer.parentElement).toBe(footerHost);
    expect(footerHost?.parentElement).toBe(surface);
    expect(viewport?.contains(footer)).toBe(false);
    expect(screen.getByRole("button", { name: "保存" })).toHaveAttribute(
      "form",
      form?.id,
    );
    expect(scrollContent?.contains(footer)).toBe(false);
    expect(footer).toHaveClass("shrink-0");
    expect(footer).not.toHaveClass("sticky", "fixed", "bottom-0");
    expect(scrollContent?.contains(screen.getByRole("checkbox"))).toBe(true);
    for (let index = 0; index < 4; index++)
      await user.click(screen.getByRole("button", { name: "添加" }));
    expect(screen.getAllByLabelText("敏感值")).toHaveLength(4);
    for (const field of screen.getAllByLabelText("敏感值"))
      expect(scrollContent?.contains(field)).toBe(true);
    expect(
      scrollContent?.contains(
        screen.getByText(/连接器可以启动本地命令或访问远程服务/),
      ),
    ).toBe(true);
    expect(footer).not.toHaveTextContent(/来源|连接器可以启动/);
    expect(footer.parentElement).toBe(footerHost);
    await user.click(screen.getByRole("button", { name: "重置" }));
    expect(screen.queryByLabelText("连接器保存状态")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("敏感值")).not.toBeInTheDocument();
    expect(surface?.querySelector('[data-slot="connector-form-content"]')).toBe(
      scrollContent,
    );
    expect(create).not.toHaveBeenCalled();
  },
);
