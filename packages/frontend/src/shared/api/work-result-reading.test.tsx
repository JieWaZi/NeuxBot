import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { useRef, useState } from "react";
import { MemoryRouter } from "react-router-dom";
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
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { useChatRuntime } from "@/modules/chat/runtime/use-chat-runtime";
import { ChatThread } from "@/modules/chat/components/chat-thread";
import { conversationKeyOf } from "@/modules/chat/model/conversation-identity";
import { NotificationPresenter } from "@/modules/notification/components/notification-presenter";
import { NotificationBell } from "@/modules/notification/components/notification-bell";
import type {
  NotificationItem,
  NotificationSnapshot,
} from "@/modules/notification/model/notification";
import type { GlobalEventObserver } from "./global-events";
import "@/shared/i18n";
import { client } from "./generated/client.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkResultReceipt as Receipt } from "./generated/types.gen";
import {
  ChatWorkResultReader,
  useVisibleWorkResult,
} from "./work-result-reading";

const { eventObservers } = vi.hoisted(() => ({
  eventObservers: new Set<GlobalEventObserver>(),
}));
const { observerTopics } = vi.hoisted(() => ({
  observerTopics: new WeakMap<GlobalEventObserver, string>(),
}));
const { toastSpies } = vi.hoisted(() => ({
  toastSpies: {
    info: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
    dismiss: vi.fn(),
  },
}));
vi.mock("@/shared/ui/sonner", () => ({ toast: toastSpies }));

function intersection(target: Element): IntersectionObserverEntry {
  return {
    target,
    isIntersecting: true,
    intersectionRect: new DOMRect(0, 0, 100, 100),
    boundingClientRect: target.getBoundingClientRect(),
    rootBounds: target.parentElement?.getBoundingClientRect() ?? null,
    intersectionRatio: 1,
    time: performance.now(),
  };
}

const server = setupServer();
const origin = "http://reading.test";
const owner = "a".repeat(32);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 100, 100),
  );
});
afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  eventObservers.clear();
});
afterAll(() => server.close());
const selector = (receipt: Receipt) =>
  `[data-result="${receipt.reference?.messageId}"]`;
function Reader({
  message = "A",
  rendered = true,
}: {
  message?: string;
  rendered?: boolean;
}) {
  const root = useRef<HTMLDivElement>(null);
  useVisibleWorkResult({
    ownerKind: "chat",
    ownerId: owner,
    root,
    selector,
    rendered,
  });
  return (
    <div ref={root}>
      <p data-result={message}>原结果正文</p>
    </div>
  );
}

function ProductionChat({
  status = "completed",
}: {
  status?: "running" | "completed" | "failed";
}) {
  const { runtime, isRunning } = useChatRuntime({
    conversationKey: conversationKeyOf(`chat:${owner}`, owner),
    queueKey: owner,
    chatId: owner,
    messages: [
      {
        id: "final-message",
        role: "assistant",
        status,
        displayParts:
          status === "failed" ? [] : [{ type: "text", text: "真实最终正文" }],
        ...(status === "failed"
          ? { errorCode: "tool_failed", errorMessage: "真实执行失败" }
          : {}),
      },
    ],
    latestSequence: 1,
    activeExecutionId: status === "running" ? "final-message" : undefined,
    isHistoryReady: true,
    isLoading: false,
    isSendDisabled: false,
    attachmentOnlyText: "请查看附件。",
    incompleteErrorText: "执行未完成",
    selection: {
      runtimeId: "codex",
      model: "gpt-5",
      reasoning: "high",
      permissionLevel: "full-access",
    },
    send: vi.fn(),
    cancel: vi.fn(),
    onStarted: vi.fn(),
    onTerminal: vi.fn(),
  });
  return (
    <ChatThread
      runtime={runtime}
      isRunning={isRunning}
      fileScopeChatId={owner}
      resultSource={{ chatId: owner, owner: { kind: "chat", id: owner } }}
      wakerName="测试伙伴"
      emptyState={<div>空对话</div>}
      renderComposerContextStrip={undefined}
      showComposer={false}
      sendFailure={{ kind: "none" }}
      hasEarlierMessages={false}
      isLoadingEarlier={false}
      onLoadEarlier={async () => undefined}
    />
  );
}

