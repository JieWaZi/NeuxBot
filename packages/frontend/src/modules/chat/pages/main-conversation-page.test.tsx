import { createHash, webcrypto } from "node:crypto";
import userEvent from "@testing-library/user-event";
import {
  ChatComposerProviders,
  seedChatComposerResources,
} from "@/test/chat-composer-fixture";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { ApiError } from "@/shared/api/errors";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import type {
  GetMainConversationData,
  GetMainConversationResponse,
} from "@/shared/api/generated/types.gen";
import type * as GlobalEvents from "@/shared/api/global-events";
import type * as SharedPlatform from "@/shared/platform";
import "@/app/workspace-i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  mainConversationOptions,
  sourceHistoryOptions,
  sourceScopeOptions,
  type ConversationSubject,
} from "../api/main-conversation";
import { MainConversationPage } from "./main-conversation-page";

type HistoryRequest = Pick<GetMainConversationData, "path" | "query">;
type HistoryPage = Pick<
  GetMainConversationResponse,
  "messages" | "hasMore" | "nextBeforeSequence"
>;

const calls = vi.hoisted(() => ({
  removeMessage: vi.fn(),
  readState: vi.fn(),
  markUnread: vi.fn(),
  fork: vi.fn(),
  send: vi.fn(),
  sendSource: vi.fn(),
  approve: vi.fn(),
  ordinaryApprove: vi.fn(),
  activity: vi.fn(),
  ticketActivity: vi.fn(),
  subscribe: vi.fn<typeof GlobalEvents.subscribeGlobalEvents>(),
  answer: vi.fn(),
  source: vi.fn(),
  extra: vi.fn(),
  timeline: vi.fn(),
  history:
    vi.fn<
      (
        request: HistoryRequest,
      ) => Promise<{ data: GetMainConversationResponse }>
    >(),
  resolveFile: vi.fn(),
  selectFiles: vi.fn<typeof SharedPlatform.selectFiles>(),
}));
vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  listWorkTickets: async () => ({ data: [] }),
  listWorkTicketActivity: async () => ({ data: calls.ticketActivity() ?? [] }),
  getMainConversation: async (options: HistoryRequest) => {
    if (calls.history.getMockImplementation()) return calls.history(options);
    return {
      data: {
        id: "main",
        ...options.path,
        activeWork: calls.activity() ?? [],
        messages: [
          ...(calls.extra() ?? []),
          {
            id: "u-without-reply",
            sequence: 0,
            role: "user",
            content: "只交办，尚无公开回复",
            sourceChatId: "work-alone",
          },
          {
            id: "m1",
            sequence: 1,
            role: "assistant",
            content: "发现 A",
            sourceChatId: "work-a",
            sourceWakerId: "w",
          },
          {
            id: "m2",
            sequence: 2,
            role: "assistant",
            content: "发现 B",
            sourceChatId: "work-b",
            sourceWakerId: "w",
          },
        ],
        ...calls.timeline(options.query?.beforeSequence),
      },
    };
  },
  getMainConversationSource: calls.source,
  deleteMainConversationMessage: calls.removeMessage,
  getMainConversationReading: calls.readState,
  markMainConversationUnread: calls.markUnread,
  forkChat: calls.fork,
  sendMainConversationMessage: calls.send,
  sendMainConversationSourceMessage: calls.sendSource,
  resolveMainConversationSourceApproval: calls.approve,
  resolveApproval: calls.ordinaryApprove,
  resolveMainConversationSourceElicitation: calls.answer,
  getWaker: async () => ({ data: { id: "w", name: "伙伴 A" } }),
  getChannel: async () => ({
    data: { channel: { id: "w", leadWakerId: "w" }, members: [] },
  }),
  listProjects: async () => ({
    data: {
      resources: [
        { id: "A", name: "项目 A", available: true },
        { id: "B", name: "项目 B", available: true },
      ],
    },
  }),
}));
vi.mock("@/shared/api/global-events", async (original) => ({
  ...(await original<typeof GlobalEvents>()),
  subscribeGlobalEvents: calls.subscribe,
}));

vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof SharedPlatform>()),
  resolveWorkspaceResourceReference: calls.resolveFile,
  selectFiles: calls.selectFiles,
}));
vi.mock("../components/workspace-file-viewer", () => ({
  WorkspaceFileViewer: ({
    scopeChatId,
    rootPath,
    path,
  }: {
    scopeChatId: string;
    rootPath: string;
    path: string;
  }) => (
    <output>
      {scopeChatId} {rootPath} {path}
    </output>
  ),
}));
const originalScroll = HTMLElement.prototype.scrollTo;
beforeAll(() => {
  vi.stubGlobal("crypto", webcrypto);
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
afterAll(() => {
  vi.unstubAllGlobals();
  HTMLElement.prototype.scrollTo = originalScroll;
});
beforeEach(() => {
  calls.removeMessage.mockReset();
  calls.readState.mockReset();
  calls.markUnread.mockReset();
  calls.fork.mockReset();
  calls.extra.mockReset();
  calls.timeline.mockReset();
  calls.history.mockReset();
  calls.source.mockReset();
  calls.send.mockReset();
  calls.sendSource.mockReset();
  calls.approve.mockReset();
  calls.ordinaryApprove.mockReset();
  calls.activity.mockReset();
  calls.ticketActivity.mockReset();
  calls.subscribe.mockReset().mockReturnValue(() => {});
  calls.answer.mockReset();
  calls.selectFiles.mockReset().mockResolvedValue([]);
});
function show(
  source?: string,
  search = "",
  kind: ConversationSubject["subjectType"] = "waker",
) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
  seedChatComposerResources(client, "w", "伙伴 A");
  const mainHref = `/chat/main/${kind}/w`;
  const router = createMemoryRouter(
    [
      {
        path: "/chat/main/:subjectType/:subjectId",
        element: <MainConversationPage />,
      },
    ],
    {
      initialEntries: source
        ? [mainHref, mainHref + "?source=" + source]
        : [mainHref + search],
    },
  );
  const result = render(
    <QueryClientProvider client={client}>
      <ChatComposerProviders>
        <WorkspaceProvider>
          <TooltipProvider>
            <RouterProvider router={router} />
          </TooltipProvider>
        </WorkspaceProvider>
      </ChatComposerProviders>
    </QueryClientProvider>,
  );
  return {
    ...result,
    client,
    router,
    back: () =>
      act(async () => {
        await router.navigate(-1);
      }),
  };
}
function mainConversationChanged(
  sequence: number,
  kind: ConversationSubject["subjectType"] = "waker",
) {
  const resource = { kind, id: "w" };
  const listener = calls.subscribe.mock.calls.find(
    ([topic, subject]) =>
      topic === "main-conversations" &&
      subject.kind === resource.kind &&
      subject.id === resource.id,
  )?.[2];
  if (!listener) throw new Error("主会话没有订阅伙伴的公开状态");
  act(() =>
    listener.onEvent({
      protocolVersion: 1,
      epoch: "epoch",
      connectionId: "connection",
      deliverySequence: sequence,
      topic: "main-conversations",
      type: "main-conversations.changed",
      resource,
      eventId: String(sequence),
      payload: {},
    }),
  );
}
describe("公开主会话", () => {
  it.each(["waker", "team"] as const)(
    "在%s的发起消息下只展示由此发出的子单",
    async (kind) => {
      const input = turnInput("请让伙伴核对发布说明", 1, "root-work");
      calls.timeline.mockReturnValue({ messages: [input] });
      const child = {
        ticketId: "child",
        parentTicketId: "parent",
        sourceMessageId: input.id,
        title: "独立执行的委派",
        state: "completed",
        workChatId: "child-work",
        workSubjectType: "waker",
        workSubjectId: "receiver",
        received: false,
      };
      calls.ticketActivity.mockReturnValue([
        child,
        {
          ...child,
          ticketId: "received",
          title: "不可插入主会话的受托工作",
          received: true,
        },
      ]);
      show(undefined, "", kind);
      expect(
        await screen.findByRole("link", { name: /独立执行的委派/ }),
      ).toHaveAttribute("href", "/chat/main/waker/receiver?source=child-work");
      expect(screen.queryByText("不可插入主会话的受托工作")).toBeNull();
    },
  );

  it("伙伴菜单从对应工作回合分支，并按公开消息标记未读", async () => {
    const user = userEvent.setup();
    const input = turnInput("检查故障", 1, "test-work");
    const result = {
      ...turnReply("已定位故障", 2, input.id, "test-work"),
      forkExecutionId: "source-execution",
      readable: true,
    };
    calls.timeline.mockReturnValue({ messages: [input, result] });
    calls.readState.mockResolvedValue({
      data: {
        complete: true,
        conversationId: "main",
        unreadGeneration: 4,
        throughSequence: 0,
        holdUnread: false,
      },
    });
    calls.markUnread.mockResolvedValue({ data: {} });
    calls.fork.mockResolvedValue({ data: {} });
    show();
    await screen.findByText(result.content);
    const trigger = within(timelineRow(result.id)).getByRole("button", {
      name: "更多操作",
    });
    await user.click(trigger);
    await user.click(
      await screen.findByRole("menuitem", { name: "从此处标为未读" }),
    );
    await waitFor(() => {
      expect(calls.markUnread).toHaveBeenCalledWith(
        expect.objectContaining({
          path: { subjectType: "waker", subjectId: "w" },
          body: { fromMessageID: result.id, observedUnreadGeneration: 4 },
        }),
      );
    });
    await user.click(trigger);
    await user.click(
      await screen.findByRole("menuitem", { name: "从这里分支" }),
    );
    expect(calls.fork).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { chatId: "test-work" },
        body: {
          executionId: "source-execution",
          clientRequestId: expect.any(String),
        },
      }),
    );
  });
  it("伙伴发言的菜单可以删除公开消息，已有交办仍留在主会话", async () => {
    const user = userEvent.setup();
    const input = turnInput("交办原话", 1);
    const result = turnReply("需要移除的结果", 2, input.id);
    calls.timeline.mockReturnValue({ messages: [input, result] });
    calls.removeMessage.mockImplementationOnce(async () => {
      calls.timeline.mockReturnValue({ messages: [input] });
      return { data: { success: true } };
    });
    show();
    await screen.findByText(result.content);
    expect(
      within(timelineRow(result.id)).getByRole("button", { name: "回复" }),
    ).toBeVisible();
    await user.click(
      within(timelineRow(result.id)).getByRole("button", { name: "更多操作" }),
    );
    const menu = screen.getByRole("menu");
    expect(within(menu).queryByRole("menuitem", { name: "回复" })).toBeNull();
    expect(
      within(menu).getByRole("menuitem", { name: "复制消息" }),
    ).toBeVisible();
    await user.click(within(menu).getByRole("menuitem", { name: "删除" }));
    const confirmation = screen.getByRole("dialog", { name: "删除消息？" });
    expect(calls.removeMessage).not.toHaveBeenCalled();
    await user.click(
      within(confirmation).getByRole("button", { name: "删除" }),
    );
    await waitFor(() => expect(screen.queryByText(result.content)).toBeNull());
    expect(screen.getByText(input.content)).toBeInTheDocument();
    expect(calls.removeMessage).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { subjectType: "waker", subjectId: "w", messageId: result.id },
      }),
    );
  });
  it("交办已有公开答复时保留复制和回复，通过伙伴头像进入工作记录", async () => {
    calls.extra.mockReturnValue([
      {
        id: "my-message",
        sequence: 4,
        role: "user",
        content: "哈哈哈",
        sourceChatId: "work-alone",
        sourceWakerId: "w",
      },
      turnReply("公开结果", 5, "my-message", "work-alone"),
      turnInput("另一个项目没有公开回复", 6, "other-work"),
    ]);
    show();
    const text = await screen.findByText("哈哈哈");
    const row = text.closest<HTMLElement>("[data-message-id]");
    if (!row) throw Error("缺少用户消息");
    expect(
      within(row).getByRole("button", { name: "回复" }),
    ).toBeInTheDocument();
    const menu = openMessageMenu(row);
    expect(
      within(menu).getByRole("menuitem", { name: "复制消息" }),
    ).toBeVisible();
    expect(
      within(menu).queryByRole("menuitem", { name: "打开工作会话" }),
    ).toBeNull();
    fireEvent.keyDown(menu, { key: "Escape" });
    expect(
      within(timelineRow("公开结果")).getByRole("button", {
        name: "打开工作会话",
      }),
    ).toBeVisible();
    expect(
      within(openMessageMenu(timelineRow("另一个项目没有公开回复"))).getByRole(
        "menuitem",
        {
          name: "打开工作会话",
        },
      ),
    ).toBeVisible();
  });
  it("冷读只有交办的旧主会话时，可从该气泡打开原工作记录", async () => {
    const user = userEvent.setup();
    const workChatId = "silent-work";
    const input = turnInput("请检查临时目录", 1, workChatId);
    calls.timeline.mockReturnValue({ messages: [input] });
    calls.source.mockResolvedValue({
      data: {
        chat: { id: workChatId, wakerId: "w" },
        resources: [
          {
            id: "private-final",
            role: "assistant",
            status: "completed",
            displayParts: [{ type: "text", text: "临时目录已检查" }],
          },
        ],
      },
    });

    show();
    await screen.findByText(input.content);
    const row = timelineRow(input.id);
    const action = within(row).getByRole("button", {
      name: "更多操作",
    });
    expect(action).toHaveTextContent(/^$/);
    expect(action.querySelector("svg")).toBeInTheDocument();
    const messageColumn = row.querySelector(
      "[data-chat-message-bubble]",
    )?.parentElement;
    const actions = messageColumn?.nextElementSibling;
    expect(messageColumn).toHaveClass("min-w-0", "max-w-[78%]");
    expect(actions).toHaveClass(
      "order-first",
      "opacity-0",
      "group-hover/user-message:opacity-100",
      "group-focus-within/user-message:opacity-100",
      "[@media(hover:none)]:opacity-100",
    );
    expect(actions).toContainElement(action);
    expect(actions).toContainElement(
      within(row).getByRole("button", { name: "回复" }),
    );
    expect(screen.queryByText("临时目录已检查")).toBeNull();
    expect(calls.source).not.toHaveBeenCalled();

    action.focus();
    expect(action).toHaveFocus();
    await user.keyboard("{Enter}");
    const openWork = await screen.findByRole("menuitem", {
      name: "打开工作会话",
    });
    fireEvent.keyDown(openWork, { key: "Home" });
    expect(openWork).toHaveFocus();
    expect(screen.getByRole("menuitem", { name: "复制消息" })).toBeVisible();
    await user.keyboard("{Enter}");
    expect(await screen.findByText("临时目录已检查")).toBeVisible();
    expect(calls.source.mock.calls[0]?.[0]).toMatchObject({
      path: { ...workPath, workChatId },
    });
  });
  it("同一来源的未答复交办和失败交办均可从消息菜单打开工作记录", async () => {
    const earlier = turnInput("上次交办", 1);
    const replied = turnReply("上次公开答复", 2, earlier.id);
    const failed = {
      ...turnInput("失败的旧交办", 3),
      sourceWakerId: "w",
      failureCode: "RuntimeQuotaExhausted",
    };
    const latest = turnInput("这次只有交办", 4);
    calls.timeline.mockReturnValue({
      messages: [earlier, replied, failed, latest],
    });
    show();

    await screen.findByText(latest.content);
    for (const input of [latest, failed]) {
      const menu = openMessageMenu(timelineRow(input.id));
      expect(
        within(menu).getByRole("menuitem", {
          name: "打开工作会话",
        }),
      ).toBeVisible();
      fireEvent.keyDown(menu, { key: "Escape" });
    }
    const earlierMenu = openMessageMenu(timelineRow(earlier.id));
    expect(
      within(earlierMenu).queryByRole("menuitem", { name: "打开工作会话" }),
    ).toBeNull();
    fireEvent.keyDown(earlierMenu, { key: "Escape" });
    expect(
      screen.getAllByRole("button", { name: "打开工作会话" }),
    ).toHaveLength(1);
  });
  it("首次公开发言前失败在输入框显示，刷新后可从原消息菜单进入原工作续聊", async () => {
    const user = userEvent.setup();
    const workChatId = "failed-work";
    const input = turnInput("检查执行环境", 1, workChatId);
    calls.timeline.mockReturnValue({ messages: [input] });
    calls.activity.mockReturnValue([
      {
        workChatId,
        sourceWakerId: "w",
        executionId: "first-run",
        inputMessageId: input.id,
        hasPublicReply: false,
      },
    ]);
    const view = show();
    expect(await screen.findByText(input.content)).toBeVisible();
    expect(screen.getByText("正在等待回复")).toBeVisible();
    const menu = openMessageMenu(timelineRow(input.id));
    expect(
      within(menu).getByRole("menuitem", {
        name: "打开工作会话",
      }),
    ).toBeVisible();
    fireEvent.keyDown(menu, { key: "Escape" });

    calls.activity.mockReturnValue([]);
    calls.timeline.mockReturnValue({
      messages: [
        { ...input, sourceWakerId: "w", failureCode: "RuntimeQuotaExhausted" },
      ],
    });
    mainConversationChanged(1);
    const reason = "当前额度已用尽，请切换模型或等待额度恢复后重试";
    const error = await screen.findByRole("alert");
    expect(error).toHaveTextContent(reason);
    expect(error.closest("[data-chat-composer-frame]")).toBeInTheDocument();
    expect(screen.queryByText("正在等待回复")).toBeNull();

    view.unmount();
    const { container } = show();
    expect(await screen.findByRole("alert")).toHaveTextContent(reason);
    expect(screen.getAllByText(reason)).toHaveLength(1);
    expect(container.querySelectorAll("[data-message-id]")).toHaveLength(1);
    calls.source.mockResolvedValue({
      data: {
        chat: { ...work, id: workChatId, lastEndedExecutionStatus: "failed" },
        resources: [],
      },
    });
    const failedMenu = openMessageMenu(timelineRow(input.id));
    await user.click(
      within(failedMenu).getByRole("menuitem", { name: "打开工作会话" }),
    );
    const editor = await screen.findByRole("textbox", { name: "输入消息" });
    await user.type(editor, "继续这次检查");
    calls.sendSource.mockResolvedValue({ data: { executionId: "next-run" } });
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() =>
      expect(calls.sendSource.mock.lastCall?.[0]).toMatchObject({
        path: { ...workPath, workChatId },
        body: { text: "继续这次检查" },
      }),
    );
    expect(calls.send).not.toHaveBeenCalled();
  });
  it("新交办不绑定当前目录，公开结果文件仍按消息的来源目录打开", async () => {
    const user = userEvent.setup();
    calls.send.mockClear().mockResolvedValue({
      data: {
        message: { id: "pub", sourceChatId: "source-a" },
        executionId: "run-pub",
      },
    });
    calls.extra.mockReturnValue([
      {
        id: "pub-file",
        sequence: 3,
        role: "assistant",
        content: "[结果](./report.md)",
        sourceChatId: "source-a",
        sourceWakerId: "w",
      },
    ]);
    calls.source.mockResolvedValue({
      data: {
        chat: { id: "source-a", workspaceDirectory: "/project/a" },
        resources: [],
      },
    });
    calls.resolveFile.mockResolvedValue({
      status: "resolved",
      canonicalPath: "/project/a/report.md",
      workspaceRoot: "/project/a",
      sourceRoots: [],
    });
    show();
    const result = await screen.findByRole("link", { name: "结果" });
    expect(
      await screen.findByRole("button", { name: "选择模型 · GPT-5" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "添加附件" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "执行权限：默认权限" }),
    ).toBeEnabled();
    expect(screen.queryByTestId("composer-workspace-control")).toBeNull();
    await user.type(
      screen.getByRole("textbox", { name: "输入消息" }),
      "检查同名文件",
    );
    const sendButton = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(sendButton).toBeEnabled());
    await user.click(sendButton);
    await waitFor(() => expect(calls.send).toHaveBeenCalledOnce());
    const body = calls.send.mock.calls.at(-1)?.[0].body;
    expect(body).toMatchObject({ message: { text: "检查同名文件" } });
    expect(body).not.toHaveProperty("projectId");
    expect(body).not.toHaveProperty("workingDirectory");
    fireEvent.click(result);
    await waitFor(() =>
      expect(calls.resolveFile).toHaveBeenCalledWith(
        "source-a",
        "./report.md",
        "/project/a",
      ),
    );
    expect(await screen.findByText(/source-a \/project\/a/)).toBeVisible();
    calls.extra.mockReset();
  });
  it("公开上传原件与同名项目文件分别按各自真实目录在右侧打开", async () => {
    const user = userEvent.setup();
    const path = "attachments/sent/capture.png";
    const projectRoot = "/project/a";
    const attachmentRoot = "/private/work-a/attachments";
    calls.extra.mockReturnValue([
      {
        id: "uploaded",
        sequence: 3,
        role: "user",
        content: "请核对附件\n\ncapture.png",
        inputContent: "请核对附件",
        attachments: [{ name: "capture.png", path, managed: true }],
        sourceChatId: "work-a",
      },
      {
        id: "project-file",
        sequence: 4,
        role: "assistant",
        content: `另有 [同名项目文件](./${path}) 可以比较。`,
        sourceChatId: "work-a",
        sourceWakerId: "w",
      },
    ]);
    calls.source.mockResolvedValue({
      data: {
        chat: { id: "work-a", workspaceDirectory: projectRoot },
        resources: [],
      },
    });
    calls.resolveFile.mockImplementation(
      async (_chatId, _path, baseDirectory) =>
        baseDirectory === ""
          ? {
              status: "resolved",
              canonicalPath: `${attachmentRoot}/sent/capture.png`,
              workspaceRoot: attachmentRoot,
              sourceRoots: [attachmentRoot],
            }
          : {
              status: "resolved",
              canonicalPath: `${projectRoot}/${path}`,
              workspaceRoot: projectRoot,
              sourceRoots: [attachmentRoot],
            },
    );
    show();
    await user.click(
      await screen.findByRole("button", { name: "capture.png" }),
    );
    expect(calls.resolveFile).toHaveBeenCalledWith("work-a", path, "");
    expect(
      await screen.findByText(`work-a ${attachmentRoot} sent/capture.png`),
    ).toBeVisible();
    expect(screen.getByText("请核对附件")).toBeVisible();
    const projectLink = screen.getByRole("link", { name: "同名项目文件" });
    await user.click(projectLink);
    expect(calls.resolveFile).toHaveBeenCalledWith(
      "work-a",
      `./${path}`,
      projectRoot,
    );
    expect(
      await screen.findByText(`work-a ${projectRoot} ${path}`),
    ).toBeVisible();
  });
  it.each(["主会话", "来源记录"])(
    "%s 输入框直接发送选中的图片附件",
    async (view) => {
      const user = userEvent.setup();
      const path = "/tmp/main-conversation-capture.png";
      calls.selectFiles.mockResolvedValue([path]);
      calls.send.mockResolvedValue({ data: { executionId: "new-execution" } });
      calls.sendSource.mockResolvedValue({
        data: { executionId: "source-execution" },
      });
      calls.source.mockResolvedValue({
        data: {
          chat: {
            id: "work-alone",
            workspaceDirectory: "/project/a",
            wakerId: "w",
          },
          resources: [],
        },
      });
      const source = view === "来源记录";
      show(source ? "work-alone" : undefined);
      const attachment = await screen.findByRole("button", {
        name: "添加附件",
      });
      await user.click(attachment);
      expect(
        await screen.findByText("main-conversation-capture.png"),
      ).toBeVisible();
      const send = screen.getByRole("button", { name: "发送消息" });
      await waitFor(() => expect(send).toBeEnabled());
      await user.click(send);
      const request = source ? calls.sendSource : calls.send;
      await waitFor(() => expect(request).toHaveBeenCalledOnce());
      const body = request.mock.calls[0]?.[0].body;
      expect(source ? body : body.message).toMatchObject({
        attachmentOnly: true,
        attachments: [{ sourcePath: path }],
      });
    },
  );
  it.each([
    { view: "主会话", code: "RuntimeSessionStartFailed", reply: "发现 A" },
    {
      view: "来源记录",
      code: "RuntimeSessionRestoreFailed",
      reply: "来源答复",
    },
  ])(
    "$view 会话准备失败时可从原样重试返回编辑",
    async ({ view, code, reply }) => {
      const user = userEvent.setup();
      const source = view === "来源记录";
      const path = "/tmp/recover-composer.png";
      calls.selectFiles.mockResolvedValue([path]);
      calls.source.mockImplementation(async ({ path }) => ({
        data: {
          chat: { id: path.workChatId, wakerId: "w" },
          resources: [
            {
              id: "source-answer",
              role: "assistant",
              status: "completed",
              createdSequence: 1,
              displayParts: [{ type: "text", text: "来源答复" }],
            },
          ],
        },
      }));
      const request = source ? calls.sendSource : calls.send;
      const failure = new ApiError("private diagnostic", code, 500);
      request
        .mockRejectedValueOnce(failure)
        .mockRejectedValueOnce(failure)
        .mockResolvedValue({ data: { executionId: "updated-attempt" } });
      show(source ? "work-alone" : undefined);
      const row = (await screen.findByText(reply)).closest("[data-message-id]");
      if (!(row instanceof HTMLElement)) throw Error("缺少可回复的消息");
      await user.click(within(row).getByRole("button", { name: "回复" }));
      await user.click(screen.getByRole("button", { name: "添加附件" }));
      expect(await screen.findByText("recover-composer.png")).toBeVisible();
      const editor = screen.getByRole("textbox", { name: "输入消息" });
      await user.type(editor, "继续核对这张图");
      const send = screen.getByRole("button", { name: "发送消息" });
      await waitFor(() => expect(send).toBeEnabled());
      await user.click(send);

      const edit = await screen.findByRole("button", { name: "返回编辑" });
      await user.click(screen.getByRole("button", { name: "重试原发送" }));
      await waitFor(() => expect(request).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(edit).toBeEnabled());
      const original = request.mock.calls[0]?.[0].body;
      expect(request.mock.calls[1]?.[0].body).toEqual(original);
      await user.click(edit);

      await waitFor(() => expect(editor).toHaveTextContent("继续核对这张图"));
      expect(editor).toHaveAttribute("contenteditable", "true");
      expect(await screen.findByText("recover-composer.png")).toBeVisible();
      expect(screen.getByRole("button", { name: "移除回复" })).toBeVisible();
      expect(screen.queryByRole("button", { name: "重试原发送" })).toBeNull();
      const permission = await screen.findByRole("button", {
        name: "执行权限：默认权限",
      });
      await user.click(permission);
      await user.click(screen.getByRole("menuitemradio", { name: /完全访问/ }));
      await user.type(editor, "并记录结果");
      await waitFor(() => expect(send).toBeEnabled());
      await user.click(send);

      await waitFor(() => expect(request).toHaveBeenCalledTimes(3));
      const updated = request.mock.calls[2]?.[0].body;
      expect(updated.submissionId).not.toBe(original.submissionId);
      expect(updated.replyToMessageId).toBe(source ? "source-answer" : "m1");
      expect(source ? updated : updated.message).toMatchObject({
        text: expect.stringContaining("并记录结果"),
        permissionLevel: "full-access",
        attachments: [{ sourcePath: path }],
      });
    },
  );
  it("从工作记录的地址读取原交办，呈现该工作本次失败", async () => {
    calls.source.mockResolvedValue({
      data: {
        chat: {
          id: "work-alone",
          wakerId: "w",
          lastEndedExecutionStatus: "failed",
        },
        resources: [
          {
            id: "input",
            role: "user",
            content: "先检查仓库",
            createdSequence: 1,
          },
          {
            id: "failed",
            role: "assistant",
            status: "failed",
            createdSequence: 2,
            errorMessage: "实际工作失败",
            errorMessageSource: "public",
          },
        ],
      },
    });
    const { back } = show("work-alone");
    expect(await screen.findByRole("alert")).toHaveTextContent("实际工作失败");
    expect(calls.source.mock.calls.at(-1)?.[0].path.workChatId).toBe(
      "work-alone",
    );
    await back();
    expect(screen.queryByText("实际工作失败")).toBeNull();
  });
  it("在来源工作记录中继续使用聊天输入，沿原工作范围发送", async () => {
    const user = userEvent.setup();
    calls.source.mockResolvedValue({
      data: {
        chat: {
          id: "work-alone",
          wakerId: "w",
          workspaceDirectory: "/project/a",
        },
        resources: [],
      },
    });
    let acknowledge = () => {};
    const receipt = new Promise<void>((resolve) => {
      acknowledge = resolve;
    });
    calls.sendSource.mockImplementation(async () => {
      await receipt;
      return { data: { execution: { id: "source-run" } } };
    });
    const { back } = show("work-alone");
    const editor = await screen.findByRole("textbox", { name: "输入消息" });
    expect(editor).toHaveAttribute("contenteditable", "true");
    await user.type(editor, "只跟进这次工作");
    const send = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(send).toBeEnabled());
    await user.click(send);
    await waitFor(() => expect(calls.sendSource).toHaveBeenCalledTimes(1));
    const request = calls.sendSource.mock.calls[0]?.[0];
    expect(request).toMatchObject({
      path: { subjectType: "waker", subjectId: "w", workChatId: "work-alone" },
      body: {
        text: "只跟进这次工作",
        runtimeId: "codex",
        model: "gpt-5",
        submissionId: expect.any(String),
      },
    });
    expect(request.body).not.toHaveProperty("workingDirectory");
    expect(request.body).not.toHaveProperty("projectId");
    expect(calls.send).not.toHaveBeenCalled();
    await back();
    expect(
      await screen.findByText("只跟进这次工作", { selector: "p" }),
    ).toBeVisible();
    expect(screen.queryByText("正在等待回复")).toBeNull();
    const publicInputId = createHash("sha256")
      .update(JSON.stringify(["user", "main", request.body.submissionId]))
      .digest("hex")
      .slice(0, 32);
    calls.extra.mockReturnValue([
      {
        id: publicInputId,
        sequence: 3,
        role: "user",
        content: "只跟进这次工作",
        sourceChatId: "work-alone",
      },
      {
        id: "answer",
        sequence: 4,
        role: "assistant",
        content: "这是这次工作的答复",
        sourceChatId: "work-alone",
        replyToMessageId: publicInputId,
      },
    ]);
    await act(async () => {
      acknowledge();
    });
    await waitFor(() => expect(screen.queryByText("正在等待回复")).toBeNull());
    expect(
      await screen.findByText("只跟进这次工作", { selector: "p" }),
    ).toBeVisible();
    expect(await screen.findByText("这是这次工作的答复")).toBeVisible();
  });
  it("回复选中的发言，来源查看保留草稿，回执不明重试原请求", async () => {
    calls.source.mockResolvedValue({
      data: {
        chat: {
          id: "work-a",
          wakerId: "w",
          projectName: "项目 A",
          workspaceDirectory: "/work/a",
        },
        resources: [
          { id: "u1", role: "user", content: "第一轮交办", createdSequence: 1 },
          { id: "u2", role: "user", content: "第二轮交办", createdSequence: 2 },
        ],
      },
    });
    calls.send
      .mockRejectedValueOnce(new TypeError("connection lost"))
      .mockResolvedValueOnce({
        data: {
          message: { id: "u", sourceChatId: "work-b" },
          executionId: "run-u",
        },
      });
    const { back } = show();
    const first = (await screen.findByText("发现 A")).closest(
      "[data-message-id]",
    );
    const second = (await screen.findByText("发现 B")).closest(
      "[data-message-id]",
    );
    if (!(first instanceof HTMLElement) || !(second instanceof HTMLElement))
      throw Error("missing public rows");
    const viewport = first.closest<HTMLElement>("[data-chat-scroll-viewport]");
    if (!viewport) throw Error("missing conversation viewport");
    expect(viewport).toHaveClass("[overflow-anchor:none]");
    Object.defineProperties(viewport, {
      scrollHeight: { value: 1200 },
      clientHeight: { value: 300 },
    });
    viewport.scrollTop = 146;
    fireEvent.scroll(viewport);
    fireEvent.click(within(second).getByRole("button", { name: "回复" }));
    await userEvent
      .setup()
      .type(screen.getByRole("textbox", { name: "输入消息" }), "继续查 B");
    fireEvent.click(
      within(first).getByRole("button", { name: "打开工作会话" }),
    );
    expect(await screen.findByText("第一轮交办")).toBeVisible();
    expect(await screen.findByText("第二轮交办")).toBeVisible();
    expect(calls.source.mock.calls.at(-1)?.[0].path.workChatId).toBe("work-a");
    await back();
    expect(screen.getByRole("textbox")).toHaveValue("继续查 B");
    expect(screen.queryByTestId("composer-workspace-control")).toBeNull();
    expect(first.closest("[data-chat-scroll-viewport]")).toBe(viewport);
    expect(viewport.scrollTop).toBe(146);
    const sendButton = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(sendButton).toBeEnabled());
    await userEvent.setup().click(sendButton);
    await waitFor(() => expect(calls.send).toHaveBeenCalledOnce());
    fireEvent.click(await screen.findByRole("button", { name: "重试原发送" }));
    await waitFor(() => expect(calls.send).toHaveBeenCalledTimes(2));
    const firstRequest = calls.send.mock.calls[0]?.[0].body;
    expect(firstRequest).toMatchObject({
      replyToMessageId: "m2",
      message: { text: "继续查 B", runtimeId: "codex", model: "gpt-5" },
    });
    expect(firstRequest).not.toHaveProperty("projectId");
    expect(firstRequest.submissionId).toBeTruthy();
    expect(calls.send.mock.calls[1]?.[0].body).toEqual(firstRequest);
  });
});

