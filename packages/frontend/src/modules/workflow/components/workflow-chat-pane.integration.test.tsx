import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { useState } from "react";
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
import "@/shared/i18n";
import { client } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import type * as SceneWorkspace from "@/shared/workspace/app-scene-workspace";
import { chatWorkspaceHandlers } from "../test/chat-workspace-handlers";
import { WorkflowChatPane } from "./workflow-chat-pane";
import { ChatQueueProvider, WorkbenchProvider } from "@/modules/chat";

vi.mock("@/shared/api/events", () => ({ subscribeChat: () => () => {} }));

const scene = vi.hoisted(() => ({ active: true }));
vi.mock("@/shared/workspace/app-scene-workspace", async (importOriginal) => ({
  ...(await importOriginal<typeof SceneWorkspace>()),
  useRouteSceneActive: () => scene.active,
}));
const api = "http://creation.test/api/v1";
const server = setupServer();
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
  client.setConfig({ baseUrl: "http://creation.test" });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  localStorage.clear();
  server.use(...chatWorkspaceHandlers("http://creation.test"));
});
afterEach(() => {
  scene.active = true;
  server.resetHandlers();
});
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
});

it("新对话提供已解析配置，模型修改后同步给画布入口，隐藏场景不允许外部发送", async () => {
  const configuration = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const content = () => (
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
                id="flow-1"
                onConfigurationChange={configuration}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>
  );
  const view = render(content());
  await waitFor(() =>
    expect(configuration).toHaveBeenLastCalledWith({
      runtimeId: "codex",
      model: "native",
      reasoning: "",
      accountId: "system",
      permissionLevel: "default",
    }),
  );
  await userEvent
    .setup()
    .click(await screen.findByRole("button", { name: "native" }));
  await userEvent
    .setup()
    .click(await screen.findByRole("radio", { name: "selected-model" }));
  await waitFor(() =>
    expect(configuration).toHaveBeenLastCalledWith(
      expect.objectContaining({ runtimeId: "codex", model: "selected-model" }),
    ),
  );
  scene.active = false;
  view.rerender(content());
  await waitFor(() =>
    expect(configuration).toHaveBeenLastCalledWith(undefined),
  );
  queryClient.clear();
});

it("已有工作流历史加载后续发输入能够发送", async () => {
  server.use(
    http.get(`${api}/chats/saved-chat`, () =>
      HttpResponse.json({ chat: { id: "saved-chat" } }),
    ),
    http.get(`${api}/chats/saved-chat/messages`, () =>
      HttpResponse.json({
        resources: [
          { id: "saved-user", role: "user", content: "已有需求", revision: 1 },
        ],
        latestSequence: 1,
      }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
                id="saved-flow"
                session={{
                  id: "saved-session",
                  chatId: "saved-chat",
                  defaults: { runtimeId: "codex" },
                }}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  await screen.findByText("已有需求");
  const user = userEvent.setup();
  await user.type(
    screen.getByRole("textbox", { name: /输入消息/ }),
    "继续修改阶段说明",
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: /^发送消息$/ })).toBeEnabled(),
  );
  queryClient.clear();
});

it("已有工作流相同文本的连续发送使用独立接纳身份", async () => {
  const requests: { requestId: string; content: string }[] = [];
  server.use(
    http.get(`${api}/chats/saved-chat`, () =>
      HttpResponse.json({ chat: { id: "saved-chat" } }),
    ),
    http.get(`${api}/chats/saved-chat/messages`, () =>
      HttpResponse.json({
        resources: requests.flatMap((body, index) => [
          {
            id: `user-${index + 1}`,
            role: "user",
            content: body.content,
            executionId: `execution-${index + 1}`,
            revision: 1,
          },
          {
            id: `assistant-${index + 1}`,
            role: "assistant",
            content: "已处理",
            displayParts: [
              { id: `part-${index + 1}`, type: "text", text: "已处理" },
            ],
            executionId: `execution-${index + 1}`,
            status: "completed",
            revision: 2,
          },
        ]),
        latestSequence: requests.length * 2,
      }),
    ),
    http.post(
      `${api}/workflows/saved-flow/creation/messages`,
      async ({ request }) => {
        const body = (await request.json()) as {
          requestId: string;
          content: string;
        };
        requests.push(body);
        const number = requests.length;
        return HttpResponse.json({
          id: "saved-session",
          chatId: "saved-chat",
          acceptedExecutionId: `execution-${number}`,
          execution: {
            id: `execution-${number}`,
            userMessageId: `user-${number}`,
            status: "completed",
          },
        });
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                id="saved-flow"
                session={{
                  id: "saved-session",
                  chatId: "saved-chat",
                  defaults: { runtimeId: "codex" },
                }}
                emptyState={null}
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  const input = screen.getByRole("textbox", { name: /输入消息/ });
  for (const number of [1, 2]) {
    await user.type(input, "继续");
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^发送消息$/ })).toBeEnabled(),
    );
    await user.keyboard("{Enter}");
    await waitFor(() => expect(requests).toHaveLength(number));
    await waitFor(() => expect(input).not.toHaveTextContent("继续"));
  }
  expect(requests.map((item) => item.content)).toEqual(["继续", "继续"]);
  expect(requests[0]?.requestId).not.toBe(requests[1]?.requestId);
  queryClient.clear();
});

