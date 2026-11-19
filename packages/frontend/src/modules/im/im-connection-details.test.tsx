import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import "@/shared/i18n";
import { IMConnectionDetails } from "./im-connection-details";
import { IMConnectionDetailPage } from "./im-connection-detail-page";

const api = vi.hoisted(() => ({
  connections: vi.fn<(options?: unknown) => Promise<{ data: object }>>(),
  runtimes: vi.fn<() => Promise<{ data: object }>>(),
  catalog: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
  remove: vi.fn<(options: unknown) => Promise<{ data: object }>>(async () => ({
    data: {},
  })),
  update: vi.fn<(options: unknown) => Promise<{ data: object }>>(async () => ({
    data: {},
  })),
  bindingUpdate: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: {} }),
  ),
  conversations: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: { resources: [] } }),
  ),
  selectConversation: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: {} }),
  ),
  bindings: vi.fn<(options: unknown) => Promise<{ data: object }>>(
    async () => ({ data: { resources: [], totalSize: 0 } }),
  ),
  project: vi.fn<(options: unknown) => Promise<{ data: object }>>(async () => ({
    data: { id: "project", name: "资讯整理" },
  })),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  listImConnections: api.connections,
  listImBindings: api.bindings,
  getProject: api.project,
  listProjects: async () => ({ data: { resources: [], totalSize: 0 } }),
  listRuntimes: api.runtimes,
  getRuntimeModelCatalog: api.catalog,
  listAiAccounts: async () => ({ data: { resources: [] } }),
  listImConversations: api.conversations,
  selectImConversation: api.selectConversation,
  updateImConnection: api.update,
  updateImBinding: api.bindingUpdate,
  deleteImConnection: api.remove,
  getImConnectionDependencies: async () => ({
    data: { workflows: [], pendingDeliveries: 0, digest: "empty" },
  }),
  createImConnection: async () => ({ data: { id: "new", revision: 1 } }),
  startImLogin: async () => ({
    data: { id: "login", status: "wait", qrContent: "https://example.invalid" },
  }),
  getImLogin: async () => ({
    data: { id: "login", status: "wait", qrContent: "https://example.invalid" },
  }),
  cancelImLogin: async () => ({ data: {} }),
  listWakers: async () => ({
    data: { resources: [{ id: "waker", name: "日常助理" }], totalSize: 1 },
  }),
  listChannels: async () => ({
    data: {
      resources: [
        { channel: { id: "team", name: "研发团队", leadWakerId: "waker" } },
      ],
      totalSize: 1,
    },
  }),
}));
beforeAll(() => {
  Element.prototype.hasPointerCapture = () => false;
});
afterAll(() => {
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
});
const clients: QueryClient[] = [];
beforeEach(() => {
  api.connections.mockReset().mockResolvedValue({
    data: {
      resources: [
        {
          id: "connection",
          appId: "bot-account",
          name: "我的微信",
          platform: "weixin",
          enabled: true,
          keepAwake: true,
          state: "connected",
          revision: 3,
          conversationCount: 2,
        },
      ],
    },
  });
  api.remove.mockReset().mockResolvedValue({ data: {} });
  api.update.mockClear();
  api.bindingUpdate.mockReset().mockResolvedValue({ data: {} });
  api.remove.mockClear();
  api.bindings
    .mockReset()
    .mockResolvedValue({ data: { resources: [], totalSize: 0 } });
  api.conversations.mockReset().mockResolvedValue({ data: { resources: [] } });
  api.selectConversation.mockReset().mockResolvedValue({ data: {} });
  api.project
    .mockReset()
    .mockResolvedValue({ data: { id: "project", name: "资讯整理" } });
  api.runtimes.mockReset().mockResolvedValue({
    data: {
      resources: [
        {
          id: "sample",
          name: "测试智能体",
          enabled: true,
          models: [{ id: "model", name: "测试模型" }],
        },
      ],
    },
  });
  api.catalog.mockReset();
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

function show(initialBindingId?: string, page = false) {
  const onClose = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  function Harness() {
    const location = useLocation();
    return (
      <>
        <output data-testid="location">
          {location.pathname}
          {location.search}
        </output>
        {page ? (
          <IMConnectionDetailPage connectionId="connection" />
        ) : (
          <IMConnectionDetails
            connectionId="connection"
            initialBindingId={initialBindingId}
          />
        )}
      </>
    );
  }
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/settings/im?section=publishing"]}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { onClose, client };
}

it("详情正文首屏读取内容使用骨架，始终不创建弹窗", async () => {
  api.connections.mockImplementation(() => new Promise(() => {}));
  show();
  expect(screen.getByRole("status", { name: "正在读取…" })).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("查看只读展示连接与聊天状态，不提供编辑授权或新建操作", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "approved",
          name: "已授权聊天",
          connectionId: "connection",
          state: "approved",
        },
        {
          id: "pending",
          name: "待授权聊天",
          connectionId: "connection",
          state: "pending",
        },
      ],
      totalSize: 2,
    },
  });
  show("pending");
  await screen.findByRole("heading", { name: "连接信息" });
  const dialog = document.body;
  expect(
    await within(dialog).findByRole("heading", { name: "已授权聊天" }),
  ).toBeInTheDocument();
  expect(
    within(dialog).getByRole("heading", { name: "待授权聊天" }),
  ).toBeInTheDocument();
  expect(
    within(dialog).getAllByRole("heading", { level: 4 })[0],
  ).toHaveTextContent("待授权聊天");
  expect(within(dialog).getByText("bot-account")).toBeInTheDocument();
  expect(
    within(dialog).getByRole("heading", { name: "连接信息" }),
  ).toBeInTheDocument();
  expect(within(dialog).queryByRole("table")).not.toBeInTheDocument();
  expect(
    within(dialog).queryByRole("button", { name: /编辑|删除|授权|新建|切换/ }),
  ).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(api.update).not.toHaveBeenCalled();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(api.remove).not.toHaveBeenCalled();
  expect(api.selectConversation).not.toHaveBeenCalled();
});

