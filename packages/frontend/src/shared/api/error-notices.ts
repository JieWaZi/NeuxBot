import {
  ApiError,
  apiErrorMessageKey,
  serviceConnectionMessageKey,
} from "./errors";
import { useMemo, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

/** 错误提示是本地展示状态；不复制服务端消息或执行状态。 */
export type ErrorNotice = {
  id: string;
  scope: string;
  /** 操作的稳定身份；独立事件提示使用自身身份，不随其他请求成功清理。 */
  operation: string;
  /** 一次真实失败的版本；回调绑定或页面卸载不产生新失败版本。 */
  version: string;
  message: string;
  messageKey?: typeof serviceConnectionMessageKey;
  createdAt: number;
  action?: "retry" | "check";
};
type ErrorNoticeInput = Omit<
  ErrorNotice,
  "createdAt" | "operation" | "version"
> & {
  operation?: string;
};

const storageKey = "ally:error-notices";
const browserNetworkMessages = new Set([
  "failed to fetch",
  "fetch failed",
  "load failed",
  "networkerror when attempting to fetch resource.",
]);
/** 网络断连提示只用于当前会话，不从本地存储跨重启恢复。 */
function isTransientNetworkNotice(notice: ErrorNotice): boolean {
  if (!notice.id.startsWith("api:") && !notice.id.startsWith("history:")) {
    return false;
  }
  return (
    notice.id.endsWith(":NETWORK") ||
    browserNetworkMessages.has(notice.message.trim().toLowerCase())
  );
}
function readStored<T>(key: string, fallback: T): T {
  try {
    return JSON.parse(localStorage.getItem(key) ?? "null") ?? fallback;
  } catch {
    return fallback;
  }
}
const stored: unknown = readStored(storageKey, []);
function storedNotice(
  input: ErrorNoticeInput,
): Omit<ErrorNotice, "createdAt" | "version"> {
  const messageKey =
    input.messageKey === serviceConnectionMessageKey
      ? input.messageKey
      : apiErrorMessageKey(input.message);
  const notice = { ...input, operation: input.operation ?? input.id };
  return messageKey ? { ...notice, message: "", messageKey } : notice;
}
let notices: ErrorNotice[] = Array.isArray(stored)
  ? stored
      .filter(
        (item): item is ErrorNotice =>
          typeof item === "object" &&
          item !== null &&
          typeof item.id === "string" &&
          typeof item.scope === "string" &&
          typeof item.operation === "string" &&
          typeof item.version === "string" &&
          typeof item.message === "string" &&
          typeof item.createdAt === "number",
      )
      .filter((item) => !isTransientNetworkNotice(item))
      .map((item) => ({
        ...storedNotice(item),
        action: undefined,
        version: item.version,
        createdAt: item.createdAt,
      }))
  : [];
const listeners = new Set<() => void>();
const actions = new Map<string, () => void | Promise<void>>();
function publish() {
  try {
    localStorage.setItem(storageKey, JSON.stringify(notices));
  } catch {
    /* 本地存储不可用时，本次应用运行内仍保留提示。 */
  }
  listeners.forEach((listener) => listener());
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function reportErrorNotice(
  input: ErrorNoticeInput,
  action?: () => void | Promise<void>,
) {
  const normalized = storedNotice(input);
  if (action) actions.set(normalized.id, action);
  const previous = notices.find((notice) => notice.id === normalized.id);
  notices = [
    ...notices.filter((notice) => notice.id !== normalized.id),
    {
      ...previous,
      ...normalized,
      action: normalized.action ?? previous?.action,
      createdAt: Date.now(),
      version: crypto.randomUUID(),
    },
  ];
  publish();
}

/** 开始操作时记录待恢复的具体错误版本，迟到成功不能清除后来产生的失败。 */
export function captureErrorNoticeRecovery(operation: string) {
  return notices.filter((notice) => notice.operation === operation);
}

export function resolveErrorNoticeRecovery(recovery: readonly ErrorNotice[]) {
  dismissErrorNotices(
    notices
      .filter((notice) =>
        recovery.some(
          (captured) =>
            captured.id === notice.id && captured.version === notice.version,
        ),
      )
      .map((notice) => notice.id),
  );
}
export function dismissErrorNotice(id: string) {
  notices = notices.filter((notice) => notice.id !== id);
  actions.delete(id);
  publish();
}
export function dismissErrorNotices(ids: readonly string[]) {
  const selected = new Set(ids);
  if (!selected.size) return;
  notices = notices.filter((notice) => !selected.has(notice.id));
  for (const id of selected) actions.delete(id);
  publish();
}
export function runErrorNoticeAction(id: string) {
  return actions.get(id)?.();
}
export function useErrorNotices() {
  const { t } = useTranslation("common");
  const storedNotices = useSyncExternalStore(
    subscribe,
    () => notices,
    () => notices,
  );
  return useMemo(
    () =>
      storedNotices.map((notice) =>
        notice.messageKey
          ? { ...notice, message: t(notice.messageKey) }
          : notice,
      ),
    [storedNotices, t],
  );
}
/** 仅真正关联聊天的请求进入消息区，资源管理错误继续就近通知。 */
export function apiErrorScope(path: string): string {
  const chat = path.match(/\/chats\/([^/]+)(?:\/|$)/);
  if (chat?.[1]) return `chat:${chat[1]}`;
  const team = path.match(/\/channels\/[^/]+\/threads\/([^/]+)(?:\/|$)/);
  if (team?.[1]) return `team:${team[1]}`;
  return "global";
}

/** 页面离开后释放闭包，错误事实仍保留，避免旧恢复动作操作其他会话。 */
export function clearErrorNoticeActions(scope: string) {
  let changed = false;
  notices = notices.map((notice) => {
    if (notice.scope !== scope || !notice.action) return notice;
    actions.delete(notice.id);
    changed = true;
    return { ...notice, action: undefined };
  });
  if (changed) publish();
}

/** 已经被 API 拦截器记录的错误不重复提示；本地操作失败也进入当前消息区。 */
export function reportChatError(
  error: unknown,
  scope: string,
  fallback: string,
  operation = `chat-operation:${scope}:${fallback}`,
) {
  if (error instanceof ApiError && error.noticeId) return;
  const message =
    error instanceof ApiError && error.code !== "NETWORK"
      ? error.message
      : fallback;
  const messageKey = apiErrorMessageKey(message);
  const previous = notices.find(
    (notice) =>
      notice.id.startsWith("chat-operation:") &&
      notice.scope === scope &&
      notice.operation === operation &&
      (notice.message === message ||
        (messageKey !== undefined && notice.messageKey === messageKey)),
  );
  reportErrorNotice({
    id: previous?.id ?? `chat-operation:${crypto.randomUUID()}`,
    scope,
    message,
    operation,
  });
}

/** 查询缓存提供真正的只读重试，成功结果仍由缓存统一更新。 */
export function bindErrorNoticeRetry(
  error: unknown,
  retry: () => Promise<unknown>,
) {
  if (!(error instanceof ApiError) || !error.noticeId) return;
  const notice = notices.find((item) => item.id === error.noticeId);
  if (!notice) return;
  actions.set(notice.id, async () => {
    const recovery = captureErrorNoticeRecovery(notice.operation);
    await retry();
    resolveErrorNoticeRecovery(recovery);
  });
  notices = notices.map((current) =>
    current.id === notice.id ? { ...current, action: "retry" } : current,
  );
  publish();
}
