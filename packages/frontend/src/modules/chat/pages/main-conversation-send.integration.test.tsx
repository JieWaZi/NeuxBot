import { createHash, webcrypto } from "node:crypto";
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
import {
  getWakerQueryKey,
  listAiAccountsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type * as GlobalEvents from "@/shared/api/global-events";
import { conversationReadingOptions } from "@/shared/api/reading-queries";
import { nativeModelCatalogQueryKey } from "@/shared/api/runtime-queries";
import { initializeApi } from "@/shared/api/runtime";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { WorkspaceProvider } from "@/shared/workspace/workspace-context";
import {
  ChatComposerProviders,
  seedChatComposerResources,
} from "@/test/chat-composer-fixture";
import { mockChatPresentation } from "@/test/chat-presentation-fixture";
import {
  mainConversationOptions,
  sourceHistoryOptions,
} from "../api/main-conversation";
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
const profile = {
  id: subjectId,
  name: "交付伙伴",
  avatar: "bloub:plush:nuage:curieux:38bdf8",
};
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
  http.get(`${origin}/api/v1/work-tickets`, () => HttpResponse.json([])),
  http.get(`${origin}/api/v1/wakers/${subjectId}`, () =>
    HttpResponse.json(profile),
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

function mainConversationObserver() {
  const listener = subscribe.mock.calls.findLast(
    ([topic, scope]) =>
      topic === "main-conversations" &&
      scope.kind === "waker" &&
      scope.id === subjectId,
  )?.[2];
  if (!listener) throw new Error("主会话未订阅此伙伴");
  return listener;
}

function mainConversationChanged() {
  act(() =>
    mainConversationObserver().onEvent({
      protocolVersion: 1,
      epoch: "test",
      connectionId: "test",
      deliverySequence: 1,
      topic: "main-conversations",
      type: "main-conversations.changed",
      resource: { kind: "waker", id: subjectId },
      eventId: "1",
      payload: {},
    }),
  );
}

function mountMainConversation(
  search = "",
  setup?: (client: QueryClient) => void,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  seedChatComposerResources(client, subjectId, "交付伙伴");
  setup?.(client);
  client.setQueryData(
    getWakerQueryKey({ path: { wakerId: subjectId } }),
    profile,
  );
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

it("在来源中选择模型即记录切换，重开后仍显示该选择", async () => {
  const sourcePath = `${path}/sources/${workChatId}`;
  const update = vi.fn();
  let selectedModel: string | undefined;
  server.use(
    http.get(`${sourcePath}/messages`, () =>
      HttpResponse.json({
        chat: {
          id: workChatId,
          wakerId: subjectId,
          lastRuntimeId: "codex",
          lastAccountId: "system",
          lastModel: "gpt-5",
        },
        privateContinuation: true,
        selectedModel,
        modelChanges: selectedModel
          ? [
              {
                sequence: 2,
                createdAt: 2000,
                actor: "user",
                fromModel: "gpt-5",
                toModel: selectedModel,
              },
            ]
          : [],
        resources: [
          {
            id: "source-original-input",
            role: "user",
            content: "原工作",
            createdSequence: 1,
            createdAt: 1000,
          },
        ],
      }),
    ),
    http.patch(`${sourcePath}/model`, async ({ request }) => {
      const body = (await request.json()) as { model: string };
      update(body);
      selectedModel = body.model;
      return HttpResponse.json({ selectedModel });
    }),
  );
  const setup = (client: QueryClient) =>
    client.setQueryData(
      nativeModelCatalogQueryKey({ runtimeId: "codex", accountId: "system" }),
      {
        runtimeId: "codex",
        accountId: "system",
        state: "fresh",
        defaultModel: "gpt-5",
        models: [
          { id: "gpt-5", name: "GPT-5" },
          { id: "gpt-fast", name: "GPT Fast" },
        ],
      },
    );
  const route = "?source=" + workChatId;
  const source = mountMainConversation(route, setup);
  const user = userEvent.setup();
  await screen.findByText("原工作");
  await user.click(screen.getByRole("button", { name: /GPT-5/ }));
  await user.click(await screen.findByRole("radio", { name: "GPT Fast" }));
  expect(
    screen.queryByRole("dialog", { name: "在当前聊天中继续" }),
  ).not.toBeInTheDocument();
  await waitFor(() =>
    expect(update).toHaveBeenCalledExactlyOnceWith({ model: "gpt-fast" }),
  );
  expect(
    await screen.findByText(/将下轮模型从 gpt-5 改为 gpt-fast/),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: /GPT Fast/ })).toBeVisible();
  source.unmount();
  mountMainConversation(route, setup);
  expect(
    await screen.findByText(/将下轮模型从 gpt-5 改为 gpt-fast/),
  ).toBeVisible();
  expect(await screen.findByRole("button", { name: /GPT Fast/ })).toBeVisible();
  expect(update).toHaveBeenCalledTimes(1);
});

it.each([
  {
    label: "默认会话（不接入旧工作会话）",
    quoteAfterSwitch: false,
    workView: false,
  },
  { label: "后来引用的独立来源", quoteAfterSwitch: true, workView: false },
  { label: "当前工作记录", quoteAfterSwitch: false, workView: true },
])(
  "伙伴主会话换账号时接续$label，取消切换保留草稿",
  async ({ quoteAfterSwitch, workView }) => {
    const defaultSource = {
      id: workChatId,
      lastRuntimeId: "codex",
      lastAccountId: "system",
    };
    const quotedSourceId = "d".repeat(32);
    const quoted = {
      id: "quote-work",
      conversationId,
      sequence: 1,
      role: "assistant",
      content: "独立工作回报",
      sourceChatId: quotedSourceId,
      sourceWakerId: subjectId,
      readable: true,
    };
    const post = vi.fn();
    server.use(
      http.get(path, () =>
        HttpResponse.json({
          ...emptyTimeline,
          defaultSource,
          messages: quoteAfterSwitch ? [quoted] : [],
        }),
      ),
      http.get(`${path}/reading`, () =>
        HttpResponse.json({ conversationId, complete: true }),
      ),
      http.get(`${path}/sources/${quotedSourceId}/messages`, () =>
        HttpResponse.json({
          chat: {
            ...defaultSource,
            id: quotedSourceId,
            wakerId: subjectId,
            lastModel: "gpt-5",
          },
          privateContinuation: workView,
          resources: [],
        }),
      ),
      http.post(
        `${path}/sources/${quotedSourceId}/messages`,
        async ({ request }) => {
          expect(new URL(request.url).searchParams.get("switchSession")).toBe(
            "true",
          );
          post(await request.json());
          return HttpResponse.json({});
        },
      ),
      http.post(`${path}/messages`, async ({ request }) => {
        const body =
          (await request.json()) as SendMainConversationMessageData["body"];
        post(body);
        return HttpResponse.json({
          message: {
            ...committedInput(body.submissionId),
            sourceChatId: quoteAfterSwitch ? quotedSourceId : "",
          },
        });
      }),
    );
    const account = {
      id: "alternate",
      runtimeId: "codex",
      name: "备用账号",
      authMode: "api-key",
      hasCredential: true,
      endpoint: {
        protocol: "openai-responses",
        baseUrl: "https://api.openai.com/v1",
      },
      selectedModels: ["alternate-model"],
    };
    server.use(
      http.get(`${origin}/api/v1/ai-accounts`, () =>
        HttpResponse.json({ resources: [account] }),
      ),
      http.get(`${origin}/api/v1/runtimes/codex/models`, ({ request }) => {
        expect(new URL(request.url).searchParams.get("accountId")).toBe(
          account.id,
        );
        return HttpResponse.json({
          runtimeId: "codex",
          accountId: account.id,
          state: "fresh",
          defaultModel: "alternate-model",
          models: [{ id: "alternate-model", name: "备用模型" }],
        });
      }),
    );
    mountMainConversation(
      workView ? `?source=${quotedSourceId}` : "",
      (client) =>
        client.setQueryData(listAiAccountsQueryKey(), { resources: [account] }),
    );
    const user = userEvent.setup();
    await user.type(
      await screen.findByRole("textbox", { name: "输入消息" }),
      messageText,
    );
    const selectAlternate = async () => {
      if (!screen.queryByRole("combobox", { name: "账号" }))
        await user.click(screen.getByRole("button", { name: /GPT-5/ }));
      await user.click(await screen.findByRole("combobox", { name: "账号" }));
      await user.click(await screen.findByRole("option", { name: "备用账号" }));
      await user.click(await screen.findByRole("radio", { name: "备用模型" }));
    };
    if (!quoteAfterSwitch && !workView) {
      await selectAlternate();
      expect(
        screen.queryByRole("dialog", { name: "在当前聊天中继续" }),
      ).not.toBeInTheDocument();
      await user.keyboard("{Escape}");
      const send = screen.getByRole("button", { name: "发送消息" });
      await waitFor(() => expect(send).toBeEnabled());
      await user.click(send);
      await waitFor(() => expect(post).toHaveBeenCalledOnce());
      expect(post.mock.calls[0]?.[0]).not.toHaveProperty("historySourceChatId");
      expect(post.mock.calls[0]?.[0]).toMatchObject({
        message: { model: "alternate-model", accountId: account.id },
      });
      return;
    }
    if (quoteAfterSwitch) {
      const work = (
        await screen.findByText(quoted.content)
      ).closest<HTMLElement>("[data-message-id]");
      if (!work) throw new Error("缺少独立工作回报");
      await user.click(within(work).getByRole("button", { name: "回复" }));
    }
    await selectAlternate();
    const dialog = await screen.findByRole("dialog", {
      name: "在当前聊天中继续",
    });
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.getByRole("button", { name: /GPT-5/ })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "输入消息" })).toHaveTextContent(
      messageText,
    );
    expect(post).not.toHaveBeenCalled();

    await selectAlternate();
    await user.click(screen.getByRole("radio", { name: "自动总结" }));
    const turns = screen.getByLabelText("总结不可用时携带的最近轮数");
    await user.clear(turns);
    await user.type(turns, "3");
    await user.click(screen.getByRole("button", { name: "确认切换并继续" }));
    expect(screen.getByRole("textbox", { name: "输入消息" })).toHaveTextContent(
      messageText,
    );
    const send = screen.getByRole("button", { name: "发送消息" });
    await waitFor(() => expect(send).toBeEnabled());
    await user.click(send);
    const configuration = {
      model: "alternate-model",
      accountId: account.id,
      startNewSession: true,
      historyMode: "summary",
      historyTurns: 3,
    };
    await waitFor(() =>
      expect(post).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining(
          workView
            ? configuration
            : {
                historySourceChatId: quoteAfterSwitch
                  ? quotedSourceId
                  : workChatId,
                ...(quoteAfterSwitch ? { replyToMessageId: quoted.id } : {}),
                message: expect.objectContaining(configuration),
              },
        ),
      ),
    );
  },
);