it("生产聊天正文只在前台被实际看见后确认准确引用，焦点恢复不丢阅读", async () => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  const focus = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  Object.defineProperty(HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: vi.fn(),
  });
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  const observations: {
    callback: IntersectionObserverCallback;
    target?: Element;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      current: { callback: IntersectionObserverCallback; target?: Element };
      constructor(callback: IntersectionObserverCallback) {
        this.current = { callback };
        observations.push(this.current);
      }
      observe(target: Element) {
        this.current.target = target;
      }
      disconnect() {}
      unobserve() {}
    },
  );
  const viewed = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
      HttpResponse.json({
        result: {
          ownerKind: "chat",
          ownerId: owner,
          latestVersion: 1,
          viewedVersion: 0,
          reference: {
            chatId: owner,
            executionId: "final-message",
            messageId: "final-message",
          },
        },
      }),
    ),
    http.post(
      `${origin}/api/v1/work-board/results/chat/${owner}/view`,
      async ({ request }) => {
        viewed(await request.json());
        return HttpResponse.json({ success: true });
      },
    ),
  );
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <MemoryRouter initialEntries={[`/chats/${owner}`]}>
      <QueryClientProvider client={query}>
        <TooltipProvider>
          <ChatQueueProvider>
            <ProductionChat />
          </ChatQueueProvider>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  await screen.findByText("真实最终正文");
  await waitFor(() =>
    expect(
      document.querySelector(
        `[data-message-id="final-message"] [data-work-result-body]`,
      ),
    ).not.toBeNull(),
  );
  await waitFor(() =>
    expect(observations.some((value) => value.target)).toBe(true),
  );
  focus.mockReturnValue(false);
  act(() => {
    window.dispatchEvent(new Event("blur"));
  });
  const deliverVisible = () => {
    for (const observation of observations)
      if (observation.target)
        observation.callback(
          [intersection(observation.target)],
          {} as IntersectionObserver,
        );
  };
  await act(async () => {
    deliverVisible();
    await Promise.resolve();
  });
  expect(viewed).not.toHaveBeenCalled();
  focus.mockReturnValue(true);
  act(() => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() =>
    expect(observations.some((value) => value.target)).toBe(true),
  );
  act(deliverVisible);
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
  expect(viewed).toHaveBeenLastCalledWith({
    version: 1,
    reference: {
      chatId: owner,
      executionId: "final-message",
      messageId: "final-message",
    },
  });
  view.unmount();
  query.clear();
});

it("首次成果 GET 挂起时 WS 新版本先到，直播正文结束后才确认新版本", async () => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  Object.defineProperty(HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: vi.fn(),
  });
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  const observations: {
    callback: IntersectionObserverCallback;
    target?: Element;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      current: { callback: IntersectionObserverCallback; target?: Element };
      constructor(callback: IntersectionObserverCallback) {
        this.current = { callback };
        observations.push(this.current);
      }
      observe(target: Element) {
        this.current.target = target;
      }
      disconnect() {}
      unobserve() {}
    },
  );
  let releaseInitial: (value: object) => void = () => {};
  const read = vi.fn();
  const viewed = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, async () => {
      read();
      if (read.mock.calls.length === 1)
        return HttpResponse.json(
          await new Promise<object>((resolve) => {
            releaseInitial = resolve;
          }),
        );
      return HttpResponse.json({
        result: {
          ownerKind: "chat",
          ownerId: owner,
          latestVersion: 2,
          viewedVersion: 0,
          reference: {
            chatId: owner,
            executionId: "final-message",
            messageId: "final-message",
          },
        },
      });
    }),
    http.post(
      `${origin}/api/v1/work-board/results/chat/${owner}/view`,
      async ({ request }) => {
        viewed(await request.json());
        return HttpResponse.json({ success: true });
      },
    ),
  );
  const queries = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const content = (status: "running" | "completed") => (
    <MemoryRouter initialEntries={[`/chats/${owner}`]}>
      <QueryClientProvider client={queries}>
        <TooltipProvider>
          <ChatQueueProvider>
            <ProductionChat status={status} />
          </ChatQueueProvider>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  const view = render(content("running"));
  await screen.findByText("真实最终正文");
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
  expect(document.querySelector("[data-work-result-owner-id]")).toHaveAttribute(
    "data-work-result-chat-id",
    owner,
  );
  act(() => {
    for (const observer of eventObservers)
      observer.onEvent({
        protocolVersion: 1,
        epoch: "epoch",
        connectionId: "connection",
        deliverySequence: 1,
        topic: "work-results",
        type: "work-results.changed",
        resource: { kind: "chat", id: owner },
        eventId: "receipt-version-2",
        payload: {},
      });
  });
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  await act(async () => {
    releaseInitial({});
    await Promise.resolve();
  });
  expect(viewed).not.toHaveBeenCalled();
  view.rerender(content("completed"));
  await waitFor(() => {
    const body = document.querySelector(
      `[data-message-id="final-message"] [data-work-result-body]`,
    );
    expect(body).not.toBeNull();
    expect(observations.some((value) => value.target === body)).toBe(true);
  });
  act(() => {
    for (const observation of observations)
      if (observation.target)
        observation.callback(
          [intersection(observation.target)],
          {} as IntersectionObserver,
        );
  });
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
  expect(viewed).toHaveBeenLastCalledWith({
    version: 2,
    reference: {
      chatId: owner,
      executionId: "final-message",
      messageId: "final-message",
    },
  });
  expect(document.querySelector("[data-work-result-owner-id]")).toHaveAttribute(
    "data-work-result-version",
    "2",
  );
  view.unmount();
  queries.clear();
});

