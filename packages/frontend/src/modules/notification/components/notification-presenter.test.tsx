import { FailureNoticeContent } from "./failure-notice-content";
import { useState, type ReactNode } from "react";
import {
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
} from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { ChatApprovalCard } from "@/modules/chat/components/chat-approval-card";
import { ChatElicitationCard } from "@/modules/chat/components/chat-elicitation-card";
import { HumanRequestCard } from "@/modules/workflow/components/human-request";
import { OperationRequestCard } from "@/modules/workflow/components/operation-request";
import { TeamTaskBoard } from "@/modules/team/components/team-task-board";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
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
import type {
  GlobalEventObserver,
  GlobalEventFrame,
} from "@/shared/api/global-events";
import { client } from "@/shared/api/generated/client.gen";
import { TooltipProvider } from "@/shared/ui/tooltip";
import "@/shared/i18n";
import {
  getNotificationSnapshotQueryKey,
  notificationListOptions,
  notificationItemQueryKey,
} from "../api/notification-queries";
import type {
  NotificationItem,
  NotificationSnapshot,
} from "../model/notification";
import { NotificationPresenter } from "./notification-presenter";
import { NotificationBell } from "./notification-bell";

const { observers, warning, info, error, dismiss } = vi.hoisted(() => ({
  observers: new Set<GlobalEventObserver>(),
  warning: vi.fn(),
  info: vi.fn(),
  error: vi.fn(),
  dismiss: vi.fn(),
}));
vi.mock("@/shared/api/global-events", () => ({
  registerCoreConnectionRecovery: () => () => {},
  subscribeGlobalEvents: (
    _topic: string,
    _resource: unknown,
    observer: GlobalEventObserver,
  ) => {
    observers.add(observer);
    observer.onState?.("open");
    return () => observers.delete(observer);
  },
}));
vi.mock("@/shared/ui/sonner", () => ({
  toast: { warning, info, error, dismiss },
}));
const origin = "http://notification.test";
const notice: NotificationItem = {
  id: "approval:request-1",
  kind: "approval",
  bucket: "action",
  source: "chat",
  title: "执行命令",
  description: "需要确认",
  createdAt: 101,
  version: 1,
  executor: { id: "waker-1", kind: "waker", name: "伙伴" },
  target: {
    kind: "approval",
    chatId: "chat-1",
    wakerId: "waker-1",
    requestId: "request-1",
  },
  dismissed: false,
};
function snapshot(
  revision = 1,
  overrides: Partial<NotificationSnapshot> = {},
): NotificationSnapshot {
  return {
    epoch: "a".repeat(32),
    revision,
    synced: true,
    actionCount: 120,
    unreadResultCount: 0,
    unreadFailureCount: 0,
    totalCount: 120,
    actionPreview: [{ ...notice, createdAt: 50 }],
    unreadPreview: [],
    generatedAt: 100,
    ...overrides,
  };
}
let startup = snapshot();
let dismissedIds: string[] = [];
const server = setupServer(
  http.get(`${origin}/api/v1/notifications/snapshot`, () =>
    HttpResponse.json(startup),
  ),
  http.post(
    `${origin}/api/v1/notifications/:noticeId/dismiss`,
    ({ params }) => {
      dismissedIds.push(String(params.noticeId));
      return HttpResponse.json({ success: true });
    },
  ),
);
beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
beforeEach(() => {
  // 自动卸载会关闭上一项测试的提示，下一项从空调用记录开始。
  vi.clearAllMocks();
  client.setConfig({ baseUrl: origin });
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  startup = snapshot();
  dismissedIds = [];
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: true,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
});
afterEach(() => {
  server.resetHandlers();
  observers.clear();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
afterAll(() => server.close());
function NotificationShell() {
  const [connected, setConnected] = useState(false);
  return (
    <>
      <NotificationPresenter onConnectionChange={setConnected} />
      <NotificationBell connected={connected} onOpen={() => {}} />
    </>
  );
}
function mount(children?: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <TooltipProvider>
          <NotificationShell />
          {children}
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { queryClient, ...result };
}
async function emit(type: string, payload: unknown, paint = true) {
  const frame: GlobalEventFrame = {
    protocolVersion: 1,
    epoch: "a".repeat(32),
    connectionId: "connection",
    deliverySequence: 1,
    topic: "notifications",
    type,
    resource: { kind: "global" },
    eventId: "event",
    payload:
      type === "notifications.updated" &&
      typeof payload === "object" &&
      payload !== null
        ? { affectedResources: [], ...payload }
        : payload,
  };
  await act(async () => {
    for (const observer of observers) observer.onEvent(frame);
    if (paint)
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
  });
}
it("恢复完整计数超过预览，显示99+与准确可访问名称且不回放历史提示", async () => {
  const view = mount();
  await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
  expect(screen.getByText("99+")).toBeInTheDocument();
  await emit("notifications.snapshot", { snapshot: startup });
  expect(warning).not.toHaveBeenCalled();
  view.unmount();
  expect(observers.size).toBe(0);
});
it("同一通知去重，自动收起只提交dismiss，不提前减少数字", async () => {
  const { queryClient } = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const next = { ...notice, id: "approval:request-2" };
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [next],
    removals: [],
  });
  await emit("notifications.updated", {
    snapshot: snapshot(3),
    upserts: [next],
    removals: [],
  });
  expect(warning).toHaveBeenCalledTimes(1);
  const options = warning.mock.calls[0]?.[1];
  expect(options.duration).toBe(8000);
  act(() => options.onAutoClose());
  await waitFor(() => expect(dismissedIds).toEqual(["approval:request-2"]));
  expect(
    queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.totalCount,
  ).toBe(120);
});
it("真实前台已展示的请求抑制重复提示，仍保留铃铛计数", async () => {
  mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const card = document.createElement("section");
  card.dataset.notificationId = "approval:request-2";
  vi.spyOn(card, "getBoundingClientRect").mockReturnValue(rect(40, 80));
  document.body.append(card);
  try {
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [{ ...notice, id: card.dataset.notificationId }],
      removals: [],
    });
    expect(warning).not.toHaveBeenCalled();
    expect(dismissedIds).toEqual([]);
  } finally {
    card.remove();
  }
});
it("重连恢复不回放，失效事件关闭旧提示，非法数字不会覆盖旧摘要", async () => {
  const { queryClient } = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [{ ...notice, id: "approval:request-2" }],
    removals: [],
  });
  act(() => {
    for (const observer of observers) observer.onResync?.("connection_changed");
  });
  await emit("notifications.snapshot", { snapshot: snapshot(3) });
  await emit("notifications.updated", {
    snapshot: snapshot(4),
    upserts: [{ ...notice, id: "approval:request-2" }],
    removals: ["approval:request-2"],
  });
  expect(warning).toHaveBeenCalledTimes(1);
  expect(dismiss).toHaveBeenCalledWith("approval:request-2");
  await emit("notifications.updated", {
    snapshot: snapshot(5, { totalCount: -1 }),
    upserts: [],
    removals: [],
  });
  expect(
    queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.revision,
  ).toBe(4);
});
it.each(["delivery_gap", "invalid_frame"])(
  "%s 的服务器恢复快照重新接通铃铛，之后只提示真实新通知",
  async (reason) => {
    mount();
    await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
    await emit("notifications.snapshot", { snapshot: startup });
    act(() => {
      for (const observer of observers) observer.onResync?.(reason);
    });
    await screen.findByRole("button", { name: "通知 120 项，正在同步" });
    const backlog = {
      ...notice,
      id: "approval:during-gap",
      createdAt: 150,
    };
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [backlog],
      removals: [],
    });
    expect(warning).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "通知 120 项，正在同步" }),
    ).toBeInTheDocument();
    const restored = snapshot(3, {
      generatedAt: 200,
      actionPreview: [backlog],
    });
    await emit("notifications.snapshot", { snapshot: restored });
    await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
    expect(warning).not.toHaveBeenCalled();
    await emit("notifications.updated", {
      snapshot: { ...restored, revision: 4 },
      upserts: [
        backlog,
        { ...notice, id: "approval:after-gap", createdAt: 201 },
      ],
      removals: [],
    });
    expect(warning).toHaveBeenCalledTimes(1);
    expect(warning.mock.calls[0]?.[1]?.id).toBe("approval:after-gap");
  },
);
it("晚返回REST不能覆盖同epoch的较新推送，也不能倒退到重启前epoch", async () => {
  let finish = () => {};
  let started = false;
  server.use(
    http.get(`${origin}/api/v1/notifications/snapshot`, async () => {
      await new Promise<void>((resolve) => {
        finish = resolve;
        started = true;
      });
      return HttpResponse.json(snapshot());
    }),
  );
  const { queryClient } = mount();
  await waitFor(() => expect(observers.size).toBe(1));
  await waitFor(() => expect(queryClient.isFetching()).toBe(1));
  await waitFor(() => expect(started).toBe(true));
  await emit("notifications.snapshot", {
    snapshot: snapshot(2, { epoch: "b".repeat(32) }),
  });
  finish();
  await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  expect(
    queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.epoch,
  ).toBe("b".repeat(32));
  expect(
    queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.revision,
  ).toBe(2);
});

