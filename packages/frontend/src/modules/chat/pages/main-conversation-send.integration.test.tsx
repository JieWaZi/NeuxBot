import { createHash, webcrypto } from "node:crypto";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import type {
  GetMainConversationResponse,
  SendMainConversationMessageData,
  SendMainConversationMessageResponse,
} from "@/shared/api/generated/types.gen";
import type * as GlobalEvents from "@/shared/api/global-events";
import { conversationReadingOptions } from "@/shared/api/reading-queries";
import { initializeApi } from "@/shared/api/runtime";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import {
  ChatComposerProviders,
  seedChatComposerResources,
} from "@/test/chat-composer-fixture";
import { mockChatPresentation } from "@/test/chat-presentation-fixture";
import { MainConversationPage } from "./main-conversation-page";

const subscribe = vi.hoisted(() =>
  vi.fn<typeof GlobalEvents.subscribeGlobalEvents>(),
);
vi.mock("@/shared/api/global-events", async (original) => ({
  ...(await original<typeof GlobalEvents>()),
  subscribeGlobalEvents: subscribe,
}));

const origin = "http://127.0.0.1:8080";
const subjectId = "a".repeat(32);
const conversationId = "b".repeat(32);
const workChatId = "c".repeat(32);
const messageText = "请检查今天的交付结果";
const path = `${origin}/api/v1/main-conversations/waker/${subjectId}`;
const emptyTimeline = {
  id: conversationId,
  subjectType: "waker",
  subjectId,
  messages: [],
  activeWork: [],
  hasMore: false,
} satisfies GetMainConversationResponse;
const server = setupServer(
  http.get(path, () => HttpResponse.json(emptyTimeline)),
  http.get(`${origin}/api/v1/wakers/${subjectId}`, () =>
    HttpResponse.json({ id: subjectId, name: "交付伙伴" }),
  ),
  http.get(`${origin}/api/v1/wakers/${subjectId}/projects`, () =>
    HttpResponse.json({ resources: [] }),
  ),
  http.get(`${origin}/api/v1/ai-accounts`, () =>
    HttpResponse.json({ resources: [] }),
  ),
);
const originalScrollTo = HTMLElement.prototype.scrollTo;
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
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
beforeEach(async () => {
  subscribe.mockReset().mockReturnValue(() => {});
  await initializeApi();
});
afterEach(() => server.resetHandlers());
afterAll(() => {
  server.close();
  vi.unstubAllGlobals();
  HTMLElement.prototype.scrollTo = originalScrollTo;
});

function deferred<T = undefined>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function committedInput(submissionId: string) {
  // Core derives the same public identity for each retry of this submission.
  const id = createHash("sha256")
    .update(JSON.stringify(["user", conversationId, submissionId]))
    .digest("hex")
    .slice(0, 32);
  return {
    id,
    conversationId,
    sequence: 1,
    role: "user",
    content: messageText,
    sourceChatId: workChatId,
    createdAt: 1795650000,
  };
}

function mainConversationChanged() {
  const resource = { kind: "waker", id: subjectId } as const;
  const listener = subscribe.mock.calls.find(
    ([topic, scope]) =>
      topic === "main-conversations" &&
      scope.kind === resource.kind &&
      scope.id === resource.id,
  )?.[2];
  if (!listener) throw new Error("主会话未订阅此伙伴");
  act(() =>
    listener.onEvent({
      protocolVersion: 1,
      epoch: "test",
      connectionId: "test",
      deliverySequence: 1,
      topic: "main-conversations",
      type: "main-conversations.changed",
      resource,
      eventId: "1",
      payload: {},
    }),
  );
}

function mountMainConversation(search = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  seedChatComposerResources(client, subjectId, "交付伙伴");
  const view = render(
    <QueryClientProvider client={client}>
      <ChatComposerProviders>
        <WorkspaceProvider>
          <TooltipProvider>
            <MemoryRouter
              initialEntries={[`/chat/main/waker/${subjectId}${search}`]}
            >
              <Routes>
                <Route
                  path="/chat/main/:subjectType/:subjectId"
                  element={<MainConversationPage />}
                />
              </Routes>
            </MemoryRouter>
          </TooltipProvider>
        </WorkspaceProvider>
      </ChatComposerProviders>
    </QueryClientProvider>,
  );
  return { client, ...view };
}

