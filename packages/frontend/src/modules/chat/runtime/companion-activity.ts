import type { CompanionActivity } from "@/shared/companion/activity";
import type { AllyChatMessage } from "./chat-projection";

/** 仅时间线最新助手跟随真实执行状态，历史头像保持空闲。 */
export function messageCompanionActivity(
  message: Pick<
    AllyChatMessage,
    "displayParts" | "status" | "preparingSession"
  > & {
    isLast: boolean;
  },
  interaction: "waiting" | "stopping" | undefined = undefined,
): CompanionActivity {
  if (!message.isLast) return "idle";
  if (message.status === "completed") return "completed";
  if (message.status === "canceled") return "stopped";
  if (message.status === "failed") return "failed";
  if (interaction === "stopping") return "stopped";
  if (interaction === "waiting") return "waiting";
  const parts = message.displayParts ?? [];
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    const part = parts[index];
    if (part?.type !== "tool" || part.toolStatus !== "running") continue;
    if (
      [
        "search_web",
        "fetch_web",
        "search_files",
        "read_file",
        "list_files",
        "view_image",
      ].includes(part.toolAction ?? "") ||
      part.toolCategory === "file_search" ||
      part.toolCategory === "file_read"
    )
      return "searching";
    if (part.toolCategory === "mcp" || part.toolCategory === "network")
      return "connecting";
    return "working";
  }
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    const part = parts[index];
    if (part?.type === "text" && part.text?.trim()) return "responding";
    if (part?.type === "reasoning" && part.text?.trim()) return "thinking";
    if (part?.type === "tool" || part?.type === "process_commentary")
      return "thinking";
  }
  return message.status === "pending" && message.preparingSession
    ? "connecting"
    : "thinking";
}