it("主会话新气泡入场并推移贴底旧消息，历史和恢复保持静态", async () => {
  const allowReceipt = deferred();
  const allowEarlier = deferred();
  const loadEarlier = vi.fn();
  let posted: ReturnType<typeof committedInput> | undefined;
  let accepted: GetMainConversationResponse["pendingInputs"];
  let height = 260;
  const existingTime = new Date(2026, 10, 27, 9).getTime();
  let messages: NonNullable<GetMainConversationResponse["messages"]> = [
    {
      id: "existing",
      sequence: 2,
      role: "user",
      content: "已读历史",
      createdAt: existingTime,
    },
  ];
  server.use(
    http.get(path, async ({ request }) => {
      if (new URL(request.url).searchParams.has("beforeSequence")) {
        loadEarlier();
        await allowEarlier.promise;
        return HttpResponse.json({
          ...emptyTimeline,
          messages: [
            { id: "paged", sequence: 1, role: "user", content: "翻页补入" },
          ],
        });
      }
      if (messages.some((row) => row.id === "live"))
        height = Math.max(height, 420);
      return HttpResponse.json({
        ...emptyTimeline,
        hasMore: true,
        nextBeforeSequence: 2,
        messages,
        pendingInputs: accepted,
      });
    }),
    http.post(`${path}/messages`, async ({ request }) => {
      const { submissionId } =
        (await request.json()) as SendMainConversationMessageData["body"];
      const message = { ...committedInput(submissionId), sequence: 7 };
      posted = message;
      await allowReceipt.promise;
      return HttpResponse.json({ message, executionId: message.id });
    }),
  );
  const view = mountMainConversation();
  const previous = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "animate",
  );
  const cancel = vi.fn();
  const animate = vi.fn(function (this: HTMLElement) {
    return { cancel, finished: new Promise(() => {}) };
  });
  Object.defineProperty(HTMLElement.prototype, "animate", {
    configurable: true,
    value: animate,
  });
  const root = document.documentElement;
  root.style.setProperty("--duration-layout", "220ms");
  root.style.setProperty("--duration-standard", "220ms");
  const media = window.matchMedia;
  let reduced = false;
  window.matchMedia = (query) => ({
    media: query,
    matches: query.includes("prefers-reduced-motion") && reduced,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => true,
  });
  const bounds = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect");
  const hidden = vi.spyOn(document, "hidden", "get");
  const visibility = vi.spyOn(document, "visibilityState", "get");
  bounds.mockReturnValue(new DOMRect(0, 10, 320, 40));
  try {
    await screen.findByText("已读历史");
    expect(animate).not.toHaveBeenCalled();
    const viewport = view.container.querySelector<HTMLElement>(
      "[data-chat-scroll-viewport]",
    );
    if (!viewport) throw new Error("缺少主会话滚动区");
    const { queryKey } = mainConversationOptions({
      subjectType: "waker",
      subjectId,
    });
    const initialVersion =
      view.client.getQueryState(queryKey)?.dataUpdateCount ?? 0;
    await act(async () =>
      mainConversationObserver().onResync?.("subscription_joined"),
    );
    await waitFor(() =>
      expect(
        view.client.getQueryState(queryKey)?.dataUpdateCount,
      ).toBeGreaterThan(initialVersion),
    );
    Object.defineProperties(viewport, {
      clientHeight: { configurable: true, get: () => 100 },
      scrollHeight: { configurable: true, get: () => height },
    });
    viewport.getBoundingClientRect = () => new DOMRect(0, 0, 320, 100);
    viewport.scrollTop = 160;
    viewport.scrollTo = (options?: ScrollToOptions | number) => {
      if (typeof options !== "number") viewport.scrollTop = options?.top ?? 0;
    };
    const report = {
      id: "live",
      sequence: 3,
      role: "assistant",
      content: "实时回报",
      createdAt: existingTime + 16 * 60 * 1000,
      automaticTaskReceipts: [
        {
          receiptId: "live-receipt",
          taskId: "live-task",
          name: "实时回报任务",
          schedule: { kind: "once" as const, timezone: "Asia/Shanghai" },
        },
      ],
    };
    const earlier = screen.getByRole("button", { name: "加载更早消息" });
    fireEvent.click(earlier);
    await waitFor(() => expect(loadEarlier).toHaveBeenCalledOnce());
    messages = [...messages, report];
    mainConversationChanged();
    allowEarlier.resolve(undefined);
    await screen.findByText(report.content);
    expect(screen.getByText("翻页补入")).toBeVisible();
    expect(viewport.scrollTop).toBe(320);
    const reportTime = viewport.querySelector(
      '[data-chat-time-divider="live"]',
    );
    expect(reportTime).toBeVisible();
    expect(animate.mock.contexts).toEqual(
      expect.arrayContaining([
        view.container.querySelector('[data-message-id="existing"]'),
        view.container.querySelector('[data-message-id="live"]'),
        reportTime,
        screen.getByText("实时回报任务").closest("[data-chat-message-bubble]"),
        screen.getByText(report.content).closest("[data-chat-message-bubble]"),
      ]),
    );
    expect(animate).toHaveBeenCalledWith(
      [{ transform: "translateY(160px)" }, { transform: "translateY(0)" }],
      expect.objectContaining({ composite: "add" }),
    );
    expect(animate).toHaveBeenCalledWith(
      [
        expect.objectContaining({ opacity: 0, transformOrigin: "left bottom" }),
        expect.objectContaining({
          offset: 0.7,
          opacity: 1,
          transform: "translateY(0) scale(1.01)",
        }),
        expect.objectContaining({ opacity: 1, transformOrigin: "left bottom" }),
      ],
      expect.objectContaining({ duration: 220 }),
    );

    animate.mockClear();
    messages = messages.map((row) =>
      row.id === report.id ? { ...row, content: "实时回报已更新" } : row,
    );
    mainConversationChanged();
    await screen.findByText("实时回报已更新");
    expect(animate).not.toHaveBeenCalled();

    viewport.scrollTop = 100;
    fireEvent.scroll(viewport);
    expect(cancel).toHaveBeenCalled();
    messages = [
      { id: "older", sequence: 0, role: "user", content: "更早历史" },
      ...messages,
      { id: "recovered", sequence: 4, role: "assistant", content: "断线补读" },
    ];
    height = 480;
    await act(async () =>
      mainConversationObserver().onResync?.("delivery_gap"),
    );
    await screen.findByText("断线补读");
    expect(screen.getByText("更早历史")).toBeVisible();
    expect(screen.getByText("实时回报已更新")).toBeVisible();
    expect(animate).not.toHaveBeenCalled();
    expect(viewport.scrollTop).toBe(100);

    viewport.scrollTop = 380;
    fireEvent.scroll(viewport);
    reduced = true;
    messages = [
      ...messages,
      {
        id: "reduced",
        sequence: 5,
        role: "assistant",
        content: "减少动态效果",
      },
    ];
    height = 540;
    mainConversationChanged();
    await screen.findByText("减少动态效果");
    expect(viewport.scrollTop).toBe(440);
    expect(animate).not.toHaveBeenCalled();

    reduced = false;
    hidden.mockReturnValue(true);
    visibility.mockReturnValue("hidden");
    fireEvent(document, new Event("visibilitychange"));
    hidden.mockReturnValue(false);
    visibility.mockReturnValue("visible");
    fireEvent(document, new Event("visibilitychange"));
    const queued = { id: "remote-input", content: "另一窗口的排队消息" };
    accepted = [queued];
    mainConversationChanged();
    const queuedBubble = (await screen.findByText(queued.content)).closest(
      "[data-chat-message-bubble]",
    );
    expect(animate.mock.contexts).toEqual([queuedBubble]);
    const promoted = {
      ...queued,
      sequence: 6,
      role: "user",
      content: "排队消息已确认",
    };
    accepted = undefined;
    messages = [...messages, promoted];
    mainConversationChanged();
    await screen.findByText(promoted.content);
    expect(animate.mock.contexts).toEqual([queuedBubble]);
    animate.mockClear();

    const user = userEvent.setup();
    await user.type(
      screen.getByRole("textbox", { name: "输入消息" }),
      messageText,
    );
    await user.click(screen.getByRole("button", { name: "发送消息" }));
    const sent = (await screen.findByText(messageText)).closest(
      "[data-chat-message-bubble]",
    );
    expect(animate.mock.contexts).toEqual([sent]);
    expect(animate).toHaveBeenCalledWith(
      [
        expect.objectContaining({ transformOrigin: "right bottom" }),
        expect.objectContaining({ transformOrigin: "right bottom" }),
        expect.objectContaining({ transformOrigin: "right bottom" }),
      ],
      expect.objectContaining({ duration: 220 }),
    );
    await waitFor(() => expect(posted).toBeDefined());
    if (!posted) throw new Error("缺少已提交的主会话消息");
    const directReply = {
      id: "direct-reply",
      sequence: 8,
      role: "assistant",
      content: "发送回执对账先读到的回复",
      replyToMessageId: posted.id,
    };
    messages = [...messages, posted, directReply];
    allowReceipt.resolve(undefined);
    const directBubble = (await screen.findByText(directReply.content)).closest(
      "[data-chat-message-bubble]",
    );
    await waitFor(() => expect(view.client.isMutating()).toBe(0));
    expect(
      screen.getByText(messageText).closest("[data-chat-message-bubble]"),
    ).toBe(sent);
    expect(animate.mock.contexts).toEqual([sent, directBubble]);
    const version = view.client.getQueryState(queryKey)?.dataUpdateCount ?? 0;
    mainConversationChanged();
    await waitFor(() =>
      expect(
        view.client.getQueryState(queryKey)?.dataUpdateCount,
      ).toBeGreaterThan(version),
    );
    expect(animate.mock.contexts).toEqual([sent, directBubble]);
  } finally {
    allowEarlier.resolve(undefined);
    allowReceipt.resolve(undefined);
    view.unmount();
    bounds.mockRestore();
    hidden.mockRestore();
    visibility.mockRestore();
    window.matchMedia = media;
    root.style.removeProperty("--duration-layout");
    root.style.removeProperty("--duration-standard");
    if (previous)
      Object.defineProperty(HTMLElement.prototype, "animate", previous);
    else Reflect.deleteProperty(HTMLElement.prototype, "animate");
  }
});

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