it("Core 地址变化后保持同一通知缓存并接纳新epoch后续更新", async () => {
  mount();
  await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
  await emit("notifications.snapshot", { snapshot: startup });
  act(() => {
    client.setConfig({ baseUrl: "http://restarted-core.test" });
    for (const observer of observers) observer.onResync?.("connection_changed");
  });
  const restored = snapshot(1, {
    epoch: "b".repeat(32),
    actionCount: 1,
    totalCount: 1,
  });
  await emit("notifications.snapshot", { snapshot: restored });
  await screen.findByRole("button", { name: "通知 1 项，打开任务看板" });
  await emit("notifications.updated", {
    snapshot: { ...restored, revision: 2, actionCount: 2, totalCount: 2 },
    upserts: [{ ...notice, id: "approval:request-2" }],
    removals: [],
  });
  await screen.findByRole("button", { name: "通知 2 项，打开任务看板" });
  expect(warning).toHaveBeenCalledTimes(1);
});

it("标题限制按Unicode码点匹配Core，不丢弃包含emoji的合法通知", async () => {
  mount();
  await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
  await emit("notifications.snapshot", { snapshot: startup });
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [{ ...notice, id: "approval:unicode", title: "📊".repeat(200) }],
    removals: [],
  });
  expect(warning).toHaveBeenCalledTimes(1);
});

