import type { WorkspaceAttentionOpen } from "@neuxbot/platform-contracts/services";
import { chatHref } from "@/shared/lib/chat-navigation";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";

export function workspaceAttentionMainOwner(target: WorkspaceAttentionOpen) {
  if (target?.work.kind !== "main_conversation") return;
  if (target.channelId) return { kind: "team" as const, id: target.channelId };
  if (target.wakerId) return { kind: "waker" as const, id: target.wakerId };
}

export function workspaceAttentionOpenHref(
  target: WorkspaceAttentionOpen,
): string {
  if (!target) return "/work-board";
  const isRequest = target.operation === "request_opened";
  const request = isRequest ? target.requestId : undefined;
  const message = request ? undefined : target.messageId;
  const query = new URLSearchParams();
  if (request) query.set("request", request);
  if (message) query.set("message", message);
  switch (target.work.kind) {
    case "main_conversation": {
      const owner = workspaceAttentionMainOwner(target);
      if (!owner) break;
      if (!isRequest)
        return mainConversationHref({ ...owner, messageId: message });
      if (request && target.chatId)
        return mainConversationHref({
          ...owner,
          request: { requestId: request, workChatId: target.chatId },
        });
      break;
    }
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
  return `/work-board?mode=${isRequest ? "action" : "awaiting_view"}`;
}