it("详情内对话分页不修改设置页地址", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        { id: "binding", connectionId: "connection", state: "approved" },
      ],
      totalSize: 1,
    },
  });
  api.conversations.mockResolvedValue({
    data: {
      resources: Array.from({ length: 11 }, (_, index) => ({
        id: `conversation-${index}`,
        title: `对话 ${index + 1}`,
        createdAt: 1000 - index,
        chatId: `chat-${index}`,
      })),
    },
  });
  const user = userEvent.setup();
  show();
  expect(await screen.findByText("对话 1")).toBeInTheDocument();
  expect(screen.queryByText("对话 11")).not.toBeInTheDocument();
  expect(
    screen.getAllByRole("link", { name: "查看执行过程" })[0],
  ).toHaveAttribute("href", "/chats/chat-0");
  expect(
    screen.queryByRole("button", { name: "切换到此对话" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "新建对话" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "下一页" }));
  expect(screen.getByText("对话 11")).toBeInTheDocument();
  expect(api.selectConversation).not.toHaveBeenCalled();
  expect(screen.getByTestId("location")).toHaveTextContent(
    "/settings/im?section=publishing",
  );
});

it("合并多个聊天的记录时保留与名称相同的外部聊天 ID", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: ["first", "second"].map((id) => ({
        id,
        name: `im-${id}`,
        externalChatId: `im-${id}`,
        state: "approved",
      })),
      totalSize: 2,
    },
  });
  api.conversations
    .mockResolvedValueOnce({
      data: { resources: [{ id: "one", title: "对话一" }] },
    })
    .mockResolvedValueOnce({
      data: { resources: [{ id: "two", title: "对话二" }] },
    });
  show();
  const list = within(await screen.findByRole("list", { name: "对话记录" }));
  expect(list.getByText("对话一").closest("li")).toHaveTextContent("im-first");
  expect(list.getByText("对话二").closest("li")).toHaveTextContent("im-second");
});

