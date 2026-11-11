import type { CompanionActivity } from "@/shared/companion/activity";
import type { AllyChatMessage } from "./chat-projection";

/** 最新助手跟随真实工作阶段，失败保留警示，完成保留通知。 */
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
  if (message.status === "failed") return "failed";
  if (message.status === "canceled" || interaction) return "idle";
  const parts = message.displayParts ?? [];
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    const part = parts[index];
    if (part?.type !== "tool" || part.toolStatus !== "running") continue;
    return "working";
  }
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    const part = parts[index];
    if (part?.type === "text" && part.text?.trim()) return "responding";
    if (part?.type === "reasoning" && part.text?.trim()) return "thinking";
    if (part?.type === "tool" || part?.type === "process_commentary")
      return "thinking";
  }
  return "thinking";
}
