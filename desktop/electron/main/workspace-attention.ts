import type { GlobalEventResource } from "@neuxbot/platform-contracts/core-event-protocol";
import {
  workspaceAttentionLiveSchema,
  workspaceAttentionTargetSchema,
  type NotificationPreferences,
  type WorkspaceAttentionLive,
  type WorkspaceAttentionOpen,
  type WorkspaceAttentionTarget,
} from "@neuxbot/platform-contracts/workspace-attention";
import type { CoreConnection } from "../shared/contracts";
import type { CoreEventClient } from "../shared/core-event-client";

const topic = "work-board";
const resource = { kind: "global" } as const;
const maxReminders = 128;
const maxSeen = 512;

type NativeReminder = {
  once(event: "click" | "close" | "failed", listener: () => void): unknown;
  show(): void;
  close(): void;
};

type Reminder = {
  event: WorkspaceAttentionLive;
  controller: AbortController;
  work?: WorkspaceAttentionTarget["work"];
  native?: NativeReminder;
  recheck?: boolean;
};

type Options = {
  events: Pick<CoreEventClient, "subscribe">;
  preferences: () => NotificationPreferences;
  isFocused: () => boolean;
  resolveTarget: (
    target: WorkspaceAttentionLive,
    includePreview: boolean,
    signal: AbortSignal,
  ) => Promise<WorkspaceAttentionTarget>;
  showNotification: (content: {
    title: string;
    body: string;
  }) => NativeReminder | undefined;
  open: (
    takeTarget: () => WorkspaceAttentionOpen | undefined,
  ) => void | Promise<void>;
};

function isRequest(event: WorkspaceAttentionLive) {
  return (
    event.operation === "request_opened" || event.operation === "request_closed"
  );
}

function reminderKey(event: WorkspaceAttentionLive) {
  const owner = [event.work.kind, event.work.id, event.chatId];
  if (isRequest(event)) {
    if (!event.requestId || !event.requestKind) return undefined;
    return JSON.stringify([
      ...owner,
      event.requestKind,
      event.requestId,
      event.executionId,
      event.requestFence,
    ]);
  }
  return JSON.stringify([
    ...owner,
    "result",
    event.messageId ?? event.executionId,
  ]);
}

