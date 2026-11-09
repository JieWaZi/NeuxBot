import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from "react-router-dom";
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
import { IntegrationBreadcrumbs } from "@/modules/settings";
import { useIMConnections } from "./queries";
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
        <BreadcrumbHarness />
        <Routes>
          <Route path="/im" element={<IMPage />} />
          <Route path="/settings/integrations" element={<IMPage />} />
          <Route
            path="/im/:connectionId"
            element={<IMConnectionDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
function BreadcrumbHarness() {
  const [params] = useSearchParams();
  const query = useIMConnections();
  return (
    <IntegrationBreadcrumbs
      connection={query.data?.resources?.find(
        (item) => item.id === params.get("connection"),
      )}
    />
  );
}
function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="current-path">
      {location.pathname}
      {location.search}
    </output>
  );
}
async function openPlatform(platform = "微信") {
  await userEvent.click(
    await screen.findByRole("button", { name: `${platform}连接` }),
  );
}
it.each([
  ["weixin", "微信"],
  ["feishu", "飞书"],
  ["wecom", "企业微信"],
])(
  "%s 二级标题的悬停返回恢复集成总览并保留读取条件",
  async (platform, name) => {
    const user = userEvent.setup();
    show(`/settings/integrations?tab=im&platform=${platform}&keep=value`);
    await user.hover(await screen.findByRole("heading", { name }));
    await user.click(screen.getByRole("button", { name: "返回集成" }));
    expect(screen.getByTestId("current-path")).toHaveTextContent(
      "/settings/integrations?keep=value",
    );
    for (const label of ["微信连接", "飞书连接", "企业微信连接"])
      expect(screen.getByRole("button", { name: label })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "返回集成" }),
    ).not.toBeInTheDocument();
  },
);

it("平台列表整行进入 URL 二级配置，返回恢复三项列表", async () => {
  show();
  for (const platform of ["微信", "飞书", "企业微信"]) {
    expect(
      screen.getByRole("button", { name: `${platform}连接` }),
    ).not.toHaveAttribute("aria-expanded");
    expect(
      screen.queryByRole("button", { name: `添加${platform}机器人` }),
    ).not.toBeInTheDocument();
  }
  await openPlatform();
  expect(await screen.findByText("我的微信")).toBeVisible();
  expect(screen.getByRole("heading", { name: "微信" })).toBeVisible();
  await userEvent.click(screen.getByRole("link", { name: "集成" }));
  expect(screen.queryByText("我的微信")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "飞书连接" })).toBeVisible();
});

it("平台直接添加机器人，保持现有扫码流程", async () => {
  show();
  await openPlatform("飞书");
  await userEvent.click(screen.getByRole("button", { name: "添加飞书机器人" }));
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(screen.getByTestId("current-path")).toHaveTextContent("/im");
});

it("没有连接仍展示全部平台，空平台可进入和添加", async () => {
  api.connections.mockResolvedValue({ data: { resources: [] } });
  show();
  await openPlatform("企业微信");
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

it("读取连接期间平台和添加操作立即可用，进入后展示加载骨架", async () => {
  api.connections.mockImplementation(() => new Promise(() => {}));
  show();
  expect(screen.getByRole("button", { name: "飞书连接" })).toBeEnabled();
  await openPlatform();
  expect(screen.getByRole("button", { name: "添加微信机器人" })).toBeEnabled();
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
  await openPlatform();
  expect(screen.getByRole("button", { name: "添加微信机器人" })).toBeEnabled();
  expect(screen.queryByText(/尚未添加.*连接/)).not.toBeInTheDocument();
  api.connections.mockResolvedValue({ data: { resources: [] } });
  await userEvent.click(screen.getByRole("button", { name: "重试读取" }));
  expect(
    await screen.findByText("尚未添加微信连接，点击「添加」开始连接。"),
  ).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("同平台连接按行展示，进入其他平台不会混入这些连接", async () => {
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
  await openPlatform();
  expect(screen.queryByText("研发机器人")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: "集成" }));
  await openPlatform("飞书");
  expect(
    await screen.findByRole("button", { name: "研发机器人" }),
  ).toBeEnabled();
  expect(screen.getByRole("button", { name: "客服机器人" })).toBeEnabled();
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
  await openPlatform();
  expect(await screen.findByText("待授权")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "我的微信" }));
  const authorize = await screen.findByRole("button", { name: "授权" });
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
  await openPlatform();
  await userEvent.click(
    await screen.findByRole("button", { name: "我的微信" }),
  );
  const create = await screen.findByRole("button", { name: "新建对话" });
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
  const current = screen.getByText("实际对话").closest("li");
  if (!current) throw new Error("对话应显示在记录行中");
  expect(within(current).getByRole("status")).toHaveTextContent("当前对话");
  expect(within(current).getByText(/3 轮/)).toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
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
  await openPlatform("企业微信");
  await user.click(await screen.findByRole("button", { name: "研发企微" }));
  expect(await screen.findByText("Bot ID")).toBeInTheDocument();
  expect(screen.getByText("aibot_example")).toBeInTheDocument();
  expect(screen.queryByText("应用 ID")).not.toBeInTheDocument();
  await user.click(screen.getByRole("link", { name: "企业微信" }));
  expect(
    screen.getByText("扫码创建或授权企业微信智能机器人"),
  ).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "添加企业微信机器人" }));
  expect(
    await screen.findByRole("dialog", { name: "连接企业微信" }),
  ).toBeInTheDocument();
  expect(await screen.findByTitle("扫码连接")).toBeInTheDocument();
});