const workPath = {
  subjectType: "waker",
  subjectId: "w",
  workChatId: "work-alone",
} as const;
const work = {
  id: workPath.workChatId,
  wakerId: "w",
  workspaceDirectory: "/project/a",
};
const approval = {
  approvalId: "native-tool-request",
  title: "",
  kind: "execute",
  options: [
    { kind: "allow_once", id: "allow", name: "Allow" },
    { kind: "reject_once", id: "reject", name: "Reject" },
  ],
};
async function openAssignment() {
  const user = userEvent.setup();
  const { client } = show("work-alone");
  return { user, client };
}

it.each([
  { choice: /^允许本次/, optionId: "allow" },
  { choice: /^拒绝/, optionId: "reject" },
])(
  "在来源消息区处理无标题审批并随权威回执恢复输入：$optionId",
  async ({ choice, optionId }) => {
    const waiting = {
      data: {
        chat: {
          ...work,
          activeExecutionId: "exec",
          pendingApprovals: [approval],
        },
        resources: [],
      },
    };
    calls.source.mockResolvedValue(waiting);
    let acknowledge = () => {};
    const receipt = new Promise<void>((resolve) => {
      acknowledge = resolve;
    });
    calls.approve.mockImplementation(async () => {
      await receipt;
      return { data: {} };
    });
    const { user, client } = await openAssignment();
    const panel = await screen.findByRole("region", { name: "等待工具审批" });
    expect(panel.closest("[data-chat-scroll-viewport]")).not.toBeNull();
    expect(
      screen.getAllByRole("region", { name: "等待工具审批" }),
    ).toHaveLength(1);
    expect(screen.queryByText("正在执行")).toBeNull();
    expect(within(panel).getByText("允许执行此工具操作吗？")).toBeVisible();
    const button = within(panel).getByRole("button", { name: choice });
    await user.click(button);
    expect(calls.approve).toHaveBeenCalledOnce();
    expect(calls.approve.mock.calls[0]?.[0]).toMatchObject({
      path: { ...workPath, approvalId: "native-tool-request" },
      body: { optionId },
    });
    expect(button).toBeDisabled();
    expect(await within(panel).findByText("正在提交")).toBeVisible();
    // 最初一次落库回读仍含请求，此时不能二次操作。
    await act(async () => {
      acknowledge();
    });
    expect(await within(panel).findByText("结果确认中")).toBeVisible();
    expect(button).toBeDisabled();
    calls.source.mockResolvedValue({ data: { chat: work, resources: [] } });
    await act(async () => {
      await client.invalidateQueries({
        queryKey: sourceHistoryOptions(
          { subjectType: "waker", subjectId: "w" },
          workPath.workChatId,
        ).queryKey,
      });
    });
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "等待工具审批" })).toBeNull(),
    );
    await user.type(
      screen.getByRole("textbox", { name: "输入消息" }),
      "继续检查",
    );
    calls.sendSource.mockResolvedValue({ data: { executionId: "next" } });
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    await waitFor(() => expect(calls.sendSource).toHaveBeenCalledOnce());
    expect(calls.sendSource.mock.calls[0]?.[0]).toMatchObject({
      path: workPath,
      body: { text: "继续检查" },
    });
    expect(calls.send).not.toHaveBeenCalled();
  },
);

