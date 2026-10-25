import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import "@/shared/i18n";
import { ChatQueueProvider, WorkbenchProvider } from "@/modules/chat";
import { MemoryRouter } from "react-router-dom";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { chatWorkspaceHandlers } from "../test/chat-workspace-handlers";
import { selectFiles } from "@/shared/platform";
import type * as Desktop from "@/shared/platform";
import type { ReactNode } from "react";
import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { client } from "@/shared/api/generated/client.gen";
import { record } from "../model/types";
import { PhaseAdjustmentComposer } from "./phase-adjustment-composer";

const origin = "http://workflow-phase.test";
vi.mock("@/shared/api/events", () => ({ subscribeChat: () => () => {} }));
vi.mock("@/shared/platform", async (importOriginal) => ({
  ...(await importOriginal<typeof Desktop>()),
  selectFiles: vi.fn(async () => ["/tmp/requirements.txt"]),
}));
const server = setupServer();
const renderComposer = (content: ReactNode) =>
  render(
    <MemoryRouter>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>{content}</TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </MemoryRouter>,
  );
beforeEach(() => {
  localStorage.clear();
  server.use(
    http.get(`${origin}/api/v1/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            isDefault: true,
            defaultModel: "native",
            defaultPermissionLevel: "default",
            models: [
              "native",
              "selected-model",
              "actual-model",
              "saved-model",
              "current-model",
            ].map((id) => ({
              id,
              name: id,
              permissionLevels: ["default", "auto", "full-access"],
              reasoningOptions: [
                { id: "low", name: "Low" },
                { id: "high", name: "High" },
              ],
            })),
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId`, ({ params }) =>
      HttpResponse.json({
        chat: { id: params.chatId, workspaceDirectory: "/workflow" },
      }),
    ),
    http.get(`${origin}/api/v1/chats/:chatId/messages`, () =>
      HttpResponse.json({ resources: [], latestSequence: 0 }),
    ),
    ...chatWorkspaceHandlers(origin),
  );
});
beforeAll(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

it("阶段完整 Composer 的模型、思考、权限与附件选择实际进入创作请求", async () => {
  const user = userEvent.setup();
  const sends: unknown[] = [];
  const accepted = vi.fn();
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/annotations`, () =>
      HttpResponse.json({ id: "annotation" }),
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/creation/messages`,
      async ({ request }) => {
        sends.push(await request.json());
        return HttpResponse.json({
          id: "session-1",
          chatId: "chat-1",
          acceptedExecutionId: "execution-1",
        });
      },
    ),
  );
  renderComposer(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={1}
        defaults={{ runtimeId: "codex" }}
        onAccepted={accepted}
      />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
      "Codex",
    ),
  );
  expect(screen.getByTestId("composer-workspace-control")).toHaveTextContent(
    "默认",
  );
  await user.click(await screen.findByRole("button", { name: /^native/ }));
  await user.click(screen.getByRole("button", { name: "选择模型" }));
  await user.click(
    await screen.findByRole("radio", { name: "selected-model" }),
  );
  const thinking = await screen.findByRole("slider");
  thinking.focus();
  await user.keyboard("{End}");
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: "执行权限：默认权限" }));
  await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
  await user.click(screen.getByRole("button", { name: "添加附件" }));
  await screen.findByText("requirements.txt");
  expect(selectFiles).toHaveBeenCalled();
  await user.type(
    screen.getByRole("textbox", { name: /输入消息/ }),
    "按附件补全阶段",
  );
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledWith("session-1"));
  expect(sends).toEqual([
    expect.objectContaining({
      defaults: {
        runtimeId: "codex",
        accountId: "system",
        model: "selected-model",
        reasoning: "high",
        permissionLevel: "full-access",
      },
      attachments: [{ sourcePath: "/tmp/requirements.txt" }],
      content: "调整阶段「准备会议」：按附件补全阶段",
    }),
  ]);
});

