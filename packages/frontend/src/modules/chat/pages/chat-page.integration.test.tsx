import type * as GeneratedSDK from "@/shared/api/generated/sdk.gen";
import type * as GeneratedQueries from "@/shared/api/generated/@tanstack/react-query.gen";
import { draftWorkspaceStorageKey } from "./use-draft-workspace-selections";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render as renderUI,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { startTransition, StrictMode, type ReactNode } from "react";
import {
  MemoryRouter,
  createMemoryRouter,
  RouterProvider,
  Outlet,
  Route,
  Routes,
  useNavigate,
} from "react-router-dom";
import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import "@/shared/i18n";
import type { TeamChatContext } from "@/modules/team";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  useSceneWorkspace,
  WorkspaceProvider,
} from "@/shared/workspace/workspace-context";
import { ChatQueueProvider } from "../runtime/chat-queue-store";
import { WorkbenchProvider } from "../workbench/workbench-store";
import { ChatEntryPage } from "./chat-entry-page";
import { ChatPage, ChatPageContent } from "./chat-page";
import { TeamChatPageContent } from "./team-chat-page";
import { ChatTaskListPage } from "./chat-task-list-page";
import { observeRenderedState } from "@/test/observe-rendered-state";

const render = (ui: Parameters<typeof renderUI>[0]) =>
  renderUI(ui, { wrapper: WorkbenchProvider });

const {
  historyMessageContent,
  historyMessages,
  forkSourceChatId,
  sourceChatAvailable,
  historyMessageSkills,
  historyMessageConnectors,
  chatLastPermissionLevel,
  chatLastRuntimeID,
  accountCIncludesOldModel,
  chatActiveExecutionID,
  chatPendingApprovals,
  runtimeOptions,
  searchWorkspaceFiles,
  chatWorkspaceFiles,
  listChatWorkspaceFiles,
  downloadChatWorkspaceFile,
  deleteChatWorkspaceFile,
  createChat,
  sendMessage,
  resolveApproval,
  wakerSkills,
  wakerPlugins,
  wakerSkillsError,
  wakerConnectors,
  wakerConnectorsError,
  createChatDelay,
  chatMessageDelay,
  wakerResources,
  projectResources,
  recentProjectID,
  chatListByWaker,
  chatListDelay,
} = vi.hoisted(() => ({
  historyMessageContent: { current: "直接进入也应显示的历史消息" },
  historyMessages: {
    current: undefined as Array<Record<string, unknown>> | undefined,
  },
  forkSourceChatId: { current: "" },
  sourceChatAvailable: { current: true },
  historyMessageSkills: {
    current: [] as Array<{ id: string; name: string; version: string }>,
  },
  historyMessageConnectors: {
    current: [] as Array<{ id: string; name: string }>,
  },
  chatLastPermissionLevel: { current: "" },
  chatLastRuntimeID: { current: "" },
  accountCIncludesOldModel: { current: false },
  chatActiveExecutionID: { current: "" },
  chatPendingApprovals: {
    current: [] as Array<Record<string, unknown>>,
  },
  runtimeOptions: {
    current: [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-5",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto", "full-access"],
          },
        ],
      },
    ] as Array<Record<string, unknown>>,
  },
  searchWorkspaceFiles: vi.fn(
    async (_root: string, query: string, limit = 200, scopePath?: string) => {
      const lower = query.toLowerCase();
      const files = [
        {
          name: "AGENTS.md",
          path: "/tmp/ally/AGENTS.md",
          relativePath: "AGENTS.md",
        },
        ...(lower.includes("agents")
          ? [
              {
                name: "AGENTS.md",
                path: "/tmp/project/AGENTS.md",
                relativePath: "projects/zddiv3/AGENTS.md",
              },
            ]
          : []),
        {
          name: "readme.md",
          path: "/tmp/ally/readme.md",
          relativePath: "projects/zddiv3/readme.md",
        },
        {
          name: "package.json",
          path: "/tmp/ally/package.json",
          relativePath: "projects/zddiv3/package.json",
        },
        ...(scopePath === "attachments/generated"
          ? [
              {
                name: "report.md",
                path: "/tmp/ally/attachments/generated/report.md",
                relativePath: "attachments/generated/report.md",
              },
            ]
          : []),
      ];
      return files
        .filter(
          (file) =>
            (!scopePath ||
              file.relativePath === scopePath ||
              file.relativePath.startsWith(`${scopePath}/`)) &&
            (!lower ||
              file.name.toLowerCase().includes(lower) ||
              file.relativePath.toLowerCase().includes(lower)),
        )
        .slice(0, limit);
    },
  ),
  chatWorkspaceFiles: {
    current: {
      generated: [
        {
          name: "report.md",
          path: "/tmp/ally/attachments/generated/report.md",
          relativePath: "attachments/generated/report.md",
        },
      ],
      removedPaths: [] as string[],
    },
  },
  listChatWorkspaceFiles: vi.fn(async (_chatId: string, _root: string) => {
    void [_chatId, _root];
    return chatWorkspaceFiles.current;
  }),
  downloadChatWorkspaceFile: vi.fn(
    async (_chatId: string, _root: string, _relativePath: string) => {
      void [_chatId, _root, _relativePath];
      return true;
    },
  ),
  deleteChatWorkspaceFile: vi.fn(
    async (_chatId: string, _root: string, relativePath: string) => {
      void [_chatId, _root];
      chatWorkspaceFiles.current = {
        generated: chatWorkspaceFiles.current.generated.filter(
          (file) => file.relativePath !== relativePath,
        ),
        removedPaths: [
          ...chatWorkspaceFiles.current.removedPaths,
          relativePath,
        ],
      };
      return true;
    },
  ),
  createChat: vi.fn<(options: Record<string, unknown>) => Promise<unknown>>(
    async () => {
      if (createChatDelay.current > 0) {
        await new Promise((resolve) =>
          window.setTimeout(resolve, createChatDelay.current),
        );
      }
      return {
        chat: { id: "chat-1", wakerId: "waker-1" },
        execution: {
          id: "execution-2",
          userMessageId: "message-2",
          status: "running",
        },
      };
    },
  ),
  sendMessage: vi.fn<(options: Record<string, unknown>) => Promise<unknown>>(
    async () => ({
      chat: { id: "chat-1", wakerId: "waker-1" },
      execution: {
        id: "execution-2",
        userMessageId: "message-2",
        status: "pending",
      },
    }),
  ),
  resolveApproval: vi.fn(async () => undefined),
  wakerPlugins: { current: [] as Array<Record<string, unknown>> },
  wakerSkills: { current: [] as Array<Record<string, unknown>> },
  wakerSkillsError: { current: false },
  wakerConnectors: { current: [] as Array<Record<string, unknown>> },
  wakerConnectorsError: { current: false },
  createChatDelay: { current: 0 },
  chatMessageDelay: { current: 50 },
  wakerResources: {
    current: [
      {
        id: "waker-1",
        name: "测试伙伴",
        roleName: "开发伙伴",
      },
    ] as Array<Record<string, unknown>>,
  },
  projectResources: {
    current: [] as Array<Record<string, unknown>>,
  },
  recentProjectID: { current: "" },
  chatListByWaker: {
    current: {} as Record<string, Array<Record<string, unknown>>>,
  },
  chatListDelay: { current: 0 },
}));

vi.mock("@/shared/api/generated/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<typeof GeneratedSDK>()),
  listWakerPlugins: async () => ({
    data: {
      resources: wakerPlugins.current,
      totalSize: wakerPlugins.current.length,
    },
  }),
}));
vi.mock(
  "@/shared/api/generated/@tanstack/react-query.gen",
  async (importOriginal) => ({
    ...(await importOriginal<typeof GeneratedQueries>()),
    getWorkResultOptions: ({
      path,
    }: {
      path: { ownerKind: string; ownerId: string };
    }) => ({
      queryKey: ["work-result", path.ownerKind, path.ownerId],
      queryFn: async () => ({}),
    }),
    listAiAccountsOptions: () => ({
      queryKey: ["accounts"],
      queryFn: async () => ({
        resources: ["A", "B", "C"].map((id) => ({
          id,
          name: `账号 ${id}`,
          authMode: "api-key",
          endpoints: [
            {
              protocol: "openai-responses",
              baseUrl: "https://api.openai.com/v1",
            },
          ],
          selectedModels:
            id === "C" && accountCIncludesOldModel.current
              ? ["c-model", "b-model"]
              : [`${id.toLowerCase()}-model`],
        })),
      }),
    }),
  }),
);

vi.mock("@/modules/waker", () => ({
  listWakersOptions: () => ({
    queryKey: ["wakers"],
    queryFn: async () => ({
      resources: wakerResources.current,
    }),
  }),
  WakerDetailLayout: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@/modules/project", () => ({
  listProjectsOptions: () => ({
    queryKey: ["projects", "waker-1"],
    queryFn: async () => ({ resources: projectResources.current }),
  }),
}));

vi.mock("@/modules/connector", () => ({
  connectorSelectableInComposer: (activationPolicy?: string) =>
    activationPolicy !== "team_context",
  listWakerConnectorsOptions: () => ({
    queryKey: ["waker-connectors", "waker-1"],
    queryFn: async () => {
      if (wakerConnectorsError.current) {
        throw new Error("connectors unavailable");
      }
      return wakerConnectors.current;
    },
  }),
}));

vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService: () => false,
  normalizeBrowserURL: (url: string) => url || null,
  resolveFileReferencePath: async (request: {
    path: string;
    baseDirectory: string;
    workspaceRoot: string;
    sourceRoots: string[];
  }) => ({
    status: "resolved",
    canonicalPath: request.path.startsWith("/")
      ? request.path
      : `${request.baseDirectory}/${request.path}`,
    baseDirectory: request.baseDirectory,
    workspaceRoot: request.workspaceRoot,
    sourceRoots: request.sourceRoots,
  }),
  resolveWorkspaceResourceReference: async (
    _chatId: string,
    path: string,
    baseDirectory: string,
  ) => ({
    status: "resolved",
    canonicalPath: path.startsWith("/") ? path : `${baseDirectory}/${path}`,
    baseDirectory,
    workspaceRoot: "/tmp/ally",
    sourceRoots: [],
  }),
  searchWorkspaceFiles: (
    root: string,
    query: string,
    limit?: number,
    scopePath?: string,
  ) => searchWorkspaceFiles(root, query, limit, scopePath),
  listChatWorkspaceFiles: (chatId: string, root: string) =>
    listChatWorkspaceFiles(chatId, root),
  downloadChatWorkspaceFile: (
    chatId: string,
    root: string,
    relativePath: string,
  ) => downloadChatWorkspaceFile(chatId, root, relativePath),
  deleteChatWorkspaceFile: (
    chatId: string,
    root: string,
    relativePath: string,
  ) => deleteChatWorkspaceFile(chatId, root, relativePath),
  subscribeFileDrop: () => () => undefined,
}));

vi.mock("../api/chat-queries", () => ({
  useChatHistoryVisibility: () => undefined,
  cancelChatExecutionMutation: () => ({
    mutationFn: async () => undefined,
  }),
  createChatMutation: () => ({ mutationFn: createChat }),
  chatListInfiniteOptions: (wakerId: string) => ({
    queryKey: ["chats", "list", wakerId],
    initialPageParam: undefined,
    queryFn: async () => {
      if (chatListDelay.current > 0) {
        await new Promise((resolve) =>
          window.setTimeout(resolve, chatListDelay.current),
        );
      }
      return {
        resources: chatListByWaker.current[wakerId] ?? [],
        hasMore: false,
        recentProjectId: recentProjectID.current,
      };
    },
    getNextPageParam: () => undefined,
  }),
  chatListKey: (wakerId: string) => ["chats", "list", wakerId],
  chatMessagesInfiniteOptions: (chatId: string) => ({
    queryKey: ["chats", "messages", chatId],
    initialPageParam: undefined,
    queryFn: async () => {
      await new Promise((resolve) =>
        window.setTimeout(resolve, chatMessageDelay.current),
      );
      return {
        resources:
          chatId === "empty-member-chat"
            ? []
            : chatId === "chat-1" && historyMessages.current
              ? historyMessages.current
              : [
                  {
                    id: chatId === "chat-1" ? "message-1" : `message:${chatId}`,
                    role: ["member-chat", "team-lead-chat"].includes(chatId)
                      ? "assistant"
                      : "user",
                    content:
                      chatId === "chat-1"
                        ? historyMessageContent.current
                        : `历史消息：${chatId}`,
                    displayParts: ["member-chat", "team-lead-chat"].includes(
                      chatId,
                    )
                      ? [
                          {
                            id: "member-text",
                            text: `历史消息：${chatId}`,
                            type: "text",
                          },
                        ]
                      : undefined,
                    skills: historyMessageSkills.current,
                    connectors: historyMessageConnectors.current,
                    status: "completed",
                    sequence: 1,
                  },
                ],
        hasMore: false,
        latestSequence: 1,
      };
    },
    getNextPageParam: () => undefined,
  }),
  chatMessagesKey: (chatId: string) => ["chats", "messages", chatId],
  getChatOptions: ({ path }: { path: { chatId: string } }) => ({
    queryKey: ["chat", path.chatId],
    queryFn: async () => {
      if (path.chatId === "source-chat" && !sourceChatAvailable.current) {
        throw new Error("source chat missing");
      }
      return {
        chat: {
          id: path.chatId,
          wakerId: "waker-1",
          title: "直接进入的对话",
          forkSourceChatId: forkSourceChatId.current,
          workspaceDirectory: "/tmp/ally",
          activeExecutionId: chatActiveExecutionID.current,
          lastPermissionLevel: chatLastPermissionLevel.current,
          lastRuntimeId: chatLastRuntimeID.current,
          lastAccountId: path.chatId === "member-chat" ? "B" : "system",
          lastModel: path.chatId === "member-chat" ? "b-model" : "",
          pendingApprovals: chatPendingApprovals.current,
        },
      };
    },
  }),
  getChatQueryKey: ({ path }: { path: { chatId: string } }) => [
    "chat",
    path.chatId,
  ],
  resolveApprovalMutation: () => ({ mutationFn: resolveApproval }),
  resolveElicitationMutation: () => ({ mutationFn: vi.fn() }),
  listRuntimesOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => ({
      resources: runtimeOptions.current,
    }),
  }),
  listWakerSkillsOptions: () => ({
    queryKey: ["waker-skills", "waker-1"],
    queryFn: async () => {
      if (wakerSkillsError.current) throw new Error("skills unavailable");
      return wakerSkills.current;
    },
  }),
  listWakerChatsOptions: () => ({
    queryKey: ["waker-chats", "waker-1"],
    queryFn: async () => ({
      resources: [
        {
          id: "chat-1",
          wakerId: "waker-1",
          title: "直接进入的对话",
          workspaceDirectory: "/tmp/ally",
          updatedAt: 1,
        },
      ],
      pageNum: 1,
      pageSize: 10,
      totalSize: 1,
    }),
  }),
  sendMessageMutation: () => ({ mutationFn: sendMessage }),
}));

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const originalScrollTo = HTMLElement.prototype.scrollTo;

beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  HTMLElement.prototype.scrollTo = vi.fn();
});

beforeEach(() => {
  const preferences = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => preferences.get(key) ?? null,
      setItem: (key: string, value: string) => preferences.set(key, value),
      removeItem: (key: string) => preferences.delete(key),
    },
  });

  wakerPlugins.current = [];
  historyMessageContent.current = "直接进入也应显示的历史消息";
  historyMessages.current = undefined;
  forkSourceChatId.current = "";
  sourceChatAvailable.current = true;
  historyMessageSkills.current = [];
  historyMessageConnectors.current = [];
  chatLastPermissionLevel.current = "";
  chatLastRuntimeID.current = "";
  accountCIncludesOldModel.current = false;
  chatActiveExecutionID.current = "";
  chatPendingApprovals.current = [];
  runtimeOptions.current = [
    {
      id: "codex",
      name: "Codex",
      enabled: true,
      state: "ready",
      defaultModel: "gpt-5",
      defaultPermissionLevel: "full-access",
      models: [
        {
          id: "gpt-5",
          name: "GPT-5",
          permissionLevels: ["default", "auto", "full-access"],
        },
      ],
    },
  ];
  wakerSkills.current = [];
  wakerSkillsError.current = false;
  wakerConnectors.current = [];
  wakerConnectorsError.current = false;
  createChatDelay.current = 0;
  chatMessageDelay.current = 50;
  wakerResources.current = [
    {
      id: "waker-1",
      name: "测试伙伴",
      roleName: "开发伙伴",
    },
  ];
  projectResources.current = [];
  recentProjectID.current = "";
  chatListByWaker.current = {};
  chatListDelay.current = 0;
  searchWorkspaceFiles.mockClear();
  chatWorkspaceFiles.current = {
    generated: [
      {
        name: "report.md",
        path: "/tmp/ally/attachments/generated/report.md",
        relativePath: "attachments/generated/report.md",
      },
    ],
    removedPaths: [],
  };
  listChatWorkspaceFiles.mockClear();
  downloadChatWorkspaceFile.mockClear();
  deleteChatWorkspaceFile.mockClear();
  createChat.mockClear();
  sendMessage.mockClear();
  resolveApproval.mockClear();
  resolveApproval.mockResolvedValue(undefined);
});

afterAll(() => {
  HTMLElement.prototype.scrollTo = originalScrollTo;
  vi.unstubAllGlobals();
});

