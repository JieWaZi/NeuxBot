import type { GithubComJieWaZiNeuxBotServerConstantsChannelTaskStatus as ChannelTaskStatus } from "@/shared/api/generated/types.gen";

const taskStatusPriority: Record<ChannelTaskStatus, number> = {
  in_progress: 6,
  in_review: 5,
  pending: 4,
  failed: 3,
  completed: 2,
  cancelled: 1,
};

/** 从同一成员的多轮任务中挑选当前最值得展示的状态。 */
export function teamMemberTaskStatusOf(
  statuses: Array<ChannelTaskStatus | undefined>,
): ChannelTaskStatus | undefined {
  return statuses.reduce<ChannelTaskStatus | undefined>((selected, status) => {
    if (!status) return selected;
    if (
      !selected ||
      taskStatusPriority[status] > taskStatusPriority[selected]
    ) {
      return status;
    }
    return selected;
  }, undefined);
}
