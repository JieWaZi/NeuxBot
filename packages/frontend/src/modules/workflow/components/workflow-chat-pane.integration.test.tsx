import { getChatQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
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
import type { subscribeChat } from "@/shared/api/events";
import type * as SceneWorkspace from "@/shared/workspace/app-scene-workspace";
import { chatWorkspaceHandlers } from "../test/chat-workspace-handlers";
import { WorkflowChatPane } from "./workflow-chat-pane";
import { ChatQueueProvider, WorkbenchProvider } from "@/modules/chat";

type ChatSubscription = Parameters<typeof subscribeChat>[1];
const chatSubscriptions = vi.hoisted(() => new Map<string, ChatSubscription>());
vi.mock("@/shared/api/events", () => ({
  subscribeChat: (chatId: string, options: ChatSubscription) => {
    chatSubscriptions.set(chatId, options);
    return () => {
      if (chatSubscriptions.get(chatId) === options)
        chatSubscriptions.delete(chatId);
    };
  },
}));

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
  chatSubscriptions.clear();
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
  const question = await screen.findByText("补充创作操作范围");
  expect(question.closest("[data-chat-main-content]")).toBeNull();
  expect(question.closest("[data-chat-composer]")).toBeInTheDocument();
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

it("工作流创作通过实时事件呈现 WorkBuddy 三题并提交原 Chat 后继续执行", async () => {
  const answers: unknown[] = [];
  const pendingElicitations: unknown[] = [];
  const snapshot = {
    id: "creation-live-chat",
    activeExecutionId: "creation-live-execution",
    latestSequence: 1,
    lastRuntimeId: "workbuddy",
    lastModel: "deepseek-v4.1-flash",
    lastPermissionLevel: "full-access",
    pendingElicitations,
  };
  server.use(
    http.get(`${api}/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "workbuddy",
            name: "WorkBuddy",
            enabled: true,
            state: "ready",
            isDefault: true,
            defaultModel: "deepseek-v4.1-flash",
            defaultPermissionLevel: "full-access",
            models: [
              {
                id: "deepseek-v4.1-flash",
                name: "DeepSeek",
                permissionLevels: ["full-access"],
              },
            ],
          },
        ],
      }),
    ),
    http.get(`${api}/chats/creation-live-chat`, () =>
      HttpResponse.json({ chat: snapshot }),
    ),
    http.get(`${api}/chats/creation-live-chat/messages`, () =>
      HttpResponse.json({
        resources: [],
        latestSequence: snapshot.latestSequence,
      }),
    ),
    http.put(
      `${api}/chats/creation-live-chat/elicitations/native-question`,
      async ({ request }) => {
        answers.push(await request.json());
        return HttpResponse.json({});
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
                id="flow-1"
                session={{ id: "creation-1", chatId: "creation-live-chat" }}
                onNewChat={() => {}}
                mainContent={{
                  contentId: "test-canvas",
                  title: "画布",
                  content: <div>工作流画布</div>,
                }}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(chatSubscriptions.has("creation-live-chat")).toBe(true),
  );
  expect(
    screen.queryByText("Please answer the following questions."),
  ).not.toBeInTheDocument();
  const requestedEvent = {
    sequence: 2,
    type: "elicitation.requested",
    elicitationRequest: {
      elicitationId: "native-question",
      mode: "form",
      message: "Please answer the following questions.",
      requestedSchema: {
        type: "object",
        properties: {
          question_0: {
            type: "array",
            title: "数据源",
            description: "从哪些类型的数据源抓取 AI 资讯？",
            uniqueItems: true,
            items: {
              anyOf: [
                {
                  const: "主流科技媒体",
                  title: "主流科技媒体",
                  description: "TechCrunch、The Verge",
                },
                {
                  const: "官方博客与论文",
                  title: "官方博客与论文",
                  description: "官方博客、arXiv",
                },
                {
                  const: "技术社区",
                  title: "技术社区",
                  description: "Hacker News、Reddit",
                },
                {
                  const: "中文科技媒体",
                  title: "中文科技媒体",
                  description: "机器之心、量子位",
                },
              ],
            },
          },
          question_0_custom: {
            type: "string",
            title: "Other",
            _meta: {
              _askUserQuestionCustomAnswer: {
                isCustomAnswer: true,
                questionId: "question_0",
              },
            },
          },
          question_1: {
            type: "string",
            title: "时间范围",
            description: "汇总覆盖多长时间范围？",
            oneOf: [
              { const: "过去 24 小时", title: "过去 24 小时" },
              { const: "近 7 天", title: "近 7 天" },
              { const: "近 3 天", title: "近 3 天" },
            ],
          },
          question_1_custom: {
            type: "string",
            title: "Other",
            _meta: {
              _askUserQuestionCustomAnswer: {
                isCustomAnswer: true,
                questionId: "question_1",
              },
            },
          },
          question_2: {
            type: "string",
            title: "交付方式",
            description: "总结结果以什么形式交付？",
            oneOf: [
              { const: "仅返回聊天摘要", title: "仅返回聊天摘要" },
              {
                const: "生成 Markdown 报告文件",
                title: "生成 Markdown 报告文件",
              },
              {
                const: "报告文件 + IM 推送",
                title: "报告文件 + IM 推送",
              },
            ],
          },
          question_2_custom: {
            type: "string",
            title: "Other",
            _meta: {
              _askUserQuestionCustomAnswer: {
                isCustomAnswer: true,
                questionId: "question_2",
              },
            },
          },
        },
      },
    },
  };
  act(() => {
    snapshot.latestSequence = 2;
    pendingElicitations.push(requestedEvent.elicitationRequest);
    // 使用原生 JSON 形状进入事件边界；RawMessage 生成类型不能表达实际对象表单。
    chatSubscriptions
      .get("creation-live-chat")
      ?.onEvent(JSON.parse(JSON.stringify(requestedEvent)));
  });
  const source = await screen.findByRole("group", {
    name: "从哪些类型的数据源抓取 AI 资讯？",
  });
  expect(source.closest("[data-chat-composer]")).toBeInTheDocument();
  expect(source.closest("[data-chat-main-content]")).toBeNull();
  expect(
    screen.queryByRole("textbox", { name: /输入消息/ }),
  ).not.toBeInTheDocument();
  expect(answers).toEqual([]);
  const user = userEvent.setup();
  await user.click(
    within(source).getByRole("button", { name: /主流科技媒体/ }),
  );
  await user.click(
    within(source).getByRole("button", { name: /官方博客与论文/ }),
  );
  await user.click(within(source).getByRole("button", { name: "其他…" }));
  await user.type(
    within(source).getByRole("textbox", { name: "其他…" }),
    "公司研究博客",
  );
  const time = screen.getByRole("group", { name: "汇总覆盖多长时间范围？" });
  await user.click(within(time).getByRole("button", { name: "过去 24 小时" }));
  await user.click(within(time).getByRole("button", { name: "近 7 天" }));
  expect(
    within(time).getByRole("button", { name: "过去 24 小时" }),
  ).toHaveAttribute("aria-pressed", "false");
  const delivery = screen.getByRole("group", {
    name: "总结结果以什么形式交付？",
  });
  await user.click(
    within(delivery).getByRole("button", { name: "生成 Markdown 报告文件" }),
  );
  await user.click(screen.getByRole("button", { name: "继续" }));
  await waitFor(() =>
    expect(answers).toEqual([
      {
        content: {
          question_0: ["主流科技媒体", "官方博客与论文"],
          question_0_custom: "公司研究博客",
          question_1: "近 7 天",
          question_2: "生成 Markdown 报告文件",
        },
        declined: false,
      },
    ]),
  );
  // 请求仍待确认时保留表单；随后交付同一 Chat 的回答事实。
  expect(
    screen.getByRole("group", { name: "从哪些类型的数据源抓取 AI 资讯？" }),
  ).toBeInTheDocument();
  act(() => {
    snapshot.latestSequence = 3;
    pendingElicitations.length = 0;
    chatSubscriptions.get("creation-live-chat")?.onEvent({
      sequence: 3,
      type: "elicitation.resolved",
      elicitationResolution: {
        elicitationId: "native-question",
        result: "accepted",
      },
    });
  });
  await waitFor(() =>
    expect(
      screen.queryByText("Please answer the following questions."),
    ).not.toBeInTheDocument(),
  );
  expect(
    await screen.findByRole("textbox", { name: /输入消息/ }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "停止执行" })).toBeInTheDocument();
  expect(answers).toHaveLength(1);
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
          chat: {
            id: "first-chat",
            activeExecutionId: "real-execution",
            latestSequence: 3,
          },
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

it("优化建议只填入正文，发送时仍携带原运行引用", async () => {
  const bodies: unknown[] = [];
  server.use(
    http.get(`${api}/workflows/flow-1/runs/source-run`, () =>
      HttpResponse.json({
        run: { id: "source-run", revisionId: "source-revision" },
      }),
    ),
    http.post(
      `${api}/workflows/flow-1/creation/messages`,
      async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          id: "created-session",
          chatId: "result-chat",
          acceptedExecutionId: "execution",
          execution: { id: "execution", status: "running" },
        });
      },
    ),
    http.get(`${api}/chats/result-chat`, () =>
      HttpResponse.json({ chat: { id: "result-chat" } }),
    ),
    http.get(`${api}/chats/result-chat/messages`, () =>
      HttpResponse.json({ resources: [] }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  function Content() {
    return (
      <WorkflowChatPane
        id="flow-1"
        optimizeRunId="source-run"
        onNewChat={() => {}}
        draft="请优化异常处理"
        mainContent={{
          contentId: "test-canvas",
          title: "画布",
          content: <div>画布</div>,
        }}
      />
    );
  }
  render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <Content />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const input = await screen.findByRole("textbox", { name: "输入消息" });
  await waitFor(() => expect(input).toHaveTextContent("请优化异常处理"));
  expect(
    screen.queryByRole("button", { name: "关闭" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/source-run/)).not.toBeInTheDocument();
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await userEvent.click(send);
  await waitFor(() => expect(bodies).toHaveLength(1));
  expect(bodies[0]).toHaveProperty("content", "请优化异常处理");
  expect(bodies[0]).toHaveProperty("runReference", {
    runId: "source-run",
    revisionId: "source-revision",
  });
  queryClient.clear();
});

it("阶段引用位于输入正文内部，删除后仅发送仍可见的引用", async () => {
  const bodies: unknown[] = [];
  server.use(
    http.post(
      `${api}/workflows/flow-chips/creation/messages`,
      async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          id: "session-chips",
          chatId: "chat-chips",
          acceptedExecutionId: "execution-chips",
          execution: { id: "execution-chips", status: "completed" },
        });
      },
    ),
    http.get(`${api}/chats/chat-chips`, () =>
      HttpResponse.json({ chat: { id: "chat-chips" } }),
    ),
    http.get(`${api}/chats/chat-chips/messages`, () =>
      HttpResponse.json({ resources: [] }),
    ),
  );
  function Content() {
    const [refs, setRefs] = useState([
      { revisionId: "revision", kind: "phase" as const, id: "prepare" },
      { revisionId: "revision", kind: "phase" as const, id: "review" },
    ]);
    return (
      <WorkflowChatPane
        id="flow-chips"
        canvasReferences={refs}
        canvasReferenceLabels={{
          "phase:prepare": "准备材料",
          "phase:review": "检查材料",
        }}
        onRemoveCanvasReference={(ref) =>
          setRefs((values) => values.filter((item) => item.id !== ref.id))
        }
        onCanvasReferencesSent={() => setRefs([])}
        draft="增加完整性检查"
        onNewChat={() => {}}
        mainContent={{
          contentId: "chips-canvas",
          title: "画布",
          content: <div>流程</div>,
        }}
      />
    );
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <Content />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const first = await screen.findByText("准备材料");
  const body = first.closest("[data-chat-composer-input-body]");
  expect(body).not.toBeNull();
  expect(body?.querySelector("[contenteditable=true]")).toBeTruthy();
  await userEvent.click(
    screen.getByRole("button", { name: "移除准备材料引用" }),
  );
  expect(bodies).toEqual([]);
  expect(screen.queryByText("准备材料")).not.toBeInTheDocument();
  const send = screen.getByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await userEvent.click(send);
  await waitFor(() => expect(bodies).toHaveLength(1));
  expect(bodies[0]).toHaveProperty("canvasReferences", [
    { revisionId: "revision", kind: "phase", id: "review" },
  ]);
  client.clear();
});

it("已有创作接纳直接交付完整聊天快照，不补读详情或刷新版本运行目录", async () => {
  let detailReads = 0;
  server.use(
    http.get(`${api}/chats/snapshot-chat`, () => {
      detailReads++;
      return HttpResponse.json({
        chat: { id: "snapshot-chat", latestSequence: 1 },
      });
    }),
    http.get(`${api}/chats/snapshot-chat/messages`, () =>
      HttpResponse.json({ resources: [], latestSequence: 1 }),
    ),
    http.post(`${api}/workflows/flow/creation/messages`, () =>
      HttpResponse.json({
        id: "session",
        chatId: "snapshot-chat",
        acceptedExecutionId: "accepted",
        chat: {
          id: "snapshot-chat",
          latestSequence: 7,
          activeExecutionId: "accepted",
        },
        execution: {
          id: "accepted",
          userMessageId: "input",
          status: "running",
        },
      }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const invalidation = vi.spyOn(queryClient, "invalidateQueries");
  const view = render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <WorkflowChatPane
                id="flow"
                session={{
                  id: "session",
                  chatId: "snapshot-chat",
                  defaults: { runtimeId: "codex" },
                }}
                onNewChat={() => {}}
                mainContent={{
                  contentId: "canvas",
                  title: "画布",
                  content: <div>画布</div>,
                }}
              />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const input = screen.getByRole("textbox", { name: /输入消息/ });
  const user = userEvent.setup();
  await user.type(input, "解释当前流程");
  await waitFor(() =>
    expect(screen.getByRole("button", { name: /^发送消息$/ })).toBeEnabled(),
  );
  await user.keyboard("{Enter}");
  await waitFor(() =>
    expect(
      queryClient.getQueryData(
        getChatQueryKey({ path: { chatId: "snapshot-chat" } }),
      ),
    ).toMatchObject({
      chat: { latestSequence: 7, activeExecutionId: "accepted" },
    }),
  );
  expect(detailReads).toBe(1);
  expect(
    invalidation.mock.calls.some(([filter]) =>
      (JSON.stringify(filter?.queryKey) ?? "").includes("Workflow"),
    ),
  ).toBe(false);
  view.unmount();
  queryClient.clear();
});

it("旧请求接纳后保留用户新选择的阶段引用", async () => {
  const bodies: unknown[] = [];
  const onAccepted = vi.fn();
  let release!: () => void;
  const accepted = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.post(
      `${api}/workflows/flow-late/creation/messages`,
      async ({ request }) => {
        bodies.push(await request.json());
        await accepted;
        return HttpResponse.json({
          id: "session-late",
          chatId: "chat-late",
          acceptedExecutionId: "execution-late",
          chat: { id: "chat-late", activeExecutionId: "execution-late" },
          execution: { id: "execution-late", status: "running" },
        });
      },
    ),
  );
  function Content() {
    const [reference, setReference] = useState("A");
    return (
      <>
        <button onClick={() => setReference("B")}>改选阶段 B</button>
        <WorkflowChatPane
          id="flow-late"
          draft="修改所选阶段"
          onNewChat={() => {}}
          onSession={onAccepted}
          canvasReferences={
            reference
              ? [{ revisionId: "revision", kind: "phase", id: reference }]
              : []
          }
          canvasReferenceLabels={{ "phase:A": "阶段 A", "phase:B": "阶段 B" }}
          onCanvasReferencesSent={() => setReference("")}
          mainContent={{
            contentId: "canvas-late",
            title: "画布",
            content: <div>流程</div>,
          }}
        />
      </>
    );
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <Content />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(bodies).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "改选阶段 B" }));
  expect(screen.getByText("阶段 B")).toBeInTheDocument();
  await act(async () => {
    release();
    await accepted;
  });
  await waitFor(() => expect(onAccepted).toHaveBeenCalled());
  expect(bodies[0]).toHaveProperty("canvasReferences", [
    { revisionId: "revision", kind: "phase", id: "A" },
  ]);
  expect(screen.getByText("阶段 B")).toBeInTheDocument();
  view.unmount();
  queryClient.clear();
});

it("首次接纳前入队的消息绑定已创建的同一个创作会话", async () => {
  const bodies: unknown[] = [];
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  server.use(
    http.post(
      `${api}/workflows/flow-queue/creation/messages`,
      async ({ request }) => {
        bodies.push(await request.json());
        if (bodies.length === 1) await gate;
        return HttpResponse.json({
          id: "queue-session",
          chatId: "queue-chat",
          acceptedExecutionId: `queue-execution-${bodies.length}`,
          chat: { id: "queue-chat", latestSequence: 2 },
          execution: {
            id: `queue-execution-${bodies.length}`,
            status: "completed",
          },
        });
      },
    ),
    http.get(`${api}/chats/queue-chat`, () =>
      HttpResponse.json({ chat: { id: "queue-chat", latestSequence: 2 } }),
    ),
    http.get(`${api}/chats/queue-chat/messages`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "queue-execution-1",
            executionId: "queue-execution-1",
            role: "assistant",
            status: "completed",
            content: "第一条已完成",
            displayParts: [{ type: "text", text: "第一条已完成" }],
          },
        ],
        latestSequence: 2,
      }),
    ),
  );
  function Content() {
    const [session, setSession] = useState<{ id: string; chatId: string }>();
    return (
      <WorkflowChatPane
        id="flow-queue"
        session={session}
        draft="第一条"
        onNewChat={() => {}}
        onSession={(id) => setSession({ id, chatId: "queue-chat" })}
        mainContent={{
          contentId: "canvas-queue",
          title: "画布",
          content: <div>流程</div>,
        }}
      />
    );
  }
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <WorkbenchProvider>
        <ChatQueueProvider>
          <TooltipProvider>
            <MemoryRouter>
              <Content />
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </WorkbenchProvider>
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  const send = await screen.findByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(bodies).toHaveLength(1));
  const input = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(input, "第二条");
  await user.click(await screen.findByRole("button", { name: "加入发送队列" }));
  await act(async () => {
    release();
    await gate;
  });
  await waitFor(() => expect(bodies).toHaveLength(2));
  expect(bodies[1]).toHaveProperty("sessionId", "queue-session");
  view.unmount();
  queryClient.clear();
});
