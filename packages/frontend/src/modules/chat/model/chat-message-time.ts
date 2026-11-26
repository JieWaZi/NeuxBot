import type { ChatEvent } from "@/shared/api/events";

/** 只有创建事件的记录时刻可以补齐缺失的消息创建时间。 */
export function chatEventCreatedAt(event: ChatEvent, previous?: number) {
  const time = event.message?.createdAt || previous;
  if (time) return time;
  if (
    event.type === "message.user_created" ||
    event.type === "execution.created" ||
    (event.sequence && event.message?.createdSequence === event.sequence)
  )
    return event.recordedAt;
  return undefined;
}