it("读取失败保留旧计数并显示陈旧，恢复同步不重放积压提示", async () => {
  mount();
  await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
  await emit("notifications.snapshot", { snapshot: startup });
  await emit("notifications.updated", {
    snapshot: snapshot(2, { synced: false }),
    upserts: [],
    removals: [],
  });
  await screen.findByRole("button", { name: "通知 120 项，正在同步" });
  expect(screen.getByText("99+")).toBeInTheDocument();
  expect(warning).not.toHaveBeenCalled();
  await emit("notifications.updated", {
    snapshot: snapshot(3),
    upserts: [{ ...notice, id: "approval:recovered" }],
    removals: [],
  });
  await screen.findByRole("button", { name: "通知 120 项，打开任务看板" });
  expect(warning).not.toHaveBeenCalled();
  await emit("notifications.updated", {
    snapshot: snapshot(4),
    upserts: [{ ...notice, id: "approval:new-after-recovery", createdAt: 102 }],
    removals: [],
  });
  expect(warning).toHaveBeenCalledTimes(1);
});

function resultNotice(): NotificationItem {
  return {
    ...notice,
    id: "result:chat:chat-result:1",
    kind: "result",
    bucket: "unread",
    target: {
      kind: "result",
      ownerKind: "chat",
      ownerId: "chat-result",
      resultVersion: 1,
      reference: { chatId: "chat-result", messageId: "message-result" },
    },
  };
}
function rect(top: number, height: number, left = 0, width = 300) {
  return {
    top,
    bottom: top + height,
    left,
    right: left + width,
    width,
    height,
    x: left,
    y: top,
    toJSON: () => ({}),
  };
}
function presentedMessage(messageId: string, clipped = false) {
  const viewport = document.createElement("section");
  viewport.dataset.workResultOwnerKind = "chat";
  viewport.dataset.workResultOwnerId = "chat-result";
  viewport.dataset.workResultChatId = "chat-result";
  viewport.dataset.workResultVersion = "1";
  viewport.style.overflowY = "auto";
  const message = document.createElement("article");
  message.dataset.messageId = messageId;
  const body = document.createElement("div");
  body.dataset.workResultBody = "";
  body.textContent = "真实已结束成果";
  vi.spyOn(viewport, "getBoundingClientRect").mockReturnValue(rect(20, 200));
  vi.spyOn(body, "getBoundingClientRect").mockReturnValue(
    rect(clipped ? 240 : 40, 80),
  );
  message.append(body);
  viewport.append(message);
  document.body.append(viewport);
  return { viewport, body };
}
it.each(["chat", "team_thread"] as const)(
  "当前前台%s的精确成果正文可见时不重复弹提示",
  async (ownerKind) => {
    const result = resultNotice();
    result.target.ownerKind = ownerKind;
    const view = mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    const content = presentedMessage("message-result");
    content.viewport.dataset.workResultOwnerKind = ownerKind;
    try {
      await emit("notifications.updated", {
        snapshot: snapshot(2),
        upserts: [result],
        removals: [],
      });
      expect(info).not.toHaveBeenCalled();
      expect(dismissedIds).toEqual([]);
    } finally {
      content.viewport.remove();
      view.unmount();
    }
  },
);
it("当前会话审批表单滚动到可视区外时仍提示且不处理待办", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const content = presentedMessage("unrelated-message", true);
  const approval = { ...notice, id: "approval:below-viewport" };
  content.body.removeAttribute("data-work-result-body");
  content.body.dataset.notificationId = approval.id;
  try {
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [approval],
      removals: [],
    });
    expect(warning).toHaveBeenCalledTimes(1);
    expect(dismissedIds).toEqual([]);
  } finally {
    content.viewport.remove();
    view.unmount();
  }
});
it.each(["滚动区外", "失去焦点", "隐藏Scene", "其他消息"])(
  "%s的正文不抑制成果提示",
  async (state) => {
    const view = mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    const content = presentedMessage(
      state === "其他消息" ? "other-message" : "message-result",
      state === "滚动区外",
    );
    if (state === "失去焦点")
      vi.spyOn(document, "hasFocus").mockReturnValue(false);
    if (state === "隐藏Scene") content.viewport.setAttribute("inert", "");
    try {
      await emit("notifications.updated", {
        snapshot: snapshot(2),
        upserts: [resultNotice()],
        removals: [],
      });
      expect(info).toHaveBeenCalledTimes(1);
      expect(dismissedIds).toEqual([]);
    } finally {
      content.viewport.remove();
      view.unmount();
    }
  },
);
it("fork继承同一个messageId但实际会话不同，仍提示原会话成果", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const content = presentedMessage("message-result");
  content.viewport.dataset.workResultOwnerId = "fork-chat";
  content.viewport.dataset.workResultChatId = "fork-chat";
  try {
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [resultNotice()],
      removals: [],
    });
    expect(info).toHaveBeenCalledTimes(1);
  } finally {
    content.viewport.remove();
    view.unmount();
  }
});
it.each([true, false])(
  "工作流实际Run归属匹配=%s决定是否抑制成果提示",
  async (matching) => {
    const result = resultNotice();
    result.target = {
      kind: "result",
      ownerKind: "workflow_run",
      ownerId: "run-result",
      resultVersion: 1,
      reference: { workflowRunId: "run-result" },
    };
    const content = presentedMessage("unrelated-message");
    content.viewport.dataset.workResultOwnerKind = "workflow_run";
    content.viewport.dataset.workResultOwnerId = matching
      ? "run-result"
      : "other-run";
    content.body.removeAttribute("data-work-result-body");
    content.body.dataset.workResultOutput = "";
    const view = mount();
    try {
      await screen.findByRole("button", { name: /通知 120 项/ });
      await emit("notifications.snapshot", { snapshot: startup });
      await emit("notifications.updated", {
        snapshot: snapshot(2),
        upserts: [result],
        removals: [],
      });
      expect(info).toHaveBeenCalledTimes(matching ? 0 : 1);
    } finally {
      content.viewport.remove();
      view.unmount();
    }
  },
);
it.each(["同一执行", "其他执行", "后台场景", "窗口失焦"])(
  "工作流画布提示抑制：%s",
  async (state) => {
    const result = resultNotice();
    result.target = {
      kind: "result",
      ownerKind: "workflow_run",
      ownerId: "run-result",
      resultVersion: 1,
      reference: { workflowRunId: "run-result" },
    };
    const content = presentedMessage("unrelated-message");
    content.viewport.removeAttribute("data-work-result-owner-kind");
    content.viewport.dataset.notificationWorkflowRun =
      state === "其他执行" ? "other-run" : "run-result";
    if (state === "后台场景") content.viewport.setAttribute("inert", "");
    if (state === "窗口失焦")
      vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const view = mount();
    try {
      await screen.findByRole("button", { name: /通知 120 项/ });
      await emit("notifications.snapshot", { snapshot: startup });
      await emit("notifications.updated", {
        snapshot: snapshot(2),
        upserts: [result],
        removals: [],
      });
      expect(info).toHaveBeenCalledTimes(state === "同一执行" ? 0 : 1);
      expect(dismissedIds).toEqual([]);
      expect(
        screen.getByRole("button", { name: /通知 120 项/ }),
      ).toBeInTheDocument();
    } finally {
      content.viewport.remove();
      view.unmount();
    }
  },
);

