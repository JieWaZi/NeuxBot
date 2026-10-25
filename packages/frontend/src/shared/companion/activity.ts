export type CompanionActivity =
  | "resting"
  | "idle"
  | "starting"
  | "thinking"
  | "searching"
  | "working"
  | "connecting"
  | "responding"
  | "waiting"
  | "completed"
  | "failed"
  | "stopped";

export const companionEmotions: Record<CompanionActivity, string> = {
  resting: "00",
  idle: "02",
  starting: "31",
  thinking: "30",
  searching: "40",
  working: "32",
  connecting: "36",
  responding: "39",
  waiting: "35",
  completed: "33",
  failed: "34",
  stopped: "41",
};

/** 列表只拥有伙伴聚合状态；细分工作动作由对话的真实消息决定。 */
export function wakerCompanionActivity(
  state?: "idle" | "running" | "waiting",
): CompanionActivity {
  return state === "running"
    ? "working"
    : state === "waiting"
      ? "waiting"
      : "idle";
}