it("从直答记录在主会话追问，并按回执在原记录等待公开答复", async () => {
  const original = {
    id: "question",
    sequence: 1,
    role: "user",
    content: "上次讨论了什么？",
  };
  const answer = {
    id: "direct-answer",
    sequence: 2,
    role: "assistant",
    content: "上次讨论了发版安排。",
    replyToMessageId: original.id,
    sourceWakerId: subjectId,
  };
  const unrelated = {
    id: "unrelated",
    sequence: 3,
    role: "assistant",
    content: "一个独立话题",
  };
  let accepted:
    | NonNullable<GetMainConversationResponse["pendingInputs"]>[number]
    | undefined;
  let settled = false;
  server.use(
    http.get(path, () =>
      HttpResponse.json({
        ...emptyTimeline,
        pendingInputs: accepted && !settled ? [accepted] : [],
        messages: [
          original,
          answer,
          unrelated,
          ...(accepted && settled
            ? [
                { ...accepted, role: "user", sequence: 4 },
                {
                  id: "follow-up-answer",
                  sequence: 5,
                  role: "assistant",
                  content: "我们接着整理发版清单。",
                  replyToMessageId: accepted.id,
                  sourceWakerId: subjectId,
                },
              ]
            : []),
        ],
      }),
    ),
    http.post(`${path}/messages`, async ({ request }) => {
      const body =
        (await request.json()) as SendMainConversationMessageData["body"];
      expect(body.replyToMessageId).toBe(answer.id);
      const input = committedInput(body.submissionId);
      accepted = {
        id: input.id,
        replyToMessageId: body.replyToMessageId,
        content: input.content,
        createdAt: input.createdAt,
      };
      return HttpResponse.json({ queuedInput: accepted });
    }),
  );
  const search = `?record=${answer.id}`;
  const view = mountMainConversation(search);
  const record = within(
    await screen.findByRole("region", { name: "默认会话" }),
  );
  expect(await record.findByText(answer.content)).toBeVisible();
  expect(record.queryByText(unrelated.content)).toBeNull();
  const editor = record.getByRole("textbox", { name: "输入消息" });
  const user = userEvent.setup();
  await user.type(editor, messageText);
  const send = record.getByRole("button", { name: "发送消息" });
  await waitFor(() => expect(send).toBeEnabled());
  await user.click(send);
  await waitFor(() => expect(accepted).toBeDefined());
  await waitFor(() => expect(view.client.isMutating()).toBe(0));
  expectSingleMessage(record);
  expect(record.getByText("正在等待回复")).toBeVisible();
  view.unmount();

  mountMainConversation(search);
  const reopened = within(
    await screen.findByRole("region", { name: "默认会话" }),
  );
  expect(await reopened.findByText("正在等待回复")).toBeVisible();
  expectSingleMessage(reopened);
  settled = true;
  mainConversationChanged();
  expect(await reopened.findByText("我们接着整理发版清单。")).toBeVisible();
  expectSingleMessage(reopened);
  expect(reopened.queryByText("正在等待回复")).toBeNull();
  expect(reopened.queryByText(unrelated.content)).toBeNull();
});

