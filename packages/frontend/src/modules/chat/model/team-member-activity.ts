import type { GithubComJieWaZiNeuxBotServerConstantsChannelTaskStatus as ChannelTaskStatus } from "@/shared/api/generated/types.gen";

export type TeamMemberActivityPhase =
  | "failed"
  | "idle"
  | "planning"
  | "queued"
  | "reviewing"
  | "running"
  | "waiting_approval"
  | "waiting_input";

type TeamMemberActivityInput = {
  activeExecutionId?: string;
  leadCoordinating?: boolean;
  taskStatus?: ChannelTaskStatus;
  waitingApproval?: boolean;
  waitingElicitation?: boolean;
};

/**
 * 把 Chat 交互事实和任务事实收敛为唯一成员展示状态。
 * 需要用户处理的状态优先于运行状态，运行状态优先于任务结果。
 */
export function teamMemberActivityOf({
  activeExecutionId,
  leadCoordinating = false,
  taskStatus,
  waitingApproval = false,
  waitingElicitation = false,
}: TeamMemberActivityInput): TeamMemberActivityPhase {
  if (waitingElicitation) return "waiting_input";
  if (waitingApproval) return "waiting_approval";
  if (leadCoordinating) return "planning";
  if (activeExecutionId || taskStatus === "in_progress") return "running";
  if (taskStatus === "in_review") return "reviewing";
  if (taskStatus === "pending") return "queued";
  if (taskStatus === "failed") return "failed";
  return "idle";
}
