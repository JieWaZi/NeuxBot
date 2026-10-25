import { CoreEventClient } from "@neuxbot/platform-contracts/core-event-client";
import {
  globalEventResourceKey,
  type GlobalEventDelivery,
  type GlobalEventObserver,
  type GlobalEventResource,
  type GlobalEventState,
  type GlobalEventTopic,
} from "@neuxbot/platform-contracts/core-event-protocol";
import { platform, requirePlatformService } from "@/shared/platform/provider";
import { getCoreConnection } from "@/shared/platform";
import { initializeApi } from "./runtime";
import { logger } from "@/shared/diagnostics/logger";
export type {
  GlobalEventFrame,
  GlobalEventObserver,
  GlobalEventResource,
  GlobalEventState,
  GlobalEventTopic,
} from "@neuxbot/platform-contracts/core-event-protocol";

type Subscription = {
  observers: Set<GlobalEventObserver>;
  stop: () => void;
  state: GlobalEventState;
  resync: () => void;
  after?: number | string;
};
const subscriptions = new Map<string, Subscription>();
let browserClient: CoreEventClient | undefined;
let refreshConnection: Promise<unknown> | undefined;
const connectionRecoveries = new Set<
  (identity: { epoch: string; connectionId: string }) => void
>();
let activeConnectionId: string | undefined;

/** 原生授权与观察通过连接生命周期重建，不增加任何业务轮询或主题订阅。 */
export function registerCoreConnectionRecovery(
  listener: (identity: { epoch: string; connectionId: string }) => void,
): () => void {
  connectionRecoveries.add(listener);
  return () => {
    connectionRecoveries.delete(listener);
  };
}

/** Core 重启后的受限连接查询先更新生成 Client，随后消费者恢复原权威快照。 */
function restoreConnection(): Promise<unknown> {
  refreshConnection ??= initializeApi().finally(() => {
    refreshConnection = undefined;
  });
  return refreshConnection;
}