it.each(["main", "source"])(
  "在 %s 接纳同一工作的新交办，重新打开后恢复并由真实消息接管",
  async (viewName) => {
    const sourcePath = path + "/sources/" + workChatId + "/messages";
    const search = viewName === "source" ? "?source=" + workChatId : "";
    const sourceMessageId = "native-queued-user";
    const sourceChat = { id: workChatId, wakerId: subjectId };
    let phase: "running" | "queued" | "handoff" = "running";
    const allowHistory = deferred();
    let original: ReturnType<typeof committedInput> | undefined;
    const queuedInput = () =>
      original && {
        id: original.id,
        workChatId,
        sourceMessageId,
        content: messageText,
        createdAt: 1795650000,
      };
    server.use(
      http.get(path, async () => {
        if (phase === "queued") await allowHistory.promise;
        return HttpResponse.json({
          ...emptyTimeline,
          messages: phase === "handoff" && original ? [original] : [],
          pendingInputs:
            phase === "queued" || phase === "handoff" ? [queuedInput()] : [],
        });
      }),
      http.get(sourcePath, () =>
        HttpResponse.json({
          chat: {
            ...sourceChat,
            activeExecutionId: phase === "handoff" ? "" : "running-work",
          },
          resources:
            phase === "handoff"
              ? [
                  {
                    id: sourceMessageId,
                    role: "user",
                    content: messageText,
                    createdSequence: 1,
                  },
                ]
              : [],
        }),
      ),
      http.post(
        viewName === "source" ? sourcePath : path + "/messages",
        async ({ request }) => {
          const body =
            (await request.json()) as SendMainConversationMessageData["body"];
          original = committedInput(body.submissionId);
          phase = "queued";
          return HttpResponse.json({ queuedInput: queuedInput() });
        },
      ),
    );
    const visibleInputs = () =>
      screen
        .queryAllByText(messageText)
        .filter((node) => !node.closest("[hidden]"));
    const visibleUserRow = () =>
      visibleInputs()[0]?.closest<HTMLElement>("[data-role=user]");
    const view = mountMainConversation(search);
    try {
      const editor = await screen.findByRole("textbox", { name: "输入消息" });
      const user = userEvent.setup();
      await user.type(editor, messageText);
      const send = screen.getByRole("button", { name: "发送消息" });
      expect(send).toBeEnabled();
      await user.click(send);
      await waitFor(() => expect(phase).toBe("queued"));
      await waitFor(() => expect(view.client.isMutating()).toBe(0));
      await waitFor(() => expect(visibleInputs()).toHaveLength(1));
      expect(screen.queryByText("正在等待回复")).toBeNull();
      expect(visibleUserRow()).toHaveAttribute(
        "data-message-id",
        viewName === "source" ? sourceMessageId : original?.id,
      );
    } finally {
      allowHistory.resolve(undefined);
      view.unmount();
    }

    const reopened = mountMainConversation(search);
    try {
      await waitFor(() => expect(visibleInputs()).toHaveLength(1));
      phase = "handoff";
      mainConversationChanged();
      const subject = { subjectType: "waker", subjectId } as const;
      await waitFor(() => {
        if (viewName === "source") {
          const { queryKey } = sourceHistoryOptions(subject, workChatId);
          expect(
            reopened.client.getQueryData(queryKey)?.pages[0]?.resources,
          ).toEqual([expect.objectContaining({ id: sourceMessageId })]);
        } else {
          const { queryKey } = mainConversationOptions(subject);
          expect(
            reopened.client.getQueryData(queryKey)?.pages[0]?.messages,
          ).toEqual([expect.objectContaining({ id: original?.id })]);
        }
      });
      expect(visibleInputs()).toHaveLength(1);
      expect(visibleUserRow()).toHaveAttribute(
        "data-message-id",
        viewName === "source" ? sourceMessageId : original?.id,
      );
    } finally {
      reopened.unmount();
    }
  },
);