it("旧连接深链用真实平台归一 URL，保留聊天授权定位", async () => {
  show("/im?connection=connection&binding=chat-one&keep=value");
  expect(await screen.findByText("我的微信", { exact: true })).toBeVisible();
  await waitFor(() =>
    expect(screen.getByTestId("current-path")).toHaveTextContent(
      "platform=weixin",
    ),
  );
  expect(screen.getByTestId("current-path")).toHaveTextContent(
    "binding=chat-one",
  );
  expect(screen.getByTestId("current-path")).toHaveTextContent(
    "connection=connection",
  );
  expect(screen.getByTestId("current-path")).toHaveTextContent("keep=value");
});

it("旧连接深链首次读取保留顶栏父级路径和详情骨架，不闪平台总览", async () => {
  api.connections.mockImplementation(() => new Promise(() => {}));
  show("/im?connection=connection&binding=one");
  expect(screen.getByText("连接详情", { exact: true })).toBeVisible();
  expect(screen.getByRole("link", { name: "集成" })).toHaveAttribute("href");
  expect(screen.getByRole("status", { name: "正在读取…" })).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(
    screen.queryByRole("button", { name: "微信连接" }),
  ).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("link", { name: "集成" }));
  expect(screen.getByRole("button", { name: "微信连接" })).toBeVisible();
});
it("旧连接深链读取失败保留顶栏父级路径，错误可重试", async () => {
  api.connections.mockRejectedValue(new Error("连接读取失败"));
  show("/im?connection=connection");
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByText("连接详情", { exact: true })).toBeVisible();
  expect(screen.getByRole("link", { name: "集成" })).toHaveAttribute("href");
  expect(
    screen.queryByRole("button", { name: "微信连接" }),
  ).not.toBeInTheDocument();
  api.connections.mockResolvedValue({
    data: {
      resources: [{ id: "connection", platform: "feishu", name: "飞书机器人" }],
    },
  });
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByText("飞书机器人", { exact: true })).toBeVisible();
});

it("平台连接整行进入 URL 三级页面，查看与页内编辑不创建 Dialog", async () => {
  const user = userEvent.setup();
  show();
  await openPlatform();
  const row = await screen.findByRole("button", { name: "我的微信" });
  expect(
    screen.queryByRole("button", { name: "编辑我的微信连接" }),
  ).not.toBeInTheDocument();
  await user.click(row);
  expect(await screen.findByText("我的微信", { exact: true })).toBeVisible();
  expect(screen.getByTestId("current-path")).toHaveTextContent(
    "connection=connection",
  );
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "编辑" }));
  expect(screen.getByRole("textbox", { name: "名称" })).toHaveValue("我的微信");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.clear(screen.getByRole("textbox", { name: "名称" }));
  await user.type(screen.getByRole("textbox", { name: "名称" }), "工作微信");
  api.update.mockImplementation(async () => {
    const connection = {
      id: "connection",
      appId: "bot-account",
      name: "工作微信",
      platform: "weixin",
      enabled: true,
      keepAwake: true,
      state: "connected",
      revision: 4,
    };
    api.connections.mockResolvedValue({ data: { resources: [connection] } });
    return { data: connection };
  });
  await user.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByText("工作微信", { exact: true })).toBeVisible();
  expect(
    screen.queryByRole("textbox", { name: "名称" }),
  ).not.toBeInTheDocument();
  expect(screen.getByTestId("current-path")).toHaveTextContent(
    "connection=connection",
  );
  await user.click(screen.getByRole("link", { name: "微信" }));
  expect(await screen.findByRole("heading", { name: "微信" })).toBeVisible();
  expect(screen.getByTestId("current-path")).not.toHaveTextContent(
    "connection=",
  );
});
it("旧连接授权深链直接进入三级，只读进入不自动登录且取消编辑仍留三级", async () => {
  const user = userEvent.setup();
  show("/im?connection=connection&binding=chat-one&platform=weixin");
  expect(await screen.findByText("我的微信", { exact: true })).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "编辑" }));
  await user.clear(screen.getByRole("textbox", { name: "名称" }));
  await user.type(screen.getByRole("textbox", { name: "名称" }), "未保存名称");
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.getByText("我的微信", { exact: true })).toBeVisible();
  expect(
    screen.queryByRole("textbox", { name: "名称" }),
  ).not.toBeInTheDocument();
  expect(api.update).not.toHaveBeenCalled();
  expect(screen.getByTestId("current-path")).toHaveTextContent(
    "binding=chat-one",
  );
});