it("只在真实结果可见后提交观察版本，A 在 B 发布后查看不会确认 B", async () => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  const callbacks: IntersectionObserverCallback[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: IntersectionObserverCallback) {
        callbacks.push(callback);
      }
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
  let latest = 1;
  const viewed = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
      HttpResponse.json({
        result: {
          ownerKind: "chat",
          ownerId: owner,
          latestVersion: latest,
          viewedVersion: 0,
          reference: { chatId: owner, messageId: latest === 1 ? "A" : "B" },
          availableAt: 100,
        },
      }),
    ),
    http.post(
      `${origin}/api/v1/work-board/results/chat/${owner}/view`,
      async ({ request }) => {
        viewed(await request.json());
        return HttpResponse.json({ success: true });
      },
    ),
  );
  const query = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const { rerender } = render(
    <QueryClientProvider client={query}>
      <Reader />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(callbacks).toHaveLength(1));
  expect(viewed).not.toHaveBeenCalled();
  latest = 2;
  const rect = new DOMRect(0, 0, 100, 100);
  const entry = {
    target: document.querySelector('[data-result="A"]'),
    isIntersecting: true,
    intersectionRect: rect,
  } as IntersectionObserverEntry;
  act(() => callbacks[0]?.([entry], {} as IntersectionObserver));
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
  expect(viewed).toHaveBeenLastCalledWith({
    version: 1,
    reference: { chatId: owner, messageId: "A" },
  });
  await waitFor(() =>
    expect(
      query
        .getQueryCache()
        .findAll()
        .some((item) =>
          JSON.stringify(item.state.data).includes('"latestVersion":2'),
        ),
    ).toBe(true),
  );
  act(() => callbacks[0]?.([entry], {} as IntersectionObserver));
  expect(viewed).toHaveBeenCalledTimes(1);
  rerender(
    <QueryClientProvider client={query}>
      <Reader message="B" />
    </QueryClientProvider>,
  );
  await waitFor(() => expect(callbacks.length).toBeGreaterThan(1));
  act(() =>
    callbacks.at(-1)?.(
      [
        {
          ...entry,
          target: document.querySelector('[data-result="B"]'),
        } as IntersectionObserverEntry,
      ],
      {} as IntersectionObserver,
    ),
  );
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(2));
  expect(viewed).toHaveBeenLastCalledWith({
    version: 2,
    reference: { chatId: owner, messageId: "B" },
  });
});

it.each([
  { visibility: "hidden", rendered: true },
  { visibility: "visible", rendered: false },
] as const)(
  "不可见或未渲染结果不查询也不提交阅读：$visibility / $rendered",
  async ({ visibility, rendered }) => {
    vi.spyOn(document, "visibilityState", "get").mockReturnValue(visibility);
    const read = vi.fn();
    server.use(
      http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () => {
        read();
        return HttpResponse.json({});
      }),
    );
    render(
      <QueryClientProvider client={new QueryClient()}>
        <Reader rendered={rendered} />
      </QueryClientProvider>,
    );
    await act(async () => {
      await Promise.resolve();
    });
    expect(read).not.toHaveBeenCalled();
  },
);

