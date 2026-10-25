import type { CoreConnection } from "./resources";
import {
  globalEventFrameSchema,
  globalEventResourceKey,
  type GlobalEventObserver,
  type GlobalEventResource,
  type GlobalEventState,
  type GlobalEventTopic,
} from "./core-event-protocol";

type Subscription = {
  topic: GlobalEventTopic;
  resource: GlobalEventResource;
  cursor?: number | string;
  observers: Set<GlobalEventObserver>;
  seenEvents: Set<string>;
  eventOrder: string[];
  state: GlobalEventState;
  failures: number;
  requestId?: string;
  retry?: ReturnType<typeof setTimeout>;
};

/** 单个运行环境只有一个物理连接；领域游标与连接交付序号各自保留。 */
export class CoreEventClient {
  private readonly subscriptions = new Map<string, Subscription>();
  private socket?: WebSocket;
  private closing?: Promise<void>;
  private retry?: ReturnType<typeof setTimeout>;
  private deadline?: ReturnType<typeof setTimeout>;
  private connecting = false;
  private disposed = false;
  private state: GlobalEventState = "closed";
  private failures = 0;
  private connected = false;
  private connection?: CoreConnection;
  private connectionId?: string;
  private sequence = 0;
  private requestSequence = 0;
  constructor(
    private readonly options: {
      connection: () => Promise<CoreConnection>;
      createSocket?: (url: string, protocols: string[]) => WebSocket;
      log?: (message: string) => void;
    },
  ) {}