it("重新挂载工作流对话时展示正在执行的真实智能体和模型", async () => {
  server.use(
    http.get(`${api}/chats/creation-chat`, () =>
      HttpResponse.json({
        chat: { id: "creation-chat", activeExecutionId: "creation-execution" },
      }),
    ),
    http.get(`${api}/chats/creation-chat/messages`, () =>
      HttpResponse.json({ resources: [] }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
                id="flow-1"
                session={{
                  id: "creation-1",
                  chatId: "creation-chat",
                  defaults: {
                    runtimeId: "codex",
                    model: "actual-model",
                    permissionLevel: "auto",
                  },
                }}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );

  expect(
    await screen.findByRole("button", { name: "actual-model" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /自动审批/ })).toBeInTheDocument();
  queryClient.clear();
});

it("多个保留的工作流场景各自把建议写入当前普通聊天输入框", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
                id="flow-1"
                draft="第一个流程"
              />
              <WorkflowChatPane
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
                onNewChat={() => {}}
                id="flow-2"
                draft="第二个流程"
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );

  const inputs = await screen.findAllByRole("textbox", { name: /输入消息/ });
  expect(inputs).toHaveLength(2);
  await waitFor(() => {
    expect(inputs[0]).toHaveTextContent("第一个流程");
    expect(inputs[1]).toHaveTextContent("第二个流程");
    for (const send of screen.getAllByRole("button", { name: "发送消息" })) {
      expect(send).toBeEnabled();
    }
  });
  queryClient.clear();
});

it("工作流对话仅在活跃场景处理本 Chat 的原生请求，默认不提交任何答案", async () => {
  const approvals: unknown[] = [];
  const inputs: unknown[] = [];
  const snapshot = {
    id: "creation-chat",
    activeExecutionId: "creation-execution",
    pendingApprovals: [
      {
        approvalId: "permission-1",
        title: "读取创作批准的工作目录",
        kind: "read",
        options: [{ id: "allow-1", name: "Allow once", kind: "allow_once" }],
      },
    ],
    pendingElicitations: [
      {
        elicitationId: "input-1",
        mode: "form",
        message: "补充创作操作范围",
        requestedSchema: {
          type: "object",
          properties: { directory: { type: "string", title: "工作目录" } },
        },
      },
    ],
  };
  server.use(
    http.get(`${api}/chats/creation-chat`, () =>
      HttpResponse.json({ chat: snapshot }),
    ),
    http.get(`${api}/chats/creation-chat/messages`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.put(
      `${api}/chats/creation-chat/approvals/permission-1`,
      async ({ request }) => {
        approvals.push(await request.json());
        snapshot.pendingApprovals = [];
        return HttpResponse.json({});
      },
    ),
    http.put(
      `${api}/chats/creation-chat/elicitations/input-1`,
      async ({ request }) => {
        inputs.push(await request.json());
        snapshot.pendingElicitations = [];
        return HttpResponse.json({});
      },
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const content = () => (
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <TooltipProvider>
              <MemoryRouter>
                <WorkflowChatPane
                  mainContent={{
                    contentId: "test-canvas",
                    title: "画布",
                    content: <div>工作流画布</div>,
                  }}
                  onNewChat={() => {}}
                  id="flow-1"
                  session={{ id: "creation-1", chatId: "creation-chat" }}
                />
              </MemoryRouter>
            </TooltipProvider>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>
  );
  const view = render(content());
  await screen.findByText("补充创作操作范围");
  expect(screen.queryByText("读取创作批准的工作目录")).not.toBeInTheDocument();
  expect(approvals).toEqual([]);
  expect(inputs).toEqual([]);

  scene.active = false;
  view.rerender(content());
  expect(screen.queryByText("补充创作操作范围")).not.toBeInTheDocument();
  fireEvent.keyDown(window, { key: "Enter" });
  expect(approvals).toEqual([]);
  scene.active = true;
  view.rerender(content());
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "跳过" }));
  await waitFor(() =>
    expect(inputs).toEqual([{ content: {}, declined: true }]),
  );
  await screen.findByText("读取创作批准的工作目录");
  expect(approvals).toEqual([]);
  await user.click(screen.getByRole("button", { name: /允许本次/ }));
  await waitFor(() => expect(approvals).toEqual([{ optionId: "allow-1" }]));
  queryClient.clear();
});