it("来源结构化提问在消息区提交答案，权威工作返回后继续聊天", async () => {
  const question = {
    elicitationId: "native-question",
    message: "需要目标目录",
    mode: "form",
    requestedSchema: {
      type: "object",
      properties: { target: { type: "string", title: "目标目录" } },
      required: ["target"],
    },
  };
  calls.source.mockResolvedValue({
    data: {
      chat: {
        ...work,
        activeExecutionId: "exec",
        pendingElicitations: [question],
      },
      resources: [
        {
          id: "before-question",
          role: "user",
          content: "先检查目录",
          createdSequence: 2,
        },
        {
          id: question.elicitationId,
          role: "assistant",
          executionId: "exec",
          question,
          createdSequence: 3,
        },
        {
          id: "after-question",
          role: "user",
          content: "后来已追加的消息",
          createdSequence: 4,
        },
      ],
    },
  });
  calls.answer.mockImplementation(async () => {
    calls.source.mockResolvedValue({ data: { chat: work, resources: [] } });
    return { data: {} };
  });
  const { user } = await openAssignment();
  const field = await screen.findByRole("textbox", { name: "目标目录" });
  expect(field.closest("[data-chat-scroll-viewport]")).not.toBeNull();
  expect(screen.getAllByRole("textbox", { name: "目标目录" })).toHaveLength(1);
  expect(screen.getByText("先检查目录").compareDocumentPosition(field)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(
    field.compareDocumentPosition(screen.getByText("后来已追加的消息")),
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  await user.type(field, "docs");
  await user.keyboard("{Enter}");
  await waitFor(() => expect(calls.answer).toHaveBeenCalledOnce());
  expect(calls.answer.mock.calls[0]?.[0]).toMatchObject({
    path: { ...workPath, elicitationId: "native-question" },
    body: { content: { target: "docs" }, declined: false },
  });
  await waitFor(() =>
    expect(screen.queryByRole("textbox", { name: "目标目录" })).toBeNull(),
  );
  await user.type(
    screen.getByRole("textbox", { name: "输入消息" }),
    "沿原会话继续",
  );
  calls.sendSource.mockResolvedValue({ data: { executionId: "next" } });
  await user.click(screen.getByRole("button", { name: "发送消息" }));
  await waitFor(() => expect(calls.sendSource).toHaveBeenCalledOnce());
  expect(calls.sendSource.mock.calls[0]?.[0]).toMatchObject({
    path: workPath,
    body: { text: "沿原会话继续" },
  });
  expect(calls.send).not.toHaveBeenCalled();
});

it("加载提问所在的工作历史页时迁移卡片并保留已输入的答案", async () => {
  const question = {
    elicitationId: "question-on-earlier-page",
    mode: "form",
    requestedSchema: {
      properties: { target: { type: "string", title: "补充目录" } },
    },
  };
  const chat = {
    ...work,
    activeExecutionId: "exec",
    pendingElicitations: [question],
  };
  calls.source.mockImplementation(async ({ query }) => ({
    data:
      query?.beforeSequence === 9
        ? {
            chat,
            resources: [
              {
                id: "older-message",
                role: "user",
                content: "较早的交办",
                createdSequence: 7,
              },
              {
                id: question.elicitationId,
                role: "assistant",
                question,
                createdSequence: 8,
              },
            ],
          }
        : {
            chat,
            hasMore: true,
            nextBeforeSequence: 9,
            resources: [
              {
                id: "newer-message",
                role: "user",
                content: "较新的工作消息",
                createdSequence: 9,
              },
            ],
          },
  }));
  const { user } = await openAssignment();
  const field = await screen.findByRole("textbox", { name: "补充目录" });
  const newer = screen.getByText("较新的工作消息");
  expect(newer.compareDocumentPosition(field)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  await user.type(field, "draft/docs");
  expect(field).toHaveValue("draft/docs");
  await user.click(screen.getByRole("button", { name: "加载更早消息" }));
  expect(
    (await screen.findByText("较早的交办")).compareDocumentPosition(field),
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  expect(field.compareDocumentPosition(newer)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(screen.getByRole("textbox", { name: "补充目录" })).toBe(field);
  expect(field).toHaveValue("draft/docs");
});

it("来源提问跳过失败留在消息区，重试确认后解锁队列并显示已跳过", async () => {
  const question = (elicitationId: string, title: string) => ({
    elicitationId,
    mode: "form",
    requestedSchema: {
      type: "object",
      properties: { target: { type: "string", title } },
      required: ["target"],
    },
  });
  const first = question("first-question", "第一目标目录");
  const second = question("second-question", "第二目标目录");
  const chat = { ...work, activeExecutionId: "exec" };
  calls.source.mockResolvedValue({
    data: {
      chat: { ...chat, pendingElicitations: [first, second] },
      resources: [
        {
          id: first.elicitationId,
          role: "assistant",
          question: first,
          createdSequence: 1,
        },
      ],
    },
  });
  calls.answer
    .mockRejectedValueOnce(
      new ApiError("暂时无法跳过此问题", "Unavailable", 503),
    )
    .mockImplementationOnce(async () => {
      calls.source.mockResolvedValue({
        data: {
          chat: { ...chat, pendingElicitations: [second] },
          resources: [
            {
              id: "settled",
              role: "assistant",
              question: { ...first, result: "declined" },
            },
          ],
        },
      });
      return { data: {} };
    });
  const { user } = await openAssignment();
  const firstField = await screen.findByRole("textbox", {
    name: "第一目标目录",
  });
  const secondField = screen.getByRole("textbox", { name: "第二目标目录" });
  expect(secondField).toBeDisabled();
  const firstCard = firstField.closest<HTMLElement>(
    "[data-chat-interaction-id]",
  );
  if (!firstCard) throw Error("缺少第一条提问");
  const skip = within(firstCard).getByRole("button", { name: "跳过" });
  await user.click(skip);
  const error = await screen.findByText("暂时无法跳过此问题");
  expect(error.closest("[data-chat-scroll-viewport]")).not.toBeNull();
  expect(skip).toBeEnabled();
  expect(secondField).toBeDisabled();
  await user.click(skip);
  await waitFor(() => expect(secondField).toBeEnabled());
  expect(screen.getByText("已跳过")).toBeVisible();
  expect(error).not.toBeInTheDocument();
  expect(calls.answer).toHaveBeenCalledTimes(2);
  for (const [request] of calls.answer.mock.calls) {
    expect(request).toMatchObject({
      path: { ...workPath, elicitationId: "first-question" },
      body: { content: {}, declined: true },
    });
  }
});

it("团队主题事件撤下已打开来源中被撤销成员的待处理请求", async () => {
  const subject = { subjectType: "team", subjectId: "w" } as const;
  const question = {
    elicitationId: "member-question",
    mode: "form",
    requestedSchema: {
      properties: { target: { type: "string", title: "成员待答目录" } },
    },
  };
  const chat = { ...work, activeExecutionId: "exec" };
  const resources = [
    {
      id: "work-input",
      role: "user",
      content: "保留成员原工作记录",
      createdSequence: 1,
    },
  ];
  calls.source.mockResolvedValue({
    data: {
      chat: {
        ...chat,
        pendingApprovals: [approval],
        pendingElicitations: [question],
      },
      resources,
    },
  });
  const { client } = show(workPath.workChatId, "", subject.subjectType);
  const otherSource = sourceHistoryOptions(subject, "another-work").queryKey;
  client.setQueryData(otherSource, { pages: [], pageParams: [] });
  const field = await screen.findByRole("textbox", { name: "成员待答目录" });
  const panel = screen.getByRole("region", { name: "等待工具审批" });
  expect(field).toBeVisible();
  expect(panel).toBeVisible();

  calls.source.mockResolvedValue({ data: { chat, resources } });
  mainConversationChanged(1, subject.subjectType);

  await waitFor(() => {
    expect(field).not.toBeInTheDocument();
    expect(panel).not.toBeInTheDocument();
  });
  expect(screen.getByText("保留成员原工作记录")).toBeVisible();
  expect(calls.source.mock.lastCall?.[0]).toMatchObject({
    path: { ...subject, workChatId: workPath.workChatId },
    query: { limit: 50 },
  });
  expect(client.getQueryState(otherSource)?.isInvalidated).toBe(false);
});

it("来源的最近一次核验被拒后收回审批、输入与键盘操作", async () => {
  calls.source.mockResolvedValue({
    data: {
      chat: {
        ...work,
        activeExecutionId: "exec",
        pendingApprovals: [approval],
      },
      resources: [],
    },
  });
  const { user, client } = await openAssignment();
  expect(
    await screen.findByRole("region", { name: "等待工具审批" }),
  ).toBeVisible();
  calls.source.mockRejectedValue(
    new ApiError("工作来源已不再授权", "Forbidden", 403),
  );
  await act(async () => {
    await client.invalidateQueries({
      queryKey: sourceHistoryOptions(
        { subjectType: "waker", subjectId: "w" },
        workPath.workChatId,
      ).queryKey,
    });
  });
  await waitFor(() =>
    expect(screen.queryByRole("region", { name: "等待工具审批" })).toBeNull(),
  );
  expect(screen.queryByRole("textbox", { name: "输入消息" })).toBeNull();
  await user.keyboard("{Enter}{Escape}");
  expect(calls.approve).not.toHaveBeenCalled();
  expect(calls.answer).not.toHaveBeenCalled();
});

it("公开时间线直接处理原工作的审批，处理后跟随执行直至完成", async () => {
  const user = userEvent.setup();
  const workChatId = "work-needing-approval";
  const activity = {
    executionId: "run-waiting",
    workChatId,
    sourceWakerId: "w",
    inputMessageId: "需要审批的交办",
    hasPublicReply: true,
  };
  const sourceChat = {
    ...work,
    id: workChatId,
    activeExecutionId: activity.executionId,
  };
  calls.activity.mockReturnValue([{ ...activity, waitingApproval: true }]);
  calls.timeline.mockReturnValue({
    messages: [
      turnInput(activity.inputMessageId, 1, workChatId),
      turnReply("等待前的公开发言", 2, activity.inputMessageId, workChatId),
      turnInput("后来的交办", 3, "later-work"),
    ],
  });
  calls.source.mockResolvedValue({
    data: {
      chat: { ...sourceChat, pendingApprovals: [approval] },
      resources: [],
    },
  });
  show();

  const card = await screen.findByRole("region", { name: "等待工具审批" });
  const reply = timelineRow("等待前的公开发言");
  const later = timelineRow("后来的交办");
  const sourceAction = { name: "打开工作会话" };
  const avatar = within(reply).getByRole("button", sourceAction);
  expect(screen.getAllByRole("button", sourceAction)).toEqual([avatar]);
  expect(reply.compareDocumentPosition(card)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(card.compareDocumentPosition(later)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBeVisible();
  expect(calls.source.mock.lastCall?.[0].path).toEqual({
    ...workPath,
    workChatId,
  });
  expect(calls.approve).not.toHaveBeenCalled();
  expect(calls.ordinaryApprove).not.toHaveBeenCalled();
  calls.approve.mockImplementation(async () => {
    calls.activity.mockReturnValue([activity]);
    calls.source.mockResolvedValue({
      data: { chat: sourceChat, resources: [] },
    });
    return { data: {} };
  });
  await user.click(within(card).getByRole("button", { name: /^允许本次/ }));
  await waitFor(() => expect(card).not.toBeInTheDocument());
  expect(calls.approve).toHaveBeenCalledOnce();
  expect(calls.approve.mock.lastCall?.[0]).toMatchObject({
    path: { ...workPath, workChatId, approvalId: approval.approvalId },
    body: { optionId: "allow" },
  });

  mainConversationChanged(1);
  await waitFor(() => {
    expect(screen.queryByText("正在等待回复")).toBeNull();
    expect(screen.queryByText("等待审批")).toBeNull();
    expect(screen.queryByRole("region", { name: "等待工具审批" })).toBeNull();
    expect(screen.getByRole("button", sourceAction)).toBe(avatar);
    expect(avatar).not.toHaveAccessibleDescription("工作中");
    expect(reply.nextElementSibling).toBe(later);
  });
  calls.activity.mockReturnValue([]);
  calls.timeline.mockReturnValue({
    messages: [turnReply("核验已经完成", 4, "需要审批的交办", workChatId)],
  });
  mainConversationChanged(2);
  expect(await screen.findByText("核验已经完成")).toBeVisible();
  expect(screen.queryByText("正在等待回复")).toBeNull();
  expect(calls.ordinaryApprove).not.toHaveBeenCalled();
});

it("续跑工单的进展和报告共用身份，待审批跟随当前执行的最后一条发言", async () => {
  const root = turnInput("原工单交办", 1);
  const input = turnInput("继续原工单", 2);
  const executionId = "resumed-execution";
  const progress = {
    ...turnReply("续跑进展", 3, input.id),
    forkExecutionId: executionId,
  };
  const report = {
    ...turnReply("续跑报告", 4, root.id),
    forkExecutionId: executionId,
  };
  const another = {
    ...turnReply("原交办下另一轮的报告", 5, root.id),
    forkExecutionId: "another-execution",
  };
  const legacy = turnReply("仅关联续跑交办的历史发言", 6, input.id);
  calls.activity.mockReturnValue([
    {
      workChatId: input.sourceChatId,
      sourceWakerId: "w",
      executionId,
      inputMessageId: input.id,
      hasPublicReply: true,
      waitingApproval: true,
    },
  ]);
  calls.timeline.mockReturnValue({
    messages: [root, input, progress, report, another, legacy],
  });
  calls.source.mockResolvedValue({
    data: {
      chat: {
        ...work,
        id: input.sourceChatId,
        activeExecutionId: executionId,
        pendingApprovals: [approval],
      },
      resources: [],
    },
  });
  show();

  const card = await screen.findByRole("region", { name: "等待工具审批" });
  const reportRow = timelineRow(report.id);
  const sourceAction = { name: "打开工作会话" };
  expect(
    within(timelineRow(progress.id)).getByRole("button", sourceAction),
  ).toBeVisible();
  expect(within(reportRow).queryByRole("button", sourceAction)).toBeNull();
  expect(
    within(timelineRow(another.id)).getByRole("button", sourceAction),
  ).toBeVisible();
  expect(reportRow.compareDocumentPosition(card)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(card.compareDocumentPosition(timelineRow(another.id))).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
});

it.each(["审批", "继续发送"])(
  "来源工作记录%s完成后保留正在读取的历史页",
  async (operation) => {
    const pendingApproval = operation === "审批";
    let settled = false;
    let releaseOlderPage = () => {};
    const olderPage = new Promise<void>((resolve) => {
      releaseOlderPage = resolve;
    });
    let pageSignal: AbortSignal | undefined;
    let headReads = 0;
    calls.source.mockImplementation(async ({ query, signal }) => {
      if (query?.beforeSequence === 9) {
        pageSignal ??= signal;
        await olderPage;
        return {
          data: {
            chat: work,
            resources: [
              {
                id: "older",
                role: "user",
                content: "上翻找到的来源消息",
                createdSequence: 8,
              },
            ],
          },
        };
      }
      headReads++;
      return {
        data: {
          chat:
            pendingApproval && !settled
              ? {
                  ...work,
                  activeExecutionId: "exec",
                  pendingApprovals: [approval],
                }
              : work,
          hasMore: true,
          nextBeforeSequence: 9,
          resources: [
            {
              id: "latest",
              role: "user",
              content: settled ? "最新的权威来源消息" : "最新的来源消息",
              createdSequence: 9,
            },
          ],
        },
      };
    });
    const resolve = pendingApproval ? calls.approve : calls.sendSource;
    resolve.mockImplementation(async () => {
      settled = true;
      return { data: {} };
    });
    const { user, client } = await openAssignment();
    try {
      await screen.findByText("最新的来源消息");
      if (!pendingApproval) {
        await user.type(
          screen.getByRole("textbox", { name: "输入消息" }),
          "继续检查",
        );
      }
      const initialHeadReads = headReads;
      await user.click(screen.getByRole("button", { name: "加载更早消息" }));
      await waitFor(() => expect(pageSignal).toBeDefined());

      const button = pendingApproval
        ? within(
            screen.getByRole("region", { name: "等待工具审批" }),
          ).getByRole("button", { name: /^允许本次/ })
        : screen.getByRole("button", { name: "发送消息" });
      await user.click(button);
      await waitFor(() => expect(resolve).toHaveBeenCalledOnce());
      expect(pageSignal?.aborted).toBe(false);
      expect(headReads).toBe(initialHeadReads);

      releaseOlderPage();
      expect(await screen.findByText("上翻找到的来源消息")).toBeVisible();
      expect(await screen.findByText("最新的权威来源消息")).toBeVisible();
      await waitFor(() => expect(client.isMutating()).toBe(0));
    } finally {
      releaseOlderPage();
    }
  },
);

it("主会话的并发结构化提问在原交办后展示，回答只提交对应工作的请求", async () => {
  const first = {
    workChatId: "first-work",
    executionId: "first-run",
    inputMessageId: "第一项交办",
    hasPublicReply: false,
  };
  const second = {
    workChatId: "second-work",
    executionId: "second-run",
    inputMessageId: "第二项交办",
    hasPublicReply: false,
  };
  const question = (label: string) => ({
    elicitationId: "same-native-id",
    message: `确认${label}`,
    mode: "form",
    requestedSchema: {
      type: "object",
      properties: { target: { type: "string", title: label } },
      required: ["target"],
    },
  });
  const waiting = { waitingElicitation: true, sourceWakerId: "w" };
  calls.activity.mockReturnValue([
    { ...waiting, ...first },
    { ...waiting, ...second },
  ]);
  calls.timeline.mockImplementation((before?: number) =>
    before === 2
      ? { messages: [turnInput(first.inputMessageId, 1, first.workChatId)] }
      : {
          messages: [
            turnInput(second.inputMessageId, 2, second.workChatId),
            turnInput("第三项交办", 3, "non-waiting-work"),
          ],
          hasMore: true,
          nextBeforeSequence: 2,
        },
  );
  const workSnapshots = new Map([
    [first.workChatId, { ...first, label: "第一项目录" }],
    [second.workChatId, { ...second, label: "第二项目录" }],
  ]);
  let answered = false;
  calls.source.mockImplementation(async ({ path: { workChatId } }) => {
    const snapshot = workSnapshots.get(workChatId);
    if (!snapshot) throw new Error("只应读取等待输入的工作");
    return {
      data: {
        chat: {
          ...work,
          id: snapshot.workChatId,
          activeExecutionId: snapshot.executionId,
          pendingElicitations:
            answered && workChatId === first.workChatId
              ? []
              : [question(snapshot.label)],
        },
        resources: [],
      },
    };
  });
  calls.answer.mockImplementation(async () => {
    answered = true;
    calls.activity.mockReturnValue([
      { ...first, sourceWakerId: waiting.sourceWakerId },
      { ...waiting, ...second },
    ]);
    return { data: {} };
  });

  const user = userEvent.setup();
  show();
  const answer = await screen.findByRole("textbox", { name: "第一项目录" });
  const other = await screen.findByRole("textbox", { name: "第二项目录" });
  expect(
    calls.source.mock.calls.map(([options]) => options.path.workChatId).sort(),
  ).toEqual([first.workChatId, second.workChatId]);
  await user.type(answer, "docs");
  await user.click(screen.getByRole("button", { name: "加载更早消息" }));
  await screen.findByText("第一项交办");
  expect(screen.getByRole("textbox", { name: "第一项目录" })).toBe(answer);
  expect(answer).toHaveValue("docs");
  expect(timelineRow("第一项交办").compareDocumentPosition(answer)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(answer.compareDocumentPosition(timelineRow("第二项交办"))).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(other.compareDocumentPosition(timelineRow("第三项交办"))).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  await user.click(answer);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(calls.answer).toHaveBeenCalledOnce());
  expect(calls.answer.mock.calls[0]?.[0]).toMatchObject({
    path: {
      ...workPath,
      workChatId: first.workChatId,
      elicitationId: "same-native-id",
    },
    body: { content: { target: "docs" }, declined: false },
  });
  await waitFor(() => expect(answer).not.toBeInTheDocument());
  expect(other).toBeVisible();
});

it("公开交办的已失效提问在重新加载后仍显示原问题与状态", async () => {
  const input = turnInput("审核待发布内容", 2, "interrupted-work");
  const question = {
    elicitationId: "review-question",
    message: "选择要检查的变更",
    mode: "form",
    requestedSchema: {
      type: "object",
      properties: { target: { type: "string", title: "检查范围" } },
      required: ["target"],
    },
  };
  calls.timeline.mockReturnValue({ messages: [input] });
  calls.activity.mockReturnValue([
    {
      workChatId: input.sourceChatId,
      executionId: "interrupted-run",
      inputMessageId: input.id,
      waitingElicitation: true,
    },
  ]);
  calls.source.mockResolvedValue({
    data: {
      chat: {
        id: input.sourceChatId,
        activeExecutionId: "interrupted-run",
        pendingElicitations: [question],
      },
    },
  });
  const beforeReload = show();
  expect(
    await screen.findByRole("textbox", { name: "检查范围" }),
  ).toBeVisible();
  calls.timeline.mockReturnValue({
    messages: [
      { ...input, questions: [{ ...question, result: "invalidated" }] },
    ],
  });
  mainConversationChanged(1);
  const settled = await screen.findByText("已失效");
  expect(settled).toBeVisible();
  expect(
    timelineRow(input.id).parentElement?.lastElementChild,
  ).toContainElement(settled);
  expect(screen.queryByRole("textbox", { name: "检查范围" })).toBeNull();
  beforeReload.unmount();

  calls.activity.mockReturnValue([]);
  calls.source.mockClear();
  show();
  expect(await screen.findByText(question.message)).toBeVisible();
  expect(screen.getAllByText(question.message)).toHaveLength(1);
  expect(screen.getByText("已失效")).toBeVisible();
  expect(screen.queryByRole("textbox", { name: "检查范围" })).toBeNull();
  expect(calls.source).not.toHaveBeenCalled();
  expect(calls.answer).not.toHaveBeenCalled();
});

it("工作看板深链只定位指定来源中同名的待回答请求", async () => {
  const first = turnInput("并发的第一条交办", 1, "first-request-work");
  const picked = turnInput("看板选中的交办", 2, "picked-request-work");
  const requestId = "reused-native-request-id";
  const label = "看板应定位的输入";
  calls.timeline.mockReturnValue({ messages: [first, picked] });
  calls.activity.mockReturnValue(
    [first, picked].map((row) => ({
      workChatId: row.sourceChatId,
      executionId: row.sourceChatId + "-execution",
      inputMessageId: row.id,
      waitingElicitation: true,
    })),
  );
  calls.source.mockImplementation(async ({ path: { workChatId } }) => ({
    data: {
      chat: {
        id: workChatId,
        activeExecutionId: workChatId + "-execution",
        pendingElicitations: [
          {
            elicitationId: requestId,
            message: "请补充工作范围",
            mode: "form",
            requestedSchema: {
              type: "object",
              properties: {
                answer: {
                  type: "string",
                  title:
                    workChatId === picked.sourceChatId ? label : "另一份输入",
                },
              },
              required: ["answer"],
            },
          },
        ],
      },
    },
  }));
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    show(
      undefined,
      "?request=" + requestId + "&requestChat=" + picked.sourceChatId,
    );
    const other = await screen.findByRole("textbox", { name: "另一份输入" });
    const answer = await screen.findByRole("textbox", { name: label });
    const card = answer.closest("[data-chat-interaction-id]");
    const staleCard = other.closest("[data-chat-interaction-id]");
    await waitFor(() => expect(jump.mock.contexts).toContain(card));
    expect(jump.mock.contexts).not.toContain(staleCard);
    expect(calls.answer).not.toHaveBeenCalled();
  } finally {
    jump.mockRestore();
  }
});

it("主会话只在工作快照成功核验后呈现可操作卡片，并可在原处重试", async () => {
  const activity = {
    workChatId: work.id,
    sourceWakerId: work.wakerId,
    executionId: "current-run",
    inputMessageId: "u-without-reply",
    hasPublicReply: false,
    waitingApproval: true,
  };
  calls.activity.mockReturnValue([activity]);
  calls.source
    .mockRejectedValueOnce(new ApiError("工作读取失败", "Internal", 500))
    .mockResolvedValueOnce({
      data: {
        chat: {
          ...work,
          activeExecutionId: "old-run",
          pendingApprovals: [approval],
        },
      },
    })
    .mockResolvedValue({
      data: {
        chat: {
          ...work,
          activeExecutionId: activity.executionId,
          pendingApprovals: [approval],
        },
      },
    });
  const user = userEvent.setup();
  const { client } = show();
  const failed = await screen.findByRole("alert");
  expect(screen.queryByRole("region", { name: "等待工具审批" })).toBeNull();
  await user.click(within(failed).getByRole("button", { name: "重试" }));
  await waitFor(() => expect(failed).not.toBeInTheDocument());
  expect(screen.queryByText("等待审批")).toBeNull();
  expect(screen.queryByRole("region", { name: "等待工具审批" })).toBeNull();
  const firstRead = calls.source.mock.calls[0]?.[0];
  expect(firstRead).toMatchObject({ path: workPath, query: { limit: 1 } });
  await act(async () => {
    await client.invalidateQueries({
      queryKey: sourceScopeOptions(workPath, work.id).queryKey,
    });
  });
  expect(
    await screen.findByRole("region", { name: "等待工具审批" }),
  ).toBeVisible();
  expect(calls.approve).not.toHaveBeenCalled();
});

const turnInput = (id: string, sequence: number, sourceChatId = "work-a") => ({
  id,
  sequence,
  role: "user",
  content: id,
  sourceChatId,
});

const turnReply = (
  id: string,
  sequence: number,
  replyToMessageId?: string,
  sourceChatId = "work-a",
  sourceWakerId = "w",
) => ({
  id,
  sequence,
  role: "assistant",
  content: id,
  sourceChatId,
  sourceWakerId,
  replyToMessageId,
});

function timelineRow(id: string, selector?: string) {
  const row = screen
    .getByText(id, { selector })
    .closest<HTMLElement>("[data-message-id]");
  if (!row) throw new Error(`未找到主会话消息 ${id}`);
  return row;
}

function openMessageMenu(row: HTMLElement) {
  fireEvent.keyDown(within(row).getByRole("button", { name: "更多操作" }), {
    key: "Enter",
  });
  return screen.getByRole("menu");
}

function serveMainHistory(
  read: (query: HistoryRequest) => HistoryPage | Promise<HistoryPage>,
) {
  calls.history.mockImplementation(async (request) => ({
    data: {
      id: `main-${request.path.subjectId}`,
      ...request.path,
      activeWork: [],
      ...(await read(request)),
    },
  }));
}

it("主会话显示首条、跨天和超过十五分钟的时间分隔，早页接上后按实际相邻消息判断", async () => {
  const previous = {
    ...turnInput("昨天的交办", 1),
    createdAt: new Date(2026, 10, 26, 23, 54).getTime(),
  };
  const first = {
    ...turnReply("今天的第一条回复", 2, previous.id),
    createdAt: new Date(2026, 10, 27, 0, 2).getTime(),
  };
  const withinGap = {
    ...turnReply("刚好间隔十五分钟", 3, previous.id),
    createdAt: new Date(2026, 10, 27, 0, 17).getTime(),
  };
  const afterGap = {
    ...turnReply("超过十五分钟后回复", 4, previous.id),
    createdAt: new Date(2026, 10, 27, 0, 33).getTime(),
  };
  serveMainHistory(({ query }) =>
    query?.beforeSequence === first.sequence
      ? { messages: [previous] }
      : {
          messages: [first, withinGap, afterGap],
          hasMore: true,
          nextBeforeSequence: first.sequence,
        },
  );
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(2026, 10, 27, 12));
  try {
    show();
    const firstTime = await screen.findByText("今天 00:02", {
      selector: "time",
    });
    expect(firstTime.nextElementSibling).toBe(timelineRow(first.id));
    expect(screen.queryByText("今天 00:17")).toBeNull();
    expect(timelineRow(withinGap.id).previousElementSibling).toBe(
      timelineRow(first.id),
    );
    const laterTime = screen.getByText("今天 00:33", { selector: "time" });
    expect(laterTime.nextElementSibling).toBe(timelineRow(afterGap.id));
    expect(within(timelineRow(afterGap.id)).getByText("伙伴 A")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    const previousTime = await screen.findByText("昨天 23:54", {
      selector: "time",
    });
    expect(previousTime.nextElementSibling).toBe(timelineRow(previous.id));
    expect(firstTime.previousElementSibling).toBe(timelineRow(previous.id));
  } finally {
    vi.useRealTimers();
  }
});

function earlierCursors() {
  return calls.history.mock.calls
    .map(([request]) => request.query?.beforeSequence)
    .filter((cursor) => cursor !== undefined);
}

function messageQuote(reply: string) {
  return within(timelineRow(reply)).getByRole("button", {
    name: /定位原消息/,
  });
}

function conversationViewport(message: string) {
  const viewport = timelineRow(message).closest<HTMLElement>(
    "[data-chat-scroll-viewport]",
  );
  if (!viewport) throw new Error("缺少主会话消息滚动区");
  Object.defineProperties(viewport, {
    scrollHeight: { configurable: true, value: 1200 },
    clientHeight: { configurable: true, value: 300 },
  });
  return viewport;
}

it("无工作来源的回复头像进入完整工作记录，自动查找旧页的实际交办", async () => {
  const original = turnReply("上轮公开的交付问题", 1, undefined, "");
  const request = {
    ...turnInput("请对照上轮的问题", 2, ""),
    replyToMessageId: original.id,
  };
  const reply = turnReply("请提供项目名称", 3, request.id, "");
  serveMainHistory(({ query }) =>
    query?.beforeSequence === reply.sequence
      ? { messages: [original, request], hasMore: false }
      : {
          messages: [reply],
          hasMore: true,
          nextBeforeSequence: reply.sequence,
        },
  );
  const user = userEvent.setup();
  const { router, back } = show();
  await screen.findByText(reply.content);
  const avatar = within(timelineRow(reply.id)).getByRole("button", {
    name: "打开默认会话",
  });

  const editor = screen.getByRole("textbox", { name: "输入消息" });
  await user.type(editor, "沿用这份草稿");
  avatar.focus();
  await user.keyboard("{Enter}");
  expect(new URLSearchParams(router.state.location.search).get("record")).toBe(
    reply.id,
  );
  expect(screen.queryByRole("dialog")).toBeNull();
  const record = within(
    await screen.findByRole("region", { name: "默认会话" }),
  );
  expect(
    await record.findByText(request.content, { selector: "p" }),
  ).toBeVisible();
  expect(record.getByText(reply.content)).toBeVisible();
  expect(record.getByRole("textbox", { name: "输入消息" })).toBe(editor);
  expect(editor).toHaveTextContent("沿用这份草稿");

  await user.click(record.getByRole("button", { name: /定位原消息/ }));
  expect(new URLSearchParams(router.state.location.search).get("message")).toBe(
    original.id,
  );
  expect(timelineRow(original.id, "p")).toBeVisible();
  await back();
  expect(await screen.findByRole("region", { name: "默认会话" })).toBeVisible();
  expect(editor).toHaveTextContent("沿用这份草稿");
  expect(earlierCursors()).toEqual([reply.sequence]);
  expect(calls.source).not.toHaveBeenCalled();
});

it("旧直答地址保留原定位，并展示同一伙伴的多轮公开问答", async () => {
  const newerInput = turnInput("新问答的输入", 130, "");
  const latest = turnReply("更新的一条回复", 150, newerInput.id, "");
  const other = turnReply("另一项工作的结果", 140, undefined, "other-work");
  const olderOwner = turnReply(
    "旧负责人的一条回复",
    145,
    newerInput.id,
    "",
    "old",
  );
  const request = turnInput("旧交办实际文本", 40, "");
  const reply = turnReply("该交办的直答", 80, request.id, "");
  const pages = new Map<number | undefined, HistoryPage>([
    [
      undefined,
      {
        messages: [newerInput, other, olderOwner, latest],
        nextBeforeSequence: 130,
        hasMore: true,
      },
    ],
    [130, { messages: [reply], nextBeforeSequence: 80, hasMore: true }],
    [80, { messages: [request], hasMore: false }],
  ]);
  serveMainHistory(({ query }) => {
    const page = pages.get(query?.beforeSequence);
    if (!page) throw Error("读取了无关历史页");
    return page;
  });
  show(undefined, `?record=${encodeURIComponent(reply.id)}`);
  const record = within(
    await screen.findByRole("region", { name: "默认会话" }),
  );
  expect(await record.findByText(request.content)).toBeVisible();
  expect(record.getByText(reply.content)).toBeVisible();
  expect(record.getByText(latest.content)).toBeVisible();
  expect(record.getByText(newerInput.content)).toBeVisible();
  expect(record.queryByText(other.content)).toBeNull();
  expect(record.queryByText(olderOwner.content)).toBeNull();
  expect(earlierCursors()).toEqual([130, 80]);
  expect(calls.source).not.toHaveBeenCalled();
});

it("已有来源的头像保留真实地址，并定位同一来源的旧执行", async () => {
  const workChatId = "shared-old-work";
  const executionId = "earlier-execution";
  const publication = {
    ...turnReply("较早执行发布的结果", 1, undefined, workChatId),
    forkExecutionId: executionId,
  };
  calls.timeline.mockReturnValue({ messages: [publication] });
  calls.source.mockImplementation(async ({ path, query }) => {
    expect(path.workChatId).toBe(workChatId);
    return {
      data: {
        chat: { id: workChatId, wakerId: "w" },
        ...(query?.beforeSequence === 50
          ? {
              resources: [
                {
                  id: executionId,
                  role: "assistant",
                  status: "completed",
                  executionId,
                  fork: { available: true },
                  readable: true,
                  displayParts: [
                    {
                      id: "earlier-text",
                      type: "text",
                      text: "较早执行的原生答复",
                    },
                  ],
                  createdSequence: 10,
                },
              ],
              hasMore: false,
            }
          : {
              forkLatestExecutionId: "newer-execution",
              resources: [
                {
                  id: "newer-work-input",
                  role: "user",
                  content: "同来源的另外一次交办",
                  createdSequence: 50,
                },
              ],
              hasMore: true,
              nextBeforeSequence: 50,
            }),
      },
    };
  });
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    const { router } = show();
    await screen.findByText(publication.content);
    await userEvent.click(
      within(timelineRow(publication.id)).getByRole("button", {
        name: "打开工作会话",
      }),
    );
    expect(
      new URLSearchParams(router.state.location.search).get("source"),
    ).toBe(workChatId);
    expect(
      new URLSearchParams(router.state.location.search).get("message"),
    ).toBe(publication.id);
    await waitFor(() =>
      expect(
        calls.source.mock.calls.some(
          ([options]) => options.query?.beforeSequence === 50,
        ),
      ).toBe(true),
    );
    const answer = await screen.findByText("较早执行的原生答复");
    const answerRow = answer.closest<HTMLElement>("[data-message-id]");
    if (!answerRow) throw new Error("缺少原执行消息");
    await waitFor(() => expect(jump.mock.contexts).toContain(answerRow));
    const row = within(answerRow);
    expect(
      row.queryByRole("button", { name: "阅读操作" }),
    ).not.toBeInTheDocument();
    calls.fork.mockResolvedValue({ data: {} });
    await userEvent.click(row.getByRole("button", { name: "分支到新聊天" }));
    expect(calls.fork).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        path: { chatId: workChatId },
        body: { executionId, clientRequestId: expect.any(String) },
      }),
    );
  } finally {
    jump.mockRestore();
  }
});

it("同一伙伴的两条简报打开完整的普通会话，续聊只出现在这里", async () => {
  const user = userEvent.setup();
  const workChatId = "partner-default-chat";
  const smallTalk = turnReply("在呢", 1, undefined, workChatId);
  const news = turnReply("新闻查好了", 2, undefined, workChatId);
  calls.timeline.mockReturnValue({ messages: [smallTalk, news] });
  calls.source.mockResolvedValue({
    data: {
      chat: { id: workChatId, wakerId: "w" },
      privateContinuation: true,
      resources: [
        { id: "hello", role: "user", content: "你好", createdSequence: 1 },
        {
          id: "answer-hello",
          executionId: "answer-hello",
          role: "assistant",
          status: "completed",
          displayParts: [{ type: "text", text: "你好，今天想聊什么？" }],
          createdSequence: 2,
        },
        {
          id: "check-news",
          role: "user",
          content: "查一下今天的新闻",
          createdSequence: 3,
        },
        {
          id: "answer-news",
          executionId: "answer-news",
          role: "assistant",
          status: "completed",
          displayParts: [{ type: "text", text: "今天的新闻完整内容。" }],
          createdSequence: 4,
        },
      ],
      publishedMessages: [
        {
          id: smallTalk.id,
          executionId: "answer-hello",
          content: smallTalk.content,
        },
        { id: news.id, executionId: "answer-news", content: news.content },
      ],
    },
  });
  let finishSend = () => {};
  const received = new Promise<void>((resolve) => {
    finishSend = resolve;
  });
  calls.sendSource.mockImplementation(async () => {
    await received;
    return { data: { execution: { id: "answer-followup" } } };
  });
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  const { back, router } = show();
  try {
    for (const report of [smallTalk, news]) {
      await screen.findByText(report.content);
      await user.click(
        within(timelineRow(report.id)).getByRole("button", {
          name: "打开工作会话",
        }),
      );
      const params = new URLSearchParams(router.state.location.search);
      expect(params.get("source")).toBe(workChatId);
      expect(params.get("message")).toBe(report.id);
      const completeGreeting = await screen.findByText("你好，今天想聊什么？");
      const completeNews = screen.getByText("今天的新闻完整内容。");
      expect(completeGreeting).toBeVisible();
      expect(completeNews).toBeVisible();
      const target = report === smallTalk ? completeGreeting : completeNews;
      await waitFor(() =>
        expect(jump.mock.contexts).toContain(
          target.closest("[data-message-id]"),
        ),
      );
      expect(screen.getByText(report.content)).not.toBeVisible();
      if (report === smallTalk) await back();
    }
    const editor = await screen.findByRole("textbox", { name: "输入消息" });
    const followUp = "只在普通会话追问";
    await user.type(editor, followUp);
    const send = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(send).toBeEnabled());
    await user.click(send);
    await waitFor(() => expect(calls.sendSource).toHaveBeenCalledOnce());
    expect(calls.sendSource.mock.calls[0]?.[0]).toMatchObject({
      path: { subjectType: "waker", subjectId: "w", workChatId },
      body: { text: followUp },
    });
    await back();
    expect(await screen.findByText(news.content)).toBeVisible();
    expect(screen.queryByText(followUp)).toBeNull();
    expect(calls.send).not.toHaveBeenCalled();
  } finally {
    await act(async () => finishSend());
    jump.mockRestore();
  }
});

it("回投报告显示原会话来源并打开公开记录", async () => {
  const report = {
    ...turnReply("原交办已结案", 1, undefined, ""),
    originalWork: {
      chatId: "deleted-chat",
      ticketId: "original-ticket",
      title: "发布前检查",
    },
  };
  calls.timeline.mockReturnValue({ messages: [report] });
  const { router } = show();
  await screen.findByText(report.content);
  const row = within(timelineRow(report.id));
  const origin = "来自已删除的会话「发布前检查」";
  expect(row.getByText(origin)).toBeVisible();
  await userEvent.click(row.getByRole("button", { name: "打开工作会话" }));
  const target = new URLSearchParams(router.state.location.search);
  expect(target.get("record")).toBe(report.id);
  expect(target.get("source")).toBeNull();
  const record = within(
    await screen.findByRole("region", { name: "工作会话" }),
  );
  expect(record.getByText(report.content)).toBeVisible();
  expect(record.getByText(origin)).toBeVisible();
  expect(record.queryByRole("link")).toBeNull();
  expect(calls.source).not.toHaveBeenCalled();
});

it("转交回件凭原发言打开接手人的只读工作记录", async () => {
  const workChatId = "receiver-work";
  const executionId = "receiver-execution";
  const report = {
    ...turnReply("接手人提交了结果。", 1, undefined, workChatId),
    sourceWakerId: "receiver",
    sourceReadOnly: true,
  };
  calls.timeline.mockReturnValue({ messages: [report] });
  calls.source.mockImplementation(async ({ path, query }) => {
    expect(path).toEqual({ subjectType: "waker", subjectId: "w", workChatId });
    expect(query?.publicationId).toBe(report.id);
    return {
      data: {
        chat: { id: workChatId, wakerId: "receiver" },
        forkLatestExecutionId: executionId,
        resources: [
          { id: "handoff-input", role: "user", content: "已授权的交接说明。" },
          {
            id: executionId,
            executionId,
            role: "assistant",
            status: "completed",
            fork: { available: true },
            content: "接手执行的原生结果。",
          },
        ],
        publishedMessages: [
          { id: report.id, executionId, content: report.content },
        ],
      },
    };
  });
  const { router } = show();
  await screen.findByText(report.content);
  await userEvent.click(
    within(timelineRow(report.id)).getByRole("button", {
      name: "打开工作会话",
    }),
  );
  const target = new URLSearchParams(router.state.location.search);
  expect(target.get("source")).toBe(workChatId);
  expect(target.get("publication")).toBe(report.id);
  expect(await screen.findByText("已授权的交接说明。")).toBeVisible();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "回复" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "分支到新聊天" }),
  ).not.toBeInTheDocument();
  expect(calls.source).toHaveBeenCalledTimes(1);
});