it("同名聊天可按外部 ID 区分授权卡片、编辑入口与对话归属", async () => {
  const binding = {
    connectionId: "connection",
    channelId: "team",
    state: "approved",
  };
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        { ...binding, id: "first", name: "协作群", externalChatId: "oc-first" },
        {
          ...binding,
          id: "second",
          name: " 协作群 ",
          externalChatId: "oc-second",
        },
        { ...binding, id: "third", name: "独立群", externalChatId: "oc-third" },
        {
          ...binding,
          id: "fourth",
          name: "oc-fourth",
          externalChatId: "oc-fourth",
        },
      ],
      totalSize: 4,
    },
  });
  api.conversations
    .mockResolvedValueOnce({
      data: { resources: [{ id: "one", title: "对话一" }] },
    })
    .mockResolvedValueOnce({
      data: { resources: [{ id: "two", title: "对话二" }] },
    });
  show(undefined, true);

  const first = within(
    await screen.findByRole("article", { name: "协作群 · oc-first" }),
  );
  const second = within(
    screen.getByRole("article", { name: "协作群 · oc-second" }),
  );
  expect(screen.getByRole("article", { name: "独立群" })).not.toHaveTextContent(
    "oc-third",
  );
  expect(screen.getByRole("heading", { name: "oc-fourth" })).toBeVisible();
  const list = within(await screen.findByRole("list", { name: "对话记录" }));
  expect(list.getByText("对话一").closest("li")).toHaveTextContent(
    "协作群 · oc-first",
  );
  expect(list.getByText("对话二").closest("li")).toHaveTextContent(
    "协作群 · oc-second",
  );

  await userEvent
    .setup()
    .click(second.getByRole("button", { name: "编辑协作群 · oc-second" }));
  expect(first.getByRole("switch")).toBeDisabled();
  expect(second.getByRole("switch")).toBeEnabled();
});

it("三级仅配置Card显示连接状态，空记录无表头计数，删除有完整影响说明", async () => {
  show(undefined, true);
  await screen.findByText("bot-account");
  expect(
    screen.queryByRole("heading", { name: "我的微信" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "返回微信" }),
  ).not.toBeInTheDocument();
  expect(screen.getAllByText("已连接")).toHaveLength(1);
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByText("0 条对话")).not.toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "危险操作" })).toBeInTheDocument();
  expect(
    screen.getByText(/删除“我的微信”及其凭据和聊天绑定/),
  ).toBeInTheDocument();
  expect(
    screen
      .getByText("等待首条消息，聊天会自动出现在这里。")
      .closest('[data-slot="settings-card"]'),
  ).toBeInTheDocument();
  expect(
    (await screen.findByText("尚无实际对话")).closest(
      '[data-slot="settings-card"]',
    ),
  ).toBeInTheDocument();
});

it("连接配置原Card内编辑，授权、记录、危险区始终保留", async () => {
  show(undefined, true);
  await screen.findByText("bot-account");
  await userEvent.setup().click(screen.getByRole("button", { name: "编辑" }));
  expect(
    screen
      .getByRole("textbox", { name: /名称/ })
      .closest('[data-slot="settings-card"]'),
  ).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "聊天接入" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "对话记录" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "危险操作" })).toBeInTheDocument();
});

it.each(["weixin", "feishu", "wecom"])(
  "%s 的授权项可就地编辑，取消不提交且其他分区仍可见",
  async (platform) => {
    api.connections.mockResolvedValue({
      data: { resources: [{ id: "connection", platform, name: "机器人" }] },
    });
    api.bindings.mockResolvedValue({
      data: {
        resources: [
          {
            id: "binding",
            name: "协作群",
            connectionId: "connection",
            channelId: "team",
            state: "approved",
            allowWorkflowDelivery: true,
          },
        ],
        totalSize: 1,
      },
    });
    show(undefined, true);
    const user = userEvent.setup();
    const edit = await screen.findByRole("button", { name: "编辑协作群" });
    expect(screen.getByRole("switch")).toBeChecked();
    expect(screen.getByRole("switch")).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "授权" }),
    ).not.toBeInTheDocument();
    await user.click(edit);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByRole("switch")).toBeEnabled();
    await user.click(screen.getByRole("switch"));
    expect(
      screen.getByRole("heading", { name: "对话记录" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "危险操作" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByRole("switch")).toBeChecked();
    expect(screen.getByRole("switch")).toBeDisabled();
    expect(api.update).not.toHaveBeenCalled();
    expect(api.bindingUpdate).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "编辑协作群" })).toHaveFocus(),
    );
  },
);