/** 主题/资源共享注册，最后一个调用方释放才退订；不保存服务器事实。 */
export function subscribeGlobalEvents(
  topic: GlobalEventTopic,
  resource: GlobalEventResource,
  observer: GlobalEventObserver,
): () => void {
  const key = globalEventResourceKey(topic, resource);
  let subscription = subscriptions.get(key);
  const joining = Boolean(subscription);
  if (!subscription) {
    const observers = new Set<GlobalEventObserver>();
    subscription = {
      observers,
      state: "connecting",
      stop: () => {},
      resync: () => {},
      after: observer.after,
    };
    const current = subscription;
    subscriptions.set(key, subscription);
    let active = true;
    let restoring = false;
    let hasOpened = false;
    let restoreVersion = 0;
    let buffer: Parameters<GlobalEventObserver["onEvent"]>[0][] = [];
    let bufferBytes = 0;
    let overflow = false;
    let failures = 0;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const deliver: GlobalEventObserver = {
      after: observer.after,
      onEvent: (frame) => {
        if (!active) return;
        if (restoring) {
          bufferBytes += JSON.stringify(frame).length;
          if (buffer.length >= 256 || bufferBytes > 1_048_576) {
            buffer = [];
            bufferBytes = 0;
            overflow = true;
          } else buffer.push(frame);
          return;
        }
        if (activeConnectionId !== frame.connectionId) {
          const recovering = activeConnectionId !== undefined;
          activeConnectionId = frame.connectionId;
          if (recovering)
            for (const listener of connectionRecoveries)
              listener({
                epoch: frame.epoch,
                connectionId: frame.connectionId,
              });
        }
        for (const item of observers) item.onEvent(frame);
      },
      onState: (state, changed) => {
        if (!active) return;
        current.state = state;
        if (
          hasOpened &&
          (state === "reconnecting" || state === "unavailable")
        ) {
          restoring = true;
          // 旧物理连接的积压由原历史恢复，不能在新 epoch 快照后重新交付。
          buffer = [];
          bufferBytes = 0;
        }
        if (state === "open") hasOpened = true;
        for (const item of observers) item.onState?.(state, changed);
      },
      onResync: (reason) => {
        if (!active) return;
        restoring = true;
        if (retry) clearTimeout(retry);
        retry = undefined;
        const version = ++restoreVersion;
        void restoreConnection()
          .then(async () => {
            if (!active || version !== restoreVersion) return;
            await Promise.all(
              [...observers].map((item) =>
                item.onResync?.(overflow ? "buffer_overflow" : reason),
              ),
            );
            if (!active || version !== restoreVersion) return;
            restoring = false;
            failures = 0;
            if (overflow) {
              overflow = false;
              buffer = [];
              bufferBytes = 0;
              current.resync();
              return;
            }
            const pending = buffer;
            buffer = [];
            bufferBytes = 0;
            for (const frame of pending) deliver.onEvent(frame);
            // 原连接仍可用的缺口/非法帧需要服务器补快照或 backlog；
            // 重连和资源重试已自动补齐，不能重复注册形成恢复循环。
            if (
              reason === "resync_required" ||
              reason === "delivery_gap" ||
              reason === "invalid_frame"
            )
              current.resync();
          })
          .catch(() => {
            if (!active || version !== restoreVersion) return;
            for (const item of observers) item.onState?.("unavailable");
            failures += 1;
            retry = setTimeout(
              () => {
                retry = undefined;
                deliver.onResync?.(reason);
              },
              Math.min(250 * 2 ** Math.min(failures - 1, 6), 10_000),
            );
          });
      },
    };
    if (platform().events) {
      const bridge = requirePlatformService("events");
      const subscriptionId = crypto.randomUUID();
      const stopEvents = bridge.onCoreEvent((delivery: GlobalEventDelivery) => {
        if (!active || delivery.subscriptionId !== subscriptionId) return;
        if (delivery.kind === "event") deliver.onEvent(delivery.frame);
        else if (delivery.kind === "state")
          deliver.onState?.(delivery.state, delivery.connectionChanged);
        else deliver.onResync?.(delivery.reason);
      });
      const registration = bridge.subscribe_core_events({
        subscriptionId,
        topic,
        resource,
        after: observer.after,
      });
      void registration.catch(() => {
        deliver.onState?.("unavailable");
      });
      current.resync = () => {
        void registration
          .then(() =>
            bridge.resync_core_events({ subscriptionId, after: current.after }),
          )
          .catch(() => {});
      };
      current.stop = () => {
        active = false;
        if (retry) clearTimeout(retry);
        buffer = [];
        stopEvents();
        // 避免注册 IPC 比卸载晚返回而留下悬空订阅。
        void registration
          .then(() => bridge.unsubscribe_core_events({ subscriptionId }))
          .catch(() => {});
      };
    } else {
      browserClient ??= new CoreEventClient({
        connection: getCoreConnection,
        log: (message) => logger.warn(message),
      });
      const client = browserClient;
      const stop = client.subscribe(topic, resource, deliver);
      current.resync = () =>
        client.requestResync(topic, resource, current.after);
      current.stop = () => {
        active = false;
        if (retry) clearTimeout(retry);
        stop();
      };
    }
  }
  subscription.observers.add(observer);
  observer.onState?.(subscription.state);
  if (joining && subscription.state === "open") {
    const current = subscription;
    void restoreConnection()
      .then(() => {
        if (!current.observers.has(observer)) return;
        observer.onResync?.("subscription_joined");
        if (topic === "notifications") current.resync();
      })
      .catch(() => observer.onState?.("unavailable"));
  }
  let active = true;
  return () => {
    if (!active) return;
    active = false;
    subscription.observers.delete(observer);
    if (!subscription.observers.size) {
      subscriptions.delete(key);
      subscription.stop();
    }
  };
}

/** 恢复历史后更新传输游标；不发业务请求，也不替代领域权威事实。 */
export function updateGlobalEventCursor(
  topic: GlobalEventTopic,
  resource: GlobalEventResource,
  after: number | string,
): void {
  const subscription = subscriptions.get(
    globalEventResourceKey(topic, resource),
  );
  if (subscription) subscription.after = after;
}

/** 会话结束时释放全部订阅，下一次登录建立新的连接身份。 */
export async function resetGlobalEvents(): Promise<void> {
  for (const item of subscriptions.values()) item.stop();
  subscriptions.clear();
  const previous = browserClient;
  browserClient = undefined;
  activeConnectionId = undefined;
  refreshConnection = undefined;
  connectionRecoveries.clear();
  await previous?.close();
}