it("阶段账号切换沿用聊天的上下文确认并真实提交，不覆盖主聊天账号偏好", async () => {
  const user = userEvent.setup();
  const sends: unknown[] = [];
  const accepted = vi.fn();
  localStorage.setItem("ally.account.chat:chat-1", "system");
  server.use(
    http.get(`${origin}/api/v1/ai-accounts`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "phase-account",
            name: "阶段账号",
            authMode: "api-key",
            endpoints: [
              {
                protocol: "openai-responses",
                baseUrl: "https://example.test/v1",
              },
            ],
            selectedModels: ["account-model"],
          },
        ],
      }),
    ),
    http.get(`${origin}/api/v1/ai-accounts/phase-account/thinking`, () =>
      HttpResponse.json({ available: false, options: [] }),
    ),
    http.get(`${origin}/api/v1/chats/chat-1`, () =>
      HttpResponse.json({
        chat: {
          id: "chat-1",
          lastRuntimeId: "codex",
          lastAccountId: "system",
          lastModel: "native",
        },
      }),
    ),
    http.get(`${origin}/api/v1/chats/chat-1/messages`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "previous-user",
            role: "user",
            content: "已有创作需求",
            revision: 1,
          },
        ],
        latestSequence: 1,
      }),
    ),
    http.post(`${origin}/api/v1/workflows/flow/annotations`, () =>
      HttpResponse.json({ id: "annotation" }),
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/creation/messages`,
      async ({ request }) => {
        sends.push(await request.json());
        return HttpResponse.json({
          id: "session-1",
          chatId: "chat-1",
          acceptedExecutionId: "execution-1",
        });
      },
    ),
  );
  renderComposer(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={1}
        session={{ id: "session-1", chatId: "chat-1" }}
        defaults={{ runtimeId: "codex", accountId: "system" }}
        onAccepted={accepted}
      />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
      "Codex",
    ),
  );
  await user.click(screen.getByTestId("composer-runtime-control"));
  await user.click(screen.getByRole("menuitemradio", { name: /Codex/ }));
  await user.click(screen.getByRole("menuitemradio", { name: "阶段账号" }));
  expect(
    await screen.findByRole("dialog", { name: "在当前聊天中继续" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "确认切换并继续" }));
  await screen.findByRole("button", { name: "account-model" });
  await user.type(
    screen.getByRole("textbox", { name: /输入消息/ }),
    "切换账号调整",
  );
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledWith("session-1"));
  expect(sends).toEqual([
    expect.objectContaining({
      sessionId: "session-1",
      defaults: expect.objectContaining({
        accountId: "phase-account",
        model: "account-model",
      }),
      startNewSession: true,
      historyMode: "recent",
      historyTurns: 5,
    }),
  ]);
  expect(localStorage.getItem("ally.account.chat:chat-1")).toBe("system");
  expect(screen.queryByText("已有创作需求")).not.toBeInTheDocument();
});

it("已有创作正在执行时阶段输入等待空闲，仍展示完整配置并保留独立输入", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  server.use(
    http.get(`${origin}/api/v1/chats/chat-1`, () =>
      HttpResponse.json({
        chat: { id: "chat-1", activeExecutionId: "running-execution" },
      }),
    ),
  );
  renderComposer(
    <QueryClientProvider client={queryClient}>
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={1}
        session={{ id: "session-1", chatId: "chat-1" }}
        defaults={{ runtimeId: "codex" }}
        onAccepted={() => {}}
      />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
      "Codex",
    ),
  );
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: /输入消息/ })).toHaveAttribute(
      "contenteditable",
      "false",
    ),
  );
  queryClient.setQueryData(getChatQueryKey({ path: { chatId: "chat-1" } }), {
    chat: { id: "chat-1" },
  });
  await waitFor(() =>
    expect(screen.getByRole("textbox", { name: /输入消息/ })).toHaveAttribute(
      "contenteditable",
      "true",
    ),
  );
});

it("阶段要求绑定基准版本与稳定阶段，发送失败后保留输入并以同一身份重试", async () => {
  const user = userEvent.setup();
  const annotations: unknown[] = [];
  const sends: unknown[] = [];
  const accepted = vi.fn();
  const defaults = { runtimeId: "codex", model: "saved-model" };
  server.use(
    http.post(
      `${origin}/api/v1/workflows/flow/annotations`,
      async ({ request }) => {
        annotations.push(await request.json());
        return HttpResponse.json({ id: "annotation-1" });
      },
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/creation/messages`,
      async ({ request }) => {
        sends.push(await request.json());
        return sends.length === 1
          ? HttpResponse.json({ message: "暂时无法发送" }, { status: 500 })
          : HttpResponse.json({
              id: "session-1",
              chatId: "chat-1",
              acceptedExecutionId: "execution-1",
            });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  const chatKey = getChatQueryKey({ path: { chatId: "chat-1" } });
  queryClient.setQueryData(chatKey, { chat: { id: "chat-1" } });
  renderComposer(
    <QueryClientProvider client={queryClient}>
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={2}
        session={{ id: "session-1", chatId: "chat-1", defaults }}
        defaults={defaults}
        onAccepted={accepted}
      />
    </QueryClientProvider>,
  );
  expect(screen.getByRole("button", { name: "发送消息" })).toBeDisabled();
  await user.type(
    screen.getByRole("textbox", { name: /输入消息/ }),
    "为材料清单增加负责人",
  );
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await screen.findByRole("alert");
  expect(screen.getByRole("textbox")).toHaveValue("为材料清单增加负责人");
  expect(accepted).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledWith("session-1"));
  expect(annotations[0]).toMatchObject({
    baseRevisionId: "version-3",
    nodeId: "prepare",
    text: "为材料清单增加负责人",
  });
  expect(annotations[1]).toEqual(annotations[0]);
  expect(sends[1]).toEqual(sends[0]);
  expect(sends[0]).toMatchObject({ sessionId: "session-1", defaults });
  expect(record(sends[0])?.content).toEqual(
    "调整阶段「准备会议」：为材料清单增加负责人",
  );
  expect(screen.getByRole("textbox")).toHaveValue("");
});