it("只读和编辑共用运行控件，伙伴名称前显示头像", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          name: "伙伴聊天",
          wakerId: "waker",
          state: "approved",
          options: { runtimeId: "sample", model: "model", reasoning: "low" },
        },
      ],
      totalSize: 1,
    },
  });
  show(undefined, true);
  const user = userEvent.setup();
  const card = within(await screen.findByRole("article", { name: "伙伴聊天" }));
  const workflow = card.getByRole("switch", {
    name: "允许工作流查询并发送到此聊天",
  });
  expect(card.getByText("允许工作流查询并发送到此聊天")).toHaveClass(
    "type-label",
  );
  const target = card.getByRole("combobox", { name: "执行目标" });
  const runtime = card.getByTestId("composer-runtime-control");
  const model = card.getByTestId("composer-model-control");
  expect(model).toHaveTextContent("low");
  await waitFor(() => expect(target).toHaveTextContent("日常助理"));
  expect(target.querySelector("[data-companion-avatar]")).toBeInTheDocument();
  for (const control of [target, runtime, model, workflow])
    expect(control).toBeDisabled();

  await user.click(screen.getByRole("button", { name: "编辑伙伴聊天" }));
  expect(card.getByTestId("composer-runtime-control")).toBe(runtime);
  expect(card.getByTestId("composer-model-control")).toBe(model);
  expect(card.getByRole("combobox", { name: "执行目标" })).toBe(target);
  for (const control of [target, runtime, model, workflow])
    expect(control).toBeEnabled();
  await user.click(target);
  const option = await screen.findByRole("option", { name: "日常助理" });
  expect(option.querySelector("[data-companion-avatar]")).toBeInTheDocument();
});

it("待授权聊天在查看态显示未配置，编辑时填入默认运行配置", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "pending",
          name: "待授权聊天",
          kind: "direct",
          state: "pending",
          options: {
            runtimeId: "",
            accountId: "",
            model: "",
            reasoning: "",
            permissionLevel: "",
          },
          allowWorkflowDelivery: false,
        },
        {
          id: "configured",
          name: "已配置聊天",
          kind: "direct",
          wakerId: "waker",
          state: "approved",
          options: { runtimeId: "sample", permissionLevel: "default" },
        },
      ],
      totalSize: 2,
    },
  });
  show(undefined, true);
  const user = userEvent.setup();
  const configured = within(
    await screen.findByRole("article", { name: "已配置聊天" }),
  );
  await waitFor(() =>
    expect(
      configured.getByTestId("composer-runtime-control"),
    ).toHaveTextContent("测试智能体"),
  );
  const pending = within(screen.getByRole("article", { name: "待授权聊天" }));
  const runtime = pending.getByTestId("composer-runtime-control");
  const model = pending.getByTestId("composer-model-control");
  const permission = pending.getByTestId("composer-permission-control");
  for (const control of [runtime, model, permission]) {
    expect(control).toHaveTextContent("未配置");
    expect(control).toBeDisabled();
  }
  expect(pending.getByRole("radio", { name: "伙伴" })).not.toBeChecked();
  expect(pending.getByRole("combobox", { name: "执行目标" })).toHaveTextContent(
    "选择伙伴或团队",
  );
  expect(
    pending.queryByRole("button", { name: "授权" }),
  ).not.toBeInTheDocument();

  await user.click(pending.getByRole("button", { name: "编辑待授权聊天" }));
  expect(runtime).toHaveTextContent("测试智能体");
  expect(permission).toHaveTextContent("默认权限");
  expect(pending.getByRole("radio", { name: "伙伴" })).toBeChecked();
  expect(pending.getByRole("button", { name: "授权" })).toBeDisabled();
});