async function sendFirstMessage() {
  const { client, container } = mountMainConversation();
  const column = container.querySelector<HTMLElement>(
    "[data-chat-message-column]",
  );
  if (!column) throw new Error("缺少主会话消息区");
  const editor = screen.getByRole("textbox", { name: "输入消息" });
  const button = screen.getByRole("button", { name: "发送消息" });
  const user = userEvent.setup();
  await user.type(editor, messageText);
  await waitFor(() => expect(button).toBeEnabled());
  expect(within(column).queryByText(messageText)).toBeNull();
  await user.click(button);
  return { client, timeline: within(column), editor };
}

function expectSingleMessage(timeline: ReturnType<typeof within>) {
  const matches = timeline.queryAllByText(messageText);
  expect(matches).toHaveLength(1);
  const bubble = matches[0]?.closest(
    "[data-role=user] [data-chat-message-bubble]",
  );
  if (!bubble) throw new Error("正文没有出现在用户气泡内");
  expect(bubble).toBeVisible();
  return bubble;
}

function expectMessageBeforeWait(timeline: ReturnType<typeof within>) {
  const bubble = expectSingleMessage(timeline);
  const wait = timeline.getByText("正在等待回复");
  expect(
    bubble.compareDocumentPosition(wait) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
}

it("只将聚焦窗口中已经呈现的主会话公开正文标为已读", async () => {
  const own = {
    id: "own",
    sequence: 1,
    role: "user",
    content: "用户自己的提问",
    sourceChatId: workChatId,
  };
  const first = {
    id: "first-report",
    sequence: 2,
    role: "assistant",
    content: "第一条可读公开回报",
    readable: true,
  };
  const next = {
    id: "next-report",
    sequence: 3,
    role: "assistant",
    content: "下一条可读公开回报",
    readable: true,
  };
  const write = vi.fn();
  const read = vi.fn();
  let messages = [own, first];
  let throughSequence = 0;
  let unreadGeneration = 4;
  let holdUnread = false;
  server.use(
    http.get(path, () =>
      HttpResponse.json({
        ...emptyTimeline,
        messages,
        activeWork: [
          {
            workChatId,
            executionId: "pending-run",
            sourceWakerId: subjectId,
            inputMessageId: own.id,
            hasPublicReply: true,
            waitingApproval: true,
          },
        ],
      }),
    ),
    http.get(path + "/sources/" + workChatId + "/messages", () =>
      HttpResponse.json({
        chat: {
          id: workChatId,
          wakerId: subjectId,
          activeExecutionId: "pending-run",
          pendingApprovals: [
            {
              approvalId: "approval",
              kind: "execute",
              message: "批准后继续后台工作",
              options: [{ id: "allow", name: "允许" }],
            },
          ],
        },
        resources: [],
      }),
    ),
    http.get(path + "/reading", () => {
      read();
      return HttpResponse.json({
        conversationId,
        throughSequence,
        unreadGeneration,
        holdUnread,
        latestReadableMessageID: messages.at(-1)?.id,
        complete: true,
        epoch: "test",
      });
    }),
    http.post(path + "/reading", async ({ request }) => {
      const body = (await request.json()) as { throughMessageID?: string };
      write(body);
      throughSequence = body.throughMessageID === first.id ? 2 : 3;
      return HttpResponse.json({});
    }),
  );
  const presentation = mockChatPresentation(false);
  const { focus, present } = presentation;
  const view = mountMainConversation();
  try {
    expect(await screen.findByText(first.content)).toBeVisible();
    const approval = await screen.findByRole("region", {
      name: "等待工具审批",
    });
    expect(approval).toBeVisible();
    await waitFor(() => expect(read).toHaveBeenCalledOnce());
    present(first.id);
    expect(write).not.toHaveBeenCalled();
    focus.mockReturnValue(true);
    act(() => window.dispatchEvent(new Event("focus")));
    await waitFor(() =>
      expect(write).toHaveBeenCalledExactlyOnceWith({
        intent: "automatic",
        observedUnreadGeneration: 4,
        throughMessageID: first.id,
      }),
    );
    present(own.id);
    expect(write).toHaveBeenCalledOnce();
    messages = [...messages, next];
    mainConversationChanged();
    expect(await screen.findByText(next.content)).toBeVisible();
    expect(write).toHaveBeenCalledOnce();

    const resource = { kind: "main_conversation", id: conversationId } as const;
    const observer = subscribe.mock.calls.find(
      ([topic, scope]) =>
        topic === "reading" &&
        scope.kind === resource.kind &&
        scope.id === resource.id,
    )?.[2];
    if (!observer) throw new Error("没有订阅读取状态");
    const { queryKey } = conversationReadingOptions({
      subjectType: "waker",
      subjectId,
    });
    unreadGeneration = 5;
    holdUnread = true;
    act(() =>
      observer.onEvent({
        protocolVersion: 1,
        epoch: "test",
        connectionId: "test",
        deliverySequence: 1,
        topic: "reading",
        type: "reading.changed",
        resource,
        eventId: "seen-in-other-window",
        payload: {},
      }),
    );
    await waitFor(() =>
      expect(view.client.getQueryData(queryKey)).toMatchObject({
        unreadGeneration: 5,
        holdUnread: true,
      }),
    );
    present(next.id);
    expect(write).toHaveBeenCalledOnce();

    unreadGeneration = 6;
    holdUnread = false;
    await act(async () => observer.onResync?.("delivery_gap"));
    await waitFor(() =>
      expect(view.client.getQueryData(queryKey)).toMatchObject({
        unreadGeneration: 6,
        holdUnread: false,
      }),
    );
    present(next.id);
    await waitFor(() =>
      expect(write).toHaveBeenLastCalledWith({
        intent: "automatic",
        observedUnreadGeneration: 6,
        throughMessageID: next.id,
      }),
    );
    expect(write).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("region", { name: "等待工具审批" })).toBeVisible();
  } finally {
    view.unmount();
    presentation.restore();
  }
});

