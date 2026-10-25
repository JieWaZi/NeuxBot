import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
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
import { IMPage } from "./im-page";
import { IMConnectionDetailPage } from "./im-connection-detail-page";

const api = vi.hoisted(() => ({
  connections: vi.fn<(options?: unknown) => Promise<{ data: object }>>(),
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
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});
function show(path = "/im") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <LocationProbe />
        <Routes>
          <Route path="/im" element={<IMPage />} />
          <Route
            path="/im/:connectionId"
            element={<IMConnectionDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function LocationProbe() {
  const location = useLocation();
  return <output data-testid="current-path">{location.pathname}</output>;
}
async function expandPlatform(platform = "微信") {
  await userEvent.click(
    await screen.findByRole("button", { name: `${platform}连接` }),
  );
}
it("设置首屏展示全部平台，连接折叠且不显示资源页工具栏", async () => {
  show();
  for (const platform of ["微信", "飞书", "企业微信"]) {
    expect(
      screen.getByRole("button", { name: `${platform}连接` }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.getByRole("button", { name: `添加${platform}机器人` }),
    ).toBeEnabled();
  }
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
  expect(screen.queryByText("我的微信")).not.toBeInTheDocument();
  await expandPlatform();
  expect(await screen.findByText("我的微信")).toBeInTheDocument();
  expect(screen.getByText("bot-account")).toBeInTheDocument();
  expect(screen.getByText("已连接")).toBeInTheDocument();
  expect(screen.queryByRole("article")).not.toBeInTheDocument();
  await expandPlatform();
  expect(screen.queryByText("我的微信")).not.toBeInTheDocument();
});

it("连接行查看只读详情，关闭后保留平台展开并恢复焦点", async () => {
  show();
  await expandPlatform();
  const view = await screen.findByRole("button", {
    name: "查看我的微信连接详情",
  });
  await userEvent.click(view);
  const detail = await screen.findByRole("dialog", { name: /我的微信/ });
  expect(within(detail).getByText("bot-account")).toBeInTheDocument();
  expect(
    within(detail).getByRole("heading", { name: "聊天接入" }),
  ).toBeInTheDocument();
  expect(
    within(detail).queryByRole("button", { name: /编辑|授权|新建|删除|切换/ }),
  ).not.toBeInTheDocument();
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.update).not.toHaveBeenCalled();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(api.selectConversation).not.toHaveBeenCalled();
  expect(api.remove).not.toHaveBeenCalled();
  expect(screen.getByTestId("current-path")).toHaveTextContent("/im");
  await userEvent.click(within(detail).getByRole("button", { name: "关闭" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "微信连接" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  await waitFor(() => expect(view).toHaveFocus());
});

it.each([
  "查看我的微信连接详情",
  "编辑我的微信连接",
  "授权我的微信连接",
  "为我的微信新建对话",
  "删除我的微信连接",
])("图标操作“%s”提供可访问名称和悬停说明", async (label) => {
  const user = userEvent.setup();
  show();
  await expandPlatform();
  const action = await screen.findByRole("button", { name: label });
  expect(action).toBeEnabled();
  await user.hover(action);
  expect(
    await screen.findByRole("tooltip", { name: label }),
  ).toBeInTheDocument();
});

it("连接行编辑直接打开单层表单，取消返回原行且不提交", async () => {
  show();
  await expandPlatform();
  const edit = await screen.findByRole("button", { name: "编辑我的微信连接" });
  await userEvent.click(edit);
  const form = await screen.findByRole("dialog", { name: "编辑" });
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(within(form).getByRole("textbox", { name: "名称" })).toHaveValue(
    "我的微信",
  );
  expect(within(form).getByRole("button", { name: "保存" })).toBeEnabled();
  expect(
    within(form).queryByRole("heading", { name: "对话记录" }),
  ).not.toBeInTheDocument();
  await userEvent.click(within(form).getByRole("button", { name: "关闭" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(api.update).not.toHaveBeenCalled();
  await waitFor(() => expect(edit).toHaveFocus());
});

it("连接行删除需要确认，取消不提交且恢复操作焦点", async () => {
  show();
  await expandPlatform();
  const remove = await screen.findByRole("button", {
    name: "删除我的微信连接",
  });
  await userEvent.click(remove);
  const dialog = screen.getByRole("dialog", { name: "删除连接" });
  expect(api.remove).not.toHaveBeenCalled();
  await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(api.remove).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await waitFor(() => expect(remove).toHaveFocus());
});

it("确认删除携带当前版本和依赖摘要，刷新后保留所在平台", async () => {
  show();
  await expandPlatform();
  await userEvent.click(
    await screen.findByRole("button", { name: "删除我的微信连接" }),
  );
  const dialog = screen.getByRole("dialog", { name: "删除连接" });
  api.connections.mockResolvedValue({ data: { resources: [] } });
  const confirm = within(dialog).getByRole("button", {
    name: "删除",
  });
  await waitFor(() => expect(confirm).toBeEnabled());
  await userEvent.click(confirm);
  await waitFor(() => expect(api.remove).toHaveBeenCalledTimes(1));
  expect(api.remove.mock.calls[0]?.[0]).toMatchObject({
    path: { connectionId: "connection" },
    query: { revision: 3, dependencyDigest: "empty" },
  });
  await waitFor(() =>
    expect(screen.queryByText("我的微信")).not.toBeInTheDocument(),
  );
  expect(screen.getByRole("button", { name: "微信连接" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  expect(screen.getByTestId("current-path")).toHaveTextContent("/im");
});

it("平台直接添加机器人，保持现有扫码流程", async () => {
  show();
  await userEvent.click(screen.getByRole("button", { name: "添加飞书机器人" }));
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(screen.getByTestId("current-path")).toHaveTextContent("/im");
});

it("没有连接仍展示全部平台，空平台可展开和添加", async () => {
  api.connections.mockResolvedValue({ data: { resources: [] } });
  show();
  await expandPlatform("企业微信");
  expect(
    await screen.findByText("尚未添加企业微信连接，点击「添加」开始连接。"),
  ).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "添加企业微信机器人" }),
  );
  expect(
    await screen.findByRole("dialog", { name: "连接企业微信" }),
  ).toBeInTheDocument();
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
});

it("读取连接期间平台和添加操作立即可用，展开展示加载骨架", async () => {
  api.connections.mockImplementation(() => new Promise(() => {}));
  show();
  expect(screen.getByRole("button", { name: "添加飞书机器人" })).toBeEnabled();
  await expandPlatform();
  expect(screen.getByRole("status", { name: "正在读取…" })).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(screen.queryByText(/尚未添加.*连接/)).not.toBeInTheDocument();
});

it("连接读取失败保留平台，重试后才展示真实空状态", async () => {
  api.connections.mockRejectedValue(new Error("连接读取失败"));
  show();
  await screen.findByRole("alert");
  await expandPlatform();
  for (const platform of ["微信", "飞书", "企业微信"])
    expect(
      screen.getByRole("button", { name: `添加${platform}机器人` }),
    ).toBeEnabled();
  expect(screen.queryByText(/尚未添加.*连接/)).not.toBeInTheDocument();
  api.connections.mockResolvedValue({ data: { resources: [] } });
  await userEvent.click(screen.getByRole("button", { name: "重试读取" }));
  expect(
    await screen.findByText("尚未添加微信连接，点击「添加」开始连接。"),
  ).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("同平台连接按行展示，展开其他平台不会混入这些连接", async () => {
  api.connections.mockResolvedValue({
    data: {
      resources: [
        {
          id: "first",
          name: "研发机器人",
          appId: "cli_first",
          platform: "feishu",
          revision: 2,
        },
        {
          id: "second",
          name: "客服机器人",
          appId: "cli_second",
          platform: "feishu",
          revision: 4,
        },
      ],
    },
  });
  show();
  await expandPlatform();
  expect(screen.queryByText("研发机器人")).not.toBeInTheDocument();
  await expandPlatform("飞书");
  expect(
    await screen.findByRole("button", { name: "编辑研发机器人连接" }),
  ).toBeEnabled();
  expect(
    screen.getByRole("button", { name: "编辑客服机器人连接" }),
  ).toBeEnabled();
  expect(screen.getByText("cli_first")).toBeInTheDocument();
  expect(screen.getByText("cli_second")).toBeInTheDocument();
});

it("外部授权入口打开单层授权配置，取消不批准聊天", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "pending",
          connectionId: "connection",
          name: "待接入私聊",
          kind: "direct",
          state: "pending",
          revision: 7,
        },
      ],
      totalSize: 1,
    },
  });
  show();
  await expandPlatform();
  expect(await screen.findByText("待授权")).toBeInTheDocument();
  const authorize = screen.getByRole("button", { name: "授权我的微信连接" });
  await userEvent.click(authorize);
  const dialog = await screen.findByRole("dialog");
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  await waitFor(() => expect(authorize).toHaveFocus());
});

it("行上新对话入口打开单层确认，取消不改变聊天指针", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "connection",
          name: "工作聊天",
          state: "approved",
          wakerId: "waker",
        },
      ],
      totalSize: 1,
    },
  });
  show();
  await expandPlatform();
  const create = await screen.findByRole("button", {
    name: "为我的微信新建对话",
  });
  await userEvent.click(create);
  const dialog = await screen.findByRole("dialog", { name: "新建对话" });
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.selectConversation).not.toHaveBeenCalled();
  await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(api.selectConversation).not.toHaveBeenCalled();
  await waitFor(() => expect(create).toHaveFocus());
});