it("从看板打开早期结果时按原历史分页定位，读取历史本身不会确认阅读", async () => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  const scroll = vi.fn();
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    setTimeout(() => callback(0), 0),
  );
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true,
    value: scroll,
  });
  server.use(
    http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
      HttpResponse.json({}),
    ),
  );
  const load = vi.fn();
  function EarlierResult() {
    const viewport = useRef<HTMLDivElement>(null);
    const [earlier, setEarlier] = useState(false);
    return (
      <>
        <ChatWorkResultReader
          chatId={owner}
          viewport={viewport}
          hasEarlierMessages={!earlier}
          onLoadEarlier={async () => {
            load();
            setEarlier(true);
          }}
        />
        <div ref={viewport}>
          {earlier ? (
            <p data-message-id="old-result">历史结果</p>
          ) : (
            <p>当前历史窗口</p>
          )}
        </div>
      </>
    );
  }
  render(
    <MemoryRouter initialEntries={[`/chats/${owner}?resultMessage=old-result`]}>
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <EarlierResult />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: "start" }));
  expect(load).toHaveBeenCalledTimes(1);
});

vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    eventObservers.add(observer);
    observerTopics.set(observer, topic);
    if (topic === "notifications") observer.onState?.("open");
    return () => eventObservers.delete(observer);
  },
}));

it.each(["裁剪外", "inert"])(
  "迟到的可见回调不能确认当前实际%s的正文",
  async (state) => {
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    let callback: IntersectionObserverCallback | undefined;
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(value: IntersectionObserverCallback) {
          callback = value;
        }
        observe() {}
        disconnect() {}
        unobserve() {}
      },
    );
    const viewed = vi.fn();
    server.use(
      http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
        HttpResponse.json({
          result: {
            ownerKind: "chat",
            ownerId: owner,
            latestVersion: 1,
            viewedVersion: 0,
            reference: { chatId: owner, messageId: "A" },
          },
        }),
      ),
      http.post(
        `${origin}/api/v1/work-board/results/chat/${owner}/view`,
        () => {
          viewed();
          return HttpResponse.json({ success: true });
        },
      ),
    );
    const queries = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const view = render(
      <QueryClientProvider client={queries}>
        <Reader />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(callback).toBeDefined());
    const target = document.querySelector<HTMLElement>('[data-result="A"]');
    if (!target?.parentElement) throw new Error("缺少实际结果正文");
    if (state === "inert") target.parentElement.setAttribute("inert", "");
    else {
      target.parentElement.style.overflowY = "auto";
      Object.defineProperty(target, "getBoundingClientRect", {
        configurable: true,
        value: () => new DOMRect(0, 150, 100, 20),
      });
    }
    await act(async () => {
      callback?.([intersection(target)], {} as IntersectionObserver);
      await Promise.resolve();
    });
    expect(viewed).not.toHaveBeenCalled();
    view.unmount();
    queries.clear();
  },
);

it("生产聊天失败消息没有最终文本时，错误正文仍能确认原交付引用", async () => {
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  Object.defineProperty(HTMLElement.prototype, "scrollTo", {
    configurable: true,
    value: vi.fn(),
  });
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  let callback: IntersectionObserverCallback | undefined;
  let target: Element | undefined;
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(value: IntersectionObserverCallback) {
        callback = value;
      }
      observe(value: Element) {
        target = value;
      }
      disconnect() {}
      unobserve() {}
    },
  );
  const viewed = vi.fn();
  server.use(
    http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
      HttpResponse.json({
        result: {
          ownerKind: "chat",
          ownerId: owner,
          latestVersion: 1,
          viewedVersion: 0,
          reference: {
            chatId: owner,
            executionId: "final-message",
            messageId: "final-message",
          },
        },
      }),
    ),
    http.post(
      `${origin}/api/v1/work-board/results/chat/${owner}/view`,
      async ({ request }) => {
        viewed(await request.json());
        return HttpResponse.json({ success: true });
      },
    ),
  );
  const queries = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <MemoryRouter initialEntries={[`/chats/${owner}`]}>
      <QueryClientProvider client={queries}>
        <TooltipProvider>
          <ChatQueueProvider>
            <ProductionChat status="failed" />
          </ChatQueueProvider>
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  await screen.findByText("真实执行失败");
  await waitFor(() => expect(target).toBeDefined());
  act(() => {
    if (target) callback?.([intersection(target)], {} as IntersectionObserver);
  });
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
  expect(viewed).toHaveBeenLastCalledWith({
    version: 1,
    reference: {
      chatId: owner,
      executionId: "final-message",
      messageId: "final-message",
    },
  });
  view.unmount();
  queries.clear();
});