it("主会话接纳无工作来源的澄清，用户可引用澄清继续发送", async () => {
  const input = "帮我核对项目的交付结果";
  const followUp = "核对项目 A";
  const clarification = "你想核对哪个项目的交付结果？";
  const replyId = "d".repeat(32);
  const send = vi.fn();
  const messages: NonNullable<GetMainConversationResponse["messages"]> = [];
  server.use(
    http.get(path, () => HttpResponse.json({ ...emptyTimeline, messages })),
    http.get(path + "/reading", () =>
      HttpResponse.json({ conversationId, complete: true }),
    ),
    http.post(path + "/messages", async ({ request }) => {
      const body =
        (await request.json()) as SendMainConversationMessageData["body"];
      const content = body.message?.text;
      if (!content) throw new Error("缺少公开输入正文");
      send(body);
      const first = messages.length === 0;
      const message = {
        ...committedInput(body.submissionId),
        content,
        sequence: messages.length + 1,
        sourceChatId: "",
        replyToMessageId: body.replyToMessageId,
      };
      messages.push(message);
      if (first)
        messages.push({
          id: replyId,
          conversationId,
          sequence: 2,
          role: "assistant",
          content: clarification,
          sourceChatId: "",
          sourceWakerId: subjectId,
          replyToMessageId: message.id,
          readable: true,
        });
      return HttpResponse.json({
        message,
        executionId: "",
      } satisfies SendMainConversationMessageResponse);
    }),
  );
  const { client } = mountMainConversation();
  const user = userEvent.setup();
  const editor = screen.getByRole("textbox", { name: "输入消息" });
  const button = screen.getByRole("button", { name: "发送消息" });
  await user.type(editor, input);
  await waitFor(() => expect(button).toBeEnabled());
  await user.click(button);

  const answer = await screen.findByText(clarification);
  const answerRow = answer.closest<HTMLElement>("[data-message-id]");
  if (!answerRow) throw new Error("缺少伙伴澄清消息");
  await waitFor(() => expect(client.isMutating()).toBe(0));
  expect(screen.getByText(input)).toBeVisible();
  expect(screen.queryByText("正在等待回复")).toBeNull();
  expect(
    within(answerRow).getByRole("button", { name: "打开默认会话" }),
  ).toBeVisible();
  await user.click(within(answerRow).getByRole("button", { name: "更多操作" }));
  expect(screen.getByRole("menuitem", { name: "复制消息" })).toBeVisible();
  expect(screen.queryByRole("menuitem", { name: "打开工作会话" })).toBeNull();
  await user.keyboard("{Escape}");
  await user.click(within(answerRow).getByRole("button", { name: "回复" }));
  await user.type(editor, followUp);
  await waitFor(() => expect(button).toBeEnabled());
  await user.click(button);
  await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
  expect(send.mock.calls[1]?.[0]).toMatchObject({
    replyToMessageId: replyId,
    message: { text: followUp },
  });
  expect(
    await screen.findByText(followUp, { selector: "[data-message-id] p" }),
  ).toBeVisible();
});

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