describe("ChatPage", () => {
  it("父工作区中的成员聊天独立保存和发送所选账号", async () => {
    accountCIncludesOldModel.current = true;
    chatLastRuntimeID.current = "codex";
    window.localStorage.setItem("ally.account.chat:chat-1", "A");
    window.localStorage.setItem("ally.account.chat:member-chat", "B");
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <ChatQueueProvider>
            <TooltipProvider>
              <MemoryRouter initialEntries={["/chats/chat-1"]}>
                <Routes>
                  <Route element={<ChatLayout />}>
                    <Route
                      path="/chats/:chatId"
                      element={
                        <>
                          <TeamMemberTabControls
                            scopeKey="chat:chat-1"
                            members={[
                              {
                                chatId: "member-chat",
                                name: "成员",
                                wakerId: "waker-1",
                              },
                            ]}
                          />
                          <ChatPage />
                        </>
                      }
                    />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
        "Codex · 账号 A",
      ),
    );
    await user.click(screen.getByRole("button", { name: "测试打开成员会话" }));
    await screen.findByText("历史消息：member-chat");
    await waitFor(() =>
      expect(screen.getByTestId("composer-runtime-control")).toHaveTextContent(
        "Codex · 账号 B",
      ),
    );
    expect(screen.getByRole("button", { name: "b-model" })).toBeVisible();
    await user.click(screen.getByTestId("composer-runtime-control"));
    await user.click(screen.getByRole("menuitemradio", { name: /Codex/ }));
    await user.click(screen.getByRole("menuitemradio", { name: "账号 C" }));
    expect(
      screen.getByRole("dialog", { name: "在当前聊天中继续" }),
    ).toBeVisible();
    expect(screen.getByLabelText("携带最近对话轮数")).toHaveValue(5);
    await user.click(screen.getByRole("radio", { name: "自动总结" }));
    expect(
      screen.getByRole("combobox", { name: "总结模型" }),
    ).toHaveTextContent("b-model");
    await user.clear(screen.getByLabelText("总结不可用时携带的最近轮数"));
    await user.type(screen.getByLabelText("总结不可用时携带的最近轮数"), "3");
    await user.click(screen.getByRole("button", { name: "确认切换并继续" }));
    expect(screen.getByRole("button", { name: "c-model" })).toBeVisible();
    expect(window.localStorage.getItem("ally.account.chat:member-chat")).toBe(
      "C",
    );
    expect(window.localStorage.getItem("ally.account.chat:chat-1")).toBe("A");
    await user.type(
      screen.getByRole("textbox", { name: "输入消息" }),
      "成员消息",
    );
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() =>
      expect(sendMessage).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { chatId: "member-chat" },
          body: expect.objectContaining({
            accountId: "C",
            model: "c-model",
            startNewSession: true,
            historyTurns: 3,
            historyMode: "summary",
            summaryModel: "b-model",
          }),
        }),
        expect.anything(),
      ),
    );
  });

  it("团队时间线到达即显示，不等待负责人 Chat 请求或 SSE 连接，并持续响应后续快照", async () => {
    chatMessageDelay.current = 5_000;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const context: TeamChatContext = {
      channelId: "team-1",
      threadId: "thread-1",
      name: "研发团队",
      leadWakerId: "waker-1",
      leadProcessing: false,
      members: [
        {
          id: "waker-1",
          chatId: "team-lead-chat",
          name: "团队负责人",
          isLead: true,
          runtimeId: "codex",
          model: "gpt-5",
          permissionLevel: "auto",
          reasoning: "",
        },
      ],
      timeline: [],
      timelineLoading: true,
      threadDetailLoading: false,
      hasEarlierTimelineMessages: false,
      isLoadingEarlierTimelineMessages: false,
      onLoadEarlierTimelineMessages: vi.fn(),
      onCreateThread: vi.fn(),
      onSendMessage: vi.fn(),
      onStopWork: vi.fn(),
      onThreadChanged: vi.fn(),
      onThreadCreated: vi.fn(),
      onUpdateMemberConfigs: vi.fn(),
      renderTaskBoard: () => null,
      workState: "idle",
      workProgress: { phase: "idle", review: 0, terminal: 0, total: 0 },
    };
    const view = (teamContext: TeamChatContext) => (
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <WorkspaceProvider>
            <ChatQueueProvider>
              <TooltipProvider>
                <MemoryRouter initialEntries={["/team"]}>
                  <Routes>
                    <Route element={<ChatLayout />}>
                      <Route
                        path="/team"
                        element={
                          <TeamChatPageContent
                            chatId="team-lead-chat"
                            teamContext={teamContext}
                          />
                        }
                      />
                    </Route>
                  </Routes>
                </MemoryRouter>
              </TooltipProvider>
            </ChatQueueProvider>
          </WorkspaceProvider>
        </QueryClientProvider>
      </StrictMode>
    );
    const { rerender } = render(view(context));
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBeVisible();
    expect(screen.getByLabelText("正在加载对话消息")).toBeVisible();
    const loaded: TeamChatContext = {
      ...context,
      timelineLoading: false,
      timeline: [
        {
          coordinator: true,
          chatId: "team-lead-chat",
          message: {
            id: "timeline-user",
            role: "user",
            content: "连接之前的历史",
          },
        },
      ],
    };
    rerender(view(loaded));
    expect(await screen.findByText("连接之前的历史")).toBeVisible();
    expect(
      queryClient.getQueryState(["chats", "messages", "team-lead-chat"])
        ?.status,
    ).toBe("pending");
    rerender(
      view({
        ...loaded,
        timeline: [
          {
            coordinator: true,
            chatId: "team-lead-chat",
            message: {
              id: "timeline-user",
              role: "user",
              content: "无需重新连接的更新",
              revision: 2,
            },
          },
        ],
      }),
    );
    expect(await screen.findByText("无需重新连接的更新")).toBeVisible();
    expect(screen.queryByLabelText("正在加载对话消息")).not.toBeInTheDocument();
  });

  it("伙伴入口解析后不触发生命周期 flushSync，也不再插入整页加载", async () => {
    chatListByWaker.current = {
      "waker-1": [{ id: "chat-1", wakerId: "waker-1" }],
    };
    const error = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const router = createMemoryRouter(
      [
        {
          element: <ChatLayout />,
          children: [
            { path: "/chat", element: <ChatEntryPage /> },
            { path: "/chats/:chatId", element: <ChatPage /> },
          ],
        },
      ],
      { initialEntries: ["/chat?wakerId=waker-1"] },
    );
    const { container, unmount } = render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <ChatQueueProvider>
            <TooltipProvider>
              <RouterProvider router={router} />
            </TooltipProvider>
          </ChatQueueProvider>
        </QueryClientProvider>
      </StrictMode>,
    );
    const initialFallback = container.querySelector(
      "[data-chat-loading-tabstrip]",
    );
    const states = observeRenderedState(container, () =>
      container.querySelector("[data-chat-loading-tabstrip]"),
    );
    try {
      expect(
        await screen.findByText("直接进入也应显示的历史消息"),
      ).toBeVisible();
      expect(error.mock.calls.flat().join(" ")).not.toContain("flushSync");
      expect(router.state.location.pathname).toBe("/chats/chat-1");
      expect(
        states.samples.filter((state) => state && state !== initialFallback),
      ).toEqual([]);
    } finally {
      states.disconnect();
      unmount();
      router.dispose();
      error.mockRestore();
    }
  });
  it("团队聚合消息隐藏空态，点击工作成员头像载入该成员完整 Chat 历史", async () => {
    const user = userEvent.setup();
    wakerResources.current = [
      { id: "waker-1", name: "团队负责人" },
      { id: "waker-2", name: "执行成员" },
      { id: "waker-3", name: "空闲成员" },
    ];
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <ChatQueueProvider>
            <TooltipProvider delayDuration={0}>
              <MemoryRouter initialEntries={["/team"]}>
                <Routes>
                  <Route element={<ChatLayout rightPanel="workspace" />}>
                    <Route
                      path="/team"
                      element={
                        <>
                          <TeamMemberTabControls
                            members={[
                              {
                                chatId: "member-chat",
                                name: "执行成员",
                                wakerId: "waker-2",
                              },
                              {
                                chatId: "empty-member-chat",
                                name: "空闲成员",
                                wakerId: "waker-3",
                              },
                            ]}
                          />
                          <TeamChatPageContent
                            chatId="team-lead-chat"
                            teamContext={{
                              channelId: "team-1",
                              leadProcessing: false,
                              leadWakerId: "waker-1",
                              members: [
                                {
                                  chatId: "team-lead-chat",
                                  id: "waker-1",
                                  isLead: true,
                                  model: "gpt-5",
                                  name: "团队负责人",
                                  permissionLevel: "auto",
                                  reasoning: "",
                                  runtimeId: "codex",
                                },
                                {
                                  activeExecutionId: "member-execution",
                                  activeTaskSubject: "汇总科技动态",
                                  chatId: "member-chat",
                                  id: "waker-2",
                                  isLead: false,
                                  model: "gpt-5",
                                  name: "执行成员",
                                  permissionLevel: "auto",
                                  reasoning: "",
                                  runtimeId: "codex",
                                  taskStatus: "in_progress",
                                },
                                {
                                  chatId: "empty-member-chat",
                                  id: "waker-3",
                                  isLead: false,
                                  model: "gpt-5",
                                  name: "空闲成员",
                                  permissionLevel: "auto",
                                  reasoning: "",
                                  runtimeId: "codex",
                                },
                              ],
                              name: "研发团队",
                              timeline: [
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: true,
                                  message: {
                                    actor: "team_member",
                                    content: "成员结构化报告",
                                    displayParts: [
                                      {
                                        id: "text-1",
                                        text: "成员结构化报告",
                                        type: "text",
                                      },
                                    ],
                                    id: "aggregate-message",
                                    role: "user",
                                    sourceChatId: "member-chat",
                                  },
                                  wakerId: "waker-2",
                                },
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: true,
                                  directDelivered: true,
                                  message: {
                                    actor: "team_member",
                                    content: "成员直接答复",
                                    displayParts: [
                                      {
                                        id: "direct-text",
                                        text: "成员直接答复",
                                        type: "text",
                                      },
                                    ],
                                    id: "direct-member-answer",
                                    role: "user",
                                    sourceChatId: "member-chat",
                                  },
                                  wakerId: "waker-2",
                                },
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: false,
                                  message: {
                                    content: "负责人专属任务完整结果",
                                    displayParts: [
                                      {
                                        id: "text-2",
                                        text: "负责人专属任务完整结果",
                                        type: "text",
                                      },
                                    ],
                                    id: "lead-member-result",
                                    role: "assistant",
                                    status: "completed",
                                  },
                                  wakerId: "waker-1",
                                },
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: true,
                                  message: {
                                    content: "负责人最终汇总",
                                    displayParts: [
                                      {
                                        id: "text-3",
                                        text: "负责人最终汇总",
                                        type: "text",
                                      },
                                    ],
                                    id: "coordinator-summary",
                                    role: "assistant",
                                    status: "completed",
                                  },
                                  wakerId: "waker-1",
                                },
                                {
                                  chatId: "member-chat",
                                  coordinator: false,
                                  message: {
                                    actor: "team_lead",
                                    content: "负责人补充说明",
                                    displayParts: [
                                      {
                                        id: "text-follow-up",
                                        text: "负责人补充说明",
                                        type: "text",
                                      },
                                    ],
                                    id: "lead-follow-up",
                                    role: "user",
                                    status: "completed",
                                  },
                                  wakerId: "waker-1",
                                },
                                {
                                  chatId: "member-chat",
                                  coordinator: false,
                                  message: {
                                    displayParts: [
                                      {
                                        id: "text-4",
                                        text: "成员失败详情",
                                        type: "text",
                                      },
                                    ],
                                    errorMessage: "真正错误细节",
                                    id: "member-failed-result",
                                    role: "assistant",
                                    status: "failed",
                                  },
                                  wakerId: "waker-2",
                                },
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: false,
                                  message: {
                                    displayParts: [
                                      {
                                        id: "text-5",
                                        text: "负责人失败详情",
                                        type: "text",
                                      },
                                    ],
                                    id: "lead-failed-result",
                                    role: "assistant",
                                    status: "failed",
                                  },
                                  wakerId: "waker-1",
                                },
                                {
                                  chatId: "member-chat",
                                  coordinator: false,
                                  message: {
                                    displayParts: [
                                      {
                                        id: "text-6",
                                        text: "成员取消详情",
                                        type: "text",
                                      },
                                    ],
                                    id: "member-canceled-result",
                                    role: "assistant",
                                    status: "canceled",
                                  },
                                  wakerId: "waker-2",
                                },
                                {
                                  chatId: "team-lead-chat",
                                  coordinator: false,
                                  message: {
                                    displayParts: [
                                      {
                                        id: "text-7",
                                        text: "负责人取消详情",
                                        type: "text",
                                      },
                                    ],
                                    id: "lead-canceled-result",
                                    role: "assistant",
                                    status: "canceled",
                                  },
                                  wakerId: "waker-1",
                                },
                              ],
                              hasEarlierTimelineMessages: false,
                              isLoadingEarlierTimelineMessages: false,
                              onLoadEarlierTimelineMessages: vi.fn(),
                              onCreateThread: vi.fn(),
                              onSendMessage: vi.fn(),
                              onStopWork: vi.fn(),
                              onThreadChanged: vi.fn(),
                              onThreadCreated: vi.fn(),
                              onUpdateMemberConfigs: vi.fn(),
                              renderTaskBoard: () => <div>任务板详情</div>,
                              threadId: "thread-1",
                              workState: "running",
                              workProgress: {
                                activeMemberName: "执行成员",
                                activeTaskSubject: "汇总科技动态",
                                phase: "executing",
                                review: 0,
                                terminal: 0,
                                total: 1,
                              },
                            }}
                          />
                        </>
                      }
                    />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("负责人最终汇总")).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent("团队仍在执行");
    expect(
      screen.queryByText("执行成员正在处理“汇总科技动态”"),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "停止执行" })).toBeVisible();
    expect(screen.getByText("成员结构化报告")).toBeVisible();
    expect(screen.getByText("成员直接答复")).toBeVisible();
    expect(screen.getByText("成员报告")).toBeVisible();
    expect(screen.getAllByText("成员报告")).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: "全部成员" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("负责人专属任务完整结果"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("成员失败详情")).not.toBeInTheDocument();
    expect(screen.queryByText("负责人失败详情")).not.toBeInTheDocument();
    expect(screen.queryByText("成员取消详情")).not.toBeInTheDocument();
    expect(screen.queryByText("负责人取消详情")).not.toBeInTheDocument();
    expect(screen.queryByText("负责人补充说明")).not.toBeInTheDocument();
    expect(screen.queryByText("把目标交给团队负责人")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("navigation", { name: "团队成员会话" }),
    ).not.toBeInTheDocument();
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "负责人未发送草稿");
    const workingMember = within(screen.getByRole("status")).getByRole(
      "button",
      {
        name: "打开 执行成员 的对话",
      },
    );
    expect(
      workingMember.querySelector('[data-companion-animated="false"]'),
    ).toBeInTheDocument();
    await user.hover(workingMember);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "执行成员正在处理“汇总科技动态”",
    );
    await user.unhover(workingMember);
    expect(
      within(screen.getByRole("status")).queryByRole("button", {
        name: "打开 空闲成员 的对话",
      }),
    ).not.toBeInTheDocument();
    await user.click(workingMember);
    expect(await screen.findByText("历史消息：member-chat")).toBeVisible();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
    expect(input).toHaveTextContent("");
    fireEvent.click(
      screen.getByRole("button", { name: "测试打开空闲成员会话" }),
    );
    expect(
      await screen.findByRole("heading", { name: "今天想一起完成什么？" }),
    ).toBeVisible();
    expect(screen.queryByText("完整成员结果")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "团队负责人" }));
    expect(await screen.findByText("负责人最终汇总")).toBeVisible();
  });

  it("成员 Tab 发送直连成员 Chat，不触发 Channel 委派", async () => {
    wakerResources.current = [
      { id: "waker-1", name: "团队负责人" },
      { id: "waker-2", name: "执行成员" },
    ];
    const onSendMessage = vi.fn(async () => ({
      execution: { id: "channel-execution", status: "running" as const },
    }));
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <ChatQueueProvider>
            <TooltipProvider delayDuration={0}>
              <MemoryRouter initialEntries={["/team"]}>
                <Routes>
                  <Route element={<ChatLayout rightPanel="workspace" />}>
                    <Route
                      path="/team"
                      element={
                        <>
                          <TeamMemberTabControls
                            members={[
                              {
                                chatId: "member-chat",
                                name: "执行成员",
                                wakerId: "waker-2",
                              },
                            ]}
                          />
                          <TeamChatPageContent
                            chatId="team-lead-chat"
                            teamContext={{
                              channelId: "team-1",
                              leadProcessing: false,
                              leadWakerId: "waker-1",
                              members: [
                                {
                                  chatId: "team-lead-chat",
                                  id: "waker-1",
                                  isLead: true,
                                  model: "gpt-5",
                                  name: "团队负责人",
                                  permissionLevel: "auto",
                                  reasoning: "",
                                  runtimeId: "codex",
                                },
                                {
                                  chatId: "member-chat",
                                  id: "waker-2",
                                  isLead: false,
                                  model: "gpt-5",
                                  name: "执行成员",
                                  permissionLevel: "auto",
                                  reasoning: "",
                                  runtimeId: "codex",
                                },
                              ],
                              name: "研发团队",
                              timeline: [],
                              hasEarlierTimelineMessages: false,
                              isLoadingEarlierTimelineMessages: false,
                              onLoadEarlierTimelineMessages: vi.fn(),
                              onCreateThread: vi.fn(),
                              onSendMessage,
                              onStopWork: vi.fn(),
                              onThreadChanged: vi.fn(),
                              onThreadCreated: vi.fn(),
                              onUpdateMemberConfigs: vi.fn(),
                              renderTaskBoard: () => <div>任务板详情</div>,
                              threadId: "thread-1",
                              workState: "idle",
                              workProgress: {
                                phase: "idle",
                                review: 0,
                                terminal: 0,
                                total: 0,
                              },
                            }}
                          />
                        </>
                      }
                    />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );

    fireEvent.click(
      await screen.findByRole("button", {
        name: "测试打开执行成员会话",
      }),
    );
    await screen.findByText("历史消息：member-chat");
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "和成员直接聊聊");
    await user.click(screen.getByRole("button", { name: "发送消息" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        path: { chatId: "member-chat" },
        body: expect.objectContaining({ text: "和成员直接聊聊" }),
      }),
    );
    expect(onSendMessage).not.toHaveBeenCalled();
  });

  it("新团队对话在成员草稿 Tab 调整执行配置后于首次发送冻结", async () => {
    window.localStorage.setItem(
      draftWorkspaceStorageKey,
      JSON.stringify({
        "team:team-1": { type: "folder", path: "/tmp/team-remembered" },
        "waker-1": { type: "folder", path: "/tmp/partner-only" },
      }),
    );
    wakerResources.current = [
      { id: "waker-1", name: "团队负责人" },
      { id: "waker-2", name: "执行成员" },
    ];
    projectResources.current = [
      {
        id: "project-1",
        name: "测试项目",
        available: true,
        sources: [{ path: "/tmp/project", available: true }],
      },
    ];
    recentProjectID.current = "project-1";
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-5",
        defaultReasoning: "medium",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto", "full-access"],
            reasoningOptions: [{ id: "medium", name: "Medium" }],
          },
        ],
      },
      {
        id: "claude",
        name: "Claude",
        enabled: true,
        state: "ready",
        defaultModel: "sonnet",
        defaultReasoning: "high",
        models: [
          {
            id: "sonnet",
            name: "Sonnet",
            permissionLevels: ["default", "auto", "full-access"],
            reasoningOptions: [{ id: "high", name: "High" }],
          },
        ],
      },
    ];
    let resolveCreateThread:
      | ((
          value: Awaited<ReturnType<TeamChatContext["onCreateThread"]>>,
        ) => void)
      | undefined;
    const onCreateThread = vi.fn<TeamChatContext["onCreateThread"]>(
      () =>
        new Promise((resolve) => {
          resolveCreateThread = resolve;
        }),
    );
    const createdThread = {
      leadExecution: {
        id: "lead-execution",
        status: "running",
        userMessageId: "user-message",
      },
      members: [
        {
          chatId: "lead-chat",
          isLead: true,
          model: "gpt-5",
          permissionLevel: "auto",
          reasoning: "medium",
          runtimeId: "codex",
          wakerId: "waker-1",
        },
      ],
      thread: {
        id: "thread-1",
        leadChatId: "lead-chat",
        leadWakerId: "waker-1",
        title: "新的团队对话",
      },
    } satisfies Awaited<ReturnType<TeamChatContext["onCreateThread"]>>;
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <WorkspaceProvider>
          <ChatQueueProvider>
            <TooltipProvider delayDuration={0}>
              <MemoryRouter initialEntries={["/team?draftId=draft-1"]}>
                <Routes>
                  <Route element={<ChatLayout rightPanel="workspace" />}>
                    <Route
                      path="/team"
                      element={
                        <>
                          <TeamMemberTabControls
                            scopeKey="team:team-1:draft:draft-1"
                            members={[
                              {
                                name: "执行成员",
                                wakerId: "waker-2",
                              },
                            ]}
                          />
                          <TeamChatPageContent
                            teamContext={{
                              channelId: "team-1",
                              leadProcessing: false,
                              leadWakerId: "waker-1",
                              members: [
                                {
                                  id: "waker-1",
                                  isLead: true,
                                  model: "gpt-5",
                                  name: "团队负责人",
                                  permissionLevel: "auto",
                                  reasoning: "medium",
                                  runtimeId: "codex",
                                },
                                {
                                  id: "waker-2",
                                  isLead: false,
                                  model: "gpt-5",
                                  name: "执行成员",
                                  permissionLevel: "auto",
                                  reasoning: "medium",
                                  runtimeId: "codex",
                                },
                              ],
                              name: "研发团队",
                              timeline: [],
                              threadDetailLoading: true,
                              hasEarlierTimelineMessages: false,
                              isLoadingEarlierTimelineMessages: false,
                              onLoadEarlierTimelineMessages: vi.fn(),
                              onCreateThread,
                              onSendMessage: vi.fn(),
                              onStopWork: vi.fn(),
                              onThreadChanged: vi.fn(),
                              onThreadCreated: vi.fn(),
                              onUpdateMemberConfigs: vi.fn(),
                              renderTaskBoard: () => <div>任务板详情</div>,
                              workState: "idle",
                              workProgress: {
                                phase: "idle",
                                review: 0,
                                terminal: 0,
                                total: 0,
                              },
                            }}
                          />
                        </>
                      }
                    />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </WorkspaceProvider>
      </QueryClientProvider>,
    );

    await user.click(
      await screen.findByRole("button", {
        name: "测试打开执行成员会话",
      }),
    );
    const runtimeButton = await screen.findByTestId("composer-runtime-control");
    expect(runtimeButton).toHaveTextContent("Codex");
    expect(runtimeButton).not.toBeNull();
    if (!runtimeButton) throw new Error("未找到成员 Runtime 选择按钮");
    await user.click(runtimeButton);
    await user.click(screen.getByRole("menuitemradio", { name: /claude/i }));
    await user.keyboard("{Escape}");
    await user.click(
      screen.getByRole("button", { name: "执行权限：自动审批" }),
    );
    await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
    await user.click(screen.getByRole("tab", { name: "团队负责人" }));
    await user.type(
      screen.getByRole("textbox", { name: "输入消息" }),
      "开始工作",
    );
    await user.click(screen.getByRole("button", { name: "发送消息" }));

    await waitFor(() => expect(onCreateThread).toHaveBeenCalledTimes(1));
    expect(screen.getByText("开始工作")).toBeVisible();
    expect(screen.getByText("飞速运转")).toBeVisible();
    expect(
      screen.getByText("团队负责人", { selector: "span.type-body" }),
    ).toBeVisible();
    expect(screen.queryByText("把目标交给团队负责人")).not.toBeInTheDocument();
    expect(
      document.querySelector('[role="status"][aria-busy="true"]'),
    ).not.toBeInTheDocument();
    expect(onCreateThread.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        workingDirectory: "/tmp/team-remembered",
        projectId: undefined,
        memberConfigs: expect.arrayContaining([
          expect.objectContaining({
            model: "sonnet",
            permissionLevel: "full-access",
            reasoning: "high",
            runtimeId: "claude",
            wakerId: "waker-2",
          }),
        ]),
      }),
    );
    await act(async () => resolveCreateThread?.(createdThread));
  });

  it.each([
    {
      selection: { type: "folder", path: "/tmp/remembered" },
      expected: { workingDirectory: "/tmp/remembered", projectId: "" },
    },
    {
      selection: { type: "project", projectId: "remembered-project" },
      expected: { workingDirectory: "", projectId: "remembered-project" },
    },
  ])(
    "新对话首次发送自动带入本地工作范围 $selection.type",
    async ({ selection, expected }) => {
      projectResources.current = [
        {
          id: "remembered-project",
          name: "记住的项目",
          available: true,
          sources: [{ path: "/tmp/project", available: true }],
        },
      ];
      window.localStorage.setItem(
        draftWorkspaceStorageKey,
        JSON.stringify({ "waker-1": selection }),
      );
      const user = userEvent.setup();
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      render(
        <QueryClientProvider client={queryClient}>
          <ChatQueueProvider>
            <TooltipProvider>
              <MemoryRouter
                initialEntries={["/chat?draftId=remembered&wakerId=waker-1"]}
              >
                <Routes>
                  <Route element={<ChatLayout />}>
                    <Route path="/chat" element={<ChatPage />} />
                    <Route path="/chats/:chatId" element={<ChatPage />} />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </QueryClientProvider>,
      );
      await screen.findByRole("heading", { name: "今天想一起完成什么？" });
      await user.type(
        screen.getByRole("textbox", { name: "输入消息" }),
        "继续工作",
      );
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled(),
      );
      await user.click(screen.getByRole("button", { name: "发送消息" }));
      await waitFor(() => expect(createChat).toHaveBeenCalledTimes(1));
      expect(createChat.mock.calls[0]?.[0]).toEqual(
        expect.objectContaining({ body: expect.objectContaining(expected) }),
      );
    },
  );

  it("首条消息创建对话后保持消息区稳定且不闪回加载状态", async () => {
    projectResources.current = [
      {
        id: "project-1",
        name: "测试项目",
        available: true,
        sources: [
          {
            path: "/tmp/project",
            available: true,
          },
        ],
      },
    ];
    recentProjectID.current = "project-1";
    createChatDelay.current = 100;
    chatMessageDelay.current = 250;
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter
              initialEntries={["/chat?draftId=draft-1&wakerId=waker-1"]}
            >
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route element={<ChatEntryPage />}>
                    <Route path="/chat" element={<></>} />
                    <Route path="/chats/:chatId" element={<></>} />
                  </Route>
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByRole("heading", { name: "今天想一起完成什么？" }),
    ).toBeVisible();
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "修复首条消息闪屏");
    const states = observeRenderedState(container, () => ({
      composerPreserved:
        container.querySelector('[role="textbox"][aria-label="输入消息"]') ===
        input,
      message:
        container
          .querySelector("[data-chat-scroll-viewport]")
          ?.textContent?.includes("修复首条消息闪屏") ?? false,
      loading: Boolean(
        container.querySelector('[role="status"][aria-busy="true"]'),
      ),
      primaryAction:
        container
          .querySelector("[data-chat-primary-action]")
          ?.getAttribute("data-chat-primary-action") ?? null,
    }));
    await user.click(screen.getByRole("button", { name: "发送消息" }));

    await waitFor(() =>
      expect(screen.getByText("修复首条消息闪屏")).toBeVisible(),
    );
    expect(screen.getByText("飞速运转")).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: "今天想一起完成什么？" }),
    ).not.toBeInTheDocument();
    await waitFor(() =>
      expect(queryClient.getQueryData(["chat", "chat-1"])).toBeTruthy(),
    );
    expect(createChat.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          clientRequestId: expect.any(String),
          text: "修复首条消息闪屏",
        }),
      }),
    );
    expect(screen.getByText("修复首条消息闪屏")).toBeVisible();
    expect(screen.getByText("飞速运转")).toBeVisible();
    expect(
      container.querySelector('[role="status"][aria-busy="true"]'),
    ).not.toBeInTheDocument();
    await waitFor(() =>
      expect(
        queryClient.getQueryData(["chats", "messages", "chat-1"]),
      ).toBeTruthy(),
    );
    states.disconnect();
    const firstMessage = states.samples.findIndex((state) => state.message);
    expect(firstMessage).toBeGreaterThanOrEqual(0);
    expect(
      states.samples
        .slice(firstMessage)
        .filter((state) => !state.message || state.loading),
    ).toEqual([]);
    const firstStop = states.samples.findIndex(
      (state) => state.primaryAction === "stop",
    );
    expect(firstStop).toBeGreaterThanOrEqual(0);
    expect({
      composerReplaced: states.samples.some(
        (state) => !state.composerPreserved,
      ),
      sendReappeared: states.samples
        .slice(firstStop)
        .some((state) => state.primaryAction === "send"),
    }).toEqual({ composerReplaced: false, sendReappeared: false });
  });

  it("切换到已有缓存消息的对话时不闪回全屏加载", async () => {
    chatMessageDelay.current = 500;
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(["chats", "messages", "chat-2"], {
      pages: [
        {
          resources: [
            {
              id: "message-2",
              role: "user",
              content: "缓存中的历史消息",
              status: "completed",
              sequence: 1,
            },
          ],
          hasMore: false,
          latestSequence: 1,
        },
      ],
      pageParams: [undefined],
    });
    queryClient.setQueryData(["chat", "chat-2"], {
      chat: {
        id: "chat-2",
        wakerId: "waker-1",
        title: "缓存对话",
        workspaceDirectory: "/tmp/ally",
        activeExecutionId: "",
        pendingApprovals: [],
      },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-2?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("缓存中的历史消息")).toBeVisible();
    expect(
      document.querySelector('[role="status"][aria-busy="true"]'),
    ).not.toBeInTheDocument();
  });

  it("路由过渡期间不展示 deferred Chat 的旧消息", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(["chats", "messages", "chat-1"], {
      pages: [
        {
          resources: [
            {
              id: "message-1",
              role: "user",
              content: "旧会话消息",
              status: "completed",
              sequence: 1,
            },
          ],
          hasMore: false,
          latestSequence: 1,
        },
      ],
      pageParams: [undefined],
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-2?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route
                    path="/chats/:chatId"
                    element={
                      <ChatPageContent chatId="chat-1" routeTransitioning />
                    }
                  />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(screen.queryByText("旧会话消息")).not.toBeInTheDocument();
    expect(
      container.querySelector('[role="status"][aria-busy="true"]'),
    ).toBeVisible();
  });

  it("执行正在等待工具授权时展示等待审批状态", async () => {
    chatActiveExecutionID.current = "execution-1";
    chatPendingApprovals.current = [
      {
        approvalId: "approval-1",
        toolCallId: "tool-1",
        title: "允许执行命令吗？",
        kind: "command",
        options: [
          { id: "reject-once", name: "Reject once", kind: "reject_once" },
          { id: "allow-once", name: "Allow once", kind: "allow_once" },
        ],
      },
    ];
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const status = await waitFor(() => {
      const element = document.querySelector<HTMLElement>(
        "[data-chat-activity-status]",
      );
      if (!element) throw new Error("chat activity status not found");
      expect(element).toHaveTextContent("等待审批");
      return element;
    });
    expect(status.querySelector(".bg-warning")).toHaveClass(
      "block",
      "size-1.5",
    );
  });

  it("审批提交失败后允许重试，成功后才进入结果确认中", async () => {
    chatActiveExecutionID.current = "execution-1";
    chatPendingApprovals.current = [
      {
        approvalId: "approval-1",
        toolCallId: "tool-1",
        title: "允许执行命令吗？",
        kind: "command",
        options: [
          { id: "reject-once", name: "Reject once", kind: "reject_once" },
          { id: "allow-once", name: "Allow once", kind: "allow_once" },
        ],
      },
    ];
    resolveApproval.mockRejectedValueOnce(new Error("network"));
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const allow = await screen.findByRole("button", { name: /允许本次/ });
    await user.click(allow);
    await waitFor(() => expect(resolveApproval).toHaveBeenCalledOnce());
    expect(screen.queryByText("结果确认中")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /允许本次/ })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: /允许本次/ }));
    await waitFor(() => expect(resolveApproval).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("结果确认中")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /允许本次/ })).toBeDisabled();
  });

  it("Runtime 默认完全访问不受支持时安全降为自动审批", async () => {
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-5",
        defaultPermissionLevel: "full-access",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto"],
          },
        ],
      },
    ];
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText("部分智能体无法启用完全访问。"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "执行权限：自动审批" }),
    ).toBeEnabled();
    await user.type(screen.getByRole("textbox", { name: "输入消息" }), "继续");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledOnce());
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({ permissionLevel: "auto" }),
      }),
    );
  });

  it("历史显式权限不受支持时保持选择并阻止发送", async () => {
    chatLastPermissionLevel.current = "full-access";
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-5",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto"],
          },
        ],
      },
    ];
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText(
        "当前智能体或模型不支持已选权限，请先更换权限或模型。",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "执行权限：完全访问" }),
    ).toBeEnabled();
    await user.type(screen.getByRole("textbox", { name: "输入消息" }), "继续");
    expect(screen.getByRole("button", { name: "发送消息" })).toBeDisabled();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("探测未声明权限时保持选择并阻止发送", async () => {
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-5",
        defaultPermissionLevel: "full-access",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
          },
        ],
      },
    ];
    const user = userEvent.setup();
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText(
        "当前智能体或模型不支持已选权限，请先更换权限或模型。",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "执行权限：完全访问" }),
    ).toBeEnabled();
    await user.type(screen.getByRole("textbox", { name: "输入消息" }), "继续");
    expect(screen.getByRole("button", { name: "发送消息" })).toBeDisabled();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it("按 ACP 模型展示专属 Thinking，并在切换模型时恢复 Runtime 默认推理", async () => {
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        state: "ready",
        defaultModel: "gpt-fixture",
        defaultReasoning: "medium",
        models: [
          {
            id: "gpt-fast",
            name: "GPT Fast",
            permissionLevels: ["default", "auto", "full-access"],
            reasoningOptions: [{ id: "low", name: "Low" }],
          },
          {
            id: "gpt-fixture",
            name: "GPT Fixture",
            permissionLevels: ["default", "auto", "full-access"],
            reasoningOptions: [
              { id: "medium", name: "Medium" },
              { id: "high", name: "High" },
            ],
          },
        ],
        reasoningOptions: [
          { id: "medium", name: "Medium" },
          { id: "high", name: "High" },
        ],
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <ChatQueueProvider>
            <TooltipProvider delayDuration={0}>
              <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
                <Routes>
                  <Route element={<ChatLayout />}>
                    <Route path="/chats/:chatId" element={<ChatPage />} />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </QueryClientProvider>
      </StrictMode>,
    );

    await user.click(await screen.findByTestId("composer-model-control"));
    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await user.click(screen.getByRole("radio", { name: "GPT Fast" }));
    expect(screen.getByTestId("composer-model-control")).toHaveTextContent(
      "Low",
    );
    expect(screen.getByRole("slider")).toHaveAttribute("aria-valuetext", "Low");
    expect(screen.queryByText("High")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "选择模型" }));
    await user.click(screen.getByRole("radio", { name: "GPT Fixture" }));
    expect(screen.getByRole("slider")).toHaveAttribute(
      "aria-valuetext",
      "Medium",
    );
    expect(screen.getByRole("slider")).toHaveAttribute("aria-valuemax", "1");
    expect(screen.queryByText("Low")).not.toBeInTheDocument();
    await user.keyboard("{ArrowRight}");
    expect(document.querySelector(".composer-effort-effect")).toHaveAttribute(
      "data-active",
    );
    expect(screen.getByRole("slider")).toHaveAttribute(
      "aria-valuetext",
      "High",
    );
    await user.keyboard("{ArrowLeft}");
    expect(
      document.querySelector(".composer-effort-effect"),
    ).not.toHaveAttribute("data-active");
    expect(screen.getByRole("slider")).toHaveAttribute(
      "aria-valuetext",
      "Medium",
    );
    await user.keyboard("{Escape}");

    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "检查模型配置");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          model: "gpt-fixture",
          reasoning: "medium",
        }),
      }),
    );
  });

  it("历史 Runtime 被禁用时提示手动切换且不自动改用全局默认", async () => {
    chatLastRuntimeID.current = "codex";
    runtimeOptions.current = [
      {
        id: "codex",
        name: "Codex",
        enabled: false,
        state: "ready",
        defaultModel: "gpt-5",
        models: [
          {
            id: "gpt-5",
            name: "GPT-5",
            permissionLevels: ["default", "auto", "full-access"],
          },
        ],
      },
      {
        id: "claude",
        name: "Claude",
        enabled: true,
        isDefault: true,
        state: "ready",
        defaultModel: "sonnet",
        models: [
          {
            id: "sonnet",
            name: "Sonnet",
            permissionLevels: ["default", "auto", "full-access"],
          },
        ],
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText("当前对话的智能体已停用，请选择其他智能体"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /codex/i })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /codex/i }));
    await user.click(screen.getByRole("menuitemradio", { name: /claude/i }));
    await user.click(screen.getByRole("button", { name: "确认切换并继续" }));

    await waitFor(() =>
      expect(
        screen.queryByText("当前对话的智能体已停用，请选择其他智能体"),
      ).not.toBeInTheDocument(),
    );
    await user.keyboard("{Escape}");
    expect(screen.getByRole("button", { name: /claude/i })).toBeInTheDocument();
  });

  it("聊天与工作台作为同级窗格共享内容高度", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );
    const layout = await screen.findByTestId("chat-page-layout");
    expect(layout).toHaveClass("flex", "min-h-0", "overflow-hidden");
    expect(screen.getByRole("main")).toHaveClass("flex-1");
  });

  it("工作区使用可调整的右侧栏宽度", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout workspaceOpen />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const layout = await screen.findByTestId("chat-page-layout");
    expect(layout.style.getPropertyValue("--chat-right-panel-width")).toBe(
      "320px",
    );
    expect(screen.getByTestId("chat-right-panel-host")).toBeInTheDocument();
  });

  it("会话文件发现并展示伙伴生成的文件", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout workspaceOpen />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    await user.click(await screen.findByRole("button", { name: "查看摘要" }));
    await user.click(await screen.findByRole("button", { name: "查看全部" }));
    await act(async () => {
      await vi.dynamicImportSettled();
    });

    expect(screen.queryByRole("radio", { name: "会话文件" })).toBeNull();

    expect(await screen.findByText("report.md")).toBeVisible();
    expect(listChatWorkspaceFiles).toHaveBeenCalledWith("chat-1", "/tmp/ally");
    await user.click(
      screen.getByRole("button", { name: "report.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "下载文件" }));
    expect(downloadChatWorkspaceFile).toHaveBeenCalledWith(
      "chat-1",
      "/tmp/ally",
      "attachments/generated/report.md",
    );
    await user.click(
      screen.getByRole("button", { name: "report.md 的文件操作" }),
    );
    await user.click(screen.getByRole("menuitem", { name: "删除文件" }));
    await user.click(screen.getByRole("button", { name: "删除文件" }));
    expect(deleteChatWorkspaceFile).toHaveBeenCalledWith(
      "chat-1",
      "/tmp/ally",
      "attachments/generated/report.md",
    );
    await waitFor(() =>
      expect(screen.queryByText("report.md")).not.toBeInTheDocument(),
    );
  });

  it("插件候选失效刷新后仍将用户显式选择交给 Core 校验", async () => {
    wakerPlugins.current = [
      {
        plugin: { id: "plugin-office", name: "办公插件" },
        ready: true,
        versions: [{ id: "pv1", version: "1.0.0" }],
        installations: [
          { wakerId: "waker-1", versionId: "pv1", enabled: true },
        ],
      },
    ];
    const user = userEvent.setup();
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );
    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "/办公");
    await screen.findByRole("option", { name: /办公插件/ });
    await user.keyboard("{ArrowDown}{Enter}");
    expect(
      document.querySelector('[data-chat-composer-plugin="plugin-office"]'),
    ).toBeInTheDocument();
    wakerPlugins.current = [];
    await act(async () => {
      await client.invalidateQueries({
        predicate: (query) =>
          (query.queryKey[0] as { _id?: string })?._id ===
          "listWakerPluginReferences",
      });
    });
    await user.type(input, "整理文档");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toMatchObject({
      body: { pluginIds: ["plugin-office"] },
    });
  });

  it("只把输入框本轮选择的已启用技能发送为 skillIds", async () => {
    wakerSkills.current = [
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
        },
      },
      {
        enabled: false,
        skill: { id: "skill-disabled", name: "未启用技能" },
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "/代码");
    expect(
      await screen.findByRole("option", { name: /代码审查/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /未启用技能/ }),
    ).not.toBeInTheDocument();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(
      document.querySelector('[data-chat-composer-skill="skill-review"]'),
    ).toHaveTextContent("代码审查");
    expect(input).toHaveValue("");
    await user.type(input, "检查这次改动");
    await user.click(screen.getByRole("button", { name: "发送消息" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          text: "检查这次改动",
          skillIds: ["skill-review"],
        }),
      }),
    );
    expect(
      document.querySelector('[data-chat-composer-skill="skill-review"]'),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: "查看技能 代码审查" }),
    ).toHaveAttribute("href", "/wakers/waker-1/skills/skill-review");
  });

  it("只在输入开头或空格后响应引用触发符", async () => {
    wakerSkills.current = [
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
        },
      },
    ];
    wakerConnectors.current = [
      {
        enabled: true,
        connector: {
          id: "connector-browser",
          name: "浏览器自动化",
          description: "控制 Chrome",
        },
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(
      input,
      "curl https://chatgpt.com/ user@example.com issue#123",
    );
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(searchWorkspaceFiles).not.toHaveBeenCalled();

    await user.type(input, " /");
    expect(
      await screen.findByRole("option", { name: /代码审查/ }),
    ).toBeInTheDocument();
  });

  it("通过 # 引用已启用 MCP 连接器并使用独立颜色标签", async () => {
    wakerConnectors.current = [
      {
        enabled: true,
        connector: {
          id: "connector-browser",
          name: "浏览器自动化",
          description: "控制 Chrome",
        },
      },
      {
        enabled: false,
        connector: {
          id: "connector-disabled",
          name: "未启用连接器",
          description: "不可引用",
        },
      },
      {
        enabled: true,
        connector: {
          activationPolicy: "team_context",
          id: "builtin-collaboration",
          name: "团队协作",
          description: "仅由团队执行上下文自动挂载",
        },
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    expect(input.querySelector("[data-placeholder]")).toHaveAttribute(
      "data-placeholder",
      "输入消息…（@ 引用文件，/ 调用 Skill，# 使用 MCP；Enter 发送）",
    );
    vi.spyOn(input, "getBoundingClientRect").mockReturnValue({
      x: 120,
      y: 500,
      width: 360,
      height: 80,
      top: 500,
      right: 480,
      bottom: 580,
      left: 120,
      toJSON: () => ({}),
    });
    await user.type(input, "#浏览");
    expect(
      await screen.findByRole("option", { name: /浏览器自动化/ }),
    ).toBeInTheDocument();
    expect(screen.getByRole("listbox")).toHaveStyle({
      top: "auto",
      bottom: `${window.innerHeight - 500 + 8}px`,
    });
    expect(
      screen.queryByRole("option", { name: /未启用连接器/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /团队协作/ }),
    ).not.toBeInTheDocument();
    await user.keyboard("{ArrowDown}{Enter}");
    const tag = document.querySelector(
      '[data-chat-composer-connector="connector-browser"]',
    );
    expect(tag).toHaveTextContent("浏览器自动化");
    expect(tag).toHaveClass("bg-success/12", "text-success");

    await user.type(input, "检查页面");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          text: "检查页面",
          connectorIds: ["connector-browser"],
        }),
      }),
    );
    expect(
      await screen.findByRole("link", {
        name: "查看连接器 浏览器自动化",
      }),
    ).toHaveAttribute("href", "/wakers/waker-1/connectors/connector-browser");
  });

  it("已有对话的 @ 文件搜索只允许选择冻结项目文件", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "@AGENTS");
    expect(
      await screen.findAllByRole("option", { name: /AGENTS\.md/ }),
    ).not.toHaveLength(0);
    await user.keyboard("{Enter}");
    await waitFor(() => {
      const references = document.querySelectorAll(
        '[data-chat-composer-code-reference="/tmp/project/AGENTS.md"]',
      );
      expect(references).toHaveLength(1);
      expect(input).toContainElement(references[0] as HTMLElement);
    });
    expect(input).toHaveValue("");
    expect(searchWorkspaceFiles).toHaveBeenCalledWith(
      "/tmp/ally",
      "AGENTS",
      20,
      "projects",
    );
  });

  it("文件选择器支持上下键切换并用 Enter 选择当前项", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const scrollIntoView = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "@");
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    scrollIntoView.mockClear();
    await user.keyboard("{ArrowDown}");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    expect(screen.getAllByRole("option")[1]).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(
        document
          .querySelector("[data-chat-composer-code-reference]")
          ?.getAttribute("data-chat-composer-code-reference"),
      ).toBe("/tmp/ally/readme.md"),
    );
    scrollIntoView.mockRestore();
  });

  it("按输入顺序保留文件、正文和技能引用，并将文件引用与附件分开提交", async () => {
    wakerSkills.current = [
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
        },
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "@readme");
    await screen.findByRole("option", { name: /readme\.md/ });
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(
        document.querySelector("[data-chat-composer-code-reference]"),
      ).toHaveTextContent("readme.md"),
    );

    await user.type(input, "xx ");
    await user.type(input, "/代码");
    await screen.findByRole("option", { name: /代码审查/ });
    await user.keyboard("{Enter}");

    expect(input.textContent).toBe("readme.mdxx 代码审查");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(sendMessage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        body: expect.objectContaining({
          text: "xx",
          attachments: [],
          codeReferences: [
            {
              path: "/tmp/ally/readme.md",
              startLine: 0,
              endLine: 0,
              content: "",
            },
          ],
          skillIds: ["skill-review"],
        }),
      }),
    );
  });

  it("输入框开头按 Backspace 会移除最近添加的整文件代码引用", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "@readme");
    expect(
      await screen.findByRole("option", { name: /readme\.md/ }),
    ).toBeInTheDocument();
    await user.keyboard("{Enter}");
    await waitFor(() =>
      expect(
        document.querySelector("[data-chat-composer-code-reference]"),
      ).toHaveTextContent("readme.md"),
    );

    await user.click(input);
    await user.keyboard("{Backspace}");
    expect(
      document.querySelector("[data-chat-composer-code-reference]"),
    ).not.toBeInTheDocument();
    expect(input).toHaveValue("");

    await user.type(input, "继续");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    const request = sendMessage.mock.calls[0]?.[0] as
      | { body?: { attachments?: unknown[]; codeReferences?: unknown[] } }
      | undefined;
    expect(request?.body?.attachments ?? []).toEqual([]);
    expect(request?.body?.codeReferences ?? []).toEqual([]);
  });

  it("技能查询失败时在可见选择器中展示错误而不是空状态", async () => {
    wakerSkillsError.current = true;
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    await screen.findByRole("textbox", { name: "输入消息" });
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "/");

    expect(await screen.findByText("技能暂时无法载入")).toBeInTheDocument();
    expect(
      screen.queryByText("当前伙伴没有已启用技能"),
    ).not.toBeInTheDocument();
  });

  it("发送被后端拒绝后恢复本轮选择的技能", async () => {
    wakerSkills.current = [
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
        },
      },
    ];
    sendMessage.mockRejectedValueOnce(new Error("request rejected"));
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "/代码");
    await user.keyboard("{ArrowDown}{Enter}");
    await user.type(input, "检查这次改动");
    await user.click(screen.getByRole("button", { name: "发送消息" }));

    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    expect(
      document.querySelector('[data-chat-composer-skill="skill-review"]'),
    ).toHaveTextContent("代码审查");
    expect(input).toHaveValue("检查这次改动");
  });

  it("输入框开头按 Backspace 会移除最近选择的技能", async () => {
    wakerSkills.current = [
      {
        enabled: true,
        skill: {
          id: "skill-review",
          name: "代码审查",
          description: "检查真实改动",
        },
      },
    ];
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const input = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(input, "/代码");
    await user.keyboard("{ArrowDown}{Enter}");
    expect(
      document.querySelector('[data-chat-composer-skill="skill-review"]'),
    ).toBeInTheDocument();

    await user.click(input);
    await user.keyboard("{Backspace}");
    expect(
      document.querySelector('[data-chat-composer-skill="skill-review"]'),
    ).not.toBeInTheDocument();

    await user.type(input, "继续");
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(sendMessage).toHaveBeenCalledTimes(1));
    const request = sendMessage.mock.calls[0]?.[0] as
      { body?: { skillIds?: string[] } } | undefined;
    expect(request?.body?.skillIds ?? []).toEqual([]);
  });

  it("历史用户消息展示可点击的本轮技能", async () => {
    historyMessageSkills.current = [
      { id: "skill-review", name: "代码审查", version: "1.4.0" },
    ];
    wakerSkills.current = [
      {
        enabled: false,
        skill: { id: "skill-review", name: "代码审查" },
      },
    ];
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByRole("link", { name: "查看技能 代码审查" }),
    ).toHaveAttribute("href", "/wakers/waker-1/skills/skill-review");
    expect(screen.queryByText("v1.4.0")).not.toBeInTheDocument();
  });

  it("已卸载技能在历史消息中保留快照但不再跳转", async () => {
    historyMessageSkills.current = [
      { id: "skill-removed", name: "已卸载技能", version: "1.0.0" },
    ];
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByTitle("该技能已卸载，仅保留发送时的名称和版本"),
    ).toHaveTextContent("已卸载技能");
    expect(
      screen.queryByRole("link", { name: "查看技能 已卸载技能" }),
    ).not.toBeInTheDocument();
  });

  it("会话页不再渲染内部伙伴栏", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const mainPanel = screen.getByRole("main");
    expect(mainPanel.parentElement).toHaveClass("flex");
    expect(mainPanel).not.toHaveClass("ml-(--shell-gap)");
    expect(
      document.querySelector("[data-chat-context]"),
    ).not.toBeInTheDocument();
  });

  it("从对话任务页直接进入会话即可显示历史", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText("直接进入也应显示的历史消息"),
    ).toBeInTheDocument();
    expect(
      document.querySelector("[data-fork-history-boundary]"),
    ).not.toBeInTheDocument();
  });

  it("分支会话在继承历史末尾显示分割线，不占用顶部来源栏", async () => {
    forkSourceChatId.current = "source-chat";
    historyMessages.current = [
      {
        id: "inherited-user",
        role: "user",
        content: "继承提问",
        inherited: true,
        status: "completed",
        sequence: 1,
      },
      {
        id: "inherited-assistant",
        role: "assistant",
        content: "继承回复",
        displayParts: [
          { id: "inherited-text", type: "text", text: "继承回复" },
        ],
        inherited: true,
        status: "completed",
        sequence: 2,
      },
      {
        id: "new-user",
        role: "user",
        content: "分支的新提问",
        status: "completed",
        sequence: 3,
      },
    ];
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const inherited = await screen.findByText("继承回复");
    const sourceLink = screen.getByRole("link", { name: "返回来源会话" });
    const boundary = sourceLink.closest("[data-fork-history-boundary]");
    if (!boundary) throw new Error("分支历史分割线未显示");
    const newMessage = await screen.findByText("分支的新提问");
    expect(sourceLink).toHaveAttribute(
      "href",
      "/chats/source-chat?wakerId=waker-1",
    );
    expect(sourceLink).toHaveClass("text-accent");
    expect(
      inherited.compareDocumentPosition(boundary) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      boundary.compareDocumentPosition(newMessage) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.queryByText("此会话是分支 · 返回来源会话")).toBeNull();
    await userEvent.click(sourceLink);
    expect(
      await screen.findByText("历史消息：source-chat", {}, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it("来源会话已删除时点击分割线不会打开失效页面", async () => {
    forkSourceChatId.current = "source-chat";
    sourceChatAvailable.current = false;
    historyMessages.current = [
      {
        id: "inherited-assistant",
        role: "assistant",
        content: "继承回复",
        displayParts: [
          { id: "inherited-text", type: "text", text: "继承回复" },
        ],
        inherited: true,
        status: "completed",
        sequence: 1,
      },
    ];
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );
    await userEvent.click(
      await screen.findByRole("link", { name: "返回来源会话" }),
    );
    expect(
      await screen.findByText("来源会话已删除或暂时不可用"),
    ).toBeInTheDocument();
    expect(screen.getByText("继承回复")).toBeInTheDocument();
    expect(screen.queryByText("历史消息：source-chat")).toBeNull();
  });

  it("进入已预取的运行中对话时按顺序展示消息和独立工具段", async () => {
    chatActiveExecutionID.current = "execution-1";
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
      },
    });
    queryClient.setQueryData(["chats", "messages", "chat-1"], {
      pages: [
        {
          resources: [
            {
              id: "assistant-1",
              role: "assistant",
              executionId: "execution-1",
              status: "running",
              sequence: 2,
              displayParts: [
                { type: "process_commentary", text: "检查目标地址" },
                {
                  type: "tool",
                  toolCallId: "tool-read",
                  toolCategory: "file_read",
                  toolAction: "read_file",
                  toolStatus: "completed",
                },
                { type: "reasoning", text: "随后验证输出" },
                {
                  type: "tool",
                  toolCallId: "tool-1",
                  toolCategory: "command",
                  toolAction: "execute_command",
                  toolLabel: "curl https://chatgpt.com/",
                  toolStatus: "running",
                },
              ],
            },
          ],
          hasMore: false,
          latestSequence: 2,
        },
      ],
      pageParams: [undefined],
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    const commentary = await screen.findByText("检查目标地址");
    const reasoning = await screen.findByText("随后验证输出");
    const read = screen.getByRole("button", { name: "已读取文件" });
    const command = screen.getByRole("button", { name: "正在执行命令" });
    expect(read).toHaveAttribute("aria-expanded", "false");
    expect(command).toHaveAttribute("aria-expanded", "false");
    expect(command).toHaveAttribute("data-running-shimmer", "true");
    expect(screen.queryByText("飞速运转")).not.toBeInTheDocument();
    expect(
      document.querySelector(".agent-grid-spinner"),
    ).not.toBeInTheDocument();
    expect(commentary).toHaveClass("type-chat", "text-foreground");
    expect(reasoning).toHaveClass("type-chat", "text-foreground");
    expect(
      commentary.compareDocumentPosition(read) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      read.compareDocumentPosition(reasoning) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      reasoning.compareDocumentPosition(command) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("IDE 缩进代码保留在用户消息列内并允许横向滚动", async () => {
    historyMessageContent.current =
      "    # 需要在重建完成后删除这个文件\n    File.unlink('/tmp/zdns_node_rebuild.flag')";
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route element={<ChatLayout />}>
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => expect(container.querySelector("pre")).not.toBeNull());
    const markdown = container.querySelector('[data-chat-markdown="user"]');
    const bubble = markdown?.closest(".bg-surface-tertiary");
    expect(bubble?.parentElement).toHaveClass("min-w-0", "max-w-[78%]");
    expect(bubble).toHaveClass("min-w-0", "max-w-full");
    expect(markdown?.firstElementChild).toHaveClass(
      "[&_pre]:max-w-full",
      "[&_pre]:overflow-x-auto",
    );
  });

  it("从伙伴对话任务页导航后即可显示异步历史", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <ChatQueueProvider>
            <TooltipProvider delayDuration={0}>
              <MemoryRouter initialEntries={["/wakers/waker-1/chats"]}>
                <Routes>
                  <Route element={<ChatLayout />}>
                    <Route
                      path="/wakers/:wakerId/chats"
                      element={<ChatTaskListPage />}
                    />
                    <Route path="/chats/:chatId" element={<ChatPage />} />
                  </Route>
                </Routes>
              </MemoryRouter>
            </TooltipProvider>
          </ChatQueueProvider>
        </QueryClientProvider>
      </StrictMode>,
    );

    await user.click(
      await screen.findByRole("link", { name: "直接进入的对话" }),
    );

    await waitFor(() =>
      expect(
        queryClient.getQueryData(["chats", "messages", "chat-1"]),
      ).toMatchObject({
        pages: [
          {
            resources: [
              { content: "直接进入也应显示的历史消息", id: "message-1" },
            ],
          },
        ],
      }),
    );
    expect(
      await screen.findByText("直接进入也应显示的历史消息"),
    ).toBeInTheDocument();
  });

  it("同一伙伴切换会话时立即展示消息区加载状态并保留输入框", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ChatQueueProvider>
          <TooltipProvider delayDuration={0}>
            <MemoryRouter initialEntries={["/chats/chat-1?wakerId=waker-1"]}>
              <Routes>
                <Route
                  element={
                    <ChatLayout>
                      <TransitionChatButton />
                    </ChatLayout>
                  }
                >
                  <Route path="/chats/:chatId" element={<ChatPage />} />
                </Route>
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </ChatQueueProvider>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText("直接进入也应显示的历史消息"),
    ).toBeInTheDocument();
    const input = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(input, "尚未发送的草稿");
    chatMessageDelay.current = 250;

    fireEvent.click(screen.getByRole("button", { name: "切换到第二个会话" }));

    expect(
      screen.queryByText("直接进入也应显示的历史消息"),
    ).not.toBeInTheDocument();
    expect(
      container.querySelector('[role="status"][aria-busy="true"]'),
    ).toBeVisible();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBeVisible();
    expect(await screen.findByText("历史消息：chat-2")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(input);
    expect(input).toHaveTextContent("");
  });
});

