import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  expect,
  it,
  vi,
} from "vitest";
import type * as GlobalEvents from "@/shared/api/global-events";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { initializeApi } from "@/shared/api/runtime";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import {
  ChatComposerProviders,
  seedChatComposerResources,
} from "@/test/chat-composer-fixture";
import { MainConversationPage } from "./main-conversation-page";

const subscribe = vi.hoisted(() =>
  vi.fn<typeof GlobalEvents.subscribeGlobalEvents>(),
);
vi.mock("@/shared/api/global-events", async (original) => ({
  ...(await original<typeof GlobalEvents>()),
  subscribeGlobalEvents: subscribe,
}));

const api = "http://127.0.0.1:8080/api/v1";
const subjectId = "c".repeat(32);
const conversationId = "d".repeat(32);
const workChatId = "e".repeat(32);
const main = `${api}/main-conversations/waker/${subjectId}`;
const emptyTimeline = {
  id: conversationId,
  subjectType: "waker",
  subjectId,
  messages: [],
  activeWork: [],
  hasMore: false,
};

const server = setupServer(
  http.get(main, () => HttpResponse.json(emptyTimeline)),
  http.get(`${api}/wakers/${subjectId}`, () =>
    HttpResponse.json({ id: subjectId, name: "测试伙伴" }),
  ),
  http.get(`${api}/wakers/${subjectId}/projects`, () =>
    HttpResponse.json({ resources: [] }),
  ),
  http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
  http.get(`${api}/work-tickets`, () => HttpResponse.json([])),
);

const originalScrollTo = HTMLElement.prototype.scrollTo;
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" });
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

function mountMain(seed?: (client: QueryClient) => void) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  seedChatComposerResources(client, subjectId, "测试伙伴");
  seed?.(client);
  const router = createMemoryRouter(
    [
      {
        path: "/chat/main/:subjectType/:subjectId",
        element: <MainConversationPage />,
      },
    ],
    { initialEntries: [`/chat/main/waker/${subjectId}`] },
  );
  render(
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
  return router;
}

const original = {
  id: "a".repeat(32),
  role: "user",
  content: "跟进交付 A",
  sequence: 1,
  sourceChatId: workChatId,
};
const other = {
  id: "b".repeat(32),
  role: "user",
  content: "跟进交付 B",
  sequence: 2,
  sourceChatId: workChatId,
};

function sourceFixture() {
  return http.get(`${main}/sources/${workChatId}/messages`, () =>
    HttpResponse.json({
      chat: { id: workChatId, wakerId: subjectId },
      resources: [],
    }),
  );
}

async function openWorkRecord(
  user: ReturnType<typeof userEvent.setup>,
  text: string,
) {
  const row = (await screen.findByText(text)).closest<HTMLElement>(
    "[data-message-id]",
  );
  if (!row) throw Error("缺少原交办消息");
  await user.click(within(row).getByRole("button", { name: "更多操作" }));
  await user.click(
    await screen.findByRole("menuitem", { name: "打开工作会话" }),
  );
}

function emitTicketChange() {
  const scope = { kind: "main", id: conversationId };
  const listener = subscribe.mock.calls.find(
    ([topic, resource]) =>
      topic === "work-tickets" &&
      resource.kind === scope.kind &&
      resource.id === scope.id,
  )?.[2];
  if (!listener) throw Error("主会话工单订阅未启动");
  act(() =>
    listener.onEvent({
      protocolVersion: 1,
      epoch: "test",
      connectionId: "test",
      deliverySequence: 1,
      topic: "work-tickets",
      type: "work-tickets.changed",
      resource: scope,
      eventId: "1",
      payload: {},
    }),
  );
}

