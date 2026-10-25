import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { useState } from "react";
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
import { IMConnectionDetailDialog } from "./im-connection-detail-dialog";

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

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

function show(initialBindingId?: string) {
  const onClose = vi.fn();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  function Harness() {
    const [open, setOpen] = useState(true);
    const location = useLocation();
    return (
      <>
        <output data-testid="location">
          {location.pathname}
          {location.search}
        </output>
        {open ? (
          <IMConnectionDetailDialog
            connectionId="connection"
            initialBindingId={initialBindingId}
            onClose={() => {
              onClose();
              setOpen(false);
            }}
          />
        ) : null}
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
  return { onClose };
}

it("详情首屏保留弹窗标题与关闭入口，读取内容使用骨架", async () => {
  api.connections.mockImplementation(() => new Promise(() => {}));
  const user = userEvent.setup();
  const { onClose } = show();
  const dialog = screen.getByRole("dialog", { name: "连接详情" });
  expect(within(dialog).getByRole("status")).toHaveAttribute(
    "aria-busy",
    "true",
  );
  expect(
    within(dialog).queryByRole("button", { name: "编辑连接" }),
  ).not.toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "关闭" }));
  expect(onClose).toHaveBeenCalledTimes(1);
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
  const dialog = await screen.findByRole("dialog", { name: "我的微信" });
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
  expect(
    within(dialog).getByRole("table", { name: "对话记录" }),
  ).toBeInTheDocument();
  expect(
    within(dialog).queryByRole("button", { name: /编辑|删除|授权|新建|切换/ }),
  ).not.toBeInTheDocument();
  expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.update).not.toHaveBeenCalled();
  expect(api.bindingUpdate).not.toHaveBeenCalled();
  expect(api.remove).not.toHaveBeenCalled();
  expect(api.selectConversation).not.toHaveBeenCalled();
});

it("Escape 关闭只读详情，保留设置页地址且不提交修改", async () => {
  const user = userEvent.setup();
  const { onClose } = show();
  await screen.findByRole("dialog", { name: "我的微信" });
  await user.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByTestId("location")).toHaveTextContent(
    "/settings/im?section=publishing",
  );
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