/** 仅从主进程订阅实时提醒。请求身份保留到结束，查摘要时收到结束同样会撤回。 */
export function installWorkspaceAttention(options: Options) {
  const seen = new Set<string>();
  const reminders = new Map<string, Reminder>();
  const lifetime = new AbortController();
  let ready = false;
  let pendingOpen:
    { key: string; request: boolean; invalidated: boolean } | undefined;

  function withdraw(key: string) {
    const reminder = reminders.get(key);
    reminders.delete(key);
    reminder?.controller.abort();
    reminder?.native?.close();
  }

  function remember(key: string) {
    seen.add(key);
    if (seen.size > maxSeen) {
      const first = seen.values().next().value;
      if (first !== undefined) seen.delete(first);
    }
  }

  function reset() {
    ready = false;
    if (pendingOpen?.request) pendingOpen.invalidated = true;
    for (const [key, item] of reminders)
      if (isRequest(item.event) || !item.native) withdraw(key);
  }

  async function reconcileRequests(scope: GlobalEventResource) {
    for (const [key, reminder] of [...reminders]) {
      const event = reminder.event;
      if (
        !isRequest(event) ||
        !(
          scope.kind === "global" ||
          (scope.kind === "chat" && scope.id === event.chatId) ||
          (scope.kind === event.work.kind && scope.id === event.work.id) ||
          (scope.kind === reminder.work?.kind && scope.id === reminder.work.id)
        )
      )
        continue;
      const target = await options
        .resolveTarget(event, false, reminder.controller.signal)
        .catch(() => undefined);
      if (target?.valid === false && reminders.get(key) === reminder)
        withdraw(key);
    }
  }

  function allowed(event: WorkspaceAttentionLive) {
    const { enabled, mode } = options.preferences();
    return (
      enabled &&
      !options.isFocused() &&
      (mode === "all" || (mode === "needs-me" && isRequest(event)))
    );
  }

  async function open(event: WorkspaceAttentionLive, key: string) {
    if (event.operation === "request_closed") return;
    const intent = { key, request: isRequest(event), invalidated: false };
    pendingOpen = intent;
    const target = await options
      .resolveTarget(event, false, lifetime.signal)
      .catch(() => undefined);
    if (lifetime.signal.aborted || pendingOpen !== intent) return;
    const operation = event.operation;
    try {
      await options.open(() => {
        if (lifetime.signal.aborted || pendingOpen !== intent) return;
        pendingOpen = undefined;
        if (intent.invalidated || !target?.valid) return null;
        return {
          ...event,
          operation,
          work: target.work ?? event.work,
          chatId: target.chatId ?? event.chatId,
          wakerId: target.wakerId,
          channelId: target.channelId,
          threadId: target.threadId,
          definitionId: target.definitionId,
          runId: target.runId,
          taskId: target.taskId,
          instanceId: target.instanceId,
          automaticTaskId: target.automaticTaskId,
        };
      });
    } catch (error) {
      if (pendingOpen === intent) pendingOpen = undefined;
      throw error;
    }
  }

  async function present(
    event: WorkspaceAttentionLive,
    key: string,
    reminder: Reminder,
  ) {
    try {
      const needsTarget = isRequest(event) || event.work.kind === "chat";
      let target: WorkspaceAttentionTarget | undefined;
      do {
        reminder.recheck = false;
        const preview = options.preferences().preview;
        if (!preview && !needsTarget) break;
        target = await options
          .resolveTarget(event, preview, reminder.controller.signal)
          .catch(() => undefined);
      } while (
        !target?.valid &&
        reminder.recheck &&
        !reminder.controller.signal.aborted &&
        ready &&
        allowed(event)
      );
      if (
        reminders.get(key) !== reminder ||
        !ready ||
        !allowed(event) ||
        target?.valid === false ||
        (needsTarget && !target?.valid)
      )
        return;
      reminder.work = target?.work;
      remember(key);
      const title = options.preferences().preview ? target?.title : undefined;
      const body = options.preferences().preview ? target?.preview : undefined;
      const notification = options.showNotification({
        title: title || "NeuxBot",
        body:
          body ||
          (isRequest(event)
            ? "有一项请求需要处理"
            : event.operation === "failed"
              ? "有一项工作未能完成"
              : "有新的工作结果"),
      });
      if (!notification) return;
      reminder.native = notification;
      const release = () => {
        if (reminders.get(key) === reminder) reminders.delete(key);
      };
      notification.once("click", () => {
        release();
        void open(event, key).catch(() => undefined);
      });
      notification.once("close", release);
      notification.once("failed", release);
      notification.show();
    } finally {
      if (!reminder.native && reminders.get(key) === reminder)
        reminders.delete(key);
    }
  }

  const stop = options.events.subscribe(topic, resource, {
    onState(state, changed) {
      if (changed || state !== "open") reset();
    },
    onResync(reason) {
      if (reason === "subscription_joined") return;
      reset();
    },
    onEvent(frame) {
      if (frame.type === "work-board.ready") {
        ready = true;
        return;
      }
      if (!ready) return;
      if (
        frame.type === "work-board.changed" &&
        frame.payload &&
        typeof frame.payload === "object" &&
        "reason" in frame.payload &&
        (frame.payload.reason === "reading-recovered" ||
          frame.payload.reason === "requests-changed")
      ) {
        void reconcileRequests(frame.resource);
        return;
      }
      if (frame.type !== "work-board.live") return;
      const parsed = workspaceAttentionLiveSchema.safeParse(frame.payload);
      if (!parsed.success) return;
      const event = parsed.data;
      const key = reminderKey(event);
      if (!key) return;
      if (event.operation === "request_closed") {
        if (pendingOpen?.key === key) pendingOpen.invalidated = true;
        remember(key);
        withdraw(key);
        return;
      }
      if (seen.has(key)) return;
      const existing = reminders.get(key);
      if (existing) {
        if (!existing.native) existing.recheck = true;
        return;
      }
      if (!allowed(event)) {
        remember(key);
        return;
      }
      if (reminders.size >= maxReminders) {
        const oldest = reminders.keys().next().value;
        if (oldest !== undefined) withdraw(oldest);
      }
      const reminder = { event, controller: new AbortController() };
      reminders.set(key, reminder);
      void present(event, key, reminder).catch(() => withdraw(key));
    },
  });

  return () => {
    lifetime.abort();
    stop();
    for (const key of reminders.keys()) withdraw(key);
    seen.clear();
  };
}

/** 从原工作入口验证消息或请求仍可定位。预览与点击共用验证契约。 */
export function resolveWorkspaceAttentionTarget(
  connection: () => Promise<CoreConnection>,
) {
  return async (
    event: WorkspaceAttentionLive,
    includePreview: boolean,
    signal: AbortSignal,
  ) => {
    const core = await connection();
    const url = new URL("/api/v1/work-board/live-target", core.baseUrl);
    if (url.hostname !== "127.0.0.1" || url.protocol !== "http:" || !core.token)
      throw new Error("工作提醒服务地址无效");
    for (const [key, value] of Object.entries({
      workKind: event.work.kind,
      workId: event.work.id,
      operation: event.operation,
      chatId: event.chatId,
      messageId: event.messageId,
      requestId: event.requestId,
      requestFence: event.requestFence?.toString(),
      requestKind: event.requestKind,
      executionId: event.executionId,
      includePreview: String(includePreview),
    })) {
      if (value) url.searchParams.set(key, value);
    }
    const response = await fetch(url, {
      headers: { Authorization: `Bearer ${core.token}` },
      redirect: "error",
      signal: AbortSignal.any([signal, AbortSignal.timeout(3000)]),
    });
    if (response.status === 404 || response.status === 410)
      return { valid: false };
    if (!response.ok) throw new Error("工作提醒目标暂时不可用");
    return workspaceAttentionTargetSchema.parse(await response.json());
  };
}
