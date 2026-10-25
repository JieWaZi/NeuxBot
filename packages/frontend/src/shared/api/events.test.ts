import { afterEach, describe, expect, it, vi } from "vitest";
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
vi.mock("./runtime", () => ({
  getApiConnection: () => ({
    baseUrl: "http://127.0.0.1:8432",
    token: "test-token",
  }),
}));
vi.mock("./generated/sdk.gen", () => ({ listWorkflowEvents: mocks.history }));
mocks.subscribe.mockImplementation((_topic, _resource, observer) => {
  mocks.observers.push(observer);
  return vi.fn();
});
afterEach(() => {
  vi.restoreAllMocks();
  mocks.observers.length = 0;
  mocks.subscribe.mockClear();
  mocks.history.mockReset();
});
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
  it("Chat 缺口不越过原游标，transient 不推进游标，持久重复不重复交付", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          resources: [
            { sequence: 2, type: "execution.started" },
            { sequence: 3, type: "execution.completed" },
          ],
        }),
      ),
    );
    const onEvent = vi.fn();
    const onGap = vi.fn();
    const close = subscribeChat("chat-1", {
      after: 1,
      isTerminal: () => false,
      onEvent,
      onGap,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onEvent(
      frame("chat", { transient: true, sequence: 50, type: "runtime.event" }),
    );
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    expect(onGap).toHaveBeenCalledWith(2, 3);
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(3));
    expect(onEvent.mock.calls.map(([event]) => event.sequence)).toEqual([
      50, 2, 3,
    ]);
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    expect(onEvent).toHaveBeenCalledTimes(3);
    close();
  });
  it("断线恢复先按历史对账，再交付对账期间收到的事件，关闭取消历史请求", async () => {
    let resolve!: (value: Response) => void;
    const fetch = vi.spyOn(globalThis, "fetch").mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const onEvent = vi.fn();
    const close = subscribeChat("chat-1", {
      after: 1,
      isTerminal: () => false,
      onEvent,
    });
    const observer = mocks.observers[0];
    if (!observer) throw new Error("缺少测试订阅");
    observer.onState?.("open");
    observer.onState?.("reconnecting");
    observer.onResync?.("reconnected");
    observer.onEvent(
      frame("chat", { sequence: 3, type: "execution.completed" }),
    );
    expect(onEvent).not.toHaveBeenCalled();
    resolve(
      new Response(
        JSON.stringify({
          resources: [{ sequence: 2, type: "execution.started" }],
        }),
      ),
    );
    await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(2));
    expect(onEvent.mock.calls.map(([event]) => event.sequence)).toEqual([2, 3]);
    close();
    expect(fetch.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
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