it.each(["用户", "伙伴"])(
  "%s使用键盘激活最新页的引用会按游标逐页定位公开原话",
  async (author) => {
    const original =
      author === "伙伴"
        ? turnInput("更早的公开交办", 10)
        : turnReply("更早的公开原话", 10);
    const reply =
      author === "伙伴"
        ? {
            ...turnReply("引用原话的公开答复", 120, original.id),
            quoteReply: true,
          }
        : {
            ...turnInput("收到原话后的追问", 120),
            replyToMessageId: original.id,
          };
    const pages = new Map<number | undefined, HistoryPage>([
      [
        undefined,
        { messages: [reply], hasMore: true, nextBeforeSequence: 120 },
      ],
      [
        120,
        {
          messages: [turnReply("中间页的公开消息", 60)],
          hasMore: true,
          nextBeforeSequence: 60,
        },
      ],
      [60, { messages: [original], hasMore: true, nextBeforeSequence: 10 }],
    ]);
    serveMainHistory(({ query }) => {
      const page = pages.get(query?.beforeSequence);
      if (!page) throw new Error("定位不应越过原话所在页");
      return page;
    });
    const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
    try {
      const user = userEvent.setup();
      const { container } = show();
      await screen.findByText(reply.content);
      await screen.findByRole("textbox", { name: "输入消息" });
      expect(calls.history).toHaveBeenCalledOnce();
      expect(screen.queryByText(original.content)).toBeNull();
      const quote = messageQuote(reply.id);
      const key = vi.fn();
      const click = vi.fn();
      quote.addEventListener("keydown", key);
      quote.addEventListener("click", click);
      quote.focus();
      expect(quote).toHaveFocus();
      await user.keyboard("{Enter}");
      expect(key).toHaveBeenCalled();
      expect(click).toHaveBeenCalled();
      await waitFor(() => expect(earlierCursors()).toEqual([120, 60]));
      await screen.findByText(original.content, { selector: "p" });
      await waitFor(() =>
        expect(jump.mock.contexts).toContain(timelineRow(original.id, "p")),
      );
      expect(
        timelineRow(original.id, "p").querySelector(
          "[data-chat-message-bubble]",
        ),
      ).toHaveFocus();
      const bubble = timelineRow(reply.id).querySelector(
        "[data-chat-message-bubble]",
      );
      expect(bubble).toContainElement(messageQuote(reply.id));
      expect(messageQuote(reply.id)).toHaveTextContent(original.content);
      expect(
        calls.history.mock.calls.map(([request]) => [
          request.path.subjectType,
          request.path.subjectId,
          request.query?.beforeSequence,
        ]),
      ).toEqual([
        ["waker", "w", undefined],
        ["waker", "w", 120],
        ["waker", "w", 60],
      ]);
      expect(container.querySelectorAll("[data-message-id]")).toHaveLength(3);
      expect(calls.send).not.toHaveBeenCalled();
      expect(calls.source).not.toHaveBeenCalled();
    } finally {
      jump.mockRestore();
    }
  },
);