it.each([
  ["flow-1", false],
  ["new", false],
  ["flow-1", true],
  ["new", true],
] as const)(
  "%s 首次生成接纳后保留真实发送结果（详情读取失败=%s）",
  async (initialId, detailFails) => {
    const content = "生成材料复核工作流";
    let sent = 0;
    const sessionAccepted = vi.fn();
    const create = vi.fn(() => HttpResponse.json({ id: "flow-1" }));
    server.use(
      http.post(`${api}/workflows`, create),
      http.post(`${api}/workflows/flow-1/creation/messages`, () => {
        sent++;
        return HttpResponse.json({
          id: "session-1",
          chatId: "first-chat",
          acceptedExecutionId: "real-execution",
          execution: {
            id: "real-execution",
            userMessageId: "real-user",
            status: "running",
          },
        });
      }),
      http.get(`${api}/chats/first-chat`, () =>
        detailFails
          ? HttpResponse.json({ code: "DatabaseError" }, { status: 500 })
          : HttpResponse.json({
              chat: {
                id: "first-chat",
                activeExecutionId: "real-execution",
                pendingApprovals: [],
                pendingElicitations: [],
              },
            }),
      ),
      http.get(`${api}/chats/first-chat/messages`, () =>
        HttpResponse.json({
          resources: [
            {
              id: "real-user",
              role: "user",
              content,
              executionId: "real-execution",
              revision: 1,
            },
            {
              id: "real-execution",
              role: "assistant",
              content: "真实生成过程",
              displayParts: [
                { id: "part-1", type: "text", text: "真实生成过程" },
              ],
              executionId: "real-execution",
              status: "running",
              revision: 2,
            },
          ],
          latestSequence: 3,
        }),
      ),
    );
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    function FirstSend() {
      const [accepted, setAccepted] = useState(false);
      const [id, setId] = useState<string>(initialId);
      const [draft, setDraft] = useState(initialId === "new" ? "" : content);
      return (
        <WorkflowChatPane
          mainContent={{
            contentId: "test-canvas",
            title: "画布",
            content: <div>工作流画布</div>,
          }}
          onNewChat={() => {}}
          id={id}
          draftId="stable-draft"
          draft={draft}
          onSession={() => {
            sessionAccepted();
            setDraft("");
            setAccepted(true);
          }}
          onCreated={setId}
          session={
            accepted ? { id: "session-1", chatId: "first-chat" } : undefined
          }
        />
      );
    }
    render(
      <QueryClientProvider client={queryClient}>
        <WorkbenchProvider>
          <ChatQueueProvider>
            <TooltipProvider>
              <MemoryRouter>
                <FirstSend />
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkbenchProvider>
      </QueryClientProvider>,
    );
    const user = userEvent.setup();
    const input = await screen.findByRole("textbox", { name: /输入消息/ });
    expect(create).not.toHaveBeenCalled();
    if (initialId === "new") await user.type(input, content);
    await waitFor(() => expect(input).toHaveTextContent(content));
    await user.click(input);
    await user.keyboard("{Enter}");
    await waitFor(() => expect(sent).toBe(1), { timeout: 5000 });
    if (detailFails) {
      await waitFor(() => expect(sessionAccepted).toHaveBeenCalledOnce());
      await waitFor(() =>
        expect(
          screen.getByRole("textbox", { name: /输入消息/ }),
        ).not.toHaveTextContent(content),
      );
      expect(sent).toBe(1);
      expect(create).toHaveBeenCalledTimes(initialId === "new" ? 1 : 0);
      queryClient.clear();
      return;
    }
    await screen.findByText("真实生成过程", {}, { timeout: 5000 });
    await waitFor(() => {
      expect(screen.getAllByText(content, { exact: true })).toHaveLength(1);
      expect(screen.getAllByText("真实生成过程", { exact: true })).toHaveLength(
        1,
      );
      expect(sent).toBe(1);
      expect(
        screen.getByRole("textbox", { name: /输入消息/ }),
      ).not.toHaveTextContent(content);
      expect(create).toHaveBeenCalledTimes(initialId === "new" ? 1 : 0);
    });
    queryClient.clear();
  },
);

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));
