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
import { StrictMode } from "react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import "@/shared/i18n";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";
import type { subscribeChannel, subscribeChat } from "@/shared/api/events";
import type { subscribeGlobalEvents } from "@/shared/api/global-events";
import { client } from "@/shared/api/generated/client.gen";
import type {
  CreateChannelThreadResponse,
  ListChannelTimelineResponse,
} from "@/shared/api/generated/types.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { AppSceneWorkspaceContextProvider } from "@/shared/workspace/app-scene-workspace";
import {
  WorkspaceProvider,
  useSceneWorkspace,
} from "@/shared/workspace/workspace-context";
import { TeamThreadPage } from "./team-thread-page";

const stagePastedFile = vi.hoisted(() =>
  vi.fn<(file: File) => Promise<string>>(),
);
vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => false,
  hasPlatformService: () => false,
  subscribeFileDrop: () => () => undefined,
  stageAttachment: stagePastedFile,
}));
const stream = vi.hoisted(() => ({
  channel: undefined as
    Parameters<typeof subscribeChannel>[2]["onEvent"] | undefined,
  chat: undefined as Parameters<typeof subscribeChat>[1]["onEvent"] | undefined,
  reconcileChannel: undefined as
    Parameters<typeof subscribeChannel>[2]["reconcileCursor"] | undefined,
  workflows: new Map<
    string,
    Parameters<typeof subscribeGlobalEvents>[2]["onEvent"]
  >(),
}));
vi.mock("@/shared/api/events", () => ({
  subscribeChannel: ((_, __, { onEvent, reconcileCursor }) => {
    stream.channel = onEvent;
    stream.reconcileChannel = reconcileCursor;
    return () => {
      if (stream.channel === onEvent) stream.channel = undefined;
      if (stream.reconcileChannel === reconcileCursor)
        stream.reconcileChannel = undefined;
    };
  }) satisfies typeof subscribeChannel,
  subscribeChat: ((_, { onEvent }) => {
    stream.chat = onEvent;
    return () => {
      if (stream.chat === onEvent) stream.chat = undefined;
    };
  }) satisfies typeof subscribeChat,
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function verifyQueuedLeadInputFeedback() {
  const submissions: Array<{
    submissionId: string;
    replyToMessageId?: string;
    attachments?: Array<{ sourcePath: string }>;
    workflowId?: string;
  }> = [];
  const receipts = new Map<string, "pending" | "failed" | "cancelled">();
  const reads = vi.fn();
  let blockedReceiptRead: Promise<void> | undefined;
  const originalFile = new File(["queued file"], "queued-reply.md", {
    type: "text/markdown",
  });
  const uploadPaths = [
    "upload:first/queued-reply.md",
    "upload:fresh/queued-reply.md",
    "upload:retried/queued-reply.md",
  ];
  stagePastedFile.mockReset();
  for (const path of uploadPaths) stagePastedFile.mockResolvedValueOnce(path);
  server.use(
    http.get(`${api}/chats/member-chat`, () =>
      HttpResponse.json({
        chat: {
          id: "member-chat",
          wakerId: "member",
          workspaceDirectory: "/tmp/bot",
        },
      }),
    ),
    http.get(`${api}/chats/member-chat/messages`, () =>
      HttpResponse.json({ resources: [], hasMore: false }),
    ),
    http.get(`${api}/wakers/member/skills`, () => HttpResponse.json([])),
    http.get(`${api}/wakers/member/connectors`, () => HttpResponse.json([])),
    http.get(`${api}/wakers/member/plugin-references`, () =>
      HttpResponse.json([]),
    ),
    http.get(`${api}/workflows`, () =>
      HttpResponse.json({
        items: [
          {
            id: "queued-workflow",
            title: "Queued reply flow",
            currentRevisionId: "rev",
          },
        ],
      }),
    ),
    http.post(
      `${api}/channels/team-1/threads/thread-1/messages`,
      async ({ request }) => {
        const body = (await request.json()) as (typeof submissions)[number];
        submissions.push(body);
        const id = `queued-${submissions.length}`;
        receipts.set(id, "pending");
        return HttpResponse.json({
          queuedInput: {
            id,
            createdAt: Date.now(),
            status: "pending",
          },
        });
      },
    ),
    http.get(
      `${api}/channels/team-1/threads/thread-1/lead-inputs/list`,
      async ({ request }) => {
        const ids =
          new URL(request.url).searchParams.get("ids")?.split(",") ?? [];
        reads(ids);
        if (blockedReceiptRead) {
          await blockedReceiptRead;
          return new HttpResponse(null, { status: 503 });
        }
        return HttpResponse.json(
          ids.map((id) => ({
            id,
            status: receipts.get(id),
            errorCode:
              receipts.get(id) === "failed" ? "RuntimeUnavailable" : undefined,
          })),
        );
      },
    ),
  );
  const user = userEvent.setup();
  const enqueue = async (text: string, expectedCount: number) => {
    await user.type(screen.getByRole("textbox"), text);
    await user.click(screen.getByRole("button", { name: "加入发送队列" }));
    await waitFor(() => {
      expect(submissions).toHaveLength(expectedCount);
      expect(screen.queryByText(text)).not.toBeInTheDocument();
    });
  };
  const completed = "正常接纳的排队消息";
  await enqueue(completed, 1);
  expect(stream.chat).toBeDefined();
  act(() => {
    stream.chat?.({
      type: "message.user_created",
      sequence: 3,
      message: {
        id: "queued-user-message",
        executionId: "queued-execution",
        submissionId: submissions[0]?.submissionId,
        createdSequence: 3,
        content: completed,
        actor: "user",
        role: "user",
      },
    });
  });
  expect(await screen.findByText(completed)).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();

  await enqueue("派发前失败的排队消息", 2);
  expect(stream.channel).toBeDefined();
  const failure = {
    id: "queued-input-failed",
    threadId: "thread-1",
    type: "lead.input_failed",
    payload: {
      inputId: "queued-2",
      errorCode: "RuntimeUnavailable",
      reason: "private runtime exception",
      reportedBySend: false,
    },
  } as const;
  act(() => {
    stream.channel?.({
      ...failure,
      id: "synchronous-input-failed",
      payload: {
        ...failure.payload,
        inputId: "synchronous",
        reportedBySend: true,
      },
    });
  });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  act(() => stream.channel?.(failure));
  const notice = await screen.findByRole("alert");
  expect(notice).toHaveTextContent("排队的消息未能交给负责人");
  expect(notice).not.toHaveTextContent("private runtime exception");
  await user.click(screen.getByRole("button", { name: "关闭错误提示" }));
  act(() => stream.channel?.(failure));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();

  const attachReply = async () => {
    const target = screen
      .getByText(completed)
      .closest<HTMLElement>("[data-message-id]");
    if (!target) throw new Error("缺少引用目标");
    await user.click(within(target).getByRole("button", { name: "回复" }));
  };
  const quoted = "等待派发的原引用回复";
  await attachReply();
  await user.type(screen.getByRole("textbox"), "#Queued");
  await user.click(
    await screen.findByRole("option", { name: "Queued reply flow" }),
  );
  fireEvent.paste(screen.getByRole("textbox"), {
    clipboardData: {
      files: [originalFile],
    },
  });
  expect(await screen.findByText("queued-reply.md")).toBeVisible();
  await enqueue(quoted, 3);
  expect(submissions[2]?.replyToMessageId).toBe("queued-user-message");
  expect(submissions[2]?.workflowId).toBe("queued-workflow");
  expect(submissions[2]?.attachments).toEqual([{ sourcePath: uploadPaths[0] }]);
  await waitFor(() => expect(reads).toHaveBeenCalledWith(["queued-3"]));
  await user.type(screen.getByRole("textbox"), "临时输入");
  await user.clear(screen.getByRole("textbox"));
  act(() => {
    receipts.set("queued-3", "failed");
    stream.channel?.({
      ...failure,
      id: "queued-reply-failed",
      payload: { ...failure.payload, inputId: "queued-3" },
    });
  });
  await waitFor(() => expect(screen.getByRole("textbox")).toHaveValue(quoted));
  expect(screen.getByRole("button", { name: "移除回复" })).toBeVisible();
  expect(await screen.findByText("queued-reply.md")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();

  // 重新发送恢复的草稿，并在它等待派发时开始下一份草稿。
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "加入发送队列" })).toBeEnabled(),
  );
  await user.click(screen.getByRole("button", { name: "加入发送队列" }));
  await waitFor(() => expect(submissions).toHaveLength(4));
  expect(submissions[3]).toEqual({
    ...submissions[2],
    submissionId: submissions[3]?.submissionId,
    attachments: [{ sourcePath: uploadPaths[1] }],
  });
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
  await user.type(screen.getByRole("textbox"), "不能覆盖的新草稿");
  await user.click(screen.getByRole("button", { name: "打开开发成员" }));
  await user.type(screen.getByRole("textbox"), "另一个页签的草稿");
  act(() => {
    receipts.set("queued-4", "failed");
    stream.channel?.({
      ...failure,
      id: "second-queued-reply-failed",
      payload: { ...failure.payload, inputId: "queued-4" },
    });
  });
  expect(screen.getByRole("textbox")).toHaveValue("另一个页签的草稿");
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "切换到团队主对话" }));
  expect(screen.getByRole("textbox")).toHaveValue("不能覆盖的新草稿");
  await user.click(await screen.findByRole("button", { name: "重试" }));
  await waitFor(() => expect(submissions).toHaveLength(5));
  const retried = submissions[4];
  expect(retried?.submissionId).not.toBe(submissions[3]?.submissionId);
  expect(retried).toEqual({
    ...submissions[3],
    submissionId: retried?.submissionId,
    attachments: [{ sourcePath: uploadPaths[2] }],
  });
  expect(stagePastedFile).toHaveBeenCalledTimes(3);
  for (const [file] of stagePastedFile.mock.calls) {
    expect(file.size).toBe(originalFile.size);
    expect(file.name).toBe(originalFile.name);
    expect(file.type).toBe(originalFile.type);
  }
  expect(screen.getByRole("textbox")).toHaveValue("不能覆盖的新草稿");

  // 断线期间取消了排队的回复，恢复时只结算原队列，不复活已停止的草稿。
  await user.clear(screen.getByRole("textbox"));
  await attachReply();
  await enqueue("已取消的排队引用", 6);
  await waitFor(() =>
    expect(reads).toHaveBeenCalledWith(["queued-5", "queued-6"]),
  );
  const readsBeforeRecovery = reads.mock.calls.length;
  const blockedReceipt = deferred<undefined>();
  blockedReceiptRead = blockedReceipt.promise;
  const reconcileChannel = stream.reconcileChannel;
  if (!reconcileChannel) throw new Error("缺少团队重连回调");
  let recovered = false;
  let recovery: Promise<unknown> | undefined;
  act(() => {
    recovery = reconcileChannel(new AbortController().signal)
      .then((cursor) => {
        recovered = true;
        return cursor;
      })
      .catch(() => undefined);
  });
  try {
    await waitFor(() => {
      expect(reads.mock.calls.length).toBeGreaterThan(readsBeforeRecovery);
      expect(recovered).toBe(true);
    });
  } finally {
    await act(async () => {
      blockedReceiptRead = undefined;
      blockedReceipt.resolve(undefined);
      await recovery;
    });
  }
  receipts.set("queued-5", "cancelled");
  receipts.set("queued-6", "cancelled");
  expect(stream.reconcileChannel).toBeDefined();
  await act(async () => {
    await stream.reconcileChannel?.(new AbortController().signal);
  });
  expect(screen.getByRole("textbox")).toHaveValue("");
  expect(
    screen.queryByRole("button", { name: "移除回复" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "重试" }),
  ).not.toBeInTheDocument();
}

