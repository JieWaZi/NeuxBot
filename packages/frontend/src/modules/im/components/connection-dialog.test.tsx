import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import "@/shared/i18n";
import { ConnectionDialog } from "./connection-dialog";
import type { IMPlatform } from "../types";

const api = vi.hoisted(() => ({
  connections: vi.fn(async () => ({
    data: { resources: [{ id: "new", revision: 1, enabled: false }] },
  })),
  create: vi.fn<
    (options: unknown) => Promise<{ data: { id: string; revision: number } }>
  >(async () => ({ data: { id: "new", revision: 1 } })),
  start: vi.fn<
    (
      options: unknown,
    ) => Promise<{ data: { id: string; status: string; qrContent: string } }>
  >(async () => ({
    data: {
      id: "login",
      status: "wait",
      qrContent: "https://example.invalid/qr",
    },
  })),
  bindings: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: { resources: [], totalSize: 0 } }),
  ),
  bindingUpdate: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: {} }),
  ),
  progress: vi.fn(async () => ({
    data: {
      id: "login",
      status: "wait",
      qrContent: "https://example.invalid/qr",
    },
  })),
  cancel: vi.fn<(options: unknown) => Promise<{ data: object }>>(async () => ({
    data: {},
  })),
  update: vi.fn<
    (options: unknown) => Promise<{ data: { id: string; revision: number } }>
  >(async () => ({ data: { id: "new", revision: 2 } })),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  createImConnection: api.create,
  startImLogin: api.start,
  cancelImLogin: api.cancel,
  updateImConnection: api.update,
  getImLogin: api.progress,
  listWakers: async () => ({ data: { resources: [], totalSize: 0 } }),
  listChannels: async () => ({
    data: {
      resources: [{ channel: { id: "team", name: "研发团队" } }],
      totalSize: 1,
    },
  }),
  listImBindings: api.bindings,
  updateImBinding: api.bindingUpdate,
  listImConnections: () => api.connections(),
}));
const clients: QueryClient[] = [];
beforeEach(() => {
  Object.values(api).forEach((mock) => mock.mockClear());
  api.connections.mockResolvedValue({
    data: { resources: [{ id: "new", revision: 1, enabled: false }] },
  });
  api.bindings.mockResolvedValue({ data: { resources: [], totalSize: 0 } });
  api.progress.mockResolvedValue({
    data: {
      id: "login",
      status: "wait",
      qrContent: "https://example.invalid/qr",
    },
  });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});
function show(
  platform: IMPlatform,
  existing = false,
  state: "connected" | "login_required" | "failed" = "connected",
  errorCode?: string,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  render(
    <StrictMode>
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <ConnectionDialog
            platform={platform}
            connection={
              existing
                ? {
                    id: "new",
                    platform,
                    appId: "same-account",
                    enabled: true,
                    state,
                    errorCode,
                    name: "现有连接",
                    revision: 1,
                  }
                : undefined
            }
            onClose={vi.fn()}
            onSaved={vi.fn()}
          />
        </MemoryRouter>
      </QueryClientProvider>
    </StrictMode>,
  );
}
it("选定微信后自动生成二维码，刷新复用同一连接且不重复创建", async () => {
  const user = userEvent.setup();
  show("weixin");
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
  expect(screen.getByLabelText(/名称/)).toHaveValue("微信");
  expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "聊天接入" }),
  ).not.toBeInTheDocument();
  expect(api.create).toHaveBeenCalledTimes(1);
  expect(api.start).toHaveBeenCalledTimes(1);
  expect(
    screen.queryByRole("button", { name: "扫码连接" }),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "刷新二维码" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "刷新二维码" }));
  await waitFor(() => expect(api.start).toHaveBeenCalledTimes(2));
  expect(api.create).toHaveBeenCalledTimes(1);
  expect(api.start.mock.calls[1]?.[0]).toMatchObject({
    path: { connectionId: "new" },
  });
});
it("飞书可从扫码切换手动凭证，保存后自动启用", async () => {
  const user = userEvent.setup();
  show("feishu");
  await screen.findByTitle("扫码连接");
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "手工填写飞书应用凭据" }),
    ).toBeEnabled(),
  );
  await user.click(
    screen.getByRole("button", { name: "手工填写飞书应用凭据" }),
  );
  await user.type(screen.getByLabelText(/应用 ID/), "cli_example");
  await user.type(screen.getByLabelText(/应用密钥/), "test-secret");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { appId: "cli_example", appSecret: "test-secret", enabled: true },
  });
  expect(api.cancel).toHaveBeenCalledTimes(1);
});
it("管理已连接账号只读取设置，重新扫码由明确入口触发", async () => {
  const user = userEvent.setup();
  show("weixin", true);
  expect(
    await screen.findByRole("button", { name: "重新扫码" }),
  ).toBeInTheDocument();
  expect(screen.getByLabelText(/名称/)).toHaveValue("现有连接");
  expect(
    screen.queryByRole("switch", { name: /连接/ }),
  ).not.toBeInTheDocument();
  expect(api.start).not.toHaveBeenCalled();
  expect(api.create).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "重新扫码" }));
  await waitFor(() => expect(api.start).toHaveBeenCalledTimes(1));
  expect(api.create).not.toHaveBeenCalled();
});

