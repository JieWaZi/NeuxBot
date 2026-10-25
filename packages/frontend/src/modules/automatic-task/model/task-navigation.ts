import { chatHref } from "@/shared/lib/chat-navigation";
import type { RunView } from "./types";

/** 工作流运行打开固定版本 Run，团队和伙伴运行打开各自会话。 */
export function automaticRunHref(run: RunView) {
  if (run.workflowRunId && run.configuration?.workflowId)
    return `/workflows/${run.configuration.workflowId}/runs/${run.workflowRunId}`;
  if (run.threadId && run.configuration?.channelId)
    return `/teams/${run.configuration.channelId}/threads/${run.threadId}`;
  return run.chatId
    ? chatHref(run.chatId, run.wakerId || "")
    : `/automatic-tasks/${run.taskId}`;
}
