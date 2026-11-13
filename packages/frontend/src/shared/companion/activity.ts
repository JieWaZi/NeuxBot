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

export type CompanionAttention =
  | { kind: "working" | "waiting-answer" | "waiting-approval" }
  | { kind: "unread"; count: number };

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
