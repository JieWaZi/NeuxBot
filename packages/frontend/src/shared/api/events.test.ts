import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "./generated/client.gen";
import {
  chatMessagesKey,
  chatMessageSource,
} from "@/modules/chat/api/chat-queries";
import { useChatRuntime } from "@/modules/chat/runtime/use-chat-runtime";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { conversationKeyOf } from "@/modules/chat/model/conversation-identity";
import type { GlobalEventObserver, GlobalEventFrame } from "./global-events";
import { subscribeChat, subscribeChannel, subscribeWorkflow } from "./events";
const mocks = vi.hoisted(() => ({
  observers: [] as GlobalEventObserver[],
  subscribe: vi.fn(),
  history: vi.fn(),
}));
vi.mock("./global-events", () => ({
  subscribeGlobalEvents: mocks.subscribe,
  updateGlobalEventCursor: vi.fn(),
}));
vi.mock("./generated/sdk.gen", async (original) => ({
  ...(await original<object>()),
  listWorkflowEvents: mocks.history,
}));
mocks.subscribe.mockImplementation((_topic, _resource, observer) => {
  mocks.observers.push(observer);
  return vi.fn();
});
afterEach(() => {
  vi.restoreAllMocks();
  mocks.observers.length = 0;
  mocks.subscribe.mockClear();
  mocks.history.mockReset();
  vi.useRealTimers();
});

