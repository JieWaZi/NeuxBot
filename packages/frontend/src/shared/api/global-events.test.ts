import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  GlobalEventDelivery,
  GlobalEventFrame,
} from "@neuxbot/platform-contracts/core-event-protocol";
import {
  subscribeGlobalEvents,
  registerCoreConnectionRecovery,
  updateGlobalEventCursor,
} from "./global-events";
const mocks = vi.hoisted(() => ({
  listeners: new Set<(value: GlobalEventDelivery) => void>(),
  initialize: vi.fn(),
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
  resync: vi.fn(),
}));
vi.mock("./runtime", () => ({ initializeApi: mocks.initialize }));
vi.mock("@/shared/platform", () => ({
  isDesktopRuntime: () => true,
  getCoreConnection: vi.fn(),
}));
vi.mock("@/shared/platform/provider", () => {
  const events = {
    subscribe_core_events: mocks.subscribe,
    unsubscribe_core_events: mocks.unsubscribe,
    resync_core_events: mocks.resync,
    onCoreEvent: (listener: (value: GlobalEventDelivery) => void) => {
      mocks.listeners.add(listener);
      return () => mocks.listeners.delete(listener);
    },
  };
  return {
    configurePlatform: vi.fn(),
    platform: () => ({ events }),
    requirePlatformService: () => events,
  };
});
beforeEach(() => {
  mocks.initialize.mockResolvedValue({});
  mocks.subscribe.mockResolvedValue(true);
  mocks.unsubscribe.mockResolvedValue(true);
  mocks.resync.mockResolvedValue(true);
});
afterEach(() => {
  mocks.listeners.clear();
  vi.clearAllMocks();
});
type WithoutSubscription<T> = T extends { subscriptionId: string }
  ? Omit<T, "subscriptionId">
  : never;