it("提示先到而后进入同一次执行画布，只关闭重复提示不提交已读或收起回执", async () => {
  const result = resultNotice();
  result.target = {
    kind: "result",
    ownerKind: "workflow_run",
    ownerId: "run-result",
    reference: { workflowRunId: "run-result" },
  };
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [result],
    removals: [],
  });
  expect(info).toHaveBeenCalledTimes(1);
  const content = presentedMessage("unrelated-message");
  content.viewport.dataset.notificationWorkflowRun = "run-result";
  try {
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith(result.id));
    expect(dismissedIds).toEqual([]);
  } finally {
    content.viewport.remove();
    view.unmount();
  }
});

it("通知先到而正文在本次绘制前提交，使用新正文可见性抑制提示", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const delivery = emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [resultNotice()],
    removals: [],
  });
  const content = presentedMessage("message-result");
  try {
    await delivery;
    expect(info).not.toHaveBeenCalled();
    expect(dismissedIds).toEqual([]);
  } finally {
    content.viewport.remove();
    view.unmount();
  }
});
it("正文晚到后的真实呈现事件关闭精确早到提示，不修改关注计数", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const result = resultNotice();
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [result],
    removals: [],
  });
  expect(info).toHaveBeenCalledTimes(1);
  const other = presentedMessage("other-message");
  await act(async () => {
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => resolve()),
    );
  });
  expect(dismiss).not.toHaveBeenCalledWith(result.id);
  const content = presentedMessage("message-result");
  await waitFor(() => expect(dismiss).toHaveBeenCalledWith(result.id));
  other.viewport.remove();
  content.viewport.remove();
  expect(
    view.queryClient.getQueryData<NotificationSnapshot>(
      getNotificationSnapshotQueryKey(),
    )?.totalCount,
  ).toBe(120);
  expect(dismissedIds).toEqual([]);
  view.unmount();
});
it.each(["移除", "新快照", "卸载"])(
  "待绘制提示在%s后取消",
  async (operation) => {
    const view = mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((frame) => {
      frames.push(frame);
      return frames.length;
    });
    const result = resultNotice();
    await emit(
      "notifications.updated",
      {
        snapshot: snapshot(2),
        upserts: [result],
        removals: [],
      },
      false,
    );
    expect(frames).toHaveLength(1);
    if (operation === "卸载") view.unmount();
    else if (operation === "移除")
      await emit(
        "notifications.updated",
        { snapshot: snapshot(3), upserts: [], removals: [result.id] },
        false,
      );
    else
      await emit(
        "notifications.snapshot",
        { snapshot: snapshot(3, { epoch: "b".repeat(32) }) },
        false,
      );
    act(() => {
      for (const frame of frames) frame(0);
    });
    expect(info).not.toHaveBeenCalled();
    expect(dismissedIds).toEqual([]);
    view.unmount();
  },
);
it("隐藏窗口新通知立即交付，不等待可能暂停的绘制帧", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  const frame = vi.spyOn(window, "requestAnimationFrame");
  await emit(
    "notifications.updated",
    { snapshot: snapshot(2), upserts: [resultNotice()], removals: [] },
    false,
  );
  expect(info).toHaveBeenCalledTimes(1);
  expect(frame).not.toHaveBeenCalled();
  view.unmount();
});
async function broadUpdate(revision: number) {
  await emit("notifications.updated", {
    snapshot: snapshot(revision, {
      actionCount: 1,
      totalCount: 1,
      actionPreview: [],
    }),
    upserts: [],
    removals: [],
    affectedResources: [{ kind: "global" }],
  });
}
it.each(["清空", "删除"])(
  "%s后的全局范围变化精确关闭失效Toast，预览外仍有效提示保持",
  async () => {
    const result = resultNotice();
    const action = { ...notice, id: "approval:still-waiting" };
    const reads: string[] = [];
    server.use(
      http.get(`${origin}/api/v1/notifications/:noticeId`, ({ params }) => {
        const id = String(params.noticeId);
        if (id === "snapshot") return;
        reads.push(id);
        return id === result.id
          ? HttpResponse.json({ key: "BadRequest" }, { status: 400 })
          : HttpResponse.json(action);
      }),
    );
    mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [result, action],
      removals: [],
    });
    expect(info).toHaveBeenCalledTimes(1);
    expect(warning).toHaveBeenCalledTimes(1);
    await broadUpdate(3);
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith(result.id));
    expect(reads.sort()).toEqual([action.id, result.id].sort());
    expect(dismiss).not.toHaveBeenCalledWith(action.id);
    expect(dismissedIds).toEqual([]);
  },
);
it.each(["DatabaseError", "NETWORK"])(
  "提示权威定位遇到%s仍保留已有提示",
  async (code) => {
    const result = resultNotice();
    let reads = 0;
    server.use(
      http.get(`${origin}/api/v1/notifications/:noticeId`, ({ params }) => {
        if (params.noticeId === "snapshot") return;
        reads++;
        return code === "NETWORK"
          ? HttpResponse.error()
          : HttpResponse.json({ key: code }, { status: 500 });
      }),
    );
    mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [result],
      removals: [],
    });
    await broadUpdate(3);
    await waitFor(() => expect(reads).toBe(1));
    expect(dismiss).not.toHaveBeenCalledWith(result.id);
    expect(dismissedIds).toEqual([]);
  },
);
it("卸载取消提示定位，迟到读取与Toast回调不再写回执", async () => {
  const result = resultNotice();
  let release = () => {};
  let started = false;
  let finished = false;
  server.use(
    http.get(`${origin}/api/v1/notifications/:noticeId`, async ({ params }) => {
      if (params.noticeId === "snapshot") return;
      started = true;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      finished = true;
      return HttpResponse.json({ key: "BadRequest" }, { status: 400 });
    }),
  );
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  await emit("notifications.updated", {
    snapshot: snapshot(2),
    upserts: [result],
    removals: [],
  });
  const options = info.mock.calls[0]?.[1];
  await broadUpdate(3);
  try {
    await waitFor(() => expect(started).toBe(true));
    view.unmount();
    expect(dismiss).toHaveBeenCalledTimes(1);
    act(() => options.onAutoClose());
    await act(async () => release());
    await waitFor(() => expect(finished).toBe(true));
    expect(dismiss).toHaveBeenCalledTimes(1);
    expect(dismissedIds).toEqual([]);
  } finally {
    release();
  }
});
it.each(["notifications.snapshot", "notifications.updated", "resync"])(
  "%s 取消尚无数据的旧通知分页读取，后返回旧页不覆盖新事实",
  async (type) => {
    let release = () => {};
    let requests = 0;
    const response = (title: string, revision: number) => ({
      resources: [{ ...notice, title }],
      totalSize: 1,
      pageNum: 1,
      pageSize: 50,
      hasMore: false,
      epoch: startup.epoch,
      revision,
    });
    server.use(
      http.get(`${origin}/api/v1/notifications`, async () => {
        requests++;
        if (requests !== 1) return HttpResponse.json(response("新通知清单", 2));
        await new Promise<void>((resolve) => {
          release = resolve;
        });
        return HttpResponse.json(response("旧通知清单", 1));
      }),
    );
    function List() {
      const query = useInfiniteQuery(notificationListOptions("action"));
      return (
        <p>{query.data?.pages[0]?.resources[0]?.title || "正在读取清单"}</p>
      );
    }
    const view = mount(<List />);
    try {
      await waitFor(() => expect(requests).toBe(1));
      await screen.findByRole("button", { name: /通知 120 项/ });
      if (type === "resync") {
        act(() => {
          for (const observer of observers) observer.onResync?.("delivery_gap");
        });
      } else {
        await emit(type, {
          snapshot: snapshot(2),
          upserts: [],
          removals: [],
        });
      }
      await screen.findByText("新通知清单");
      await act(async () => release());
      expect(screen.queryByText("旧通知清单")).not.toBeInTheDocument();
      expect(requests).toBe(2);
    } finally {
      release();
      view.unmount();
    }
  },
);
it("后到资源变更不取消其他资源尚未完成的提示定位", async () => {
  const result = resultNotice();
  const action = { ...notice, id: "approval:other-resource" };
  let release = () => {};
  const reads: string[] = [];
  server.use(
    http.get(`${origin}/api/v1/notifications/:noticeId`, async ({ params }) => {
      const id = String(params.noticeId);
      if (id === "snapshot") return;
      reads.push(id);
      if (id !== result.id) return HttpResponse.json(action);
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      return HttpResponse.json({ key: "BadRequest" }, { status: 400 });
    }),
  );
  const view = mount();
  try {
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [result, action],
      removals: [],
    });
    await emit("notifications.updated", {
      snapshot: snapshot(3),
      upserts: [],
      removals: [],
      affectedResources: [{ kind: "chat", id: "chat-result" }],
    });
    await waitFor(() => expect(reads).toEqual([result.id]));
    await emit("notifications.updated", {
      snapshot: snapshot(4),
      upserts: [],
      removals: [],
      affectedResources: [{ kind: "chat", id: "chat-1" }],
    });
    await waitFor(() => expect(reads).toEqual([result.id, action.id]));
    await act(async () => release());
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith(result.id));
    expect(dismiss).not.toHaveBeenCalledWith(action.id);
  } finally {
    release();
    view.unmount();
  }
});
it("同一提示的新定位成功后，迟到旧空结果不能关闭当前提示", async () => {
  const result = resultNotice();
  let release = () => {};
  let requests = 0;
  let oldFinished = false;
  server.use(
    http.get(`${origin}/api/v1/notifications/:noticeId`, async ({ params }) => {
      if (params.noticeId === "snapshot") return;
      requests++;
      if (requests !== 1) return HttpResponse.json(result);
      await new Promise<void>((resolve) => {
        release = resolve;
      });
      oldFinished = true;
      return HttpResponse.json({ key: "BadRequest" }, { status: 400 });
    }),
  );
  const view = mount();
  try {
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [result],
      removals: [],
    });
    await broadUpdate(3);
    await waitFor(() => expect(requests).toBe(1));
    await broadUpdate(4);
    await waitFor(() => expect(requests).toBe(2));
    await act(async () => release());
    await waitFor(() => expect(oldFinished).toBe(true));
    expect(dismiss).not.toHaveBeenCalledWith(result.id);
    expect(dismissedIds).toEqual([]);
  } finally {
    release();
    view.unmount();
  }
});