it("已有飞书应用可返回扫码接入，仍使用原连接", async () => {
  const user = userEvent.setup();
  show("feishu", true);
  const back = await screen.findByRole("button", { name: "返回扫码接入" });
  expect(back).toBeEnabled();
  await user.click(back);
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
  expect(api.start).toHaveBeenCalledTimes(1);
  expect(api.start.mock.calls[0]?.[0]).toMatchObject({
    path: { connectionId: "new" },
  });
  expect(api.create).not.toHaveBeenCalled();
});

it("扫码成功保留二维码，并在下方展示绿色成功状态", async () => {
  api.progress.mockResolvedValue({
    data: {
      id: "login",
      status: "confirmed",
      qrContent: "https://example.invalid/qr",
    },
  });
  show("weixin");
  const success = await screen.findByText("连接已保存并启用");
  expect(success).toHaveClass("text-success");
  expect(screen.getByTitle("扫码连接")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "刷新二维码" }),
  ).not.toBeInTheDocument();
});

for (const platform of ["weixin", "feishu", "wecom"] as const) {
  it(`${platform} 既有连接可改名，保存实际输入并保留凭据`, async () => {
    const user = userEvent.setup();
    show(platform, true);
    const name = screen.getByLabelText(/名称/);
    await user.clear(name);
    await user.type(name, "  日常助理  ");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
    expect(api.update.mock.calls[0]?.[0]).toMatchObject({
      path: { connectionId: "new" },
      body: { name: "日常助理", appSecret: "", revision: 1 },
    });
  });
}
it("已有聊天配置的连接合并保存名称与聊天设置", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "new",
          name: "测试群",
          state: "approved",
          channelId: "team",
          revision: 4,
        },
      ],
      totalSize: 1,
    },
  });
  const user = userEvent.setup();
  show("weixin", true);
  await screen.findByText("测试群");
  const name = screen.getByLabelText(/名称/);
  await user.clear(name);
  await user.type(name, "团队机器人");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.bindingUpdate).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { name: "团队机器人" },
  });
  expect(api.bindingUpdate.mock.calls[0]?.[0]).toMatchObject({
    path: { bindingId: "binding" },
    body: { channelId: "team", revision: 4 },
  });
});

it("待授权聊天可单独修改连接名称，不要求配置伙伴或批准聊天", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "new",
          name: "待授权聊天",
          state: "pending",
          revision: 1,
        },
      ],
      totalSize: 1,
    },
  });
  const user = userEvent.setup();
  show("feishu", true);
  await screen.findByText("待授权聊天");
  const name = screen.getByLabelText(/名称/);
  await user.clear(name);
  await user.type(name, "独立连接名称");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { name: "独立连接名称" },
  });
  expect(api.bindingUpdate).not.toHaveBeenCalled();
});

it("新建机器人名称位于二维码上方，改名保存到同一连接且不提前启用", async () => {
  const user = userEvent.setup();
  show("weixin");
  const qr = await screen.findByTitle("扫码连接");
  const name = screen.getByLabelText(/名称/);
  expect(
    name.compareDocumentPosition(qr) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(api.create.mock.calls[0]?.[0]).toMatchObject({
    body: { name: "微信", enabled: false },
  });
  await user.clear(name);
  await user.type(name, "研发微信机器人");
  await user.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    path: { connectionId: "new" },
    body: { name: "研发微信机器人", enabled: false },
  });
  expect(api.create).toHaveBeenCalledTimes(1);
});

