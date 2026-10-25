import type { NotificationItem } from "@/modules/notification";
import { chatHref } from "@/shared/lib/chat-navigation";

/** 统一通知只携带领域定位，站内路由由 app 组合。 */
export function notificationHref(notice: NotificationItem): string {
  const target = notice.target;
  const params = new URLSearchParams();
  if (target.channelId && target.wakerId) params.set("wakerId", target.wakerId);
  if (
    target.requestId &&
    notice.bucket === "action" &&
    notice.kind !== "review"
  )
    params.set("request", notice.id);
  if (target.taskId && target.channelId) params.set("task", target.taskId);
  if (target.instanceId) params.set("instance", target.instanceId);
  if (notice.kind === "result") {
    const messageId = target.messageId || target.reference?.messageId;
    if (messageId) params.set("resultMessage", messageId);
    else if (target.ownerKind === "workflow_run") params.set("result", "1");
  }
  if (
    notice.bucket === "action" &&
    target.definitionId &&
    target.chatId &&
    !target.runId
  ) {
    params.set("chat", target.chatId);
    return `/workflows/${target.definitionId}?${params}`;
  }
  if (notice.bucket === "action" && target.chatId && !target.channelId) {
    const base = chatHref(target.chatId, target.wakerId || "");
    return `${base}${params.size ? `&${params}` : ""}`;
  }
  if (target.definitionId && (target.runId || target.ownerId))
    return `/workflows/${target.definitionId}/runs/${target.runId || target.ownerId}?${params}`;
  if (target.channelId && (target.threadId || target.ownerId))
    return `/teams/${target.channelId}/threads/${target.threadId || target.ownerId}?${params}`;
  if (target.chatId || target.ownerKind === "chat") {
    const base = chatHref(
      target.chatId || target.ownerId || "",
      target.wakerId || "",
    );
    return `${base}${params.size ? `&${params}` : ""}`;
  }
  if (target.taskId && target.automaticRunId)
    return `/automatic-tasks/${target.taskId}?notice=${encodeURIComponent(notice.id)}&runId=${encodeURIComponent(target.automaticRunId)}`;
  return "/work-board";
}