it.each(["超时", "卸载"])(
  "真实 Chat 恢复%s会取消挂起 HTTP，收口等待并允许下一次恢复",
  async (cause) => {
    const server = setupServer();
    const signals: AbortSignal[] = [];
    let release = () => {};
    server.use(
      http.get(
        "http://127.0.0.1:19879/api/v1/chats/recovery/messages",
        async ({ request }) => {
          signals.push(request.signal);
          if (signals.length === 1)
            await new Promise<void>((resolve) => {
              release = resolve;
              request.signal.addEventListener("abort", () => resolve(), {
                once: true,
              });
            });
          return HttpResponse.json({
            resources: [],
            latestSequence: 2,
            hasMore: false,
          });
        },
      ),
    );
    client.setConfig({ baseUrl: "http://127.0.0.1:19879" });
    server.listen({ onUnhandledRequest: "error" });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const key = chatMessagesKey("recovery");
    queryClient.setQueryData(key, {
      pages: [{ resources: [], latestSequence: 1 }],
      pageParams: [undefined],
    });
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(
        QueryClientProvider,
        { client: queryClient },
        createElement(ChatQueueProvider, null, children),
      );
    const mount = () =>
      renderHook(
        () =>
          useChatRuntime({
            conversationKey: conversationKeyOf("scope:recovery", "recovery"),
            queueKey: "recovery",
            chatId: "recovery",
            messageSource: chatMessageSource(queryClient, "recovery", {
              messages: [],
              latestSequence: 1,
              ready: true,
            }),
            activeExecutionId: "execution",
            isLoading: false,
            isSendDisabled: false,
            attachmentOnlyText: "",
            incompleteErrorText: "",
            selection: {
              runtimeId: "codex",
              model: "",
              reasoning: "",
              permissionLevel: "auto",
            },
            prepareSubmission: () => ({ accept: async () => ({}) }),
            cancel: async () => {},
            onStarted: () => {},
            onTerminal: () => {},
          }),
        { wrapper },
      );
    let view = mount();
    try {
      const firstObserver = mocks.observers.at(-1);
      if (!firstObserver) throw new Error("缺少真实 Chat 订阅");
      let settled = false;
      vi.useFakeTimers();
      act(() => {
        firstObserver.onState?.("open");
        firstObserver.onState?.("reconnecting");
        void Promise.resolve(firstObserver.onResync?.("reconnected")).then(
          () => {
            settled = true;
          },
          () => {
            settled = true;
          },
        );
      });
      await vi.waitFor(() => expect(signals).toHaveLength(1));
      if (cause === "超时")
        await act(async () => {
          await vi.advanceTimersByTimeAsync(20_001);
        });
      else act(() => view.unmount());
      await vi.waitFor(() => expect(signals[0]?.aborted).toBe(true));
      await vi.waitFor(() => expect(settled).toBe(true));
      expect(queryClient.getQueryState(key)?.fetchStatus).toBe("idle");
      if (cause === "卸载") view = mount();
      const nextObserver = mocks.observers.at(-1);
      if (!nextObserver) throw new Error("缺少恢复订阅");
      let recovered: Promise<unknown> | undefined;
      act(() => {
        recovered = Promise.resolve(nextObserver.onResync?.("reconnected"));
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      await recovered;
      expect(signals).toHaveLength(2);
      act(() =>
        nextObserver.onEvent(
          frame("chat", {
            sequence: 3,
            type: "execution.completed",
            message: {
              id: "execution",
              executionId: "execution",
              role: "assistant",
              status: "completed",
              revision: 3,
            },
          }),
        ),
      );
      expect(queryClient.getQueryData(key)).toMatchObject({
        pages: [
          {
            latestSequence: 3,
            resources: [{ id: "execution", status: "completed" }],
          },
        ],
      });
    } finally {
      view.unmount();
      queryClient.clear();
      release();
      server.close();
    }
  },
);
function frame(
  topic: "chat" | "team" | "workflow",
  payload: unknown,
): GlobalEventFrame {
  return {
    protocolVersion: 1,
    epoch: "epoch",
    connectionId: "connection",
    deliverySequence: 1,
    topic,
    type: `${topic}.event`,
    resource: { kind: topic },
    eventId: "event",
    payload,
  };
}
describe("共享 WS 领域适配", () => {
  it("Chat 缺口由权威来源cursor恢复，transient不推进且持久重复不交付", async () => {
    const reconcileCursor = vi.fn().mockResolvedValue(2);
    const onEvent = vi.fn();
    const close = subscribeChat("chat-1", {
      after: 1,
      isTerminal: () => false,
      onEvent,
      reconcileCursor,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onEvent(
      frame("chat", { transient: true, sequence: 50, type: "runtime.event" }),
    );
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(2));
    expect(reconcileCursor).toHaveBeenCalledTimes(1);
    expect(onEvent.mock.calls.map(([event]) => event.sequence)).toEqual([
      50, 3,
    ]);
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    expect(onEvent).toHaveBeenCalledTimes(2);
    close();
  });

  it("断线先等待消息来源快照再交付缓冲事件，关闭释放恢复signal", async () => {
    let resolve!: (sequence: number) => void;
    let signal: AbortSignal | undefined;
    const reconcileCursor = vi.fn((recoverySignal: AbortSignal) => {
      signal = recoverySignal;
      return new Promise<number>((done) => {
        resolve = done;
      });
    });
    const onEvent = vi.fn();
    const close = subscribeChat("chat-1", {
      after: 1,
      isTerminal: () => false,
      onEvent,
      reconcileCursor,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onState?.("open");
    observer.onState?.("reconnecting");
    const recovering = observer.onResync?.("reconnected");
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    expect(onEvent).not.toHaveBeenCalled();
    resolve(2);
    await recovering;
    expect(onEvent.mock.calls.map(([event]) => event.sequence)).toEqual([3]);
    close();
    expect(signal?.aborted).toBe(true);
  });

  it("Chat 由消息来源权威快照恢复持久游标，超过千条缺口也不再另读事件历史", async () => {
    let release!: (cursor: number) => void;
    const reconcileCursor = vi.fn(
      () =>
        new Promise<number>((resolve) => {
          release = resolve;
        }),
    );
    const history = vi.spyOn(globalThis, "fetch");
    const onEvent = vi.fn();
    const close = subscribeChat("snapshot-chat", {
      after: 2,
      isTerminal: () => false,
      onEvent,
      reconcileCursor,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onState?.("open");
    observer.onState?.("reconnecting");
    const recovering = observer.onResync?.("reconnected");
    observer.onEvent(
      frame("chat", { sequence: 2003, type: "execution.completed" }),
    );
    expect(onEvent).not.toHaveBeenCalled();
    release(2002);
    await recovering;
    expect(reconcileCursor).toHaveBeenCalledTimes(1);
    expect(history).not.toHaveBeenCalled();
    expect(onEvent.mock.calls.map(([event]) => event.sequence)).toEqual([2003]);
    observer.onEvent(frame("chat", { sequence: 2002, type: "runtime.event" }));
    expect(onEvent).toHaveBeenCalledTimes(1);
    close();
  });

  it("Team 使用原不透明游标去重，恢复使用真实 thread 快照游标", async () => {
    const reconcileCursor = vi.fn().mockResolvedValue("later-cursor");
    const onEvent = vi.fn();
    const close = subscribeChannel("channel-1", "thread-1", {
      after: "old-cursor",
      reconcileCursor,
      onEvent,
    });
    expect(mocks.subscribe).toHaveBeenCalledWith(
      "team",
      { kind: "thread", id: "thread-1", parentId: "channel-1" },
      expect.anything(),
    );
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onEvent(frame("team", { id: "cursor-1", type: "task.created" }));
    observer.onEvent(frame("team", { id: "cursor-1", type: "task.created" }));
    expect(onEvent).toHaveBeenCalledTimes(1);
    observer.onResync?.("resync_required");
    await vi.waitFor(() => expect(reconcileCursor).toHaveBeenCalledTimes(1));
    observer.onEvent(
      frame("team", { id: "later-cursor", type: "task.created" }),
    );
    expect(onEvent).toHaveBeenCalledTimes(1);
    close();
  });
  it("Workflow 恢复沿 run 身份和原 sequence 查询，不使用 WS deliverySequence", async () => {
    mocks.history.mockResolvedValue({
      data: { resources: [{ sequence: 8, type: "run.completed" }] },
    });
    const onEvent = vi.fn();
    const close = subscribeWorkflow("workflow-1", {
      runId: "run-1",
      after: 7,
      onEvent,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onResync?.("reconnected");
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
    expect(mocks.history).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { id: "workflow-1" },
        query: { after: 7, runId: "run-1" },
      }),
    );
    close();
  });
});