function TransitionChatButton() {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      onClick={() => {
        startTransition(() => {
          void navigate("/chats/chat-2?wakerId=waker-1");
        });
      }}
    >
      切换到第二个会话
    </button>
  );
}

function TeamMemberTabControls({
  members,
  scopeKey = "team:team-1:thread-1",
}: {
  members: Array<{ chatId?: string; name: string; wakerId: string }>;
  scopeKey?: string;
}) {
  const workspaceScopeKey = scopeKey;
  const workspace = useSceneWorkspace(workspaceScopeKey);
  return members.map((member) => (
    <button
      key={member.wakerId}
      type="button"
      aria-label={`测试打开${member.name}会话`}
      onClick={() =>
        workspace.dispatch({
          type: "open-member",
          member: {
            chatId: member.chatId,
            label: member.name,
            wakerId: member.wakerId,
          },
        })
      }
    />
  ));
}

function ChatLayout({
  children,
  rightPanel,
  workspaceOpen = false,
}: {
  children?: ReactNode;
  rightPanel?: "workspace";
  workspaceOpen?: boolean;
}) {
  return (
    <>
      {children}
      <Outlet
        context={{
          chatRightPanel:
            rightPanel ?? (workspaceOpen ? "workspace" : undefined),
          setChatRightPanel: () => undefined,
        }}
      />
    </>
  );
}

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: () => () => {},
}));

it("团队页面工作台中的独立侧聊自动接入共享阅读器，并以真实 Chat 归属登记", async () => {
  const chatId = "a".repeat(32);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ChatQueueProvider>
        <TooltipProvider>
          <MemoryRouter
            initialEntries={[
              `/teams/team-1/threads/${"b".repeat(32)}?wakerId=waker-1`,
            ]}
          >
            <Routes>
              <Route element={<ChatLayout />}>
                <Route
                  path="*"
                  element={<ChatPageContent chatId={chatId} embedded />}
                />
              </Route>
            </Routes>
          </MemoryRouter>
        </TooltipProvider>
      </ChatQueueProvider>
    </QueryClientProvider>,
  );
  await screen.findByText(`历史消息：${chatId}`);
  await waitFor(() =>
    expect(
      view.container.querySelector("[data-work-result-owner-id]"),
    ).toHaveAttribute("data-work-result-owner-id", chatId),
  );
  expect(
    view.container.querySelector("[data-work-result-owner-id]"),
  ).toHaveAttribute("data-work-result-owner-kind", "chat");
  expect(
    view.container.querySelector("[data-work-result-owner-id]"),
  ).toHaveAttribute("data-work-result-chat-id", chatId);
  view.unmount();
  queryClient.clear();
});