it("主会话查询工单失败时可以重试，并从原消息打开对应的工作记录", async () => {
  const listTicketPage = vi.fn();
  let pageUnavailable = false;
  server.use(
    http.get(main, () =>
      HttpResponse.json({ ...emptyTimeline, messages: [original, other] }),
    ),
    http.get(`${main}/sources/${workChatId}/messages`, () =>
      HttpResponse.json({
        chat: { id: workChatId, wakerId: subjectId },
        resources: [
          { id: "source-input", role: "user", content: "工作记录原始输入" },
        ],
      }),
    ),
    http.get(`${api}/work-tickets`, ({ request }) => {
      const ids = new URL(request.url).searchParams.getAll("messageId");
      if (ids.length > 1) listTicketPage();
      if (ids.length > 1 && pageUnavailable)
        return HttpResponse.json(
          { message: "工单服务暂不可用" },
          { status: 503 },
        );
      return HttpResponse.json(
        ids.length > 1
          ? [
              {
                messageId: original.id,
                ticket: { id: original.id, workChatId, status: "waiting" },
              },
            ]
          : [],
      );
    }),
  );
  const router = mountMain();
  const user = userEvent.setup();
  await waitFor(() => expect(listTicketPage).toHaveBeenCalled());
  pageUnavailable = true;
  emitTicketChange();
  const alert = await screen.findByRole("alert");
  pageUnavailable = false;
  await user.click(within(alert).getByRole("button", { name: "重试" }));
  await waitFor(() => expect(alert).not.toBeInTheDocument());
  await openWorkRecord(user, original.content);
  expect(new URLSearchParams(router.state.location.search).get("message")).toBe(
    original.id,
  );
  expect(await screen.findByText("工作记录原始输入")).toBeVisible();
});

it("主会话普通发送后可直接引用原消息续办", async () => {
  const send = vi.fn();
  const listTickets = vi.fn();
  const first = { ...original, sequence: 3 };
  const followUp = {
    ...first,
    id: "f".repeat(32),
    content: "追加 A 的范围",
    replyToMessageId: first.id,
    sequence: 4,
  };
  let messages = [other];
  let dispatched = false;
  server.use(
    http.get(main, () => HttpResponse.json({ ...emptyTimeline, messages })),
    sourceFixture(),
    http.get(`${api}/work-tickets`, ({ request }) => {
      const ids = new URL(request.url).searchParams.getAll("messageId");
      listTickets(ids);
      return HttpResponse.json(
        dispatched && ids.includes(first.id)
          ? [
              {
                messageId: first.id,
                ticket: { id: first.id, workChatId, status: "waiting" },
              },
            ]
          : [],
      );
    }),
    http.post(`${main}/messages`, async ({ request }) => {
      send(await request.json());
      const message = send.mock.calls.length === 1 ? first : followUp;
      messages = [...messages, message];
      return HttpResponse.json({ message });
    }),
  );
  mountMain();
  const user = userEvent.setup();
  const editor = await screen.findByRole("textbox", { name: "输入消息" });
  const sendButton = screen.getByRole("button", { name: "发送消息" });
  expect(screen.queryByRole("button", { name: "交办" })).toBeNull();
  await user.type(editor, first.content);
  await waitFor(() => expect(sendButton).toBeEnabled());
  await user.click(sendButton);
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(send.mock.calls[0]?.[0]).toMatchObject({
    message: { text: first.content },
  });
  expect(send.mock.calls[0]?.[0]).not.toHaveProperty("startWork");
  expect(send.mock.calls[0]?.[0]).not.toHaveProperty("message.startWork");
  await waitFor(() =>
    expect(listTickets).toHaveBeenCalledWith([first.id, other.id]),
  );
  dispatched = true;
  const ticketReads = listTickets.mock.calls.length;
  emitTicketChange();
  await waitFor(() =>
    expect(listTickets.mock.calls.length).toBeGreaterThan(ticketReads),
  );
  expect(screen.queryByRole("button", { name: "等待输入" })).toBeNull();
  const reply = await waitFor(() => {
    const message = document.querySelector<HTMLElement>(
      `[data-message-id="${first.id}"]`,
    );
    if (!message) throw Error("缺少已接纳的原交办气泡");
    return within(message).getByRole("button", { name: "回复" });
  });
  await user.click(reply);
  await user.type(editor, followUp.content);
  await waitFor(() => expect(sendButton).toBeEnabled());
  await user.click(sendButton);
  await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
  expect(send.mock.calls[1]?.[0]).toMatchObject({
    replyToMessageId: first.id,
    message: { text: followUp.content },
  });
  expect(send.mock.calls[1]?.[0]).not.toHaveProperty("startWork");
});