it("从结果深链进入时先加载并定位原话，再确认可见的阅读边界", async () => {
  const older = {
    id: "pinned-result",
    sequence: 6,
    role: "assistant",
    content: "从看板选中的原回报",
    readable: true,
  };
  const latest = {
    id: "recent-result",
    sequence: 12,
    role: "assistant",
    content: "原回报仍在载入时最新的发言",
    readable: true,
  };
  const pageStarted = vi.fn();
  const releasePage = deferred();
  const write = vi.fn();
  server.use(
    http.get(path, async ({ request }) => {
      if (new URL(request.url).searchParams.has("beforeSequence")) {
        pageStarted();
        await releasePage.promise;
        return HttpResponse.json({ ...emptyTimeline, messages: [older] });
      }
      return HttpResponse.json({
        ...emptyTimeline,
        hasMore: true,
        nextBeforeSequence: latest.sequence,
        messages: [latest],
      });
    }),
    http.get(path + "/reading", () =>
      HttpResponse.json({
        conversationId,
        throughSequence: 1,
        unreadGeneration: 1,
        holdUnread: false,
        latestReadableMessageID: latest.id,
        complete: true,
      }),
    ),
    http.post(path + "/reading", async ({ request }) => {
      write(await request.json());
      return HttpResponse.json({});
    }),
  );
  const presentation = mockChatPresentation();
  const jump = vi.spyOn(HTMLElement.prototype, "scrollIntoView");
  const view = mountMainConversation("?message=" + older.id);
  try {
    await screen.findByText(latest.content);
    await waitFor(() => expect(pageStarted).toHaveBeenCalledOnce());
    presentation.present(latest.id);
    expect(write).not.toHaveBeenCalled();
    releasePage.resolve(undefined);
    const content = await screen.findByText(older.content);
    await waitFor(() =>
      expect(jump.mock.contexts).toContain(
        content.closest("[data-message-id]"),
      ),
    );
    await waitFor(() => {
      presentation.present(older.id);
      expect(write).toHaveBeenCalledExactlyOnceWith({
        intent: "automatic",
        observedUnreadGeneration: 1,
        throughMessageID: older.id,
      });
    });
  } finally {
    releasePage.resolve(undefined);
    view.unmount();
    jump.mockRestore();
    presentation.restore();
  }
});