  subscribe(
    topic: GlobalEventTopic,
    resource: GlobalEventResource,
    observer: GlobalEventObserver,
  ): () => void {
    if (this.disposed) throw new Error("全局事件连接已关闭");
    const key = globalEventResourceKey(topic, resource);
    let subscription = this.subscriptions.get(key);
    if (!subscription) {
      if (this.subscriptions.size >= 128)
        throw new Error("全局事件资源订阅超过上限");
      subscription = {
        topic,
        resource,
        cursor: observer.after,
        observers: new Set(),
        seenEvents: new Set(),
        eventOrder: [],
        state: this.state === "closed" ? "connecting" : this.state,
        failures: 0,
      };
      this.subscriptions.set(key, subscription);
      if (this.state === "open") this.control("subscribe", subscription);
    }
    subscription.observers.add(observer);
    observer.onState?.(subscription.state);
    if (this.socket === undefined && this.retry === undefined)
      void this.connect();
    let active = true;
    return () => {
      if (!active) return;
      active = false;
      subscription.observers.delete(observer);
      if (subscription.observers.size) return;
      clearTimeout(subscription.retry);
      this.subscriptions.delete(key);
      this.control("unsubscribe", subscription);
      if (!this.subscriptions.size) this.stopConnection();
    };
  }
  private notifyState(state: GlobalEventState, changed = false) {
    this.state = state;
    for (const subscription of this.subscriptions.values()) {
      subscription.state = state;
      for (const observer of subscription.observers)
        observer.onState?.(state, changed);
    }
  }
  private resync(reason: string, key?: string) {
    const subscriptions = key
      ? [this.subscriptions.get(key)]
      : this.subscriptions.values();
    for (const subscription of subscriptions)
      if (subscription)
        for (const observer of subscription.observers)
          observer.onResync?.(reason);
  }
  private control(
    type: "subscribe" | "unsubscribe" | "resync",
    subscription: Subscription,
  ) {
    if (this.socket?.readyState !== 1) return;
    clearTimeout(subscription.retry);
    subscription.retry = undefined;
    const requestId = String(++this.requestSequence);
    subscription.requestId = requestId;
    this.socket.send(
      JSON.stringify({
        type,
        requestId,
        topic: subscription.topic,
        resource: subscription.resource,
        ...(type !== "unsubscribe" && subscription.cursor !== undefined
          ? { after: subscription.cursor }
          : {}),
      }),
    );
  }
  private armDeadline(socket: WebSocket, delay: number) {
    clearTimeout(this.deadline);
    this.deadline = setTimeout(() => {
      if (this.socket === socket) this.disconnected(socket);
    }, delay);
  }
  private async connect() {
    if (this.connecting || this.disposed || !this.subscriptions.size) return;
    this.connecting = true;
    this.notifyState(this.connected ? "reconnecting" : "connecting");
    try {
      await this.closing;
      if (this.disposed || !this.subscriptions.size) return;
      const connection = await this.options.connection();
      if (this.disposed || !this.subscriptions.size) return;
      const changed = Boolean(
        this.connection &&
        (this.connection.baseUrl !== connection.baseUrl ||
          this.connection.token !== connection.token),
      );
      this.connection = connection;
      const url = new URL("/api/v1/events", connection.baseUrl);
      if (url.hostname !== "127.0.0.1" || url.protocol !== "http:")
        throw new Error("全局事件服务地址无效");
      url.protocol = "ws:";
      const socket = (
        this.options.createSocket ??
        ((target, protocols) => new WebSocket(target, protocols))
      )(url.toString(), ["ally.events.v1", ...(connection.token ? [`ally.auth.${connection.token}`] : [])]);
      this.socket = socket;
      this.connectionId = undefined;
      this.sequence = 0;
      this.armDeadline(socket, 20_000);
      socket.onopen = () => {
        if (this.socket !== socket || this.disposed) return;
        const recovering = this.connected;
        this.connected = true;
        this.failures = 0;
        this.armDeadline(socket, 45_000);
        this.notifyState("open", changed);
        // 先通知消费者恢复，再让服务器发送补齐/快照，消费层可缓冲竞态。
        if (recovering || changed)
          this.resync(changed ? "core_restarted" : "reconnected");
        for (const subscription of this.subscriptions.values())
          this.control("subscribe", subscription);
      };
      socket.onmessage = (message) => {
        if (this.socket !== socket || this.disposed) return;
        this.armDeadline(socket, 45_000);
        this.receive(message.data);
      };
      socket.onerror = () => this.disconnected(socket);
      socket.onclose = () => this.disconnected(socket);
    } catch {
      // 不记录 URL、协议头或外部错误正文，避免令牌进入诊断。
      this.options.log?.("全局事件连接暂不可用，将按退避重连。");
      this.scheduleRetry();
    } finally {
      this.connecting = false;
    }
  }
  private receive(data: unknown) {
    if (typeof data !== "string" || data.length > 131_072) {
      this.resync("invalid_frame");
      if (this.socket) this.disconnected(this.socket);
      return;
    }
    let raw: unknown;
    try {
      raw = JSON.parse(data);
    } catch {
      this.resync("invalid_frame");
      return;
    }
    const result = globalEventFrameSchema.safeParse(raw);
    if (!result.success) {
      this.resync("invalid_frame");
      return;
    }
    const frame = result.data;
    if (
      this.connectionId === frame.connectionId &&
      frame.deliverySequence <= this.sequence
    )
      return;
    if (
      (!this.connectionId && frame.deliverySequence !== 1) ||
      (this.connectionId &&
        (this.connectionId !== frame.connectionId ||
          frame.deliverySequence !== this.sequence + 1))
    )
      this.resync("delivery_gap");
    this.connectionId = frame.connectionId;
    this.sequence = frame.deliverySequence;
    if (frame.type === "heartbeat") return;
    const key = globalEventResourceKey(frame.topic, frame.resource);
    const subscription = this.subscriptions.get(key);
    if (frame.type === "subscribed" || frame.type === "unsubscribed") {
      if (
        !subscription ||
        typeof frame.payload !== "object" ||
        frame.payload === null ||
        !("requestId" in frame.payload) ||
        frame.payload.requestId !== subscription.requestId
      )
        return;
      if (frame.type === "subscribed") {
        const recovering = subscription.failures > 0;
        clearTimeout(subscription.retry);
        subscription.retry = undefined;
        subscription.failures = 0;
        subscription.state = "open";
        for (const observer of subscription.observers)
          observer.onState?.("open");
        if (recovering) this.resync("resource_recovered", key);
      }
      return;
    }
    if (frame.type === "resync_required") {
      this.resync("resync_required", subscription ? key : undefined);
      if (
        subscription &&
        [...subscription.observers].every((observer) => !observer.onResync)
      )
        this.control("resync", subscription);
      return;
    }
    if (frame.type === "error") {
      if (
        !subscription ||
        typeof frame.payload !== "object" ||
        frame.payload === null ||
        !("requestId" in frame.payload) ||
        frame.payload.requestId !== subscription.requestId
      )
        return;
      clearTimeout(subscription.retry);
      subscription.retry = undefined;
      subscription.state = "unavailable";
      for (const observer of subscription.observers)
        observer.onState?.("unavailable");
      if ("retryable" in frame.payload && frame.payload.retryable === true)
        this.retryResource(key, subscription);
      else this.resync("subscription_rejected", key);
      return;
    }
    const global = this.subscriptions.get(
      globalEventResourceKey(frame.topic, { kind: "global" }),
    );
    const recipients =
      frame.resource.kind === "global"
        ? new Set(
            [...this.subscriptions.values()].filter(
              (item) => item.topic === frame.topic,
            ),
          )
        : new Set([subscription, global]);
    for (const target of recipients) {
      if (!target) continue;
      if (
        target === subscription &&
        !frame.transient &&
        frame.sourceCursor !== undefined
      )
        target.cursor =
          typeof target.cursor === "number" &&
          typeof frame.sourceCursor === "number"
            ? Math.max(target.cursor, frame.sourceCursor)
            : frame.sourceCursor;
      if (frame.eventId && target.seenEvents.has(frame.eventId)) continue;
      if (frame.eventId) {
        target.seenEvents.add(frame.eventId);
        target.eventOrder.push(frame.eventId);
        if (target.eventOrder.length > 512) {
          const old = target.eventOrder.shift();
          if (old) target.seenEvents.delete(old);
        }
      }
      for (const observer of target.observers) observer.onEvent(frame);
    }
  }
  /** 已认证连接保持；只有失败的逻辑资源重试，取消/缺失/非法来源不会循环。 */
  private retryResource(key: string, subscription: Subscription): void {
    subscription.failures += 1;
    const delay = Math.min(
      Math.min(250 * 2 ** Math.min(subscription.failures - 1, 6), 10_000) *
        (0.8 + Math.random() * 0.4),
      10_000,
    );
    subscription.retry = setTimeout(() => {
      subscription.retry = undefined;
      if (
        !this.disposed &&
        this.state === "open" &&
        this.subscriptions.get(key) === subscription
      )
        this.control("subscribe", subscription);
    }, delay);
  }
  private clearResourceRetries(): void {
    for (const subscription of this.subscriptions.values()) {
      clearTimeout(subscription.retry);
      subscription.retry = undefined;
    }
  }
  private disconnected(socket: WebSocket) {
    if (this.socket !== socket) return;
    this.socket = undefined;
    this.clearResourceRetries();
    socket.onclose = socket.onerror = socket.onmessage = socket.onopen = null;
    this.closeSocket(socket);
    clearTimeout(this.deadline);
    this.deadline = undefined;
    this.scheduleRetry();
  }
  private scheduleRetry() {
    if (this.disposed || !this.subscriptions.size || this.retry !== undefined)
      return;
    this.failures += 1;
    this.notifyState(this.failures >= 5 ? "unavailable" : "reconnecting");
    if (this.failures === 1 && this.connected) this.resync("disconnected");
    const delay =
      Math.min(250 * 2 ** Math.min(this.failures - 1, 6), 10_000) *
      (0.8 + Math.random() * 0.4);
    this.retry = setTimeout(() => {
      this.retry = undefined;
      void this.connect();
    }, delay);
  }
  private stopConnection() {
    this.clearResourceRetries();
    clearTimeout(this.retry);
    clearTimeout(this.deadline);
    this.retry = this.deadline = undefined;
    const socket = this.socket;
    this.socket = undefined;
    if (socket) {
      socket.onclose = socket.onerror = socket.onmessage = socket.onopen = null;
      this.closeSocket(socket);
    }
    this.notifyState("closed");
  }
  requestResync(
    topic: GlobalEventTopic,
    resource: GlobalEventResource,
    after?: number | string,
  ) {
    const subscription = this.subscriptions.get(
      globalEventResourceKey(topic, resource),
    );
    if (subscription) {
      if (after !== undefined) subscription.cursor = after;
      this.control("resync", subscription);
    }
  }
  private closeSocket(socket: WebSocket): void {
    if (socket.readyState === 3) return;
    const closing = new Promise<void>((resolve) => {
      const complete = () => {
        clearTimeout(timer);
        socket.removeEventListener("close", complete);
        resolve();
      };
      const timer = setTimeout(complete, 3_000);
      socket.addEventListener("close", complete, { once: true });
      if (socket.readyState !== 2) socket.close();
    });
    this.closing = closing;
    void closing.finally(() => {
      if (this.closing === closing) this.closing = undefined;
    });
  }
  /** 退出可等待关闭握手，包括最后一个资源已退订而连接仍在关闭的情况。 */
  close(): Promise<void> {
    if (this.disposed) return this.closing ?? Promise.resolve();
    this.disposed = true;
    this.stopConnection();
    this.subscriptions.clear();
    return this.closing ?? Promise.resolve();
  }
}