it.each(["http", "network"] as const)(
  "阶段发送已接纳后，详情查询 %s 失败不恢复输入或阻止会话切换",
  async (failure) => {
    const user = userEvent.setup();
    const accepted = vi.fn();
    const sends = vi.fn(() =>
      HttpResponse.json({
        id: "accepted-session",
        chatId: "accepted-chat",
        acceptedExecutionId: "accepted-execution",
        execution: { id: "accepted-execution", status: "running" },
      }),
    );
    server.use(
      http.post(`${origin}/api/v1/workflows/flow/annotations`, () =>
        HttpResponse.json({ id: "annotation" }),
      ),
      http.post(`${origin}/api/v1/workflows/flow/creation/messages`, sends),
      http.get(`${origin}/api/v1/chats/accepted-chat`, () =>
        failure === "network"
          ? HttpResponse.error()
          : HttpResponse.json({ message: "详情暂不可用" }, { status: 500 }),
      ),
    );
    renderComposer(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: {
              mutations: { retry: false },
              queries: { retry: false },
            },
          })
        }
      >
        <PhaseAdjustmentComposer
          id="flow"
          revisionId="version-3"
          phase={{ id: "prepare", title: "准备会议" }}
          ordinal={1}
          defaults={{ runtimeId: "codex" }}
          onAccepted={accepted}
        />
      </QueryClientProvider>,
    );
    const input = screen.getByRole("textbox", { name: /输入消息/ });
    await user.type(input, "为材料清单增加负责人");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() =>
      expect(accepted).toHaveBeenCalledWith("accepted-session"),
    );
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(sends).toHaveBeenCalledTimes(1);
    expect(input).toHaveValue("");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  },
);

