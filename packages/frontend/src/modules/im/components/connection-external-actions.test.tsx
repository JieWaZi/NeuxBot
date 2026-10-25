import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
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
import type { IMBinding } from "../types";
import "@/shared/i18n";
import { ConnectionAuthorizationDialog } from "./connection-authorization-dialog";
import { ConnectionNewConversationDialog } from "./connection-new-conversation-dialog";

const api = vi.hoisted(() => ({
  bindings: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
  update: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
  create: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  listImBindings: api.bindings,
  updateImBinding: api.update,
  selectImConversation: api.create,
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
vi.mock("./binding-overview", () => ({
  BindingOverview: ({ binding }: { binding: IMBinding }) => (
    <p>{binding.externalChatId}</p>
  ),
}));
vi.mock("./binding-form", () => ({
  BindingForm: ({
    binding,
    beforeFields,
  }: {
    binding: IMBinding;
    beforeFields?: ReactNode;
  }) => (
    <div>
      {beforeFields}
      <p data-testid="binding-form">{binding.id}</p>
    </div>
  ),
}));
const clients: QueryClient[] = [];
beforeAll(() => {
  Element.prototype.hasPointerCapture = () => false;
});
afterAll(() => {
  Reflect.deleteProperty(Element.prototype, "hasPointerCapture");
});
const configured: IMBinding = {
  id: "first",
  connectionId: "connection",
  name: "项目群",
  externalChatId: "chat-first",
  kind: "group",
  state: "pending",
  wakerId: "waker",
  options: {
    runtimeId: "codex",
    model: "gpt-test",
    permissionLevel: "default",
  },
  projectId: "project",
  workingDirectory: "",
  controllerIds: ["owner"],
  allowWorkflowDelivery: false,
  revision: 7,
};
beforeEach(() => {
  api.bindings
    .mockReset()
    .mockResolvedValue({ data: { resources: [configured], totalSize: 1 } });
  api.update.mockReset().mockResolvedValue({ data: {} });
  api.create.mockReset().mockResolvedValue({ data: {} });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});
function show(kind: "authorization" | "conversation") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  const onClose = vi.fn();
  const onSaved = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        {kind === "authorization" ? (
          <ConnectionAuthorizationDialog
            connectionId="connection"
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : (
          <ConnectionNewConversationDialog
            connectionId="connection"
            onClose={onClose}
            onSaved={onSaved}
          />
        )}
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { onClose, onSaved };
}

it("完整配置从外部确认授权，打开时不提交并保留版本和执行边界", async () => {
  const user = userEvent.setup();
  const { onClose, onSaved } = show("authorization");
  await screen.findByText("chat-first");
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.update).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "授权" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(api.update).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { bindingId: "first" },
      body: {
        name: "项目群",
        state: "approved",
        wakerId: "waker",
        channelId: undefined,
        projectId: "project",
        workingDirectory: "",
        options: configured.options,
        controllerIds: ["owner"],
        allowWorkflowDelivery: false,
        revision: 7,
      },
    }),
  );
});

it("缺少配置时在同一授权弹窗内显示表单，选择聊天不会隐式批准", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        { ...configured, options: {} },
        { id: "second", name: "私聊", state: "pending" },
      ],
      totalSize: 2,
    },
  });
  const user = userEvent.setup();
  show("authorization");
  expect(await screen.findByTestId("binding-form")).toHaveTextContent("first");
  await user.click(screen.getByRole("combobox", { name: "选择聊天接入" }));
  await user.click(screen.getByRole("option", { name: "私聊" }));
  expect(screen.getByTestId("binding-form")).toHaveTextContent("second");
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.update).not.toHaveBeenCalled();
});

it("已批准聊天在同层展示配置，撤销通过同弹窗确认并携带原版本", async () => {
  api.bindings.mockResolvedValue({
    data: { resources: [{ ...configured, state: "approved" }], totalSize: 1 },
  });
  const user = userEvent.setup();
  const { onSaved } = show("authorization");
  await screen.findByTestId("binding-form");
  await user.click(screen.getByRole("button", { name: "撤销授权" }));
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(api.update).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.getByTestId("binding-form")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "撤销授权" }));
  await user.click(screen.getByRole("button", { name: "撤销授权" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(api.update).toHaveBeenCalledWith(
    expect.objectContaining({
      body: expect.objectContaining({
        state: "rejected",
        revision: 7,
        options: configured.options,
      }),
    }),
  );
});

it("没有发现聊天时授权仅说明等待首消息且不提交", async () => {
  api.bindings.mockResolvedValue({ data: { resources: [], totalSize: 0 } });
  show("authorization");
  expect(
    await screen.findByText("等待首条消息，聊天会自动出现在这里。"),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "授权" }),
  ).not.toBeInTheDocument();
  expect(api.update).not.toHaveBeenCalled();
});

it("新对话只允许选择已授权聊天，确认后为所选聊天准备新对话", async () => {
  api.bindings.mockResolvedValue({
    data: {
      resources: [
        configured,
        { ...configured, id: "approved-first", state: "approved" },
        {
          ...configured,
          id: "approved-second",
          name: "产品群",
          state: "approved",
        },
      ],
      totalSize: 3,
    },
  });
  const user = userEvent.setup();
  const { onSaved, onClose } = show("conversation");
  const picker = await screen.findByRole("combobox", { name: "选择聊天接入" });
  expect(api.create).not.toHaveBeenCalled();
  await user.click(picker);
  expect(screen.getAllByRole("option")).toHaveLength(2);
  await user.click(screen.getByRole("option", { name: "产品群" }));
  await user.click(screen.getByRole("button", { name: "新建对话" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(api.create).toHaveBeenCalledWith(
    expect.objectContaining({
      path: { bindingId: "approved-second" },
      body: { linkId: "" },
    }),
  );
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
});

it("没有已授权聊天时新对话有说明且不能提交", async () => {
  show("conversation");
  await screen.findByText("请先授权聊天，再新建对话。");
  expect(screen.getByRole("button", { name: "新建对话" })).toBeDisabled();
  expect(api.create).not.toHaveBeenCalled();
});

it("新对话读取失败不伪装为空状态，重试后可取消且不提交", async () => {
  api.bindings.mockRejectedValueOnce(new Error("bindings unavailable"));
  const user = userEvent.setup();
  const { onClose } = show("conversation");
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "详情读取失败，请重试。",
  );
  expect(screen.getByRole("button", { name: "新建对话" })).toBeDisabled();
  api.bindings.mockResolvedValue({
    data: { resources: [{ ...configured, state: "approved" }], totalSize: 1 },
  });
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "新建对话" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(api.create).not.toHaveBeenCalled();
});

it("新对话提交失败留在同层弹窗，错误就近展示并可重试", async () => {
  api.bindings.mockResolvedValue({
    data: { resources: [{ ...configured, state: "approved" }], totalSize: 1 },
  });
  api.create.mockRejectedValueOnce(new Error("create failed"));
  const user = userEvent.setup();
  const { onSaved } = show("conversation");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "新建对话" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "新建对话" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "操作失败，请检查连接或执行配置后重试。",
  );
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(onSaved).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "新建对话" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
});
