import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
import { ConnectionNewConversationDialog } from "./connection-new-conversation-dialog";

const api = vi.hoisted(() => ({
  bindings: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
  create: vi.fn<(options: unknown) => Promise<{ data: object }>>(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  listImBindings: api.bindings,
  selectImConversation: api.create,
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
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
};
beforeEach(() => {
  api.bindings
    .mockReset()
    .mockResolvedValue({ data: { resources: [configured], totalSize: 1 } });
  api.create.mockReset().mockResolvedValue({ data: {} });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});
function show() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  clients.push(client);
  const onClose = vi.fn();
  const onSaved = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ConnectionNewConversationDialog
          connectionId="connection"
          onClose={onClose}
          onSaved={onSaved}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { onClose, onSaved };
}

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
  const { onSaved, onClose } = show();
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
  show();
  await screen.findByText("请先授权聊天，再新建对话。");
  expect(screen.getByRole("button", { name: "新建对话" })).toBeDisabled();
  expect(api.create).not.toHaveBeenCalled();
});

it("新对话读取失败不伪装为空状态，重试后可取消且不提交", async () => {
  api.bindings.mockRejectedValueOnce(new Error("bindings unavailable"));
  const user = userEvent.setup();
  const { onClose } = show();
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
  const { onSaved } = show();
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