async function verifyWorkflowDraftMove() {
  const staging = deferred<string>();
  stagePastedFile.mockReset();
  stagePastedFile.mockReturnValue(staging.promise);
  server.use(
    http.get(`${api}/workflows`, () =>
      HttpResponse.json({
        items: [
          { id: "flow", title: "Release report", currentRevisionId: "rev" },
        ],
      }),
    ),
    http.get(`${api}/chats/member-chat`, () =>
      HttpResponse.json({
        chat: {
          id: "member-chat",
          wakerId: "member",
          workspaceDirectory: "/tmp/bot",
        },
      }),
    ),
    http.get(`${api}/chats/member-chat/messages`, () =>
      HttpResponse.json({ resources: [], hasMore: false }),
    ),
    http.get(`${api}/wakers/member/skills`, () => HttpResponse.json([])),
    http.get(`${api}/wakers/member/connectors`, () => HttpResponse.json([])),
    http.get(`${api}/wakers/member/plugin-references`, () =>
      HttpResponse.json([]),
    ),
  );
  const user = userEvent.setup();
  const input = screen.getByRole("textbox");
  await user.type(input, "@");
  await user.click(await screen.findByRole("option", { name: /开发成员/ }));
  await user.type(input, " #release");
  await user.click(
    await screen.findByRole("option", { name: "Release report" }),
  );
  fireEvent.paste(input, {
    clipboardData: {
      files: [
        new File(["draft input"], "report.md", { type: "text/markdown" }),
      ],
    },
  });
  await waitFor(() => expect(stagePastedFile).toHaveBeenCalledOnce());
  await user.click(screen.getByRole("button", { name: "带草稿打开成员对话" }));
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveTextContent("Release report");
    expect(screen.getByRole("textbox")).not.toHaveTextContent("开发成员");
  });
  expect(screen.getByRole("button", { name: "发送消息" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "切换到团队主对话" }));
  expect(screen.getByRole("textbox")).not.toHaveTextContent("Release report");
  await user.click(screen.getByRole("button", { name: "切换到开发成员" }));
  expect(screen.getByRole("textbox")).toHaveTextContent("Release report");
  await act(async () => staging.resolve("/tmp/bot/uploads/report.md"));
  expect(await screen.findByText("report.md")).toBeVisible();
  expect(screen.getAllByText("report.md")).toHaveLength(1);
  expect(stagePastedFile).toHaveBeenCalledOnce();
}

