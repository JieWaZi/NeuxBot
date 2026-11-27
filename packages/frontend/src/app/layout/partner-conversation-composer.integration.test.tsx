import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkspaceAttentionOpen } from "@neuxbot/platform-contracts/services";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  mainConversationOptions,
  sourceHistoryOptions,
} from "@/modules/chat/api/main-conversation";
import { workspaceFileDragType } from "@/modules/chat/model/workspace-file-drag";
import { ApiError } from "@/shared/api/errors";
import { listWakersQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import type * as SDK from "@/shared/api/generated/sdk.gen";
import type {
  GetMainConversationData,
  GetMainConversationResponse,
} from "@/shared/api/generated/types.gen";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";
import type * as Platform from "@/shared/platform";
import { configurePlatform, platform } from "@/shared/platform/provider";
import { ThemeProvider } from "@/shared/theme/theme-provider";
import { TooltipProvider } from "@/shared/ui/tooltip";
import {
  ChatComposerProviders,
  seedChatComposerResources,
} from "@/test/chat-composer-fixture";
import { mockChatPresentation } from "@/test/chat-presentation-fixture";
import { AppShell } from "./app-shell";

type MainConversationRequest = Pick<GetMainConversationData, "path" | "query">;

const server = vi.hoisted(() => ({
  main: vi.fn<
    (
      request: MainConversationRequest,
    ) => Promise<{ data: GetMainConversationResponse }>
  >(),
  source: vi.fn(),
  mainReading: vi.fn(),
  markMainRead: vi.fn(),
  files: vi.fn(),
  resolveFile: vi.fn(),
  send: vi.fn(),
  executors: vi.fn(),
}));

vi.mock("@/shared/api/generated/sdk.gen", async (original) => ({
  ...(await original<typeof SDK>()),
  getMainConversation: server.main,
  getMainConversationSource: server.source,
  getMainConversationReading: server.mainReading,
  markMainConversationRead: server.markMainRead,
  sendMainConversationMessage: server.send,
  getExecutorAttention: server.executors,
}));
vi.mock("@/shared/platform", async (original) => ({
  ...(await original<typeof Platform>()),
  listChatWorkspaceFiles: server.files,
  resolveWorkspaceResourceReference: server.resolveFile,
}));

const subject = { subjectType: "waker", subjectId: "waker-1" } as const;
const mainKey = mainConversationOptions(subject).queryKey;

beforeEach(() => {
  server.main
    .mockReset()
    .mockResolvedValue({ data: { messages: [], activeWork: [] } });
  server.send.mockReset();
  server.source.mockReset();
  server.mainReading.mockReset();
  server.markMainRead.mockReset().mockResolvedValue({ data: {} });
  server.files
    .mockReset()
    .mockResolvedValue({ generated: [], removedPaths: [] });
  server.resolveFile.mockReset().mockResolvedValue({ status: "missing" });
  server.executors.mockReset().mockResolvedValue({ data: { resources: [] } });
  vi.stubGlobal("matchMedia", (media: string) => ({
    media,
    matches: false,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

it("主会话及工作记录沿用原标题栏的摘要与工作台开关", async () => {
  const user = userEvent.setup();
  const { client } = renderPartner();
  const workChatId = "authorized-work";
  const otherWorkChatId = "other-authorized-work";
  const attachmentPath = "attachments/message/doc.md";
  const source = {
    id: workChatId,
    wakerId: "waker-1",
    workspaceDirectory: "/authorized/work",
  };
  await act(async () => {
    client.setQueryData(mainKey, {
      pages: [
        {
          messages: [
            {
              id: "original-input",
              role: "user",
              sourceChatId: workChatId,
              attachments: [{ name: "第一份文档.md", path: attachmentPath }],
            },
            {
              id: "other-input",
              role: "user",
              sourceChatId: otherWorkChatId,
              attachments: [{ name: "第二份文档.md", path: attachmentPath }],
            },
            {
              id: "announcement",
              role: "assistant",
              content: "已发布处理结果",
              sourceChatId: workChatId,
              sourceWakerId: "waker-1",
            },
          ],
          activeWork: [],
        },
      ],
      pageParams: [undefined],
    });
    client.setQueryData(sourceHistoryOptions(subject, workChatId).queryKey, {
      pages: [
        {
          chat: source,
          resources: [{ id: "input", role: "user", content: "工作详情" }],
        },
      ],
      pageParams: [undefined],
    });
  });

  const closeWorkbench = async () => {
    await user.click(screen.getByTitle("关闭工作台"));
    await waitFor(() =>
      expect(screen.queryByRole("region", { name: "工作台" })).toBeNull(),
    );
  };
  const mainInput = await activeEditor();
  await user.type(mainInput, "主会话输入草稿");
  await user.click(await screen.findByRole("button", { name: "查看摘要" }));
  const overview = await screen.findByRole("dialog", { name: "查看摘要" });
  expect(
    within(overview).getByRole("button", { name: "第一份文档.md" }),
  ).toBeVisible();
  expect(
    within(overview).getByRole("button", { name: "第二份文档.md" }),
  ).toBeVisible();
  expect(server.files).not.toHaveBeenCalled();
  await user.click(within(overview).getByRole("button", { name: "查看全部" }));
  await user.click(
    await screen.findByRole("treeitem", { name: "第二份文档.md" }),
  );
  await waitFor(() =>
    expect(server.resolveFile).toHaveBeenCalledWith(
      otherWorkChatId,
      attachmentPath,
      "",
    ),
  );
  await closeWorkbench();
  await user.click(screen.getByRole("button", { name: "工作区" }));
  const reopenedWorkbench = await screen.findByRole("region", {
    name: "工作台",
  });
  expect(
    await within(reopenedWorkbench).findByRole("treeitem", {
      name: "第二份文档.md",
    }),
  ).toBeVisible();
  await closeWorkbench();
  expect(await activeEditor()).toBe(mainInput);
  expect(mainInput).toHaveValue("主会话输入草稿");

  const announcement = screen
    .getByText("已发布处理结果")
    .closest("[data-message-id]");
  if (!(announcement instanceof HTMLElement)) throw Error("缺少已公开发言");
  await user.click(
    within(announcement).getByRole("button", { name: /工作记录/ }),
  );
  expect(await screen.findByText("工作详情")).toBeVisible();
  const sourceInput = await activeEditor();
  await user.type(sourceInput, "来源工作输入草稿");
  const sourceSummary = screen.getByRole("button", { name: "查看摘要" });
  await user.click(sourceSummary);
  await waitFor(() =>
    expect(server.files).toHaveBeenCalledWith(
      workChatId,
      source.workspaceDirectory,
    ),
  );
  await user.keyboard("{Escape}");
  await waitFor(() => expect(sourceSummary).toHaveFocus());
  await user.click(screen.getByRole("button", { name: "工作区" }));
  expect(await screen.findByRole("region", { name: "工作台" })).toBeVisible();
  await closeWorkbench();
  expect(await activeEditor()).toBe(sourceInput);
  expect(sourceInput).toHaveValue("来源工作输入草稿");
  expect(screen.getByText("工作详情")).toBeVisible();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function renderPartner(workChatId?: string, includeSecondPartner = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  seedChatComposerResources(client, "waker-1", "伙伴一");
  if (includeSecondPartner) {
    seedChatComposerResources(client, "waker-2", "伙伴二");
    client.setQueryData(
      listWakersQueryKey({ query: { pageNum: 1, pageSize: 100 } }),
      {
        resources: [
          { id: "waker-1", name: "伙伴一" },
          { id: "waker-2", name: "伙伴二" },
        ],
      },
    );
  }
  client.setQueryData(mainKey, {
    pages: [{ messages: [], activeWork: [] }],
    pageParams: [undefined],
  });
  const router = createMemoryRouter([{ path: "*", element: <AppShell /> }], {
    initialEntries: [
      mainConversationHref({
        kind: subject.subjectType,
        id: subject.subjectId,
        workChatId,
      }),
    ],
  });
  const view = render(
    <QueryClientProvider client={client}>
      <ChatComposerProviders>
        <ThemeProvider>
          <TooltipProvider>
            <RouterProvider router={router} />
          </TooltipProvider>
        </ThemeProvider>
      </ChatComposerProviders>
    </QueryClientProvider>,
  );
  return { client, router, ...view };
}

async function activeEditor() {
  const scene = document.querySelector<HTMLElement>(
    '[data-route-scene-active="true"]',
  );
  if (!scene) throw Error("找不到当前聊天");
  const editor = await within(scene).findByRole("textbox", {
    name: "输入消息",
  });
  expect(editor).toHaveAttribute("contenteditable", "true");
  return editor;
}

it.each(["主会话", "工作记录"])(
  "%s正文网页链接在右侧工作台打开",
  async (view) => {
    const user = userEvent.setup();
    const url = "https://example.com/report?tab=details#summary";
    const linkName = "交付报告";
    const content = `请查看[${linkName}](${url})。`;
    const publicationURL = "https://example.com/report/published";
    const publicationName = "已公开的核验结果";
    const workChatId = view === "工作记录" ? "work-link" : undefined;
    if (workChatId) {
      server.source.mockResolvedValue({
        data: {
          chat: { id: workChatId, wakerId: subject.subjectId },
          resources: [
            {
              id: "work-link-message",
              role: "assistant",
              status: "completed",
              createdSequence: 1,
              displayParts: [{ id: "body", type: "text", text: content }],
            },
            {
              id: "work-link-publication",
              executionId: "published-run",
              role: "assistant",
              status: "completed",
              createdSequence: 2,
              displayParts: [{ id: "summary", type: "text", text: "已发布。" }],
            },
          ],
          publishedMessages: [
            {
              id: "published-link",
              executionId: "published-run",
              content: `本轮发言见[${publicationName}](${publicationURL})。`,
            },
          ],
        },
      });
    }
    const { client } = renderPartner(workChatId);
    if (!workChatId) {
      act(() => {
        client.setQueryData(mainKey, {
          pages: [
            {
              messages: [
                { id: "public-link", role: "assistant", content, sequence: 1 },
              ],
              activeWork: [],
            },
          ],
          pageParams: [undefined],
        });
      });
    }
    const openSystem = vi.spyOn(window, "open").mockReturnValue(null);
    try {
      const link = await screen.findByRole("link", { name: linkName });
      const editor = await activeEditor();
      const draft = "打开报告时保留的草稿";
      await user.type(editor, draft);
      await user.click(link);
      expect(openSystem).not.toHaveBeenCalled();
      const workbench = await screen.findByRole("region", { name: "工作台" });
      const browser = within(workbench).getByRole("region", { name: "浏览器" });
      expect(browser).toBeVisible();
      expect(
        within(browser).getByRole("textbox", { name: "浏览器地址" }),
      ).toHaveValue(url);
      expect(link).toBeVisible();

      await user.click(screen.getByTitle("关闭工作台"));
      await waitFor(() =>
        expect(screen.queryByRole("region", { name: "工作台" })).toBeNull(),
      );
      await user.click(screen.getByRole("button", { name: "工作区" }));
      const reopened = await screen.findByRole("region", { name: "工作台" });
      expect(
        within(reopened).getByRole("textbox", { name: "浏览器地址" }),
      ).toHaveValue(url);
      expect(await activeEditor()).toHaveValue(draft);
      expect(openSystem).not.toHaveBeenCalled();

      if (workChatId) {
        await user.click(screen.getByRole("link", { name: publicationName }));
        expect(
          within(reopened).getByRole("textbox", { name: "浏览器地址" }),
        ).toHaveValue(publicationURL);
        expect(openSystem).not.toHaveBeenCalled();
      }
    } finally {
      openSystem.mockRestore();
    }
  },
);

it("桌面待办导航打开公开主会话的请求但没有已读打开意图", async () => {
  const previous = platform();
  let open: ((target: WorkspaceAttentionOpen) => void) | undefined;
  configurePlatform({
    ...previous,
    host: {
      ...previous.host,
      onWorkspaceAttentionOpen(callback) {
        open = callback;
        return () => {
          open = undefined;
        };
      },
    },
  });
  const view = renderPartner(undefined, true);
  try {
    await waitFor(() => expect(open).toBeTypeOf("function"));
    await act(async () =>
      open?.({
        work: { kind: "main_conversation", id: "public-conversation" },
        operation: "request_opened",
        wakerId: "waker-2",
        chatId: "real-hidden-work",
        requestId: "approval:1",
        unreadGeneration: 10,
      }),
    );
    expect(view.router.state.location).toMatchObject({
      pathname: "/chat/main/waker/waker-2",
      search: "?request=approval%3A1&requestChat=real-hidden-work",
      state: null,
    });
    expect(server.markMainRead).not.toHaveBeenCalled();
  } finally {
    view.unmount();
    configurePlatform(previous);
  }
});

it("冷窗口从伙伴未读提示打开主会话后，实际读到公开发言才解除未读保留", async () => {
  const conversationId = "colleague-main";
  const target = { subjectType: "waker", subjectId: "waker-2" };
  const report = {
    id: "new-public-report",
    sequence: 11,
    role: "assistant",
    content: "离开后收到的公开结果",
    readable: true,
  };
  const generation = 8;
  server.executors.mockResolvedValue({
    data: {
      resources: [
        {
          executorId: target.subjectId,
          unreadMessageCount: 1,
          mainConversationUnreadGeneration: generation,
        },
      ],
    },
  });
  server.main.mockImplementation(async ({ path }) => ({
    data:
      path.subjectId === target.subjectId
        ? { id: conversationId, messages: [report] }
        : { messages: [] },
  }));
  server.mainReading.mockResolvedValue({
    data: {
      complete: true,
      conversationId,
      throughSequence: 10,
      unreadGeneration: generation,
      holdUnread: true,
      unreadMessageCount: 1,
      firstUnreadMessageID: report.id,
      latestReadableMessageID: report.id,
    },
  });
  const presentation = mockChatPresentation();
  const view = renderPartner(undefined, true);
  try {
    const partner = await screen.findByRole("button", {
      name: /^伙伴二，.*未读/,
    });
    expect(server.mainReading).not.toHaveBeenCalled();
    await userEvent.click(partner);
    expect(await screen.findByText(report.content)).toBeVisible();
    await waitFor(() =>
      expect(server.mainReading).toHaveBeenCalledWith(
        expect.objectContaining({ path: target }),
      ),
    );
    expect(server.markMainRead).not.toHaveBeenCalled();
    await waitFor(() => {
      presentation.present(report.id);
      expect(server.markMainRead).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          path: target,
          body: {
            intent: "explicit",
            observedUnreadGeneration: generation,
            throughMessageID: report.id,
          },
        }),
      );
    });
  } finally {
    view.unmount();
    presentation.restore();
  }
});

it("切换伙伴时结束旧引用定位，保留两边草稿和当前伙伴的阅读位置", async () => {
  const cursor = 60;
  const reply = {
    id: "pending-reply",
    role: "user",
    content: "旧会话中的引用",
    sequence: cursor,
    replyToMessageId: "older-original",
  };
  const other = {
    id: "other-message",
    role: "user",
    content: "新会话仍在阅读的消息",
    sequence: 1,
  };
  let releasePage = () => {};
  const earlierPage = new Promise<void>((resolve) => {
    releasePage = resolve;
  });
  server.main.mockImplementation(async ({ path, query }) => {
    if (path.subjectId === "waker-2") return { data: { messages: [other] } };
    if (query?.beforeSequence === cursor) {
      await earlierPage;
      return {
        data: {
          messages: [
            {
              id: "intermediate",
              role: "assistant",
              content: "定位过程尚未找到原话",
              sequence: 30,
            },
          ],
          hasMore: true,
          nextBeforeSequence: 30,
        },
      };
    }
    if (query?.beforeSequence !== undefined)
      throw new Error("离开的伙伴不应继续翻页");
    return {
      data: { messages: [reply], hasMore: true, nextBeforeSequence: cursor },
    };
  });
  const user = userEvent.setup();
  const { client } = renderPartner(undefined, true);
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  try {
    await act(async () => {
      await client.invalidateQueries({ queryKey: mainKey });
    });
    const originalEditor = await activeEditor();
    await user.type(originalEditor, "旧会话的草稿");
    const row = (await screen.findByText(reply.content)).closest<HTMLElement>(
      "[data-message-id]",
    );
    if (!row) throw new Error("缺少带引用的公开发言");
    jump.mockClear();
    await user.click(within(row).getByRole("button", { name: /定位原消息/ }));
    await waitFor(() =>
      expect(server.main).toHaveBeenCalledWith(
        expect.objectContaining({
          path: subject,
          query: expect.objectContaining({ beforeSequence: cursor }),
        }),
      ),
    );

    const sidebar = screen.getByRole("navigation", { name: "伙伴" });
    await user.click(within(sidebar).getByRole("button", { name: "伙伴二" }));
    const visible = await screen.findByText(other.content);
    expect(visible).toBeVisible();
    expect(row).not.toBeVisible();
    const nextEditor = await activeEditor();
    expect(nextEditor).toHaveValue("");
    await user.type(nextEditor, "另一份草稿");
    const viewport = visible.closest<HTMLElement>(
      "[data-chat-scroll-viewport]",
    );
    if (!viewport) throw new Error("缺少当前伙伴的滚动区");
    Object.defineProperties(viewport, {
      scrollHeight: { value: 1200 },
      clientHeight: { value: 300 },
    });
    viewport.scrollTop = 240;
    fireEvent.scroll(viewport);

    await act(async () => releasePage());
    await waitFor(() =>
      expect(client.isFetching({ queryKey: mainKey })).toBe(0),
    );
    expect(viewport.scrollTop).toBe(240);
    expect(nextEditor).toHaveValue("另一份草稿");
    expect(visible).toBeVisible();
    expect(jump).not.toHaveBeenCalled();

    await user.click(within(sidebar).getByRole("button", { name: "伙伴一" }));
    expect(await activeEditor()).toBe(originalEditor);
    expect(originalEditor).toHaveValue("旧会话的草稿");
    expect(row).toBeVisible();
    expect(screen.queryByText("正在定位原消息…")).toBeNull();
    expect(
      server.main.mock.calls.flatMap(([request]) =>
        request.query?.beforeSequence === undefined
          ? []
          : [[request.path.subjectId, request.query.beforeSequence]],
      ),
    ).toEqual([["waker-1", cursor]]);
    expect(jump).not.toHaveBeenCalled();
  } finally {
    releasePage();
    jump.mockRestore();
  }
});

it("工作记录输入框只接收同一来源工作台拖入的文件", async () => {
  const workChatId = "source-drag";
  const rootPath = "/authorized/work";
  const path = "report.md";
  const canonicalPath = rootPath + "/" + path;
  server.source.mockResolvedValue({
    data: {
      chat: {
        id: workChatId,
        wakerId: subject.subjectId,
        workspaceDirectory: rootPath,
      },
      resources: [],
    },
  });
  server.resolveFile.mockResolvedValue({
    status: "resolved",
    canonicalPath,
    baseDirectory: rootPath,
    workspaceRoot: rootPath,
    sourceRoots: [],
  });
  renderPartner(workChatId);
  const editor = await activeEditor();
  const dropzone = editor.closest("[data-chat-composer-dropzone]");
  if (!dropzone) throw Error("缺少聊天文件拖放区域");
  const drop = (chatId: string) =>
    fireEvent.drop(dropzone, {
      dataTransfer: {
        types: [workspaceFileDragType],
        getData: () => JSON.stringify({ chatId, rootPath, path }),
      },
    });

  drop("other-work-with-the-same-directory");
  expect(server.resolveFile).not.toHaveBeenCalled();
  drop(workChatId);
  await waitFor(() =>
    expect(server.resolveFile).toHaveBeenCalledExactlyOnceWith(
      workChatId,
      path,
      rootPath,
    ),
  );
  await waitFor(() =>
    expect(
      editor.querySelector("[data-chat-composer-code-reference]"),
    ).toHaveAttribute("data-chat-composer-code-reference", canonicalPath),
  );
});

it("工作记录的前一页暂时不可用时保留已打开的来源摘要", async () => {
  const workChatId = "source-paging";
  const rootPath = "/authorized/work";
  const cursor = 15;
  server.source
    .mockResolvedValueOnce({
      data: {
        chat: {
          id: workChatId,
          wakerId: subject.subjectId,
          workspaceDirectory: rootPath,
        },
        resources: [
          {
            id: "source-new",
            role: "user",
            content: "这一页已经读取",
            createdSequence: cursor,
            attachments: [
              {
                name: "分页前已在工作台.md",
                path: "attachments/record/doc.md",
              },
            ],
          },
        ],
        hasMore: true,
        nextBeforeSequence: cursor,
      },
    })
    .mockRejectedValue(new ApiError("前页暂时不可用", "TEMPORARY", 503));
  const user = userEvent.setup();
  const { client } = renderPartner(workChatId);
  expect(await screen.findByText("这一页已经读取")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  const summary = await screen.findByRole("dialog", { name: "查看摘要" });
  expect(
    within(summary).getByRole("button", { name: "分页前已在工作台.md" }),
  ).toBeVisible();
  expect(server.files).toHaveBeenCalledWith(workChatId, rootPath);

  fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
  await waitFor(() =>
    expect(
      client.getQueryState(sourceHistoryOptions(subject, workChatId).queryKey)
        ?.status,
    ).toBe("error"),
  );
  expect(server.source.mock.lastCall?.[0].query).toEqual({
    beforeSequence: cursor,
    limit: 50,
  });
  expect(summary).toBeVisible();
  expect(
    within(summary).getByRole("button", { name: "分页前已在工作台.md" }),
  ).toBeVisible();
});

it.each(["主会话", "头像打开的工作记录"])(
  "%s 使用普通聊天的紧凑底栏",
  async (view) => {
    const { client } = renderPartner();
    if (view === "头像打开的工作记录") {
      const workChatId = "source-composer-work";
      await act(async () => {
        client.setQueryData(mainKey, {
          pages: [
            {
              messages: [
                {
                  id: "source-composer-message",
                  sequence: 1,
                  role: "assistant",
                  content: "工作结果已发布",
                  sourceChatId: workChatId,
                  sourceWakerId: "waker-1",
                },
              ],
              activeWork: [],
            },
          ],
          pageParams: [undefined],
        });
        client.setQueryData(
          sourceHistoryOptions(subject, workChatId).queryKey,
          {
            pages: [
              {
                chat: {
                  id: workChatId,
                  wakerId: "waker-1",
                  workspaceDirectory: "/authorized/source-work",
                  projectName: "本次工作目录",
                },
                resources: [{ id: "u1", role: "user", content: "原始交办" }],
              },
            ],
            pageParams: [undefined],
          },
        );
      });
      await userEvent
        .setup()
        .click(await screen.findByRole("button", { name: /工作记录/ }));
      expect(await screen.findByText("原始交办")).toBeVisible();
    }

    const editor = await activeEditor();
    const composer = editor.closest<HTMLElement>("[data-chat-composer]");
    if (!composer) throw Error("缺少聊天输入区");
    expect(
      composer.querySelector("[data-chat-composer-context-strip]"),
    ).toBeNull();
    expect(editor.closest("[data-chat-composer-input-body]")).toHaveAttribute(
      "data-composer-compact",
      "true",
    );
    const controls = within(composer);
    const attachment = controls.getByRole("button", { name: "添加附件" });
    const permission = controls.getByRole("button", { name: /^执行权限：/ });
    const model = controls.getByRole("button", { name: "选择模型 · GPT-5" });
    const send = controls.getByRole("button", { name: "发送消息" });
    expect(attachment.parentElement?.parentElement).toBe(
      send.parentElement?.parentElement,
    );
    expect(permission.nextElementSibling).toBe(model);
    expect(model.nextElementSibling).toBe(send);
    expect(permission).toHaveTextContent(/^$/);
    expect(model.childNodes).toHaveLength(1);
    expect(model.firstElementChild?.matches("svg, img")).toBe(true);
  },
);

it("主会话与加号打开的普通草稿使用聊天编辑器，各自保留未发正文", async () => {
  const { router } = renderPartner();
  const user = userEvent.setup();
  const main = await activeEditor();
  await user.type(main, "主会话尚未发出");
  await user.click(screen.getByRole("button", { name: "新建会话" }));
  const draftLocation = router.state.location;
  expect(draftLocation.pathname).toBe("/chat");
  expect(new URLSearchParams(draftLocation.search).get("draftId")).toBeTruthy();
  const draft = await activeEditor();
  expect(draft).toHaveValue("");
  await user.type(draft, "普通对话尚未发出");
  await user.click(screen.getByRole("tab", { name: "伙伴一" }));
  expect(await activeEditor()).toHaveValue("主会话尚未发出");
  await user.click(screen.getByRole("button", { name: "新建会话" }));
  await waitFor(() =>
    expect(router.state.location.search).toBe(draftLocation.search),
  );
  expect(await activeEditor()).toHaveValue("普通对话尚未发出");
});

it("主会话发布第一段发言后收起等待文字，固定页签保持伙伴身份", async () => {
  const user = userEvent.setup();
  let accept = () => {};
  const accepted = new Promise<void>((resolve) => {
    accept = resolve;
  });
  const input = {
    id: "u1",
    conversationId: "main-waker-1",
    sequence: 1,
    role: "user",
    content: "请核对结果",
    sourceChatId: "work-1",
  };
  const conversation = {
    id: input.conversationId,
    subjectType: "waker",
    subjectId: subject.subjectId,
  };
  const finding = {
    id: "a1",
    sequence: 2,
    role: "assistant",
    content: "发现兼容性检查失败，请先暂停合并。",
    sourceChatId: "work-1",
    sourceWakerId: subject.subjectId,
    replyToMessageId: input.id,
  };
  const active = {
    workChatId: input.sourceChatId,
    executionId: "run-1",
    sourceWakerId: subject.subjectId,
    inputMessageId: input.id,
    hasPublicReply: false,
  };
  const activeWork = [active];
  server.send.mockImplementation(async () => {
    await accepted;
    return {
      data: {
        executionId: "run-1",
        message: input,
      },
    };
  });
  server.main.mockResolvedValue({
    data: { ...conversation, messages: [input], activeWork },
  });
  const { client } = renderPartner();
  const pinned = await screen.findByRole("tab", { name: "伙伴一" });
  expect(pinned).toHaveAccessibleDescription("主会话");
  await user.type(await activeEditor(), "请核对结果");
  const send = screen.getByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(server.send).toHaveBeenCalledOnce());
  const wait = await screen.findByText("正在等待回复");
  expect(wait.closest('[role="status"]')).toBeVisible();
  expect(wait.closest(".sr-only")).toBeNull();
  expect(wait).toHaveAttribute("data-text-shimmer", "true");
  expect(wait.closest("[data-running-shimmer]")).toBeNull();
  expect(pinned.querySelector("[data-native-attention]")).toBeNull();
  expect(pinned).toHaveTextContent(/^伙伴一$/);
  expect(pinned).toHaveAccessibleDescription("主会话");
  await act(async () => {
    accept();
  });
  await waitFor(() => expect(client.isMutating()).toBe(0));
  expect(client.getQueryData(mainKey)).toMatchObject({
    pages: [{ activeWork }],
  });
  expect(screen.getByText("正在等待回复")).toBeVisible();
  expect(pinned).toHaveTextContent(/^伙伴一$/);
  expect(pinned).toHaveAccessibleDescription("主会话");
  server.main.mockResolvedValue({
    data: {
      ...conversation,
      messages: [input, finding],
      activeWork: [{ ...active, hasPublicReply: true }],
    },
  });
  await act(async () => {
    await client.invalidateQueries({ queryKey: mainKey });
  });
  const first = await screen.findByText(finding.content);
  expect(first).toBeVisible();
  expect(screen.queryByText("正在等待回复")).toBeNull();
  expect(screen.queryByText("已核对结果")).toBeNull();

  server.main.mockResolvedValue({
    data: {
      ...conversation,
      messages: [
        input,
        finding,
        {
          ...finding,
          id: "a2",
          sequence: 3,
          content: "已核对结果",
        },
      ],
      activeWork: [],
    },
  });
  await act(async () => {
    await client.invalidateQueries({ queryKey: mainKey });
  });
  const second = await screen.findByText("已核对结果");
  expect(second).toBeVisible();
  expect(screen.getAllByText(finding.content)).toHaveLength(1);
  expect(first.compareDocumentPosition(second)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  await waitFor(() => expect(screen.queryByText("正在等待回复")).toBeNull());
  expect(pinned).toHaveAccessibleDescription("主会话");
});

it("伙伴的其他工作在侧栏显示，固定页签仍只显示伙伴身份", async () => {
  server.executors.mockResolvedValue({
    data: { resources: [{ executorId: "waker-1", runningChatCount: 1 }] },
  });
  renderPartner();
  expect(
    await screen.findByRole("button", { name: "伙伴一，工作中" }),
  ).toBeVisible();
  const tab = screen.getByRole("tab", { name: "伙伴一" });
  expect(tab).toHaveTextContent(/^伙伴一$/);
  expect(tab).toHaveAccessibleDescription("主会话");
  expect(screen.queryByText("正在等待回复")).toBeNull();
});

it("顶栏后退保留主会话草稿，前进返回先前通过头像打开的工作", async () => {
  const user = userEvent.setup();
  server.source.mockResolvedValue({
    data: {
      chat: {
        id: "work-b",
        activeExecutionId: "run-b",
        pendingApprovals: [{ approvalId: "approval-b", kind: "execute" }],
      },
    },
  });
  const { client, router } = renderPartner();
  const publicRow = {
    id: "published",
    role: "assistant",
    content: "公开发言",
    sequence: 1,
    sourceChatId: "work-a",
    sourceWakerId: "waker-1",
  };
  await act(async () => {
    client.setQueryData(mainKey, {
      pages: [
        {
          messages: [publicRow],
          activeWork: [
            {
              workChatId: "work-b",
              executionId: "run-b",
              sourceWakerId: "waker-1",
              inputMessageId: "older-work-b-input",
              hasPublicReply: false,
              waitingApproval: true,
            },
          ],
        },
      ],
      pageParams: [undefined],
    });
    for (const id of ["work-a", "work-b"]) {
      client.setQueryData(sourceHistoryOptions(subject, id).queryKey, {
        pages: [
          {
            chat: { id, wakerId: "waker-1" },
            resources: [
              {
                id: "input-" + id,
                role: "user",
                content: "工作 " + id,
                createdSequence: 1,
              },
            ],
          },
        ],
        pageParams: [undefined],
      });
    }
  });
  const mainInput = await activeEditor();
  await user.type(mainInput, "还没发的公开交办");
  const publicMessage = (await screen.findByText("公开发言")).closest(
    "[data-message-id]",
  );
  if (!(publicMessage instanceof HTMLElement)) throw Error("缺少公开消息");
  const viewport = publicMessage.closest<HTMLElement>(
    "[data-chat-scroll-viewport]",
  );
  if (!viewport) throw Error("缺少主会话滚动区");
  viewport.scrollTop = 165;
  await user.click(
    within(publicMessage).getByRole("button", { name: /工作记录/ }),
  );
  expect(await screen.findByText("工作 work-a")).toBeVisible();
  expect(screen.queryByRole("button", { name: "返回主会话" })).toBeNull();
  expect(screen.queryByRole("heading", { name: "工作记录" })).toBeNull();
  const back = screen.getByRole("button", { name: "后退" });
  const forward = screen.getByRole("button", { name: "前进" });
  expect(back).toBeEnabled();
  await user.click(back);
  expect(await activeEditor()).toBe(mainInput);
  expect(mainInput).toHaveValue("还没发的公开交办");
  expect(viewport.scrollTop).toBe(165);
  await user.click(forward);
  expect(await screen.findByText("工作 work-a")).toBeVisible();
  await user.click(back);
  const waitingRow = (
    await screen.findByRole("region", { name: "等待工具审批" })
  ).closest<HTMLElement>("[data-role=assistant]");
  if (!waitingRow) throw Error("缺少等待审批的来源行");
  const tab = screen.getByRole("tab", { name: "伙伴一" });
  expect(tab).toHaveTextContent(/^伙伴一$/);
  expect(tab).toHaveAccessibleDescription("主会话");
  await user.click(
    within(waitingRow).getByRole("button", { name: /工作记录/ }),
  );
  expect(await screen.findByText("工作 work-b")).toBeVisible();
  expect(forward).toBeDisabled();
  await user.click(back);
  await user.click(forward);
  expect(await screen.findByText("工作 work-b")).toBeVisible();
  expect(router.state.location.pathname).toBe("/chat/main/waker/waker-1");
  const pinned = screen.getByRole("tab", { name: "伙伴一" });
  await user.click(pinned);
  await waitFor(() => expect(router.state.location.search).toBe(""));
  expect(await activeEditor()).toBe(mainInput);
  const homeLocation = router.state.location.key;
  await user.click(pinned);
  expect(router.state.location.key).toBe(homeLocation);
  await user.click(back);
  expect(await screen.findByText("工作 work-b")).toBeVisible();
});