it.each(["main", "source"])(
  "%s 的执行失败只显示在 Composer，等再次发送被接纳后清除",
  async (view) => {
    const atSource = view === "source";
    const release = deferred();
    const sent = vi.fn();
    const failed = {
      id: "failed-public-input",
      role: "user",
      conversationId,
      sequence: 2,
      sourceChatId: workChatId,
      content: "原交办仍保留",
      failureCode: "RuntimeQuotaExhausted",
    };
    server.use(
      http.get(path, () =>
        HttpResponse.json({
          ...emptyTimeline,
          messages: [
            {
              ...failed,
              sequence: 1,
              id: "earlier-failure",
              content: "上一次交办",
            },
            failed,
          ],
        }),
      ),
      http.get(path + "/sources/" + workChatId + "/messages", () =>
        HttpResponse.json({
          chat: { id: workChatId, wakerId: subjectId },
          resources: [
            {
              id: "earlier-source-failure",
              role: "assistant",
              status: "failed",
              errorCode: "RuntimeQuotaExhausted",
              createdSequence: 1,
            },
            {
              id: "source-input",
              role: "user",
              content: failed.content,
              createdSequence: 2,
            },
            {
              id: "latest-source-failure",
              role: "assistant",
              status: "failed",
              errorCode: "RuntimeQuotaExhausted",
              createdSequence: 3,
            },
          ],
        }),
      ),
      http.post(
        (atSource ? path + "/sources/" + workChatId : path) + "/messages",
        async ({ request }) => {
          const { submissionId } =
            (await request.json()) as SendMainConversationMessageData["body"];
          sent();
          await release.promise;
          return HttpResponse.json(
            atSource
              ? { execution: { id: "accepted-source" } }
              : { message: { ...committedInput(submissionId), sequence: 3 } },
          );
        },
      ),
    );
    const { client } = mountMainConversation(
      atSource ? "?source=" + workChatId : "",
    );
    try {
      const original = (await screen.findAllByText(failed.content)).find(
        (node) => !node.closest("[hidden]"),
      );
      expect(original).toBeVisible();
      const timeline = original?.closest("[data-chat-scroll-viewport]");
      expect(timeline).toBeInTheDocument();
      const editor = screen.getByRole("textbox", { name: "输入消息" });
      const reason = "当前额度已用尽，请切换模型或等待额度恢复后重试";
      const warning = await screen.findByRole("alert");
      expect(warning).toHaveTextContent(reason);
      expect(editor.closest("[data-chat-composer-frame]")).toContainElement(
        warning,
      );
      expect(timeline).not.toHaveTextContent("执行失败");
      expect(timeline).not.toHaveTextContent(reason);
      expect(
        timeline?.querySelector('[data-message-id="earlier-source-failure"]'),
      ).toBeNull();
      expect(
        timeline?.querySelector('[data-message-id="latest-source-failure"]'),
      ).toBeNull();

      const user = userEvent.setup();
      await user.type(editor, messageText);
      await user.click(screen.getByRole("button", { name: "发送消息" }));
      await waitFor(() => expect(sent).toHaveBeenCalledOnce());
      expect(warning).toBeVisible();
      release.resolve(undefined);
      await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
      await waitFor(() => expect(client.isMutating()).toBe(0));
      expect(original).toBeVisible();
    } finally {
      release.resolve(undefined);
    }
  },
);