function TeamTestTabs({ openMember = false }: { openMember?: boolean }) {
  const workspace = useSceneWorkspace("team:team-1:thread-1");
  return (
    <>
      {openMember ? (
        <button
          type="button"
          onClick={() =>
            workspace.dispatch({
              type: "open-member",
              member: {
                chatId: "member-chat",
                label: "开发成员",
                wakerId: "member",
              },
            })
          }
        >
          打开开发成员
        </button>
      ) : null}
      {workspace.snapshot.tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          onClick={() =>
            workspace.dispatch({ type: "select-tab", tabId: tab.id })
          }
        >
          {tab.kind === "chat" ? "切换到团队主对话" : `切换到${tab.label}`}
        </button>
      ))}
    </>
  );
}

const api = "http://bot.test/api/v1";
const server = setupServer(
  http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
  http.get(`${api}/chats/:chatId/workflow-runs`, () =>
    HttpResponse.json({ resources: [] }),
  ),
);
const originalScrollTo = HTMLElement.prototype.scrollTo;
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
});
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
  client.setConfig({ baseUrl: "http://bot.test" });
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  HTMLElement.prototype.scrollTo = vi.fn();
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
  HTMLElement.prototype.scrollTo = originalScrollTo;
});

describe("团队首发完整路由交接", () => {
  it.each<{
    withExecution: boolean;
    status: "running" | "completed";
    historyFails?: boolean;
    unknownMemberOptions?: boolean;
    checkQueuedFailure?: boolean;
    checkDraftMove?: boolean;
    checkWorkflowOnly?: boolean;
  }>([
    { withExecution: true, status: "running" },
    { withExecution: false, status: "running" },
    { withExecution: false, status: "completed" },
    { withExecution: false, status: "running", historyFails: true },
    { withExecution: true, status: "running", unknownMemberOptions: true },
    { withExecution: true, status: "running", checkQueuedFailure: true },
    { withExecution: false, status: "completed", checkDraftMove: true },
    { withExecution: false, status: "completed", checkWorkflowOnly: true },
  ])(
    "首发配置与交接 execution=$withExecution，历史=$status，首次查询失败=$historyFails，成员可选配置未知=$unknownMemberOptions，队列结算=$checkQueuedFailure，草稿移交=$checkDraftMove，独立工作流=$checkWorkflowOnly",
    async ({
      withExecution,
      status,
      historyFails,
      unknownMemberOptions,
      checkQueuedFailure,
      checkDraftMove,
      checkWorkflowOnly,
    }) => {
      const creation = deferred<CreateChannelThreadResponse>();
      const history = deferred<ListChannelTimelineResponse>();
      const received = vi.fn();
      let historyRequests = 0;
      let activeWorkflows = false;
      let submissionId = "";
      const text = "团队首发连续可见";
      const thread = {
        id: "thread-1",
        leadChatId: "lead-chat",
        leadWakerId: "lead",
        title: text,
        workspaceDirectory: "/tmp/bot",
      };
      const members: CreateChannelThreadResponse["members"] = [
        {
          wakerId: "lead",
          chatId: "lead-chat",
          isLead: true,
          runtimeId: "codex",
          model: "gpt-5",
          permissionLevel: "full-access",
        },
        {
          wakerId: "member",
          chatId: "member-chat",
          isLead: false,
          runtimeId: "codex",
        },
      ];
      const execution: NonNullable<
        CreateChannelThreadResponse["leadExecution"]
      > = {
        id: "execution-1",
        userMessageId: "user-1",
        status: "running",
      };
      server.use(
        http.get(`${api}/channels/team-1`, () =>
          HttpResponse.json({
            channel: { id: "team-1", name: "研发团队", leadWakerId: "lead" },
            members: [
              {
                waker: { id: "lead", name: "团队负责人" },
                runtimeId: "codex",
                model: "gpt-5",
                permissionLevel: "full-access",
              },
              {
                waker: { id: "member", name: "开发成员" },
                runtimeId: "codex",
                model: unknownMemberOptions ? "unconfirmed" : "",
                permissionLevel: unknownMemberOptions ? "default" : undefined,
              },
            ],
          }),
        ),
        http.get(`${api}/wakers`, () =>
          HttpResponse.json({
            resources: [
              { id: "lead", name: "团队负责人" },
              { id: "member", name: "开发成员" },
            ],
          }),
        ),
        http.get(`${api}/runtimes`, () =>
          HttpResponse.json({
            resources: [
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
            ],
          }),
        ),
        http.get(`${api}/runtimes/codex/models`, ({ request }) =>
          HttpResponse.json({
            runtimeId: "codex",
            accountId: new URL(request.url).searchParams.get("accountId"),
            state: "fresh",
            defaultModel: "current-native-model",
            models: [
              {
                id: "current-native-model",
                name: "Current Native Model",
                permissionLevels: ["default", "auto", "full-access"],
              },
              ...(unknownMemberOptions
                ? [
                    {
                      id: "unconfirmed",
                      name: "Unconfirmed",
                      permissionLevels: ["default"],
                    },
                  ]
                : []),
            ],
          }),
        ),
        http.get(`${api}/wakers/lead/projects`, () =>
          HttpResponse.json({
            resources: [
              {
                id: "project",
                name: "项目",
                available: true,
                sources: [
                  { path: "/tmp/bot", available: true, isDefault: true },
                ],
              },
            ],
          }),
        ),
        http.get(`${api}/wakers/lead/recent-project`, () =>
          HttpResponse.json({ projectId: "project" }),
        ),
        http.get(`${api}/chats`, () =>
          HttpResponse.json({ resources: [], hasMore: false }),
        ),
        http.get(`${api}/wakers/lead/plugin-references`, () =>
          HttpResponse.json([]),
        ),
        http.get(`${api}/wakers/lead/skills`, () => HttpResponse.json([])),
        http.get(`${api}/wakers/lead/connectors`, () => HttpResponse.json([])),
        http.post(`${api}/channels/team-1/threads`, async ({ request }) => {
          const body = await request.json();
          if (
            body &&
            typeof body === "object" &&
            "clientRequestId" in body &&
            typeof body.clientRequestId === "string"
          )
            submissionId = body.clientRequestId;
          received(body);
          return HttpResponse.json(await creation.promise);
        }),
        http.get(`${api}/channels/team-1/threads/thread-1`, () =>
          HttpResponse.json({
            thread,
            members,
            tasks: [],
            activeWorkflows,
            leadProcessing: status === "running",
            leadActiveExecutionId:
              status === "running" ? execution.id : undefined,
          }),
        ),
        http.get(
          `${api}/channels/team-1/threads/thread-1/timeline`,
          async () => {
            const requestNumber = ++historyRequests;
            const snapshot = await history.promise;
            return historyFails && requestNumber === 1
              ? HttpResponse.json(
                  { message: "timeline unavailable" },
                  { status: 500 },
                )
              : HttpResponse.json(snapshot);
          },
        ),
        http.get(`${api}/chats/lead-chat`, () =>
          HttpResponse.json({
            chat: {
              id: "lead-chat",
              wakerId: "lead",
              workspaceDirectory: "/tmp/bot",
              activeExecutionId: withExecution ? execution.id : undefined,
            },
          }),
        ),
        http.get(`${api}/chats/lead-chat/messages`, () =>
          HttpResponse.json({ resources: [], hasMore: false }),
        ),
      );
      const queryClient = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      });
      const router = createMemoryRouter(
        [
          {
            element: (
              <Outlet context={{ setChatRightPanel: () => undefined }} />
            ),
            children: [
              {
                path: "/teams/:channelId",
                element: <TeamThreadPage />,
                children: [
                  { index: true, element: <></> },
                  { path: "threads/:threadId", element: <></> },
                ],
              },
              { path: "/other", element: <div>其他页面</div> },
            ],
          },
        ],
        { initialEntries: ["/teams/team-1?draftId=first-send"] },
      );
      const view = render(
        <StrictMode>
          <QueryClientProvider client={queryClient}>
            <TooltipProvider>
              <WorkspaceProvider>
                <WorkbenchProvider>
                  <ChatQueueProvider>
                    <AppSceneWorkspaceContextProvider
                      value={{
                        routeSceneId: "team-scene",
                        snapshot: {
                          activeTabId: "team-scene",
                          tabs: [
                            {
                              id: "team-scene",
                              label: "团队首发",
                              kind: "route",
                              lastUsed: 0,
                              mountKey: "team-scene",
                            },
                          ],
                        },
                        dispatch: () => {},
                        syncWorkspace: () => {},
                      }}
                    >
                      <RouterProvider router={router} />
                      {checkDraftMove || checkQueuedFailure ? (
                        <TeamTestTabs openMember={checkQueuedFailure} />
                      ) : null}
                    </AppSceneWorkspaceContextProvider>
                  </ChatQueueProvider>
                </WorkbenchProvider>
              </WorkspaceProvider>
            </TooltipProvider>
          </QueryClientProvider>
        </StrictMode>,
      );
      await waitFor(() =>
        expect(
          screen.getByTestId("composer-model-control"),
        ).toHaveAccessibleName(/gpt-5/),
      );
      await userEvent.setup().type(screen.getByRole("textbox"), text);
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled(),
      );
      fireEvent.click(screen.getByRole("button", { name: "发送消息" }));
      await waitFor(() => expect(received).toHaveBeenCalledOnce());
      expect(received).toHaveBeenCalledWith(
        expect.objectContaining({
          memberConfigs: expect.arrayContaining([
            expect.objectContaining({
              wakerId: "lead",
              model: "gpt-5",
            }),
            expect.objectContaining({
              wakerId: "member",
              model: unknownMemberOptions ? "unconfirmed" : "",
            }),
          ]),
        }),
      );
      expect(screen.getByText(text)).toBeVisible();
      const activeAvatar = () =>
        view.container.querySelector(
          '[data-chat-scroll-viewport] [data-role="assistant"] svg[data-companion-avatar]:not([data-activity="idle"])',
        );
      expect(activeAvatar()).toHaveAttribute("data-activity", "thinking");

      const frames: Array<{
        loading: boolean;
        message: boolean;
        running: boolean;
        composer: boolean;
      }> = [];
      const observe = () =>
        frames.push({
          loading: Boolean(
            view.container.querySelector('[role="status"][aria-busy="true"]'),
          ),
          message: Boolean(
            view.container
              .querySelector("[data-chat-scroll-viewport]")
              ?.textContent?.includes(text),
          ),
          running: Boolean(activeAvatar()),
          composer: Boolean(view.container.querySelector('[role="textbox"]')),
        });
      const observer = new MutationObserver(observe);
      observer.observe(view.container, {
        subtree: true,
        childList: true,
        attributes: true,
        characterData: true,
      });
      try {
        await act(async () =>
          creation.resolve({
            thread,
            members,
            tasks: [],
            replayed: !withExecution,
            leadExecution: withExecution ? execution : undefined,
          }),
        );
        await waitFor(() =>
          expect(router.state.location.pathname).toBe(
            "/teams/team-1/threads/thread-1",
          ),
        );
        expect(screen.getByText(text)).toBeVisible();
        expect(activeAvatar()).toBeVisible();
        expect(
          frames.filter(
            (frame) =>
              frame.loading ||
              !frame.message ||
              !frame.running ||
              !frame.composer,
          ),
        ).toEqual([]);
        await act(async () =>
          history.resolve({
            resources: [
              {
                chatId: "lead-chat",
                wakerId: "lead",
                coordinator: true,
                message: {
                  id: "user-1",
                  submissionId,
                  role: "user",
                  actor: "user",
                  content: text,
                  revision: 1,
                },
              },
              {
                chatId: "lead-chat",
                wakerId: "lead",
                coordinator: true,
                message: {
                  id: execution.id,
                  submissionId,
                  executionId: execution.id,
                  role: "assistant",
                  status,
                  revision: 1,
                  displayParts: [
                    { id: "reply", type: "text", text: "负责人的真实回复" },
                  ],
                },
              },
            ],
            hasMore: false,
          }),
        );
        if (historyFails) {
          const retry = await screen.findByRole("button", { name: "重试" });
          expect(retry).toBeVisible();
          expect(screen.getByText(text)).toBeVisible();
          fireEvent.click(retry);
        }
        expect(
          await screen.findByText("负责人的真实回复", {}, { timeout: 5_000 }),
        ).toBeVisible();
        expect(
          screen.queryByText("暂时无法载入团队对话。"),
        ).not.toBeInTheDocument();
        expect(received).toHaveBeenCalledOnce();
        expect(screen.getAllByText(text)).toHaveLength(1);
        observe();
        expect(frames.length).toBeGreaterThan(0);
        expect(
          frames.filter(
            (frame) => frame.loading || !frame.message || !frame.composer,
          ),
        ).toEqual([]);
        observer.disconnect();
        await act(async () => {
          await router.navigate("/other");
        });
        await act(async () => {
          await router.navigate("/teams/team-1/threads/thread-1");
        });
        expect(
          await screen.findByText("负责人的真实回复", {}, { timeout: 5_000 }),
        ).toBeVisible();
        expect(screen.getAllByText(text)).toHaveLength(1);
        if (status === "completed") {
          expect(activeAvatar()).toHaveAttribute("data-activity", "completed");
          expect(activeAvatar()).toHaveAttribute("data-animation", "notify");
        }
        if (checkQueuedFailure) await verifyQueuedLeadInputFeedback();
        if (checkDraftMove) await verifyWorkflowDraftMove();
        if (checkWorkflowOnly) {
          const stop = '[data-chat-primary-action="stop"]';
          const changeWorkflowStatus = (active: boolean) => {
            activeWorkflows = active;
            expect(stream.workflows.get("member-chat")).toBeDefined();
            act(() =>
              stream.workflows.get("member-chat")?.({
                topic: "workflow",
                type: "workflow.changed",
                resource: { kind: "chat", id: "member-chat" },
                epoch: "test-epoch",
                connectionId: "test-connection",
                protocolVersion: 1,
                deliverySequence: 1,
                eventId: "workflow-changed",
                payload: {},
              }),
            );
          };
          expect(view.container.querySelector(stop)).not.toBeInTheDocument();
          changeWorkflowStatus(true);
          await waitFor(() =>
            expect(view.container.querySelector(stop)).toBeVisible(),
          );
          expect(screen.getByText("团队仍在执行")).toBeVisible();
          changeWorkflowStatus(false);
          await waitFor(() =>
            expect(view.container.querySelector(stop)).not.toBeInTheDocument(),
          );
        }
      } finally {
        observer.disconnect();
        view.unmount();
        router.dispose();
        queryClient.clear();
      }
    },
  );
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: ((topic, resource, { onEvent }) => {
    if (topic !== "workflow" || !resource.id) return () => {};
    const id = resource.id;
    stream.workflows.set(id, onEvent);
    return () => {
      if (stream.workflows.get(id) === onEvent) stream.workflows.delete(id);
    };
  }) satisfies typeof subscribeGlobalEvents,
}));
