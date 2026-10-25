import type { ThreadMessage } from "@assistant-ui/react";
import type { CompanionActivity } from "@/shared/companion/activity";

/** 仅时间线末尾的助手消息跟随工作状态，历史头像保持空闲。 */
export function messageCompanionActivity(
  message: Pick<ThreadMessage, "content" | "status"> & { isLast: boolean },
  interaction: "waiting" | "stopping" | undefined = undefined,
): CompanionActivity {
  if (!message.isLast) return "idle";
  if (message.status?.type === "complete") return "completed";
  if (message.status?.type === "incomplete") {
    return message.status.reason === "cancelled" ? "stopped" : "failed";
  }
  if (interaction === "stopping") return "stopped";
  if (interaction === "waiting") return "waiting";
  for (const part of [...message.content].reverse()) {
    if (part.type !== "tool-call" || part.args.status !== "running") continue;
    const { action, category } = part.args;
    if (
      [
        "search_web",
        "fetch_web",
        "search_files",
        "read_file",
        "list_files",
        "view_image",
      ].includes(String(action)) ||
      category === "file_search" ||
      category === "file_read"
    )
      return "searching";
    if (category === "mcp" || category === "network") return "connecting";
    return "working";
  }
  for (const part of [...message.content].reverse()) {
    if (part.type === "text" && part.text.trim()) return "responding";
    if (part.type === "reasoning" && part.text.trim()) return "thinking";
    if (part.type === "tool-call" || part.type === "data") return "thinking";
  }
  return "starting";
}