it("定位跨页引用时仍能接收后台新发言", async () => {
  const original = turnReply("等待定位的公开原话", 10);
  const earlier = turnReply("上页末尾的发言", 60);
  const previous = turnReply("原来在最新页的发言", 120);
  const reply = {
    ...turnInput("引用后等待新的发言", 121),
    replyToMessageId: original.id,
  };
  const newest = turnReply("后台刚发布的发言", 122);
  let messages = [original, earlier, previous, reply];
  let releaseFirstPage = () => {};
  const firstPage = new Promise<void>((resolve) => {
    releaseFirstPage = resolve;
  });
  serveMainHistory(async ({ query }) => {
    const cursor = query?.beforeSequence;
    if (cursor === 120) await firstPage;
    const eligible = messages.filter(
      ({ sequence }) => cursor === undefined || sequence < cursor,
    );
    const page = eligible.slice(-2);
    return {
      messages: page,
      hasMore: eligible.length > page.length,
      nextBeforeSequence: page[0]?.sequence,
    };
  });

  const jump = vi
    .spyOn(HTMLElement.prototype, "scrollIntoView")
    .mockImplementation(function (this: HTMLElement) {
      if (this.dataset.messageId === original.id)
        vi.spyOn(this, "getBoundingClientRect").mockReturnValue(
          new DOMRect(0, 220, 500, 60),
        );
    });
  try {
    const user = userEvent.setup();
    const { container } = show();
    await screen.findByText(reply.content);
    const viewport = conversationViewport(reply.id);
    vi.spyOn(viewport, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 100, 640, 300),
    );
    await user.click(messageQuote(reply.id));
    await waitFor(() => expect(earlierCursors()).toContain(120));
    messages = [...messages, newest];
    mainConversationChanged(1);
    await act(async () => releaseFirstPage());

    expect(await screen.findByText(newest.content)).toBeVisible();
    await screen.findByText(original.content, { selector: "p" });
    const target = timelineRow(original.id, "p");
    await waitFor(() => expect(jump.mock.contexts).toContain(target));
    const visible = viewport.getBoundingClientRect();
    const bounds = target.getBoundingClientRect();
    expect(bounds.bottom).toBeGreaterThan(visible.top);
    expect(bounds.top).toBeLessThan(visible.bottom);
    expect(
      Array.from(container.querySelectorAll("[data-message-id]"), (row) =>
        row.getAttribute("data-message-id"),
      ),
    ).toEqual([original.id, earlier.id, previous.id, reply.id, newest.id]);
  } finally {
    releaseFirstPage();
    jump.mockRestore();
  }
});