it("工作记录里的发布回放不会推进公开主会话的阅读边界", async () => {
  const read = vi.fn();
  server.use(
    http.get(path, () =>
      HttpResponse.json({
        ...emptyTimeline,
        messages: [
          {
            id: "public-result",
            sequence: 2,
            role: "assistant",
            content: "未在公开主会话中阅读的发言",
            readable: true,
          },
        ],
      }),
    ),
    http.get(path + "/sources/" + workChatId + "/messages", () =>
      HttpResponse.json({
        chat: { id: workChatId, wakerId: subjectId },
        resources: [
          {
            id: "work-result",
            executionId: "result-run",
            role: "assistant",
            status: "completed",
            displayParts: [{ type: "text", text: "工作记录正文" }],
          },
        ],
        publishedMessages: [
          {
            id: "public-result",
            executionId: "result-run",
            content: "未在公开主会话中阅读的发言",
          },
        ],
      }),
    ),
    http.all(path + "/reading", () => {
      read();
      return HttpResponse.json({});
    }),
  );
  const presentation = mockChatPresentation();
  const view = mountMainConversation("?source=" + workChatId);
  try {
    await waitFor(() =>
      expect(
        screen
          .getAllByText("未在公开主会话中阅读的发言")
          .filter((node) => !node.closest("[hidden]")),
      ).toHaveLength(1),
    );
    presentation.present("work-result");
    presentation.present("public-result");
    expect(read).not.toHaveBeenCalled();
  } finally {
    view.unmount();
    presentation.restore();
  }
});

it("已显示的交办被另一窗口删除后，发送回执不会将它重新补到主会话", async () => {
  let snapshot: GetMainConversationResponse = emptyTimeline;
  server.use(
    http.get(path, () => HttpResponse.json(snapshot)),
    http.post(`${path}/messages`, async ({ request }) => {
      const body =
        (await request.json()) as SendMainConversationMessageData["body"];
      const message = committedInput(body.submissionId);
      snapshot = { ...emptyTimeline, messages: [message] };
      return HttpResponse.json({ message, executionId: message.id });
    }),
  );

  const { client, timeline } = await sendFirstMessage();
  await waitFor(() => expect(client.isMutating()).toBe(0));
  expect(timeline.getByText(messageText)).toBeVisible();
  snapshot = emptyTimeline;
  mainConversationChanged();
  await waitFor(() => expect(timeline.queryByText(messageText)).toBeNull());
});

it("另一窗口在发送回执到达前删除已呈现的交办时不会恢复临时气泡", async () => {
  let snapshot: GetMainConversationResponse = emptyTimeline;
  const submitted = deferred<ReturnType<typeof committedInput>>();
  const release = deferred();
  server.use(
    http.get(path, () => HttpResponse.json(snapshot)),
    http.post(`${path}/messages`, async ({ request }) => {
      const { submissionId } =
        (await request.json()) as SendMainConversationMessageData["body"];
      const message = committedInput(submissionId);
      snapshot = { ...emptyTimeline, messages: [message] };
      submitted.resolve(message);
      await release.promise;
      return HttpResponse.json({ message, executionId: message.id });
    }),
  );
  try {
    const { client, timeline } = await sendFirstMessage();
    const message = await submitted.promise;
    mainConversationChanged();
    const queryKey = ["main-conversation", "waker", subjectId];
    const savedMessage = () =>
      client.getQueryData<{ pages: GetMainConversationResponse[] }>(queryKey)
        ?.pages[0]?.messages?.[0];
    await waitFor(() => expect(savedMessage()?.id).toBe(message.id));
    expect(timeline.getByText(messageText)).toBeVisible();

    snapshot = emptyTimeline;
    mainConversationChanged();
    await waitFor(() => expect(savedMessage()).toBeUndefined());
    await waitFor(() => expect(timeline.queryByText(messageText)).toBeNull());

    release.resolve(undefined);
    await waitFor(() => expect(client.isMutating()).toBe(0));
    expect(timeline.queryByText(messageText)).toBeNull();
  } finally {
    release.resolve(undefined);
  }
});