it("模型目录读取失败时仍能保存工作流权限，并保留已保存的运行配置", async () => {
  const binding = {
    id: "binding",
    name: "伙伴聊天",
    kind: "direct",
    wakerId: "waker",
    state: "approved",
    options: {
      runtimeId: "codex",
      accountId: "system",
      model: "saved-model",
      reasoning: "low",
      permissionLevel: "auto",
    },
    allowWorkflowDelivery: true,
  };
  api.runtimes.mockResolvedValue({
    data: { resources: [{ id: "codex", enabled: true }] },
  });
  api.catalog.mockRejectedValue(new Error("catalog read failed"));
  api.bindings.mockResolvedValue({
    data: { resources: [binding], totalSize: 1 },
  });
  api.bindingUpdate.mockImplementation(async () => {
    api.bindings.mockResolvedValue({
      data: {
        resources: [{ ...binding, allowWorkflowDelivery: false }],
        totalSize: 1,
      },
    });
    return { data: {} };
  });
  show(undefined, true);
  const user = userEvent.setup();
  const card = within(await screen.findByRole("article", { name: "伙伴聊天" }));
  await user.click(screen.getByRole("button", { name: "编辑伙伴聊天" }));
  expect(await card.findByRole("alert")).toBeVisible();
  const workflow = card.getByRole("switch", {
    name: "允许工作流查询并发送到此聊天",
  });
  await user.click(workflow);
  const save = screen.getByRole("button", { name: "保存" });
  expect(save).toBeEnabled();
  await user.click(card.getByRole("button", { name: "重试读取" }));
  await waitFor(() =>
    expect(api.catalog).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { runtimeId: "codex" },
        query: { accountId: "system", refresh: true },
      }),
    ),
  );
  await user.click(save);
  await waitFor(() => expect(workflow).toBeDisabled());
  expect(workflow).not.toBeChecked();
  expect(api.bindingUpdate).toHaveBeenCalledWith(
    expect.objectContaining({
      body: {
        state: undefined,
        configuration: {
          name: "伙伴聊天",
          wakerId: "waker",
          channelId: "",
          projectId: "",
          workingDirectory: "",
          controllerIds: [],
          options: binding.options,
          allowWorkflowDelivery: false,
        },
      },
    }),
  );
  expect(card.getByTestId("composer-model-control")).toHaveTextContent(
    "saved-model",
  );
  expect(card.queryByRole("alert")).not.toBeInTheDocument();
});

it("只读态可直接重新授权已配置的聊天", async () => {
  const binding = {
    id: "binding",
    name: "协作群",
    connectionId: "connection",
    channelId: "team",
    state: "rejected",
    kind: "group",
    controllerIds: ["owner"],
    projectId: "project",
    allowWorkflowDelivery: false,
  };
  api.bindings.mockResolvedValue({
    data: { resources: [binding], totalSize: 1 },
  });
  api.bindingUpdate.mockImplementation(async () => {
    api.bindings.mockResolvedValue({
      data: {
        resources: [{ ...binding, state: "approved" }],
        totalSize: 1,
      },
    });
    return { data: {} };
  });
  show(undefined, true);
  const user = userEvent.setup();
  const authorize = await screen.findByRole("button", { name: "授权" });
  expect(screen.getByRole("switch")).toBeDisabled();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(authorize);
  expect(await screen.findByText("已授权")).toBeInTheDocument();
  expect(api.bindingUpdate).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { bindingId: "binding" },
      body: { state: "approved" },
    }),
  );
  expect(screen.getByRole("switch")).toBeDisabled();
  expect(screen.getByRole("switch")).not.toBeChecked();
});

