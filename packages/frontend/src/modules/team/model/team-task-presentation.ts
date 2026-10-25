import type {
  GithubComJieWaZiNeuxBotServerConstantsChannelTaskStatus as ChannelTaskStatus,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelTask as ChannelTask,
} from "@/shared/api/generated/types.gen";

export type TeamTaskMember = {
  id: string;
  name: string;
};

export const teamTaskGroups: ReadonlyArray<{
  id: ChannelTaskStatus;
  labelKey: string;
}> = [
  { id: "pending", labelKey: "taskBoard.groups.pending" },
  { id: "in_progress", labelKey: "taskBoard.groups.inProgress" },
  { id: "in_review", labelKey: "taskBoard.groups.inReview" },
  { id: "completed", labelKey: "taskBoard.groups.completed" },
  { id: "failed", labelKey: "taskBoard.groups.failed" },
  { id: "cancelled", labelKey: "taskBoard.groups.cancelled" },
];

export type TeamTaskStatus = ChannelTaskStatus;

export function teamTaskStatusOf(task: ChannelTask): ChannelTaskStatus {
  return task.status ?? "pending";
}

const taskTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  day: "2-digit",
  hour: "2-digit",
  hour12: false,
  minute: "2-digit",
  month: "2-digit",
});

export function formatTeamTaskTime(value?: number): string {
  if (!value) return "";
  const parts = new Map(
    taskTimeFormatter
      .formatToParts(new Date(value))
      .map((part) => [part.type, part.value] as const),
  );
  return `${parts.get("month")}-${parts.get("day")} ${parts.get("hour")}:${parts.get("minute")}`;
}

export function groupTeamTasksByStatus(
  tasks: ChannelTask[],
): Map<ChannelTaskStatus, ChannelTask[]> {
  const grouped = new Map<ChannelTaskStatus, ChannelTask[]>(
    teamTaskGroups.map((group) => [group.id, []]),
  );
  for (const task of tasks) grouped.get(teamTaskStatusOf(task))?.push(task);
  return grouped;
}