it.each(["approval", "elicitation", "human", "operation", "review"] as const)(
  "%s 的真实操作内容晚于提示出现时自动撤提示，仍保留待办且不调用处理或阅读接口",
  async (source) => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
      new DOMRect(0, 0, 100, 100),
    );
    const resolve = vi.fn();
    const id = source === "review" ? "review:task-2:1" : `${source}:request-2`;
    function PendingRequest() {
      const [show, setShow] = useState(false);
      return (
        <>
          <button onClick={() => setShow(true)}>展开原请求</button>
          {show ? (
            source === "approval" ? (
              <ChatApprovalCard
                approval={{
                  approvalId: "request-2",
                  title: "原审批",
                  options: [{ id: "allow", kind: "allow_once", name: "允许" }],
                }}
                current={1}
                total={1}
                submissionState="idle"
                onResolve={resolve}
              />
            ) : source === "elicitation" ? (
              <ChatElicitationCard
                elicitation={{
                  elicitationId: "request-2",
                  message: "原提问",
                  requestedSchema: {
                    type: "object",
                    properties: { answer: { type: "string", title: "回答" } },
                  },
                }}
                submissionState="idle"
                onResolve={resolve}
              />
            ) : source === "human" ? (
              <HumanRequestCard
                id="flow-1"
                runId="run-1"
                human={{
                  id: "request-2",
                  status: "waiting",
                  prompt: "原工作流输入",
                }}
              />
            ) : source === "operation" ? (
              <OperationRequestCard
                id="flow-1"
                runId="run-1"
                operation={{
                  id: "request-2",
                  status: "waiting",
                  description: "原工作流授权",
                }}
              />
            ) : (
              <TeamTaskBoard
                channelId="team-1"
                threadId="thread-1"
                members={[]}
                tasks={[
                  {
                    id: "task-2",
                    currentFence: 1,
                    status: "in_review",
                    subject: "原验收任务",
                  },
                ]}
                onChanged={resolve}
              />
            )
          ) : null}
        </>
      );
    }
    const view = mount(<PendingRequest />);
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [
        {
          ...notice,
          id,
          kind:
            source === "review"
              ? "review"
              : ["elicitation", "human"].includes(source)
                ? "input"
                : "approval",
        },
      ],
      removals: [],
    });
    expect(warning).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "展开原请求" }));
    await waitFor(() => expect(dismiss).toHaveBeenCalledWith(id));
    expect(resolve).not.toHaveBeenCalled();
    expect(view.queryClient.getMutationCache().getAll()).toHaveLength(0);
    expect(dismissedIds).toEqual([]);
    expect(
      screen.getByRole("button", { name: /通知 120 项/ }),
    ).toBeInTheDocument();
    view.unmount();
  },
);