it("只读态可撤销授权，编辑中取消撤销保留草稿", async () => {
  const binding = {
    id: "binding",
    name: "协作群",
    connectionId: "connection",
    channelId: "team",
    state: "approved",
    kind: "group",
    controllerIds: ["owner"],
    allowWorkflowDelivery: true,
  };
  api.bindings.mockResolvedValue({
    data: {
      resources: [binding, { ...binding, id: "second", name: "另一个群" }],
      totalSize: 2,
    },
  });
  show(undefined, true);
  const user = userEvent.setup();
  const card = within(await screen.findByRole("article", { name: "协作群" }));
  const controllers = card.getByRole("textbox", {
    name: /聊天管理员的平台身份/,
  });
  expect(controllers).toHaveAccessibleDescription(
    /用空格或逗号分隔多个 ID.*停止工作及审批/,
  );
  const revoke = card.getByRole("button", { name: "撤销授权" });
  expect(
    card.getByText("撤销后，此聊天无法发起工作。已有对话保留。"),
  ).toBeVisible();
  expect(revoke).toBeEnabled();
  await user.click(revoke);
  expect(card.getByRole("button", { name: "确认撤销" })).toBeVisible();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  await user.click(card.getByRole("button", { name: "保留授权" }));

  await user.click(await screen.findByRole("button", { name: "编辑协作群" }));
  expect(screen.getByRole("button", { name: "编辑另一个群" })).toBeDisabled();
  await user.click(card.getByRole("switch"));
  await user.click(card.getByRole("button", { name: "撤销授权" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  await user.click(card.getByRole("button", { name: "保留授权" }));
  expect(card.getByRole("switch")).toBeEnabled();
  expect(card.getByRole("switch")).not.toBeChecked();
  await user.click(screen.getByRole("button", { name: "取消" }));
  await user.click(card.getByRole("button", { name: "撤销授权" }));
  await user.click(card.getByRole("button", { name: "确认撤销" }));
  await waitFor(() =>
    expect(api.bindingUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { bindingId: "binding" },
        body: { state: "rejected" },
      }),
    ),
  );
  await waitFor(() => expect(card.getByRole("switch")).toBeDisabled());
});

it("编辑中确认撤销只提交授权状态并保留草稿", async () => {
  const binding = {
    id: "binding",
    name: "协作群",
    connectionId: "connection",
    channelId: "team",
    state: "approved",
    allowWorkflowDelivery: true,
  };
  api.bindings.mockResolvedValue({
    data: { resources: [binding], totalSize: 1 },
  });
  api.bindingUpdate.mockImplementationOnce(async () => {
    api.bindings.mockResolvedValue({
      data: { resources: [{ ...binding, state: "rejected" }], totalSize: 1 },
    });
    return { data: {} };
  });
  show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑协作群" }));
  await user.click(screen.getByRole("switch"));
  await user.click(screen.getByRole("button", { name: "撤销授权" }));
  await user.click(screen.getByRole("button", { name: "确认撤销" }));
  expect(await screen.findByText("已撤销")).toBeVisible();
  await waitFor(() => expect(screen.getByRole("switch")).toBeEnabled());
  expect(screen.getByRole("switch")).not.toBeChecked();
  expect(screen.getByRole("button", { name: "授权" })).toBeEnabled();
  expect(api.bindingUpdate.mock.calls[0]?.[0]).toMatchObject({
    path: { bindingId: "binding" },
  });
  expect(api.bindingUpdate.mock.calls[0]?.[0]).toHaveProperty("body", {
    state: "rejected",
  });
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.getByRole("switch")).toBeChecked();
  expect(screen.getByRole("switch")).toBeDisabled();
});

it("授权后台读失败保留已打开的编辑草稿", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          name: "协作群",
          connectionId: "connection",
          channelId: "team",
          state: "approved",
          allowWorkflowDelivery: true,
        },
      ],
      totalSize: 1,
    },
  });
  const { client } = show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑协作群" }));
  await user.click(screen.getByRole("switch"));
  api.bindings.mockRejectedValue(new Error("refresh failed"));
  await act(async () => {
    await client.invalidateQueries({
      queryKey: ["im", "connection-bindings", "connection"],
    });
  });
  await waitFor(() =>
    expect(screen.getAllByRole("alert").length).toBeGreaterThan(0),
  );
  expect(screen.getByRole("switch")).not.toBeChecked();
  expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
});