it.each([true, false])(
  "真实 Reader/Presenter 链路只按成功阅读回执退出待查看（提交成功：%s）",
  async (accepted) => {
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    Object.defineProperty(HTMLElement.prototype, "scrollTo", {
      configurable: true,
      value: vi.fn(),
    });
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }));
    const observations: {
      callback: IntersectionObserverCallback;
      target?: Element;
    }[] = [];
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        current: { callback: IntersectionObserverCallback; target?: Element };
        constructor(callback: IntersectionObserverCallback) {
          this.current = { callback };
          observations.push(this.current);
        }
        observe(target: Element) {
          this.current.target = target;
        }
        disconnect() {}
        unobserve() {}
      },
    );
    const reference = {
      chatId: owner,
      executionId: "final-message",
      messageId: "final-message",
    };
    const result: NotificationItem = {
      id: `result:chat:${owner}:2`,
      kind: "result",
      bucket: "unread",
      source: "chat",
      title: "真实最终正文",
      description: "",
      createdAt: 101,
      version: 2,
      dismissed: false,
      executor: { kind: "waker", id: "waker-1", name: "测试伙伴" },
      target: {
        kind: "result",
        ownerKind: "chat",
        ownerId: owner,
        resultVersion: 2,
        reference,
      },
    };
    const approval: NotificationItem = {
      id: "approval:waiting-request",
      kind: "approval",
      bucket: "action",
      source: "chat",
      title: "待审批",
      description: "",
      createdAt: 50,
      version: 1,
      dismissed: false,
      executor: result.executor,
      target: { kind: "approval", chatId: owner, requestId: "waiting-request" },
    };
    const summary = (
      revision: number,
      unread: boolean,
    ): NotificationSnapshot => ({
      epoch: "a".repeat(32),
      revision,
      synced: true,
      generatedAt: 100,
      actionCount: 1,
      unreadResultCount: unread ? 1 : 0,
      unreadFailureCount: 0,
      totalCount: unread ? 2 : 1,
      actionPreview: [approval],
      unreadPreview: unread ? [result] : [],
    });
    let viewedVersion = 0;
    const viewed = vi.fn();
    server.use(
      http.get(`${origin}/api/v1/notifications/snapshot`, () =>
        HttpResponse.json(summary(1, true)),
      ),
      http.get(`${origin}/api/v1/work-board/results/chat/${owner}`, () =>
        HttpResponse.json({
          result: {
            ownerKind: "chat",
            ownerId: owner,
            latestVersion: 2,
            viewedVersion,
            reference,
          },
        }),
      ),
      http.post(
        `${origin}/api/v1/work-board/results/chat/${owner}/view`,
        async ({ request }) => {
          viewed(await request.json());
          if (!accepted)
            return HttpResponse.json(
              { key: "DatabaseError", message: "读取故障" },
              { status: 503 },
            );
          viewedVersion = 2;
          return HttpResponse.json({ success: true });
        },
      ),
    );
    function Notices() {
      const [connected, setConnected] = useState(false);
      return (
        <>
          <NotificationPresenter onConnectionChange={setConnected} />
          <NotificationBell connected={connected} onOpen={() => {}} />
        </>
      );
    }
    const queries = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const view = render(
      <MemoryRouter initialEntries={[`/chats/${owner}`]}>
        <QueryClientProvider client={queries}>
          <TooltipProvider>
            <Notices />
            <ChatQueueProvider>
              <ProductionChat />
            </ChatQueueProvider>
          </TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    await screen.findByText("真实最终正文");
    await waitFor(() =>
      expect(observations.some((value) => value.target)).toBe(true),
    );
    const emitNotification = (type: string, payload: unknown) => {
      for (const observer of eventObservers)
        if (observerTopics.get(observer) === "notifications")
          observer.onEvent({
            protocolVersion: 1,
            epoch: "a".repeat(32),
            connectionId: "connection",
            deliverySequence: 1,
            topic: "notifications",
            type,
            resource: { kind: "global" },
            eventId: `event-${type}`,
            payload,
          });
    };
    await act(async () => {
      emitNotification("notifications.snapshot", {
        snapshot: summary(1, true),
      });
      emitNotification("notifications.updated", {
        snapshot: summary(2, true),
        upserts: [result],
        removals: [],
        affectedResources: [{ kind: "chat", id: owner }],
      });
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
    });
    expect(toastSpies.info).not.toHaveBeenCalled();
    expect(
      await screen.findByRole("button", { name: "通知 2 项，打开任务看板" }),
    ).toBeInTheDocument();
    act(() => {
      for (const observation of observations)
        if (observation.target)
          observation.callback(
            [intersection(observation.target)],
            {} as IntersectionObserver,
          );
    });
    await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
    expect(viewed).toHaveBeenLastCalledWith({ version: 2, reference });
    await waitFor(() =>
      expect(
        queries
          .getMutationCache()
          .getAll()
          .some(
            (mutation) =>
              mutation.state.status === (accepted ? "success" : "error"),
          ),
      ).toBe(true),
    );
    if (accepted) {
      act(() =>
        emitNotification("notifications.updated", {
          snapshot: summary(3, false),
          upserts: [],
          removals: [result.id],
          affectedResources: [{ kind: "chat", id: owner }],
        }),
      );
      expect(
        await screen.findByRole("button", { name: "通知 1 项，打开任务看板" }),
      ).toBeInTheDocument();
    } else
      expect(
        screen.getByRole("button", { name: "通知 2 项，打开任务看板" }),
      ).toBeInTheDocument();
    expect(viewedVersion).toBe(accepted ? 2 : 0);
    view.unmount();
    queries.clear();
  },
);