it.each(["inert", "blur", "scroll"])(
  "已经显示的请求从%s恢复可见后关闭提示，不完成请求",
  async (mode) => {
    const view = mount();
    await screen.findByRole("button", { name: /通知 120 项/ });
    await emit("notifications.snapshot", { snapshot: startup });
    const content = presentedMessage("unrelated");
    content.body.dataset.notificationId = "approval:request-2";
    let outside = mode === "scroll";
    vi.spyOn(content.body, "getBoundingClientRect").mockImplementation(() =>
      rect(outside ? 240 : 40, 80),
    );
    if (mode === "inert") content.viewport.setAttribute("inert", "");
    const focus = vi
      .spyOn(document, "hasFocus")
      .mockReturnValue(mode !== "blur");
    try {
      await emit("notifications.updated", {
        snapshot: snapshot(2),
        upserts: [{ ...notice, id: "approval:request-2" }],
        removals: [],
      });
      expect(warning).toHaveBeenCalledTimes(1);
      outside = false;
      content.viewport.removeAttribute("inert");
      focus.mockReturnValue(true);
      act(() =>
        mode === "blur"
          ? window.dispatchEvent(new Event("focus"))
          : content.viewport.dispatchEvent(new Event("scroll")),
      );
      await waitFor(() =>
        expect(dismiss).toHaveBeenCalledWith("approval:request-2"),
      );
      expect(dismissedIds).toEqual([]);
      expect(view.queryClient.getMutationCache().getAll()).toHaveLength(0);
    } finally {
      content.viewport.remove();
      view.unmount();
    }
  },
);

