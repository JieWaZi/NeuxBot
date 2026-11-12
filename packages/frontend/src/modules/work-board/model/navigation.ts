import type { WorkBoardEntry as Entry } from "../api/queries";
import { chatHref } from "@/shared/lib/chat-navigation";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardAttention as Attention } from "@/shared/api/generated/types.gen";

export function workEntryHref(entry: Entry, result = false) {
  if (entry.ownerKind === "automatic_run")
    return `/automatic-tasks/${entry.automaticTaskId}?run=${entry.ownerId}`;
  if (entry.ownerKind === "workflow_run")
    return `/workflows/${entry.workflowId}/runs/${entry.ownerId}${result ? "?result=1" : ""}`;
  if (entry.ownerKind === "chat" && entry.workflowId && entry.chatId) {
    const params = new URLSearchParams({ chat: entry.chatId });
    if (result && entry.firstUnreadMessageID)
      params.set("message", entry.firstUnreadMessageID);
    return `/workflows/${entry.workflowId}?${params}`;
  }
  const base =
    entry.ownerKind === "team_thread"
      ? `/teams/${entry.channelId}/threads/${entry.ownerId}`
      : chatHref(entry.chatId ?? entry.ownerId ?? "", entry.wakerId ?? "");
  const id = result ? entry.firstUnreadMessageID : undefined;
  return id
    ? `${base}${base.includes("?") ? "&" : "?"}message=${encodeURIComponent(id)}`
    : base;
}

export type WorkAttentionTarget = Attention;

export function workAttentionHref(entry: WorkAttentionTarget) {
  const request = entry.requestId ?? entry.id ?? "";
  if (entry.workflowId && entry.chatId && !entry.runId) {
    const params = new URLSearchParams({
      chat: entry.chatId,
      request,
    });
    return `/workflows/${entry.workflowId}?${params}`;
  }
  if (entry.ownerKind === "workflow_run" && !entry.chatId) {
    const params = new URLSearchParams({ request });
    if (entry.instanceId) params.set("instance", entry.instanceId);
    return `/workflows/${entry.workflowId}/runs/${entry.runId}?${params}`;
  }
  if (entry.ownerKind === "team_thread") {
    const params = new URLSearchParams();
    if (entry.taskId && entry.kind === "review") {
      params.set("task", entry.taskId);
      if (entry.requestFence != null)
        params.set("fence", String(entry.requestFence));
    } else {
      if (entry.wakerId) params.set("wakerId", entry.wakerId);
      if (entry.chatId) params.set("requestChat", entry.chatId);
      if (request) params.set("request", request);
    }
    return `/teams/${entry.channelId}/threads/${entry.ownerId}?${params}`;
  }
  return `${chatHref(entry.chatId ?? "", entry.wakerId ?? "")}&request=${encodeURIComponent(request)}`;
}