it.each(["接纳回执先到", "活动事件先到", "活动先到且回执丢失"])(
  "主会话首条气泡即时出现，%s时回读保持单条消息",
  async (order) => {
    const submission = deferred<SendMainConversationMessageData["body"]>();
    const releaseReceipt = deferred();
    const readStarted = deferred();
    const releaseRead = deferred();
    let snapshot: GetMainConversationResponse | undefined;
    const receiptFirst = order === "接纳回执先到";
    let timeOut = receiptFirst;
    server.use(
      http.post(`${path}/messages`, async ({ request }) => {
        const body =
          (await request.json()) as SendMainConversationMessageData["body"];
        submission.resolve(body);
        await releaseReceipt.promise;
        if (order === "活动先到且回执丢失")
          return HttpResponse.json({ message: "回执超时" }, { status: 504 });
        const message = committedInput(body.submissionId);
        return HttpResponse.json({
          message,
          executionId: message.id,
        } satisfies SendMainConversationMessageResponse);
      }),
      http.get(path, async () => {
        if (!snapshot) return HttpResponse.json(emptyTimeline);
        readStarted.resolve(undefined);
        await releaseRead.promise;
        if (timeOut) {
          timeOut = false;
          return HttpResponse.json(
            { message: "主会话回读超时" },
            { status: 504 },
          );
        }
        return HttpResponse.json(snapshot);
      }),
    );

    try {
      const { client, timeline } = await sendFirstMessage();
      const body = await submission.promise;
      expect(body).toMatchObject({
        submissionId: expect.any(String),
        message: { text: messageText },
      });
      expect(await timeline.findByText("正在等待回复")).toBeVisible();
      expectMessageBeforeWait(timeline);

      const message = committedInput(body.submissionId);
      snapshot = {
        ...emptyTimeline,
        messages: [message],
        activeWork: [
          {
            workChatId,
            executionId: message.id,
            sourceWakerId: subjectId,
            inputMessageId: message.id,
            hasPublicReply: false,
          },
        ],
      };
      if (receiptFirst) releaseReceipt.resolve(undefined);
      else mainConversationChanged();
      await readStarted.promise;
      expectMessageBeforeWait(timeline);

      releaseRead.resolve(undefined);
      if (receiptFirst) {
        const retry = await timeline.findByRole("button", { name: "重试" });
        await waitFor(() => expect(client.isMutating()).toBe(0));
        expectSingleMessage(timeline);
        await userEvent.click(retry);
      }
      expect(
        await timeline.findByRole("button", { name: /查看.*的工作记录/ }),
      ).toBeVisible();
      expectMessageBeforeWait(timeline);
      expect(
        timeline.getByText(messageText).closest("[data-message-id]"),
      ).toHaveAttribute("data-message-id", message.id);

      releaseReceipt.resolve(undefined);
      await waitFor(() => expect(client.isMutating()).toBe(0));
      expectMessageBeforeWait(timeline);
      expect(screen.queryByRole("button", { name: "重试原发送" })).toBeNull();
    } finally {
      releaseReceipt.resolve(undefined);
      releaseRead.resolve(undefined);
    }
  },
);

it.each([
  {
    scenario: "网络断开",
    failure: () => HttpResponse.error(),
    reason: "首条消息尚未确认，将使用原伙伴、模型和权限重试。",
  },
  {
    scenario: "API 给出公开原因",
    failure: () =>
      HttpResponse.json(
        { message: "服务器未能返回发送回执", messageSource: "public" },
        { status: 503 },
      ),
    reason: "服务器未能返回发送回执",
  },
  {
    scenario: "API 给出会话创建失败的稳定错误码",
    canEdit: true,
    failure: () =>
      HttpResponse.json(
        {
          key: "RuntimeSessionStartFailed",
          code: 50000000054,
          message: "internal worker endpoint",
        },
        { status: 500 },
      ),
    reason: "无法新建智能体会话，请重试",
  },
  {
    scenario: "API 仅含内部诊断",
    failure: () =>
      HttpResponse.json(
        { message: "internal worker endpoint" },
        { status: 503 },
      ),
    reason: "首条消息尚未确认，将使用原伙伴、模型和权限重试。",
  },
])(
  "主会话首发回执不明（$scenario）时显示原因并可原样重试",
  async ({ failure, reason, canEdit }) => {
    const inputs: SendMainConversationMessageData["body"][] = [];
    let message: ReturnType<typeof committedInput> | undefined;
    server.use(
      http.post(`${path}/messages`, async ({ request }) => {
        const body =
          (await request.json()) as SendMainConversationMessageData["body"];
        inputs.push(body);
        if (inputs.length === 1) return failure();
        message = committedInput(body.submissionId);
        return HttpResponse.json({
          message,
          executionId: message.id,
        } satisfies SendMainConversationMessageResponse);
      }),
      http.get(path, () =>
        HttpResponse.json({
          ...emptyTimeline,
          messages: message ? [message] : [],
        }),
      ),
    );

    const { client, timeline } = await sendFirstMessage();
    const retry = await screen.findByRole("button", { name: "重试原发送" });
    const alert = retry.closest('[role="alert"]');
    expect(alert).toHaveTextContent(reason);
    expect(alert).not.toHaveTextContent("internal worker endpoint");
    expect(screen.getByText(reason)).toHaveClass("text-destructive");
    expect(alert?.closest("[data-chat-composer-frame]")).toHaveClass(
      "bg-status-danger-surface",
    );
    expect(retry).toHaveClass("ml-auto", "shrink-0");
    if (canEdit) {
      expect(screen.getByRole("button", { name: "返回编辑" })).toBeEnabled();
    } else {
      expect(screen.queryByRole("button", { name: "返回编辑" })).toBeNull();
    }
    await userEvent.click(retry);
    await waitFor(() => {
      expect(inputs).toHaveLength(2);
      expect(client.isMutating()).toBe(0);
      expect(screen.queryByRole("button", { name: "重试原发送" })).toBeNull();
    });
    expect(inputs[1]).toEqual(inputs[0]);
    expectSingleMessage(timeline);
  },
);

