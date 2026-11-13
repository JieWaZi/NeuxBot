import type { WorkspaceAttentionOpen } from "@neuxbot/platform-contracts/services";
import { chatHref } from "@/shared/lib/chat-navigation";

export function workspaceAttentionOpenHref(
  target: WorkspaceAttentionOpen,
): string {
  if (!target) return "/work-board";
  const request =
    target.operation === "request_opened" ? target.requestId : undefined;
  const message = request ? undefined : target.messageId;
  const query = new URLSearchParams();
  if (request) query.set("request", request);
  if (message) query.set("message", message);
  switch (target.work.kind) {
    case "chat":
      if (target.definitionId) {
        query.set("chat", target.chatId || target.work.id);
        return `/workflows/${target.definitionId}?${query}`;
      }
      return `${chatHref(target.chatId || target.work.id, target.wakerId ?? "")}${query.size ? `&${query}` : ""}`;
    case "team_thread":
      if (!target.channelId) break;
      if (target.taskId && request) {
        query.delete("request");
        query.set("task", target.taskId);
        if (target.requestFence != null)
          query.set("fence", String(target.requestFence));
      } else if (request) {
        if (target.wakerId) query.set("wakerId", target.wakerId);
        if (target.chatId) query.set("requestChat", target.chatId);
      }
      return `/teams/${target.channelId}/threads/${target.threadId || target.work.id}${query.size ? `?${query}` : ""}`;
    case "workflow_run":
      if (request && target.chatId)
        return `${chatHref(target.chatId, target.wakerId ?? "")}&${query}`;
      if (!target.definitionId) break;
      if (target.instanceId) query.set("instance", target.instanceId);
      if (!request) query.set("result", "1");
      query.delete("message");
      return `/workflows/${target.definitionId}/runs/${target.runId || target.work.id}?${query}`;
    case "automatic_run":
      if (target.automaticTaskId)
        return `/automatic-tasks/${target.automaticTaskId}?run=${target.work.id}`;
  }
  return `/work-board?mode=${request ? "action" : "awaiting_view"}`;
}