it("后台正在刷新最新页时，点击引用仍能定位原话", async () => {
  const original = turnReply("等待新游标处的公开原话", 60);
  const previous = turnReply("刷新前最新页的开头", 120);
  const reply = {
    ...turnInput("刷新期间点击此引用", 121),
    replyToMessageId: original.id,
  };
  const newest = turnReply("刷新完成后看到的发言", 122);
  let changed = false;
  let releaseRefresh = () => {};
  const refresh = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  serveMainHistory(async ({ query }) => {
    if (query?.beforeSequence === 120) return { messages: [original] };
    if (query?.beforeSequence === 121)
      return { messages: [original, previous] };
    if (query?.beforeSequence !== undefined)
      throw new Error("意外的上一页游标");
    if (!changed)
      return {
        messages: [previous, reply],
        hasMore: true,
        nextBeforeSequence: 120,
      };
    await refresh;
    return {
      messages: [reply, newest],
      hasMore: true,
      nextBeforeSequence: 121,
    };
  });

  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    const user = userEvent.setup();
    show();
    await screen.findByText(reply.content);
    changed = true;
    mainConversationChanged(1);
    await waitFor(() => expect(calls.history).toHaveBeenCalledTimes(2));
    await user.click(messageQuote(reply.id));
    expect(screen.queryByText(newest.content)).toBeNull();
    expect(earlierCursors()).toEqual([]);

    await act(async () => releaseRefresh());
    expect(await screen.findByText(newest.content)).toBeVisible();
    await screen.findByText(original.content, { selector: "p" });
    await waitFor(() =>
      expect(jump.mock.contexts).toContain(timelineRow(original.id, "p")),
    );
    expect(earlierCursors()).toHaveLength(1);
    expect(screen.getByText(previous.content)).toBeVisible();
  } finally {
    releaseRefresh();
    jump.mockRestore();
  }
});

it("打开长会话但未点击引用时，后台发言只刷新首屏", async () => {
  const previous = turnReply("刷新前首屏最早的发言", 120);
  const reply = {
    ...turnInput("暂时不点击这条引用", 121),
    replyToMessageId: "older-message",
  };
  const newest = turnReply("首屏新收到的发言", 122);
  let head = [previous, reply];
  serveMainHistory(({ query }) => {
    if (query?.beforeSequence !== undefined)
      throw new Error("普通刷新不应读取历史页");
    return {
      messages: head,
      hasMore: true,
      nextBeforeSequence: head[0]?.sequence,
    };
  });

  const { container } = show();
  expect(await screen.findByText(previous.content)).toBeVisible();
  head = [reply, newest];
  mainConversationChanged(1);
  expect(await screen.findByText(newest.content)).toBeVisible();
  expect(screen.getByText(reply.content)).toBeVisible();
  expect(screen.queryByText(previous.content)).toBeNull();
  expect(container.querySelectorAll("[data-message-id]")).toHaveLength(2);
  expect(earlierCursors()).toEqual([]);
});

it("滚离已经定位的原话后，后台发言不会把视口拉回原话", async () => {
  const original = turnReply("用户已经读过的原话", 10);
  const middle = turnReply("手动滚动后正在看的发言", 60);
  const reply = {
    ...turnInput("由这个引用定位原话", 120),
    replyToMessageId: original.id,
  };
  const previous = turnReply("原来最新的发言", 121);
  const newest = turnReply("用户滚走后收到的新发言", 122);
  let messages = [original, middle, reply, previous];
  serveMainHistory(({ query }) => {
    const cursor = query?.beforeSequence;
    const eligible = messages.filter(
      ({ sequence }) => cursor === undefined || sequence < cursor,
    );
    const page = eligible.slice(-2);
    return {
      messages: page,
      hasMore: eligible.length > page.length,
      nextBeforeSequence: page[0]?.sequence,
    };
  });

  const user = userEvent.setup();
  show();
  await screen.findByText(reply.content);
  const viewport = conversationViewport(reply.id);
  vi.spyOn(viewport, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 640, 300),
  );
  const scroll = vi
    .spyOn(viewport, "scrollTo")
    .mockImplementation((options?: ScrollToOptions | number, y?: number) => {
      const top = typeof options === "number" ? y : options?.top;
      if (top !== undefined) viewport.scrollTop = top;
    });
  const jump = vi
    .spyOn(HTMLElement.prototype, "scrollIntoView")
    .mockImplementation(() => {
      viewport.scrollTop = 160;
    });
  try {
    await user.click(messageQuote(reply.id));
    await screen.findByText(original.content, { selector: "p" });
    const target = timelineRow(original.id, "p");
    await waitFor(() => expect(jump.mock.contexts).toContain(target));
    vi.spyOn(target, "getBoundingClientRect").mockImplementation(
      () => new DOMRect(0, 260 - viewport.scrollTop, 500, 40),
    );
    fireEvent.scroll(viewport);
    viewport.scrollTop = 420;
    fireEvent.scroll(viewport);
    expect(target.getBoundingClientRect().bottom).toBeLessThan(0);

    messages = [...messages, newest];
    mainConversationChanged(1);
    expect(await screen.findByText(newest.content)).toBeVisible();
    expect(screen.getByText(middle.content)).toBeVisible();
    expect(viewport.scrollTop).toBe(420);
    expect(jump).toHaveBeenCalledOnce();
    expect(earlierCursors()).toEqual([120, 121]);
  } finally {
    scroll.mockRestore();
    jump.mockRestore();
  }
});

it("取消跨页引用定位后可以用键盘重新定位同一条原话", async () => {
  const original = turnReply("主动重新定位的公开原话", 10);
  const middle = turnReply("取消时仍在请求的页面", 60);
  const reply = {
    ...turnInput("准备取消定位的引用", 120),
    replyToMessageId: original.id,
  };
  let releasePage = () => {};
  const page = new Promise<void>((resolve) => {
    releasePage = resolve;
  });
  serveMainHistory(async ({ query }) => {
    if (query?.beforeSequence === undefined)
      return { messages: [reply], hasMore: true, nextBeforeSequence: 120 };
    if (query.beforeSequence === 60) return { messages: [original] };
    await page;
    return { messages: [middle], hasMore: true, nextBeforeSequence: 60 };
  });

  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    const user = userEvent.setup();
    const { client } = show();
    await screen.findByText(reply.content);
    await user.click(messageQuote(reply.id));
    await waitFor(() => expect(earlierCursors()).toEqual([120]));
    const row = within(timelineRow(reply.id));
    await user.click(await row.findByRole("button", { name: "取消" }));
    await act(async () => releasePage());
    const queryKey = mainConversationOptions({
      subjectType: "waker",
      subjectId: "w",
    }).queryKey;
    await waitFor(() => expect(client.isFetching({ queryKey })).toBe(0));
    expect(row.queryByText("正在定位原消息…")).toBeNull();
    expect(earlierCursors()).toEqual([120]);
    expect(jump).not.toHaveBeenCalled();

    messageQuote(reply.id).focus();
    await user.keyboard("{Enter}");
    await screen.findByText(original.content, { selector: "p" });
    await waitFor(() =>
      expect(
        timelineRow(original.id, "p").querySelector(
          "[data-chat-message-bubble]",
        ),
      ).toHaveFocus(),
    );
    expect(earlierCursors().at(-1)).toBe(60);
  } finally {
    releasePage();
    jump.mockRestore();
  }
});

it("键盘定位已加载的原话，不因程序性滚动加载旧页", async () => {
  const original = turnReply("当前页的公开原话", 60);
  const reply = {
    ...turnInput("当前页里的引用", 61),
    replyToMessageId: original.id,
  };
  serveMainHistory(() => ({
    messages: [original, reply],
    hasMore: true,
    nextBeforeSequence: 60,
  }));
  const user = userEvent.setup();
  show();
  await screen.findByText(reply.content);
  const viewport = conversationViewport(reply.id);
  viewport.scrollTop = 240;
  fireEvent.scroll(viewport);
  const target = timelineRow(original.id, "p");
  const jump = vi
    .spyOn(HTMLElement.prototype, "scrollIntoView")
    .mockImplementation(() => {
      viewport.scrollTop = 12;
    });
  try {
    messageQuote(reply.id).focus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(jump.mock.contexts).toContain(target));
    const bubble = target.querySelector("[data-chat-message-bubble]");
    expect(bubble).toHaveFocus();
    expect(bubble).toHaveAttribute("data-chat-message-located");
    fireEvent.scroll(viewport);
    await act(async () => vi.dynamicImportSettled());
    expect(earlierCursors()).toEqual([]);
  } finally {
    jump.mockRestore();
  }
});

it.each([
  ["用户", "已删除"],
  ["用户", "没有访问权限"],
  ["伙伴", "已删除"],
])("%s引用的原话%s时在引用旁提示不可用", async (author, reason) => {
  const other = turnReply("同一会话里可访问的较早消息", 30);
  const reply =
    author === "伙伴"
      ? {
          ...turnReply("引用不存在的公开消息", 60, "missing-message"),
          quoteReply: true,
        }
      : {
          ...turnInput("引用不存在的公开消息", 60),
          replyToMessageId: "missing-message",
        };
  serveMainHistory(({ query }) => {
    if (query?.beforeSequence === undefined)
      return { messages: [reply], hasMore: true, nextBeforeSequence: 60 };
    if (reason === "没有访问权限")
      throw new ApiError("不可访问", "Forbidden", 403);
    return { messages: [other], hasMore: false };
  });

  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    const user = userEvent.setup();
    show();
    await screen.findByText(reply.content);
    await user.click(messageQuote(reply.id));
    const row = within(timelineRow(reply.id));
    expect(await row.findByText("原消息不可用")).toBeVisible();
    expect(earlierCursors()).toEqual([60]);
    expect(jump).not.toHaveBeenCalled();
    expect(row.queryByRole("button", { name: "重试" })).toBeNull();
  } finally {
    jump.mockRestore();
  }
});