it.each(["chat", "team_thread"] as const)(
  "共享聊天阅读器显式使用%s归属，不受外层相反业务路由影响",
  async (kind) => {
    const ownerId = kind === "chat" ? owner : "b".repeat(32);
    const reads: string[] = [];
    const viewed = vi.fn();
    let callback: IntersectionObserverCallback | undefined;
    let target: Element | undefined;
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(value: IntersectionObserverCallback) {
          callback = value;
        }
        observe(value: Element) {
          target = value;
        }
        unobserve() {}
        disconnect() {}
      },
    );
    server.use(
      http.get(
        `${origin}/api/v1/work-board/results/:kind/:id`,
        ({ params }) => {
          reads.push(`${params.kind}:${params.id}`);
          return HttpResponse.json({
            result: {
              ownerKind: kind,
              ownerId,
              latestVersion: 1,
              viewedVersion: 0,
              reference: { chatId: owner, messageId: "delivery" },
            },
          });
        },
      ),
      http.post(
        `${origin}/api/v1/work-board/results/${kind}/${ownerId}/view`,
        async ({ request }) => {
          viewed(await request.json());
          return HttpResponse.json({ success: true });
        },
      ),
    );
    function ActualContent() {
      const root = useRef<HTMLDivElement>(null);
      return (
        <>
          <ChatWorkResultReader
            chatId={owner}
            owner={{ kind, id: ownerId }}
            viewport={root}
          />
          <div ref={root}>
            <article data-message-id="delivery">
              <div data-work-result-body>原交付正文</div>
            </article>
          </div>
        </>
      );
    }
    const cache = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const view = render(
      <MemoryRouter
        initialEntries={[
          kind === "chat"
            ? `/teams/team-1/threads/${"c".repeat(32)}`
            : `/chats/${owner}`,
        ]}
      >
        <QueryClientProvider client={cache}>
          <ActualContent />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(target).toBeDefined());
    act(() => {
      if (target)
        callback?.([intersection(target)], {} as IntersectionObserver);
    });
    await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
    expect(reads.every((value) => value === `${kind}:${ownerId}`)).toBe(true);
    expect(viewed).toHaveBeenLastCalledWith({
      version: 1,
      reference: { chatId: owner, messageId: "delivery" },
    });
    view.unmount();
    cache.clear();
  },
);