function deliver(value: WithoutSubscription<GlobalEventDelivery>) {
  const registration = mocks.subscribe.mock.calls[0]?.[0];
  if (!registration) throw new Error("缺少测试注册");
  for (const listener of mocks.listeners)
    listener({ ...value, subscriptionId: registration.subscriptionId });
}
function snapshot(): GlobalEventFrame {
  return {
    protocolVersion: 1,
    epoch: "epoch",
    connectionId: "connection",
    deliverySequence: 1,
    topic: "notifications",
    type: "notifications.snapshot",
    resource: { kind: "global" },
    eventId: "event",
    payload: { snapshot: {} },
  };
}
describe("Renderer 全局事件边界", () => {
  it("相同资源只注册一次IPC，晚加入可权威补齐，最后释放才退订", async () => {
    const first = vi.fn();
    const second = vi.fn();
    const restore = vi.fn();
    const closeFirst = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent: first },
    );
    deliver({ kind: "state", state: "open" });
    const closeSecond = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent: second, onResync: restore },
    );
    expect(mocks.subscribe).toHaveBeenCalledTimes(1);
    await vi.waitFor(() =>
      expect(restore).toHaveBeenCalledWith("subscription_joined"),
    );
    deliver({ kind: "event", frame: snapshot() });
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
    closeFirst();
    expect(mocks.unsubscribe).not.toHaveBeenCalled();
    closeSecond();
    await vi.waitFor(() => expect(mocks.unsubscribe).toHaveBeenCalledTimes(1));
    expect(mocks.listeners.size).toBe(0);
  });
  it("先刷新Core地址令牌并通知恢复，然后释放早到的快照", async () => {
    let finish!: (value: object) => void;
    mocks.initialize.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const order: string[] = [];
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      {
        onEvent: () => order.push("snapshot"),
        onResync: () => {
          order.push("resync");
        },
      },
    );
    deliver({ kind: "state", state: "reconnecting" });
    deliver({ kind: "resync", reason: "core_restarted" });
    deliver({ kind: "event", frame: snapshot() });
    expect(order).toEqual([]);
    finish({});
    await vi.waitFor(() => expect(order).toEqual(["resync", "snapshot"]));
    close();
  });
  it("恢复缓冲超限明确重新请求权威快照，不静默丢失持久事件", async () => {
    let finish!: (value: object) => void;
    mocks.initialize.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const onEvent = vi.fn();
    const onResync = vi.fn();
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent, onResync },
    );
    deliver({ kind: "resync", reason: "reconnected" });
    for (let index = 0; index < 300; index++)
      deliver({ kind: "event", frame: snapshot() });
    finish({});
    await vi.waitFor(() => expect(mocks.resync).toHaveBeenCalledTimes(1));
    expect(onResync).toHaveBeenCalledWith("buffer_overflow");
    expect(onEvent).not.toHaveBeenCalled();
    close();
  });
  it("首次握手失败后成功不等待不存在的旧连接恢复", () => {
    const onEvent = vi.fn();
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent },
    );
    deliver({ kind: "state", state: "reconnecting" });
    deliver({ kind: "state", state: "unavailable" });
    deliver({ kind: "state", state: "open" });
    deliver({ kind: "event", frame: snapshot() });
    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(mocks.initialize).not.toHaveBeenCalled();
    close();
  });
  it("溢出重订阅等待领域权威对账并使用最新原游标", async () => {
    let finish!: () => void;
    const close = subscribeGlobalEvents(
      "chat",
      { kind: "chat", id: "chat-1" },
      {
        onEvent: vi.fn(),
        onResync: () =>
          new Promise<void>((resolve) => {
            finish = () => {
              updateGlobalEventCursor(
                "chat",
                { kind: "chat", id: "chat-1" },
                42,
              );
              resolve();
            };
          }),
      },
    );
    deliver({ kind: "resync", reason: "resync_required" });
    await vi.waitFor(() => expect(finish).toBeTypeOf("function"));
    expect(mocks.resync).not.toHaveBeenCalled();
    finish();
    await vi.waitFor(() =>
      expect(mocks.resync).toHaveBeenCalledWith(
        expect.objectContaining({ after: 42 }),
      ),
    );
    close();
  });
  it.each(["delivery_gap", "invalid_frame"])(
    "%s 在原连接等待共享消费者对账后重建资源订阅",
    async (reason) => {
      const finishes: (() => void)[] = [];
      const resource = { kind: "chat", id: "chat-gap" };
      const recover = () =>
        new Promise<void>((resolve) => {
          finishes.push(resolve);
        });
      const first = subscribeGlobalEvents("chat", resource, {
        onEvent: vi.fn(),
        onResync: recover,
      });
      const second = subscribeGlobalEvents("chat", resource, {
        onEvent: vi.fn(),
        onResync: recover,
      });
      try {
        deliver({ kind: "resync", reason });
        await vi.waitFor(() => expect(finishes).toHaveLength(2));
        expect(mocks.resync).not.toHaveBeenCalled();
        finishes[0]?.();
        await Promise.resolve();
        expect(mocks.resync).not.toHaveBeenCalled();
        updateGlobalEventCursor("chat", resource, 42);
        finishes[1]?.();
        await vi.waitFor(() =>
          expect(mocks.resync).toHaveBeenCalledWith(
            expect.objectContaining({ after: 42 }),
          ),
        );
        expect(mocks.subscribe).toHaveBeenCalledTimes(1);
        expect(mocks.resync).toHaveBeenCalledTimes(1);
      } finally {
        first();
        second();
      }
    },
  );
  it.each(["core_restarted", "reconnected", "resource_recovered"])(
    "%s 沿已有服务器恢复快照继续，不重复重订阅",
    async (reason) => {
      const onEvent = vi.fn();
      const onResync = vi.fn();
      const close = subscribeGlobalEvents(
        "notifications",
        { kind: "global" },
        { onEvent, onResync },
      );
      try {
        deliver({ kind: "resync", reason });
        deliver({ kind: "event", frame: snapshot() });
        await vi.waitFor(() => expect(onEvent).toHaveBeenCalledTimes(1));
        expect(onResync).toHaveBeenCalledWith(reason);
        expect(mocks.resync).not.toHaveBeenCalled();
      } finally {
        close();
      }
    },
  );
  it("新连接恢复完成后只通知原生观察一次，释放监听后不再调用", async () => {
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent: vi.fn() },
    );
    deliver({ kind: "state", state: "open" });
    deliver({
      kind: "event",
      frame: { ...snapshot(), connectionId: "native-before" },
    });
    const recovery = vi.fn();
    const stop = registerCoreConnectionRecovery(recovery);
    deliver({ kind: "resync", reason: "core_restarted" });
    deliver({
      kind: "event",
      frame: {
        ...snapshot(),
        epoch: "new-epoch",
        connectionId: "native-after",
      },
    });
    expect(recovery).not.toHaveBeenCalled();
    await vi.waitFor(() =>
      expect(recovery).toHaveBeenCalledWith({
        epoch: "new-epoch",
        connectionId: "native-after",
      }),
    );
    deliver({
      kind: "event",
      frame: {
        ...snapshot(),
        epoch: "new-epoch",
        connectionId: "native-after",
        deliverySequence: 2,
      },
    });
    expect(recovery).toHaveBeenCalledTimes(1);
    stop();
    deliver({
      kind: "event",
      frame: { ...snapshot(), connectionId: "native-later" },
    });
    expect(recovery).toHaveBeenCalledTimes(1);
    close();
  });
  it("卸载先于注册完成时仍释放后到的注册", async () => {
    let finish!: (value: boolean) => void;
    mocks.subscribe.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const close = subscribeGlobalEvents(
      "notifications",
      { kind: "global" },
      { onEvent: vi.fn() },
    );
    close();
    expect(mocks.listeners.size).toBe(0);
    expect(mocks.unsubscribe).not.toHaveBeenCalled();
    finish(true);
    await vi.waitFor(() => expect(mocks.unsubscribe).toHaveBeenCalledTimes(1));
  });
});