it("扫码成功后完成新建，保存自定义名称并保留已确认的启用状态", async () => {
  api.progress.mockResolvedValue({
    data: {
      id: "login",
      status: "confirmed",
      qrContent: "https://example.invalid/qr",
    },
  });
  api.connections.mockResolvedValue({
    data: { resources: [{ id: "new", revision: 5, enabled: true }] },
  });
  const user = userEvent.setup();
  show("feishu");
  await screen.findByText("连接已保存并启用");
  const name = screen.getByLabelText(/名称/);
  expect(name).toHaveValue("飞书");
  await user.clear(name);
  await user.type(name, "客服飞书机器人");
  await user.click(screen.getByRole("button", { name: "完成" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    path: { connectionId: "new" },
    body: { name: "客服飞书机器人", revision: 5, enabled: true },
  });
  expect(api.cancel).not.toHaveBeenCalled();
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

it("企业微信默认扫码，刷新与取消复用同一连接，不提前授权聊天", async () => {
  const user = userEvent.setup();
  show("wecom");
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
  expect(screen.getByLabelText(/名称/)).toHaveValue("企业微信");
  expect(
    screen.getByText(
      "使用手机企业微信扫描二维码，按提示创建或授权智能机器人。",
    ),
  ).toBeInTheDocument();
  expect(api.create.mock.calls[0]?.[0]).toMatchObject({
    body: { platform: "wecom", enabled: false, appId: "", appSecret: "" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "刷新二维码" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "刷新二维码" }));
  await waitFor(() => expect(api.start).toHaveBeenCalledTimes(2));
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(api.create).toHaveBeenCalledTimes(1);
  expect(api.cancel).toHaveBeenCalledWith(
    expect.objectContaining({ path: { loginId: "login" } }),
  );
  expect(api.bindingUpdate).not.toHaveBeenCalled();
});

it("企业微信可切换手工凭据，Bot ID 与 Secret 映射到现有接口并启用", async () => {
  const user = userEvent.setup();
  show("wecom");
  await screen.findByTitle("扫码连接");
  const manual = screen.getByRole("button", {
    name: "手工填写企业微信机器人凭据",
  });
  await waitFor(() => expect(manual).toBeEnabled());
  await user.click(manual);
  expect(screen.queryByLabelText(/应用 ID/)).not.toBeInTheDocument();
  expect(screen.queryByTitle("扫码连接")).not.toBeInTheDocument();
  await user.type(screen.getByLabelText(/Bot ID/), "aibot_example");
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  await user.type(screen.getByLabelText(/机器人密钥/), "wecom-secret");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: {
      platform: "wecom",
      appId: "aibot_example",
      appSecret: "wecom-secret",
      enabled: true,
    },
  });
  expect(api.create).toHaveBeenCalledTimes(1);
  expect(api.cancel).toHaveBeenCalledTimes(1);
  expect(screen.getByLabelText(/机器人密钥/)).toHaveValue("");
});

it("已有企业微信机器人维护密钥时固定身份并保存轮换密钥", async () => {
  const user = userEvent.setup();
  show("wecom", true);
  expect(screen.getByLabelText(/Bot ID/)).toBeDisabled();
  expect(screen.getByLabelText(/Bot ID/)).toHaveValue("same-account");
  expect(screen.getByLabelText(/机器人密钥/)).not.toBeRequired();
  expect(api.start).not.toHaveBeenCalled();
  await user.type(screen.getByLabelText(/机器人密钥/), "rotated-secret");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { appId: "same-account", appSecret: "rotated-secret" },
  });
});

it("已有企业微信返回扫码仍使用原连接并解释身份边界", async () => {
  const user = userEvent.setup();
  show("wecom", true);
  await user.click(screen.getByRole("button", { name: "返回扫码接入" }));
  await screen.findByTitle("扫码连接");
  expect(
    screen.getByText(
      "请重新授权当前机器人。更换机器人请添加新连接，以保留原聊天的授权边界。",
    ),
  ).toBeInTheDocument();
  expect(api.start.mock.calls[0]?.[0]).toMatchObject({
    path: { connectionId: "new" },
  });
  expect(api.create).not.toHaveBeenCalled();
});

it("企业微信扫码失败就近展示错误，重试不重复创建连接", async () => {
  api.start.mockRejectedValueOnce(new Error("login failed"));
  const user = userEvent.setup();
  show("wecom");
  expect(await screen.findByRole("alert")).toHaveTextContent("操作失败");
  await user.click(screen.getByRole("button", { name: "刷新二维码" }));
  await screen.findByTitle("扫码连接");
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  expect(api.start).toHaveBeenCalledTimes(2);
  expect(api.create).toHaveBeenCalledTimes(1);
});

it("企业微信扫码确认后引导发现聊天及单独授权，不自动写入聊天配置", async () => {
  api.progress.mockResolvedValue({
    data: {
      id: "login",
      status: "confirmed",
      qrContent: "https://example.invalid/qr",
    },
  });
  show("wecom");
  await screen.findByText("连接已保存并启用");
  expect(
    screen.getByText(
      "在企业微信单聊机器人，或在群内 @ 机器人发送首条消息，再为发现的聊天配置伙伴、团队和工作范围。每个聊天需单独授权。",
    ),
  ).toBeInTheDocument();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(api.update).not.toHaveBeenCalled();
});