it("主会话来源记录按时间线展示公开回报，进入记录不读取内部工单状态", async () => {
  const readTickets = vi.fn();
  const publication = {
    id: "publication",
    role: "assistant",
    replyToMessageId: original.id,
    content: "已复核三个发布项",
    sourceChatId: workChatId,
    sourceWakerId: subjectId,
    sequence: 3,
    executionId: "worker-turn",
  };
  server.use(
    http.get(main, () =>
      HttpResponse.json({
        ...emptyTimeline,
        messages: [original, other, publication],
      }),
    ),
    http.get(`${main}/sources/${workChatId}/messages`, () =>
      HttpResponse.json({
        chat: { id: workChatId, wakerId: subjectId },
        resources: [
          {
            id: "worker-turn",
            role: "assistant",
            status: "completed",
            createdSequence: 1,
            executionId: publication.executionId,
            displayParts: [
              { id: "process", type: "reasoning", text: "工作过程原文" },
            ],
          },
        ],
        publishedMessages: [publication],
      }),
    ),
    http.get(`${api}/work-tickets`, ({ request }) => {
      const query = new URL(request.url).searchParams;
      expect(query.get("originKind")).toBe("main");
      expect(query.get("originId")).toBe(conversationId);
      const requested = query.getAll("messageId");
      readTickets(requested);
      return HttpResponse.json(
        [
          {
            messageId: original.id,
            ticket: {
              id: original.id,
              workChatId,
              status: "waiting",
              summary: publication.content,
            },
          },
        ].filter((row) => requested.includes(row.messageId)),
      );
    }),
  );
  const router = mountMain();
  const user = userEvent.setup();
  await waitFor(() =>
    expect(readTickets).toHaveBeenCalledExactlyOnceWith([
      original.id,
      other.id,
      publication.id,
    ]),
  );
  await openWorkRecord(user, original.content);
  expect(new URLSearchParams(router.state.location.search).get("source")).toBe(
    workChatId,
  );
  await waitFor(() => {
    const row = document.querySelector<HTMLElement>(
      '[data-message-id="worker-turn"]',
    );
    if (!row) throw Error("缺少实际工作回合");
    expect(within(row).getByText(publication.content)).toBeVisible();
  });
  expect(screen.queryByRole("button", { name: "停止工单" })).toBeNull();
  expect(screen.queryByText("等待输入")).toBeNull();
  expect(readTickets).toHaveBeenCalledTimes(1);
});

it("主会话摘要只显示正在执行的任务，使用来源会话标题和 CLI 标识", async () => {
  const finishedChatId = "f".repeat(32);
  const waitingChatId = "1".repeat(32);
  const approvalChatId = "2".repeat(32);
  const runningTitle = "A 交付记录";
  const waitingTitle = "B 资料整理";
  server.use(
    http.get(main, () =>
      HttpResponse.json({
        ...emptyTimeline,
        messages: [
          original,
          { ...other, sourceChatId: waitingChatId },
          {
            id: "3".repeat(32),
            role: "user",
            content: "已完成的旧工作",
            sourceChatId: finishedChatId,
            sequence: 3,
          },
        ],
        activeWork: [
          {
            workChatId,
            executionId: "execution-running",
            inputMessageId: original.id,
            sourceWakerId: subjectId,
            title: runningTitle,
            runtimeId: "delivery-profile",
            hasPublicReply: false,
            running: true,
          },
          {
            workChatId: waitingChatId,
            executionId: "execution-waiting",
            inputMessageId: other.id,
            sourceWakerId: subjectId,
            title: waitingTitle,
            runtimeId: "codex",
            waitingElicitation: true,
            hasPublicReply: false,
            running: false,
          },
          {
            workChatId: approvalChatId,
            executionId: "execution-approval",
            sourceWakerId: subjectId,
            title: "C 等待审批",
            runtimeId: "codex",
            waitingApproval: true,
            hasPublicReply: false,
          },
          {
            workChatId: "4".repeat(32),
            executionId: "execution-pending",
            sourceWakerId: subjectId,
            title: "待启动的工作",
            runtimeId: "codex",
            hasPublicReply: false,
            running: false,
          },
          {
            workChatId: "5".repeat(32),
            executionId: "execution-from-work-record",
            sourceWakerId: subjectId,
            title: "来源记录启动的工作",
            runtimeId: "codex",
            hasPublicReply: false,
            running: false,
          },
        ],
      }),
    ),
    sourceFixture(),
    http.get(`${main}/sources/:chatId/messages`, ({ params }) =>
      HttpResponse.json({
        chat: { id: params.chatId, wakerId: subjectId },
        resources: [],
      }),
    ),
  );
  const router = mountMain((client) => {
    client.setQueryData(listRuntimesOptions().queryKey, (current) => ({
      resources: [
        ...(current?.resources ?? []),
        { id: "delivery-profile", baseRuntimeId: "workbuddy" },
      ],
    }));
  });
  const user = userEvent.setup();
  expect(await screen.findByText(original.content)).toBeVisible();
  await user.click(screen.getByRole("button", { name: "查看摘要" }));
  const summary = within(
    await screen.findByRole("dialog", { name: "查看摘要" }),
  );
  const section = summary.getByRole("region", { name: "任务" });
  expect(summary.getAllByRole("region")).toEqual([section]);
  const tasks = within(section);
  const task = tasks.getByRole("button", { name: runningTitle });
  expect(task.firstElementChild).toHaveAttribute(
    "src",
    expect.stringContaining("workbuddy.svg"),
  );
  expect(tasks.getAllByRole("button")).toEqual([task]);
  expect(tasks.queryByText(original.content)).toBeNull();
  expect(summary.queryByText("正在等待回复")).toBeNull();
  expect(screen.queryByText("等待审批")).toBeNull();
  expect(screen.queryByText("等待输入")).toBeNull();
  await user.click(task);
  const target = new URLSearchParams(router.state.location.search);
  expect(target.get("source")).toBe(workChatId);
  expect(target.get("message")).toBe(original.id);
});