it("详情展示真实对话及当前指针，同时隐藏全部变更操作", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "connection",
          name: "我的聊天",
          kind: "direct",
          state: "approved",
          activeLinkId: "old",
        },
      ],
      totalSize: 1,
    },
  });
  api.conversations.mockResolvedValue({
    data: {
      resources: [
        {
          id: "old",
          chatId: "chat",
          title: "实际对话",
          turnCount: 3,
          createdAt: 1791450000000,
        },
        {
          id: "another",
          chatId: "another-chat",
          title: "历史对话",
          turnCount: 1,
        },
      ],
    },
  });
  show("/im/connection");
  expect(await screen.findByText("实际对话")).toBeInTheDocument();
  const current = screen.getByRole("row", { name: /实际对话/ });
  expect(within(current).getByRole("status")).toHaveTextContent("当前对话");
  expect(within(current).getByRole("cell", { name: "3" })).toBeInTheDocument();
  const dialog = screen.getByRole("dialog");
  expect(
    within(dialog).queryByRole("button", { name: /编辑|授权|新建|删除|切换/ }),
  ).not.toBeInTheDocument();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(api.selectConversation).not.toHaveBeenCalled();
});

it("只读详情请求失败保留分区并提供重试，不伪装成空记录", async () => {
  api.conversations.mockRejectedValue(new Error("会话读取失败"));
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        {
          id: "binding",
          connectionId: "connection",
          name: "当前聊天",
          state: "approved",
        },
      ],
      totalSize: 1,
    },
  });
  show("/im/connection");
  await screen.findByRole("alert");
  expect(screen.queryByText("暂无对话记录")).not.toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "对话记录" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "重试读取" })).toBeEnabled();
  expect(api.selectConversation).not.toHaveBeenCalled();
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

it("企业微信平台入口直接打开扫码弹窗，详情使用 Bot ID 标识", async () => {
  api.connections.mockResolvedValue({
    data: {
      resources: [
        {
          id: "connection",
          name: "研发企微",
          platform: "wecom",
          appId: "aibot_example",
          enabled: true,
          revision: 1,
        },
      ],
    },
  });
  const user = userEvent.setup();
  show();
  await expandPlatform("企业微信");
  await user.click(
    await screen.findByRole("button", { name: "查看研发企微连接详情" }),
  );
  expect(await screen.findByText("Bot ID")).toBeInTheDocument();
  expect(
    within(screen.getByRole("dialog")).getByText("aibot_example"),
  ).toBeInTheDocument();
  expect(screen.queryByText("应用 ID")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "关闭" }));
  expect(
    screen.getByText("扫码创建或授权企业微信智能机器人"),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "添加企业微信机器人" }));
  expect(
    await screen.findByRole("dialog", { name: "连接企业微信" }),
  ).toBeInTheDocument();
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
});