it("企业微信二维码过期可刷新且不重复创建连接", async () => {
  api.progress.mockResolvedValueOnce({
    data: {
      id: "login",
      status: "expired",
      qrContent: "https://example.invalid/qr",
    },
  });
  const user = userEvent.setup();
  show("wecom");
  await screen.findByText("二维码已过期，请重新扫码");
  await user.click(screen.getByRole("button", { name: "刷新二维码" }));
  await waitFor(() => expect(api.start).toHaveBeenCalledTimes(2));
  expect(api.create).toHaveBeenCalledTimes(1);
});

it("企业微信密钥保存失败保留输入，就近提示并可重试", async () => {
  api.update.mockRejectedValueOnce(new Error("save failed"));
  const user = userEvent.setup();
  show("wecom", true);
  await user.type(screen.getByLabelText(/机器人密钥/), "retry-secret");
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("操作失败");
  expect(screen.getByLabelText(/机器人密钥/)).toHaveValue("retry-secret");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(2));
  expect(api.update.mock.calls[1]?.[0]).toMatchObject({
    body: { appId: "same-account", appSecret: "retry-secret" },
  });
});

it("企业微信授权失效时默认维护当前机器人凭据，扫码由明确入口触发", async () => {
  show("wecom", true, "login_required");
  expect(screen.getByLabelText(/Bot ID/)).toHaveValue("same-account");
  expect(screen.getByLabelText(/Bot ID/)).toBeDisabled();
  expect(screen.queryByTitle("扫码连接")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "返回扫码接入" })).toBeEnabled();
  expect(api.start).not.toHaveBeenCalled();
  expect(api.create).not.toHaveBeenCalled();
});

it("企业微信切换手工模式后取消只保存名称，不提交未完成的凭据", async () => {
  const user = userEvent.setup();
  show("wecom");
  await screen.findByTitle("扫码连接");
  const manual = screen.getByRole("button", {
    name: "手工填写企业微信机器人凭据",
  });
  await waitFor(() => expect(manual).toBeEnabled());
  await user.click(manual);
  const name = screen.getByLabelText(/名称/);
  await user.clear(name);
  await user.type(name, "研发企微");
  await user.type(screen.getByLabelText(/Bot ID/), "incomplete-bot");
  await user.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { name: "研发企微", appId: "", appSecret: "", enabled: false },
  });
});

for (const [state, errorCode] of [
  ["failed", "IMConnectionReplaced"],
  ["login_required", "IMLoginRequired"],
] as const) {
  it(`企业微信已有聊天在 ${errorCode} 后直接保存会恢复连接并保留凭据`, async () => {
    api.bindings.mockResolvedValue({
      data: {
        resources: [
          {
            id: "binding",
            connectionId: "new",
            name: "研发群",
            state: "approved",
            channelId: "team",
            revision: 4,
          },
        ],
        totalSize: 1,
      },
    });
    const user = userEvent.setup();
    show("wecom", true, state, errorCode);
    await screen.findByText("研发群");
    expect(screen.getByLabelText(/机器人密钥/)).toHaveValue("");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(api.bindingUpdate).toHaveBeenCalledTimes(1));
    expect(api.update).toHaveBeenCalledTimes(1);
    expect(api.update.mock.calls[0]?.[0]).toMatchObject({
      path: { connectionId: "new" },
      body: {
        appId: "same-account",
        appSecret: "",
        enabled: true,
        revision: 1,
      },
    });
    expect(api.start).not.toHaveBeenCalled();
    expect(api.update.mock.invocationCallOrder[0]).toBeLessThan(
      api.bindingUpdate.mock.invocationCallOrder[0] || 0,
    );
  });
}

it("企业微信待授权聊天恢复连接时不隐式批准聊天", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "new",
          name: "待授权私聊",
          state: "pending",
          revision: 4,
        },
      ],
      totalSize: 1,
    },
  });
  const user = userEvent.setup();
  show("wecom", true, "failed", "IMConnectionReplaced");
  await screen.findByText("待授权私聊");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.update).toHaveBeenCalledTimes(1));
  expect(api.update.mock.calls[0]?.[0]).toMatchObject({
    body: { appId: "same-account", appSecret: "", enabled: true },
  });
  expect(api.bindingUpdate).not.toHaveBeenCalled();
});

it("企业微信正常连接已有聊天仅保存原聊天配置，不意外重连", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "new",
          name: "研发群",
          state: "approved",
          channelId: "team",
          revision: 4,
        },
      ],
      totalSize: 1,
    },
  });
  const user = userEvent.setup();
  show("wecom", true);
  await screen.findByText("研发群");
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.bindingUpdate).toHaveBeenCalledTimes(1));
  expect(api.update).not.toHaveBeenCalled();
  expect(api.start).not.toHaveBeenCalled();
});