it("阶段调整沿用聊天默认配置并使用当前模型新建会话", async () => {
  const user = userEvent.setup();
  const sends: unknown[] = [];
  const accepted = vi.fn();
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/annotations`, () =>
      HttpResponse.json({ id: "annotation-1" }),
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/creation/messages`,
      async ({ request }) => {
        sends.push(await request.json());
        return HttpResponse.json({
          id: "new-session",
          chatId: "new-chat",
          acceptedExecutionId: "new-execution",
        });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  const content = (defaults?: { runtimeId: string; model: string }) => (
    <QueryClientProvider client={queryClient}>
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={2}
        defaults={defaults}
        onAccepted={accepted}
      />
    </QueryClientProvider>
  );
  const view = renderComposer(content());
  await user.type(screen.getByRole("textbox"), "补充材料负责人");
  const defaults = { runtimeId: "codex", model: "current-model" };
  view.rerender(
    <MemoryRouter>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>{content(defaults)}</TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </MemoryRouter>,
  );
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(accepted).toHaveBeenCalledWith("new-session"));
  expect(sends).toHaveLength(1);
  expect(record(sends[0])?.sessionId).toBeUndefined();
  expect(record(sends[0])?.defaults).toMatchObject(defaults);
});

it("阶段输入沿用聊天键盘交互：Shift+Enter 换行，普通 Enter 发送且不重复提交表单", async () => {
  const user = userEvent.setup();
  const annotations: unknown[] = [];
  const sends: unknown[] = [];
  server.use(
    http.post(
      `${origin}/api/v1/workflows/flow/annotations`,
      async ({ request }) => {
        annotations.push(await request.json());
        return HttpResponse.json({ id: "annotation-1" });
      },
    ),
    http.post(
      `${origin}/api/v1/workflows/flow/creation/messages`,
      async ({ request }) => {
        sends.push(await request.json());
        return HttpResponse.json({
          id: "session-1",
          chatId: "chat-1",
          acceptedExecutionId: "execution-1",
        });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  renderComposer(
    <QueryClientProvider client={queryClient}>
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={1}
        defaults={{ runtimeId: "codex" }}
        onAccepted={() => {}}
      />
    </QueryClientProvider>,
  );
  const input = screen.getByRole("textbox", { name: /输入消息/ });
  await user.type(input, "检查前置条件");
  await user.keyboard("{Shift>}{Enter}{/Shift}补充恢复步骤");
  expect(sends).toEqual([]);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(sends).toHaveLength(1));
  expect(annotations).toHaveLength(1);
  expect(record(annotations[0])?.text).toBe("检查前置条件\n补充恢复步骤");
  expect(record(sends[0])?.content).toBe(
    "调整阶段「准备会议」：检查前置条件\n补充恢复步骤",
  );
  await waitFor(() => expect(input).toHaveValue(""));
});

it("阶段输入复用聊天的中文输入法保护，确认候选字的 Enter 不触发批注或发送", async () => {
  const user = userEvent.setup();
  const annotations = vi.fn();
  const sends = vi.fn();
  server.use(
    http.post(`${origin}/api/v1/workflows/flow/annotations`, () => {
      annotations();
      return HttpResponse.json({ id: "annotation-1" });
    }),
    http.post(`${origin}/api/v1/workflows/flow/creation/messages`, () => {
      sends();
      return HttpResponse.json({
        id: "session-1",
        chatId: "chat-1",
        acceptedExecutionId: "execution-1",
      });
    }),
  );
  renderComposer(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <PhaseAdjustmentComposer
        id="flow"
        revisionId="version-3"
        phase={{ id: "prepare", title: "准备会议" }}
        ordinal={1}
        defaults={{ runtimeId: "codex" }}
        onAccepted={() => {}}
      />
    </QueryClientProvider>,
  );
  const input = screen.getByRole("textbox", { name: /输入消息/ });
  await user.type(input, "补充条件");
  fireEvent.compositionStart(input);
  fireEvent.keyDown(input, { key: "Enter", isComposing: true, keyCode: 229 });
  fireEvent.compositionEnd(input);
  fireEvent.keyDown(input, { key: "Enter" });
  expect(annotations).not.toHaveBeenCalled();
  expect(sends).not.toHaveBeenCalled();
  await waitFor(() => expect(input).toHaveValue("补充条件"));
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(sends).toHaveBeenCalledTimes(1));
  expect(annotations).toHaveBeenCalledTimes(1);
});