it("同一消息的旧呈现版本不能抑制新交付", async () => {
  const view = mount();
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  const content = presentedMessage("message-result");
  const newer = {
    ...resultNotice(),
    id: "result:chat:chat-result:2",
    version: 2,
  };
  try {
    await emit("notifications.updated", {
      snapshot: snapshot(2),
      upserts: [newer],
      removals: [],
    });
    expect(info).toHaveBeenCalledTimes(1);
    expect(dismiss).not.toHaveBeenCalledWith(newer.id);
  } finally {
    content.viewport.remove();
    view.unmount();
  }
});

it("失败正文先出现而通知资格稍后到达时，唯一通知消费者恢复阅读查询，计数仍等待权威移除", async () => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(
    new DOMRect(0, 0, 100, 100),
  );
  const id = "automatic-run-failed:run-1";
  const failure: NotificationItem = {
    ...notice,
    id,
    kind: "automatic_failure",
    bucket: "unread",
    source: "automatic",
    target: {
      kind: "automatic_failure",
      taskId: "task-1",
      automaticRunId: "run-1",
    },
  };
  let eligible = false;
  let reads = 0;
  const viewed = vi.fn();
  const intersections: {
    callback: IntersectionObserverCallback;
    target?: Element;
  }[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      value: (typeof intersections)[number];
      constructor(callback: IntersectionObserverCallback) {
        this.value = { callback };
        intersections.push(this.value);
      }
      observe(target: Element) {
        this.value.target = target;
      }
      unobserve() {}
      disconnect() {}
    },
  );
  server.use(
    http.get(`${origin}/api/v1/notifications/${id}`, () => {
      reads++;
      return eligible
        ? HttpResponse.json(failure)
        : HttpResponse.json({ key: "NotificationNotFound" }, { status: 404 });
    }),
    http.post(
      `${origin}/api/v1/notifications/${id}/view`,
      async ({ request }) => {
        viewed(await request.json());
        return HttpResponse.json({ success: true });
      },
    ),
  );
  const view = mount(
    <FailureNoticeContent runId="run-1">原失败记录</FailureNoticeContent>,
  );
  await screen.findByRole("button", { name: /通知 120 项/ });
  await emit("notifications.snapshot", { snapshot: startup });
  await waitFor(() => expect(reads).toBeGreaterThan(0));
  await waitFor(() => expect(view.queryClient.isFetching()).toBe(0));
  expect(viewed).not.toHaveBeenCalled();
  eligible = true;
  await emit("notifications.updated", {
    snapshot: snapshot(2, { unreadFailureCount: 1, totalCount: 121 }),
    upserts: [failure],
    removals: [],
  });
  await waitFor(() =>
    expect(
      view.queryClient.getQueryData(notificationItemQueryKey(id)),
    ).toMatchObject({ id }),
  );
  expect(error).not.toHaveBeenCalled();
  act(() => {
    for (const observer of intersections)
      if (observer.target)
        observer.callback(
          [
            {
              target: observer.target,
              isIntersecting: true,
              intersectionRect: new DOMRect(0, 0, 100, 100),
            } as IntersectionObserverEntry,
          ],
          {} as IntersectionObserver,
        );
  });
  await waitFor(() => expect(viewed).toHaveBeenCalledTimes(1));
  expect(viewed).toHaveBeenCalledWith({ version: 1, target: failure.target });
  expect(
    screen.getByRole("button", { name: /通知 121 项/ }),
  ).toBeInTheDocument();
  eligible = false;
  await emit("notifications.updated", {
    snapshot: snapshot(3),
    upserts: [],
    removals: [id],
  });
  expect(
    screen.getByRole("button", { name: /通知 120 项/ }),
  ).toBeInTheDocument();
  expect(dismissedIds).toEqual([]);
  expect(observers.size).toBe(1);
  view.unmount();
});