it("跨页引用遇到分页错误时保留上下文，在引用旁从失败游标重试", async () => {
  const original = turnReply("重试后可定位的原话", 1);
  const middle = turnReply("重试时已载入的中间页", 60);
  const reply = {
    ...turnInput("等待重试的引用", 120),
    replyToMessageId: original.id,
  };
  let fail = true;
  serveMainHistory(({ query }) => {
    if (query?.beforeSequence === undefined)
      return { messages: [reply], hasMore: true, nextBeforeSequence: 120 };
    if (query.beforeSequence === 120)
      return { messages: [middle], hasMore: true, nextBeforeSequence: 60 };
    if (fail) {
      fail = false;
      throw new ApiError("分页读取失败", "Internal", 500);
    }
    return { messages: [original], hasMore: false };
  });

  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    const user = userEvent.setup();
    show();
    await screen.findByText(reply.content);
    const editor = screen.getByRole("textbox", { name: "输入消息" });
    await user.type(editor, "留在输入框里的草稿");
    await user.click(messageQuote(reply.id));
    const row = within(timelineRow(reply.id));
    const retry = await row.findByRole("button", { name: "重试" });
    expect(screen.getByText(middle.content)).toBeVisible();
    expect(editor).toHaveTextContent("留在输入框里的草稿");
    expect(earlierCursors()).toEqual([120, 60]);
    expect(jump).not.toHaveBeenCalled();

    await user.click(retry);
    await screen.findByText(original.content, { selector: "p" });
    await waitFor(() =>
      expect(jump.mock.contexts).toContain(timelineRow(original.id, "p")),
    );
    expect(earlierCursors()).toEqual([120, 60, 60]);
    expect(
      calls.history.mock.calls.filter(
        ([request]) => request.query?.beforeSequence === undefined,
      ),
    ).toHaveLength(1);
    expect(row.queryByRole("button", { name: "重试" })).toBeNull();
    expect(editor).toHaveTextContent("留在输入框里的草稿");
  } finally {
    jump.mockRestore();
  }
});

it("首条公开发言显示静态头像和名称，连续发言与工作结束都保留原身份位置", async () => {
  const active = {
    workChatId: "work-a",
    sourceWakerId: "w",
    executionId: "source-execution-id",
    inputMessageId: "本次交办",
    hasPublicReply: false,
  };
  const other = {
    workChatId: "work-b",
    sourceWakerId: "w",
    executionId: "other-execution-id",
    inputMessageId: "并发工作的本次交办",
    hasPublicReply: false,
    waitingApproval: true,
  };
  const history = [
    turnInput("之前的交办", 1),
    turnReply("之前的结果", 2, "之前的交办"),
    turnInput("并发工作的上次交办", 3, "work-b"),
    turnReply("并发工作的上次结果", 4, "并发工作的上次交办", "work-b"),
    turnInput(other.inputMessageId, 5, other.workChatId),
    turnInput(active.inputMessageId, 6),
  ];
  const first = turnReply("第一段公开进展", 7, active.inputMessageId);
  const second = turnReply("第二段公开进展", 8, active.inputMessageId);
  const sourceText = "来源工作的实际过程";
  calls.activity.mockReturnValue([other, active]);
  calls.timeline.mockReturnValue({ messages: history });
  calls.source.mockImplementation(async ({ path: { workChatId } }) => ({
    data: {
      chat: {
        ...work,
        id: workChatId,
        ...(workChatId === other.workChatId
          ? {
              activeExecutionId: other.executionId,
              pendingApprovals: [approval],
            }
          : {}),
      },
      resources:
        workChatId === active.workChatId
          ? [
              {
                id: "work-input",
                role: "user",
                content: sourceText,
                createdSequence: 1,
              },
            ]
          : [],
    },
  }));
  const { back } = show();
  const sourceAction = { name: "打开工作会话" };
  await screen.findByRole("region", {
    name: "等待工具审批",
  });
  expect(screen.getAllByText("正在等待回复")).toHaveLength(1);
  expect(
    within(timelineRow("之前的结果")).getByRole("button", sourceAction),
  ).toBeVisible();
  expect(
    within(timelineRow("并发工作的上次结果")).getByRole("button", sourceAction),
  ).toBeVisible();
  const waitingRow = screen
    .getByText("正在等待回复")
    .closest<HTMLElement>("[data-role=assistant]");
  if (!waitingRow) throw new Error("缺少首条回复前的等待行");
  expect(waitingRow.previousElementSibling).toBe(
    timelineRow(active.inputMessageId),
  );
  await userEvent.click(within(waitingRow).getByRole("button", sourceAction));
  await waitFor(() =>
    expect(calls.source.mock.lastCall?.[0].path).toEqual({
      ...workPath,
      workChatId: active.workChatId,
    }),
  );
  await back();
  const currentApproval = await screen.findByRole("region", {
    name: "等待工具审批",
  });

  calls.activity.mockReturnValue([other, { ...active, hasPublicReply: true }]);
  calls.timeline.mockReturnValue({ messages: [...history, first] });
  mainConversationChanged(1);
  expect(await screen.findByText(first.content)).toBeVisible();
  expect(screen.queryByText("正在等待回复")).toBeNull();
  const firstRow = timelineRow(first.id);
  const head = within(firstRow);
  const partnerName = "伙伴 A";
  const workingAvatar = head.getByRole("button", sourceAction);
  const avatar = workingAvatar.querySelector("[data-companion-avatar]");
  expect(workingAvatar).toBeVisible();
  expect(workingAvatar).not.toHaveAccessibleDescription("工作中");
  expect(avatar).toHaveAttribute("data-activity", "idle");
  expect(avatar).toHaveAttribute("data-motion", "idle");
  const name = await head.findByText(partnerName);
  expect(name).toBeVisible();
  expect(firstRow.nextElementSibling).toBeNull();
  expect(currentApproval).toBeVisible();
  await userEvent.click(workingAvatar);
  expect(await screen.findByText(sourceText)).toBeVisible();
  await back();
  const resumedApproval = await screen.findByRole("region", {
    name: "等待工具审批",
  });

  calls.timeline.mockReturnValue({ messages: [...history, first, second] });
  mainConversationChanged(2);
  expect(await screen.findByText(second.content)).toBeVisible();
  expect(timelineRow(first.id)).toBe(firstRow);
  expect(head.getByRole("button", sourceAction)).toBe(workingAvatar);
  expect(head.getByText(partnerName)).toBe(name);
  expect(head.getByRole("button", { name: "回复" })).toBeInTheDocument();
  const secondRow = timelineRow(second.id);
  const tail = within(secondRow);
  expect(tail.queryByRole("button", sourceAction)).toBeNull();
  expect(tail.queryByText(partnerName)).toBeNull();
  expect(tail.queryByRole("button", { name: /定位原消息/ })).toBeNull();
  expect(tail.getByRole("button", { name: "回复" })).toBeInTheDocument();
  expect(secondRow).not.toHaveClass("-mt-1");
  expect(secondRow.parentElement).toHaveClass("gap-2");
  expect(secondRow.nextElementSibling).toBeNull();

  calls.activity.mockReturnValue([other]);
  mainConversationChanged(3);
  await waitFor(() => expect(avatar).toHaveAttribute("data-activity", "idle"));
  expect(avatar).toHaveAttribute("data-motion", "idle");
  expect(timelineRow(first.id)).toBe(firstRow);
  expect(head.getByRole("button", sourceAction)).toBe(workingAvatar);
  expect(workingAvatar.querySelector("[data-companion-avatar]")).toBe(avatar);
  expect(workingAvatar).not.toHaveAccessibleDescription("工作中");
  expect(head.getByText(partnerName)).toBe(name);
  expect(secondRow.nextElementSibling).toBeNull();
  expect(screen.getByRole("region", { name: "等待工具审批" })).toBe(
    resumedApproval,
  );
  expect(
    timelineRow(other.inputMessageId).compareDocumentPosition(resumedApproval),
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  expect(
    resumedApproval.compareDocumentPosition(timelineRow(active.inputMessageId)),
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  expect(tail.queryByRole("button", sourceAction)).toBeNull();
  expect(tail.queryByText(partnerName)).toBeNull();

  const next = turnInput("同一工作的下一轮交办", 9);
  calls.activity.mockReturnValue([
    other,
    { ...active, executionId: "next-execution", inputMessageId: next.id },
  ]);
  calls.timeline.mockReturnValue({
    messages: [...history, first, second, next],
  });
  mainConversationChanged(4);
  expect(await screen.findByText(next.content)).toBeVisible();
  expect(screen.getAllByText("正在等待回复")).toHaveLength(1);
  expect(head.getByRole("button", sourceAction)).toBe(workingAvatar);
  expect(head.getByText(partnerName)).toBeVisible();
  expect(avatar).toHaveAttribute("data-activity", "idle");
});

it("已发布答复的工作在本页没有发言时不占用独立的活动行", async () => {
  const input = turnInput("旧页中的交办", 1);
  const first = turnReply("旧页中的公开进展", 2, input.id);
  const later = turnInput("另一项工作的最新交办", 9, "work-c");
  calls.activity.mockReturnValue([
    {
      workChatId: "work-a",
      sourceWakerId: "w",
      executionId: "older-execution",
      inputMessageId: input.id,
      hasPublicReply: true,
    },
  ]);
  calls.timeline.mockImplementation((before?: number) =>
    before === later.sequence
      ? { messages: [input, first] }
      : {
          messages: [later],
          hasMore: true,
          nextBeforeSequence: later.sequence,
        },
  );
  show();
  expect(await screen.findByText(later.content)).toBeVisible();
  const sourceAction = { name: "打开工作会话" };
  expect(screen.queryByRole("button", sourceAction)).toBeNull();
  expect(timelineRow(later.id).nextElementSibling).toBeNull();
  expect(screen.queryByText(first.content)).toBeNull();
  expect(screen.queryByText(input.content)).toBeNull();
  expect(screen.queryByText("正在等待回复")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
  expect(await screen.findByText(first.content)).toBeVisible();
  const head = within(timelineRow(first.id));
  const activeAvatar = head.getByRole("button", sourceAction);
  expect(activeAvatar).toBeVisible();
  expect(activeAvatar.querySelector("[data-companion-avatar]")).toHaveAttribute(
    "data-activity",
    "idle",
  );
  expect(await head.findByText("伙伴 A")).toBeVisible();
  expect(timelineRow(later.id).nextElementSibling).toBeNull();
});

it("向前翻页后跨页合并连续身份，并保留其他回合和作者的头像", async () => {
  calls.timeline.mockImplementation((before?: number) =>
    before === 4
      ? {
          messages: [
            turnInput("较早交办", 1),
            turnReply("来源不明确的旧消息", 2),
            turnReply("上一页的同轮进展", 3, "较早交办"),
          ],
        }
      : {
          messages: [
            turnReply("当前页的同轮进展", 4, "较早交办"),
            turnReply(
              "另一位伙伴的消息",
              5,
              "较早交办",
              "work-a",
              "other-waker",
            ),
            turnReply("本轮最终消息", 6, "较早交办"),
            turnInput("另一次交办", 7),
            turnReply("另一次交办的答复", 8, "另一次交办"),
          ],
          hasMore: true,
          nextBeforeSequence: 4,
        },
  );
  show();
  await screen.findByText("当前页的同轮进展");
  const current = timelineRow("当前页的同轮进展");
  const sourceAction = { name: "打开工作会话" };
  expect(within(current).getByRole("button", sourceAction)).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
  await screen.findByText("上一页的同轮进展");
  expect(timelineRow("当前页的同轮进展")).toBe(current);
  expect(within(current).queryByRole("button", sourceAction)).toBeNull();
  for (const id of [
    "上一页的同轮进展",
    "来源不明确的旧消息",
    "另一位伙伴的消息",
    "本轮最终消息",
    "另一次交办的答复",
  ]) {
    expect(
      within(timelineRow(id)).getByRole("button", sourceAction),
    ).toBeVisible();
  }
});

it("另一窗口回答后，主会话能在待答数量相同时换成下一道题", async () => {
  calls.activity.mockReturnValue([
    {
      workChatId: work.id,
      executionId: "same-run",
      inputMessageId: "u-without-reply",
      hasPublicReply: false,
      waitingElicitation: true,
      sourceWakerId: "w",
    },
  ]);
  let question = "第一个问题";
  calls.source.mockImplementation(async () => ({
    data: {
      chat: {
        ...work,
        activeExecutionId: "same-run",
        pendingElicitations: [
          {
            elicitationId: question,
            message: question,
            mode: "form",
            requestedSchema: {
              type: "object",
              properties: { answer: { type: "string", title: question } },
              required: ["answer"],
            },
          },
        ],
      },
    },
  }));
  show();
  expect(
    await screen.findByRole("textbox", { name: "第一个问题" }),
  ).toBeVisible();
  question = "第二个问题";
  const listener = calls.subscribe.mock.calls.find(
    ([topic]) => topic === "main-conversations",
  )?.[2];
  if (!listener) throw Error("缺少主会话订阅");
  act(() =>
    listener.onEvent({
      protocolVersion: 1,
      epoch: "epoch",
      connectionId: "conn",
      deliverySequence: 1,
      topic: "main-conversations",
      type: "main-conversations.changed",
      resource: { kind: "waker", id: "w" },
      eventId: "replacement",
      payload: {},
    }),
  );
  expect(
    await screen.findByRole("textbox", { name: "第二个问题" }),
  ).toBeVisible();
  expect(screen.queryByRole("textbox", { name: "第一个问题" })).toBeNull();
  expect(calls.answer).not.toHaveBeenCalled();
});
