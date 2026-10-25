import { chatHref } from "@/shared/lib/chat-navigation";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardEntry as Entry,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardAttention as Attention,
} from "@/shared/api/generated/types.gen";

export function workEntryHref(entry: Entry, result = false) {
  if (entry.ownerKind === "workflow_run")
    return `/workflows/${entry.workflowId}/runs/${entry.ownerId}${result ? "?result=1" : ""}`;
  const base =
    entry.ownerKind === "team_thread"
      ? `/teams/${entry.channelId}/threads/${entry.ownerId}`
      : chatHref(entry.chatId ?? entry.ownerId ?? "", entry.wakerId ?? "");
  const id = result ? entry.result?.reference?.messageId : undefined;
  return id
    ? `${base}${base.includes("?") ? "&" : "?"}resultMessage=${encodeURIComponent(id)}`
    : base;
}

export function workAttentionHref(entry: Attention) {
  if (entry.workflowId && entry.chatId && !entry.runId) {
    const params = new URLSearchParams({
      chat: entry.chatId,
      request: entry.id ?? "",
    });
    return `/workflows/${entry.workflowId}?${params}`;
  }
  if (entry.ownerKind === "workflow_run" && !entry.chatId)
    return `/workflows/${entry.workflowId}/runs/${entry.runId}?request=${encodeURIComponent(entry.id ?? "")}`;
  if (entry.ownerKind === "team_thread")
    return `/teams/${entry.channelId}/threads/${entry.ownerId}?${entry.taskId ? `task=${entry.taskId}` : `wakerId=${entry.wakerId}&request=${encodeURIComponent(entry.id ?? "")}`}`;
  return `${chatHref(entry.chatId ?? "", entry.wakerId ?? "")}&request=${encodeURIComponent(entry.id ?? "")}`;
}
