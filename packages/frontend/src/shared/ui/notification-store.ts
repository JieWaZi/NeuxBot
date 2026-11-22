import { useSyncExternalStore, type ReactNode } from "react";

type NotificationKind = "info" | "error" | "success";
type NotificationID = string | number;

type NotificationOptions = {
  id?: NotificationID;
  description?: ReactNode;
  icon?: ReactNode;
  action?: { label: ReactNode; onClick: () => void };
  onDismiss?: () => void;
  onAutoClose?: () => void;
};

type Notification = Omit<NotificationOptions, "id"> & {
  id: NotificationID;
  kind: NotificationKind;
  title: ReactNode;
  expiresAt?: number;
  exiting?: boolean;
};

export const notificationDuration = 3000;
export const notificationExitDuration = 140;
let notifications: Notification[] = [];
let nextID = 0;
const listeners = new Set<() => void>();
const timers = new Map<NotificationID, ReturnType<typeof setTimeout>>();

function publish(next: Notification[]) {
  notifications = next;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useNotifications() {
  return useSyncExternalStore(subscribe, () => notifications);
}

function clearTimer(id: NotificationID) {
  clearTimeout(timers.get(id));
  timers.delete(id);
}

function remove(id: NotificationID) {
  clearTimer(id);
  publish(notifications.filter((notice) => notice.id !== id));
}

export function finishNotificationExit(id: NotificationID) {
  if (notifications.some((notice) => notice.id === id && notice.exiting))
    remove(id);
}

function dismiss(id?: NotificationID) {
  if (id === undefined) {
    for (const timer of timers.values()) clearTimeout(timer);
    timers.clear();
    publish([]);
    return;
  }
  const notice = notifications.find((current) => current.id === id);
  if (!notice || notice.exiting) return;
  if (
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches
  ) {
    remove(id);
    return;
  }
  clearTimer(id);
  // 未挂载或翻页隐藏的卡片不会触发 animationend。
  timers.set(
    id,
    setTimeout(() => finishNotificationExit(id), notificationExitDuration * 2),
  );
  publish(
    notifications.map((current) =>
      current === notice ? { ...notice, exiting: true } : current,
    ),
  );
}

export function closeNotification(id: NotificationID) {
  const notice = notifications.find((notice) => notice.id === id);
  if (!notice || notice.exiting) return;
  dismiss(id);
  notice.onDismiss?.();
}

function show(
  title: ReactNode,
  options: NotificationOptions | undefined,
  kind: NotificationKind,
  manual = false,
) {
  const id = options?.id ?? ++nextID;
  clearTimer(id);
  const expiresAt = manual
    ? undefined
    : performance.now() + notificationDuration;
  const notice = { ...options, id, kind, title, expiresAt };
  if (!manual) {
    timers.set(
      id,
      setTimeout(() => {
        dismiss(id);
        options?.onAutoClose?.();
      }, notificationDuration),
    );
  }
  publish([notice, ...notifications.filter((current) => current.id !== id)]);
  return id;
}

/** 操作反馈使用统一倒计时，主动通知通过 manual 等待用户关闭。 */
export const toast = {
  error: (title: ReactNode, options?: NotificationOptions) =>
    show(title, options, "error"),
  info: (title: ReactNode, options?: NotificationOptions) =>
    show(title, options, "info"),
  success: (title: ReactNode, options?: NotificationOptions) =>
    show(title, options, "success"),
  manual: (title: ReactNode, options?: NotificationOptions) =>
    show(title, options, "info", true),
  dismiss,
};
