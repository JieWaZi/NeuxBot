import type { TaskSchedule } from "./types";

/** 未指定时区的自动任务统一按上海时间解释。 */
export const DEFAULT_SCHEDULE_TIMEZONE = "Asia/Shanghai";

export function withScheduleTimezone(schedule: TaskSchedule): TaskSchedule {
  return {
    ...schedule,
    timezone: schedule.timezone?.trim() || DEFAULT_SCHEDULE_TIMEZONE,
  };
}
