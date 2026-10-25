import type { GithubComJieWaZiNeuxBotServerConstantsChannelEventType as ChannelEventType } from "@/shared/api/generated/types.gen";

export type TeamWorkState = "idle" | "review" | "running";

type TeamTaskSnapshot = {
  status?: string;
};

export type TeamLeadActivity = {
  activeExecutionId?: string;
  summarizing: boolean;
};

type TeamThreadRefreshCallbacks = {
  refreshDetail: () => void;
  refreshNavigation: () => void;
};

type TeamThreadRefreshScheduler = {
  clear: () => void;
  push: (eventType?: ChannelEventType) => void;
};

const lifecycleRefreshDelayMs = 300;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function stringField(record: Record<string, unknown>, key: string) {
  const value = record[key];
  return typeof value === "string" ? value : undefined;
}

function booleanField(record: Record<string, unknown>, key: string) {
  const value = record[key];
  return typeof value === "boolean" ? value : undefined;
}

export function teamLeadActivityOf(snapshot: unknown): TeamLeadActivity {
  if (!isRecord(snapshot)) return { summarizing: false };
  return {
    activeExecutionId: stringField(snapshot, "leadActiveExecutionId"),
    summarizing: booleanField(snapshot, "leadProcessing") ?? false,
  };
}

export function teamWorkStateOf(
  tasks: readonly TeamTaskSnapshot[],
  leadActivity: TeamLeadActivity,
): TeamWorkState {
  if (
    leadActivity.activeExecutionId ||
    leadActivity.summarizing ||
    tasks.some(
      (task) => task.status === "pending" || task.status === "in_progress",
    )
  ) {
    return "running";
  }
  return tasks.some((task) => task.status === "in_review") ? "review" : "idle";
}

/**
 * 任务与交互事件在 300ms 窗口内合并，负责人处理切换在下一宏任务对账。
 * NeuxBot 当前只有团队对话详情查询，因此精确刷新详情，
 * 仅生命周期变更额外刷新左侧团队对话导航。
 */
export function createTeamThreadRefreshScheduler({
  refreshDetail,
  refreshNavigation,
}: TeamThreadRefreshCallbacks): TeamThreadRefreshScheduler {
  let lifecycleTimer: number | undefined;

  const clearLifecycle = () => {
    if (lifecycleTimer !== undefined) window.clearTimeout(lifecycleTimer);
    lifecycleTimer = undefined;
  };
  return {
    clear: () => {
      clearLifecycle();
    },
    push: (eventType) => {
      clearLifecycle();
      const delay = (() => {
        switch (eventType) {
          case "team.leader.processing":
          case "team.member.direct_delivered":
            return 0;
          case undefined:
          case "":
          case "interaction.approval.requested":
          case "interaction.approval.resolved":
          case "interaction.elicitation.requested":
          case "interaction.elicitation.resolved":
          case "lead.input_queued":
          case "members.config_updated":
          case "task.cancelled":
          case "task.created":
          case "task.dispatch_deferred":
          case "task.finished":
          case "task.follow_up_queued":
          case "task.recovery_failed":
          case "task.retry_queued":
          case "task.review_approved":
          case "task.started":
          case "task.updated":
          case "thread.created":
          case "thread.work_stopped":
          case "thread.work_stopping":
            return lifecycleRefreshDelayMs;
        }
      })();
      lifecycleTimer = window.setTimeout(() => {
        lifecycleTimer = undefined;
        refreshDetail();
        refreshNavigation();
      }, delay);
    },
  };
}