it("主会话曾有不明回执时，原请求后来返回明确失败也不能返回编辑", async () => {
  const inputs: SendMainConversationMessageData["body"][] = [];
  server.use(
    http.post(`${path}/messages`, async ({ request }) => {
      inputs.push(
        (await request.json()) as SendMainConversationMessageData["body"],
      );
      if (inputs.length === 1) return HttpResponse.error();
      if (inputs.length === 2) {
        return HttpResponse.json(
          { key: "RuntimeSessionStartFailed" },
          { status: 500 },
        );
      }
      return HttpResponse.json(
        { message: "输入未通过本次校验", messageSource: "public" },
        { status: 400 },
      );
    }),
  );
  const { editor } = await sendFirstMessage();
  const retry = await screen.findByRole("button", { name: "重试原发送" });
  expect(screen.queryByRole("button", { name: "返回编辑" })).toBeNull();
  await userEvent.click(retry);
  expect(await screen.findByText("无法新建智能体会话，请重试")).toBeVisible();
  expect(screen.queryByRole("button", { name: "返回编辑" })).toBeNull();
  await userEvent.click(retry);
  const reason = await screen.findByText("输入未通过本次校验");
  expect(reason.closest('[role="alert"]')).toContainElement(retry);
  expect(screen.queryByRole("button", { name: "返回编辑" })).toBeNull();
  expect(editor).toHaveAttribute("contenteditable", "false");
  expect(inputs).toEqual([inputs[0], inputs[0], inputs[0]]);
});

it("主会话首条消息被明确拒绝后显示原因、撤回气泡并恢复原草稿", async () => {
  const releaseReceipt = deferred();
  server.use(
    http.post(`${path}/messages`, async () => {
      await releaseReceipt.promise;
      return HttpResponse.json(
        {
          key: "BadRequest",
          code: 40000000001,
          message: "输入无效",
          messageSource: "public",
        },
        { status: 400 },
      );
    }),
  );
  try {
    const { timeline, editor } = await sendFirstMessage();
    expect(await timeline.findByText("正在等待回复")).toBeVisible();
    expectMessageBeforeWait(timeline);
    releaseReceipt.resolve(undefined);
    await waitFor(() => {
      expect(timeline.queryByText(messageText)).toBeNull();
      expect(timeline.queryByText("正在等待回复")).toBeNull();
      expect(editor).toHaveTextContent(messageText);
    });
    const reason = await screen.findByText("输入无效");
    expect(reason.closest('[role="alert"]')).toBeVisible();
    expect(reason.closest("[data-chat-composer-frame]")).toHaveClass(
      "bg-status-danger-surface",
    );
    expect(screen.queryByRole("button", { name: "重试原发送" })).toBeNull();
  } finally {
    releaseReceipt.resolve(undefined);
  }
});