it("工作记录在输入框顶栏显示最新执行失败，关闭后保留草稿", async () => {
  server.use(
    http.get(path + "/sources/" + workChatId + "/messages", () =>
      HttpResponse.json({
        chat: { id: workChatId, wakerId: subjectId },
        resources: [
          {
            id: "failed-answer",
            executionId: "failed-source-run",
            role: "assistant",
            status: "failed",
            errorCode: "RuntimeExecutionFailed",
            displayParts: [{ type: "text", text: "已经生成的答复" }],
          },
        ],
      }),
    ),
  );
  mountMainConversation("?source=" + workChatId);

  const answer = await screen.findByText("已经生成的答复");
  const editor = screen.getByRole("textbox", { name: "输入消息" });
  const error = screen.getByText("智能体执行失败");
  const frame = editor.closest("[data-chat-composer-frame]");
  expect(frame).not.toBeNull();
  expect(frame).toContainElement(error);
  expect(frame).toHaveClass("bg-status-danger-surface");
  expect(answer.closest("[data-chat-scroll-viewport]")).not.toContainElement(
    error,
  );

  const user = userEvent.setup();
  await user.type(editor, "下一条草稿");
  await user.click(screen.getByRole("button", { name: "关闭错误提示" }));
  expect(screen.queryByText("智能体执行失败")).toBeNull();
  expect(screen.getByRole("textbox", { name: "输入消息" })).toBe(editor);
  expect(editor).toHaveTextContent("下一条草稿");
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
            displayParts: [{ type: "reasoning", text: "工作记录过程" }],
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
      await timeline.findByText(messageText);
      expectSingleMessage(timeline);
      const waiting = timeline.getByText("正在等待回复");
      expect(waiting).toBeVisible();
      await waitFor(() =>
        expect(
          waiting
            .closest("[data-role=assistant]")
            ?.querySelector("[data-companion-avatar]"),
        ).toHaveAttribute("data-shape", "nuage"),
      );
      expect(
        timeline.queryByRole("button", { name: "打开工作会话" }),
      ).toBeNull();

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
      expectSingleMessage(timeline);
      expect(timeline.getByText("正在等待回复")).toBeVisible();

      releaseRead.resolve(undefined);
      if (receiptFirst) {
        const retry = await timeline.findByRole("button", { name: "重试" });
        await waitFor(() => expect(client.isMutating()).toBe(0));
        expectSingleMessage(timeline);
        await userEvent.click(retry);
      }
      expect(
        await timeline.findByRole("button", { name: "打开工作会话" }),
      ).toBeVisible();
      expect(
        timeline.getByText(messageText).closest("[data-message-id]"),
      ).toHaveAttribute("data-message-id", message.id);

      releaseReceipt.resolve(undefined);
      await waitFor(() => expect(client.isMutating()).toBe(0));
      expectSingleMessage(timeline);
      expect(timeline.getByText("正在等待回复")).toBeVisible();
      expect(screen.queryByRole("button", { name: "重试原发送" })).toBeNull();

      const firstReply = "交付结果已核对";
      snapshot = {
        ...snapshot,
        messages: [
          message,
          {
            id: "first-reply",
            sequence: 2,
            role: "assistant",
            content: firstReply,
            sourceChatId: workChatId,
            sourceWakerId: subjectId,
            replyToMessageId: message.id,
          },
        ],
      };
      mainConversationChanged();
      expect(await timeline.findByText(firstReply)).toBeVisible();
      expect(timeline.queryByText("正在等待回复")).toBeNull();
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
    await timeline.findByText(messageText);
    expectSingleMessage(timeline);
    expect(timeline.getByText("正在等待回复")).toBeVisible();
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