it("后台读取新的设置后可以直接保存当前草稿", async () => {
  const binding = {
    id: "binding",
    name: "协作群",
    connectionId: "connection",
    channelId: "team",
    projectId: "project",
    state: "approved",
  };
  api.bindings.mockResolvedValue({
    data: { resources: [binding], totalSize: 1 },
  });
  const { client } = show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑协作群" }));
  await user.click(screen.getByRole("button", { name: "工作范围" }));
  await user.click(await screen.findByRole("menuitem", { name: "默认" }));
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          ...binding,
          name: "新的群名称",
          channelId: "",
          wakerId: "waker",
          options: { runtimeId: "sample" },
          projectId: "",
        },
      ],
      totalSize: 1,
    },
  });
  await act(async () => {
    await client.invalidateQueries({
      queryKey: ["im", "connection-bindings", "connection"],
    });
  });
  expect(screen.getByRole("button", { name: "保存" })).toBeEnabled();
  expect(screen.getByRole("combobox", { name: "执行目标" })).toHaveTextContent(
    "研发团队",
  );
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(api.bindingUpdate).toHaveBeenCalledTimes(1));
  expect(api.bindingUpdate.mock.calls[0]?.[0]).toMatchObject({
    body: {
      state: undefined,
      configuration: {
        name: "新的群名称",
        channelId: "team",
        wakerId: "",
        projectId: "",
      },
    },
  });
});

it("连接后台读取失败在配置Card表达陈旧并保留编辑输入", async () => {
  const { client } = show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑" }));
  await user.type(screen.getByRole("textbox", { name: /名称/ }), "新草稿");
  api.connections.mockRejectedValueOnce(new Error("offline"));
  await act(async () => {
    await client.invalidateQueries({ queryKey: ["im", "connections"] });
  });
  const state = await screen.findByText("状态待更新");
  expect(screen.queryByText("已连接")).not.toBeInTheDocument();
  expect(state.closest('[data-slot="settings-card"]')).toContainElement(
    screen.getByRole("alert"),
  );
  expect(screen.getByRole("textbox", { name: /名称/ })).toHaveValue(
    "我的微信新草稿",
  );
});

it("页内连接编辑的状态、名称、账号和时间是同一字段容器的相邻行，重连属于账号行", async () => {
  show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑" }));
  expect(screen.getAllByText("状态", { exact: true })).toHaveLength(1);
  const name = screen.getByRole("textbox", { name: /名称/ });
  const group = name.closest("fieldset");
  expect(group).toBe(screen.getByText("bot-account").closest("fieldset"));
  expect(group).toBe(
    screen.getByText("创建时间", { exact: true }).closest("fieldset"),
  );
  const rows = Array.from(group?.lastElementChild?.children ?? []);
  expect(rows).toHaveLength(4);
  expect(rows[0]).toHaveTextContent("状态");
  expect(rows[1]).toContainElement(name);
  expect(rows[2]).toHaveTextContent("机器人账号");
  expect(rows[2]).toContainElement(
    screen.getByRole("button", { name: "重新扫码" }),
  );
  expect(rows[3]).toHaveTextContent("创建时间");
});

it("页内重新扫码在账号行展开原认证内容，其他同级字段与分区保留", async () => {
  show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑" }));
  const group = screen
    .getByRole("textbox", { name: /名称/ })
    .closest("fieldset");
  await user.click(screen.getByRole("button", { name: "重新扫码" }));
  expect(group).toContainElement(await screen.findByTitle("扫码连接"));
  expect(screen.getAllByText("状态", { exact: true })).toHaveLength(1);
  expect(screen.getByText("bot-account")).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "聊天接入" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "对话记录" })).toBeInTheDocument();
});

it("页内手工凭据与名称在同一字段容器，保持只读身份及秘密替换说明", async () => {
  api.connections.mockResolvedValue({
    data: {
      resources: [
        {
          id: "connection",
          name: "飞书助手",
          platform: "feishu",
          appId: "cli_sample",
          enabled: true,
          state: "connected",
          revision: 3,
        },
      ],
    },
  });
  show(undefined, true);
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "编辑" }));
  const name = screen.getByRole("textbox", { name: /名称/ });
  const secret = screen.getByLabelText("应用密钥");
  expect(name.closest("fieldset")).toBe(secret.closest("fieldset"));
  expect(name.closest("fieldset")).toBe(
    screen.getByText("cli_sample").closest("fieldset"),
  );
  expect(secret).toHaveAttribute("type", "password");
  expect(secret).toHaveValue("");
  expect(screen.getAllByText("状态", { exact: true })).toHaveLength(1);
  expect(screen.getByRole("button", { name: "返回扫码接入" })).toBeEnabled();
  expect(
    screen.queryByText("用于区分不同连接，不改变平台账号或聊天授权。"),
  ).not.toBeInTheDocument();
});