it("在原消息的回复输入框移交工单，并在原对话显示接手人", async () => {
  const targetId = "f".repeat(32);
  const transfer = vi.fn();
  const modelSend = vi.fn();
  let messages = [original];
  server.use(
    http.get(main, () => HttpResponse.json({ ...emptyTimeline, messages })),
    sourceFixture(),
    http.get(`${api}/work-tickets`, () =>
      HttpResponse.json([
        {
          messageId: original.id,
          ticket: {
            id: original.id,
            workChatId,
            status: "waiting",
            generation: 3,
          },
        },
      ]),
    ),
    http.get(`${api}/wakers`, () =>
      HttpResponse.json({
        resources: [{ id: targetId, name: "测试接手人" }],
        totalSize: 1,
      }),
    ),
    http.get(`${api}/channels`, () =>
      HttpResponse.json({ resources: [], totalSize: 0 }),
    ),
    http.post(
      `${api}/work-tickets/${original.id}/transfer`,
      async ({ request }) => {
        transfer(await request.json());
        if (transfer.mock.calls.length === 1) {
          return HttpResponse.json(
            { message: "Service temporarily unavailable" },
            { status: 503 },
          );
        }
        messages = [
          ...messages,
          {
            ...original,
            id: "handoff",
            content: "已移交给测试接手人。",
            role: "assistant",
            sourceChatId: "",
            sequence: 2,
          },
        ];
        return HttpResponse.json({
          ticketId: original.id,
          generation: 4,
          executionId: "new-owner",
        });
      },
    ),
    http.post(`${main}/messages`, () => {
      modelSend();
      return HttpResponse.json({ message: original });
    }),
  );
  mountMain();
  const user = userEvent.setup();
  const row = (await screen.findByText(original.content)).closest<HTMLElement>(
    "[data-message-id]",
  );
  if (!row) throw Error("缺少原交办消息");
  await user.click(within(row).getByRole("button", { name: "回复" }));
  const editor = await screen.findByRole("textbox", { name: "输入消息" });
  await waitFor(() =>
    expect(editor.querySelector("[data-placeholder]")).toHaveAttribute(
      "data-placeholder",
      "输入回复，或 /移交 伙伴名",
    ),
  );
  await user.type(editor, "/移交");
  await user.keyboard("{Enter}");
  expect(
    await screen.findByText(
      "输入 /移交 和接手伙伴或团队的全名。另起一行可填写交接说明。",
    ),
  ).toBeVisible();
  expect(editor).toHaveTextContent("/移交");
  await user.type(editor, " 测试接手人{Shift>}{Enter}{/Shift}复核第一项");
  const send = screen.getByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.keyboard("{Enter}");
  await waitFor(() =>
    expect(transfer).toHaveBeenCalledWith(
      expect.objectContaining({
        originKind: "main",
        originId: conversationId,
        targetKind: "waker",
        targetId,
        expectedGeneration: 3,
        requestId: expect.any(String),
        content: "复核第一项",
      }),
    ),
  );
  await waitFor(() => expect(send).toBeEnabled());
  expect(editor).toHaveTextContent("/移交 测试接手人");
  await user.click(send);
  await waitFor(() => expect(transfer).toHaveBeenCalledTimes(2));
  expect(transfer.mock.calls[1]).toEqual(transfer.mock.calls[0]);
  expect(await screen.findByText("已移交给测试接手人。")).toBeVisible();
  expect(modelSend).not.toHaveBeenCalled();
  expect(editor).not.toHaveTextContent("/移交");
});
