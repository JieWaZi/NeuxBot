import type {
  AllyChatMessage,
  ChatRunMetrics,
} from "../runtime/chat-projection";

export type ChatContinuation = {
  continued: boolean;
  continuation: boolean;
  footerMessage: AllyChatMessage;
  /** 分组首头像跟随末段状态，续接段不再重复挂载头像。 */
  activityMessage?: AllyChatMessage;
};

const cumulativeMetrics = [
  "durationMs",
  "inputTokens",
  "outputTokens",
  "reasoningOutputTokens",
  "cacheReadTokens",
  "cacheCreationTokens",
] as const;

/** 只连接相邻且身份匹配的 steer；真实执行状态、工具和文件成果仍归原消息。 */
export function chatContinuations(
  messages: readonly AllyChatMessage[],
): ReadonlyMap<string, ChatContinuation> {
  const result = new Map<string, ChatContinuation>();
  let previous: AllyChatMessage | undefined;
  let userCount = 0;
  let leaderID: string | undefined;
  for (const message of messages) {
    if (message.role === "user") {
      userCount += 1;
      continue;
    }
    if (message.role !== "assistant") {
      previous = undefined;
      continue;
    }
    const id = message.id;
    if (id && message.steerPending && message.status === "canceled") {
      result.set(id, {
        continued: true,
        continuation: false,
        footerMessage: message,
      });
    }
    if (
      id &&
      previous?.id &&
      previous.executionId &&
      previous.status === "canceled" &&
      userCount === 1 &&
      message.steerExecutionId === previous.executionId
    ) {
      const prior = result.get(previous.id);
      const source = prior?.footerMessage ?? previous;
      result.set(previous.id, {
        ...prior,
        continued: true,
        continuation: prior?.continuation ?? false,
        footerMessage: source,
      });
      const runMetrics: ChatRunMetrics = {
        ...message.runMetrics,
        startedAt:
          source.runMetrics?.startedAt ?? message.runMetrics?.startedAt,
      };
      for (const key of cumulativeMetrics) {
        const before = source.runMetrics?.[key];
        const after = message.runMetrics?.[key];
        if (before !== undefined || after !== undefined)
          runMetrics[key] = (before ?? 0) + (after ?? 0);
      }
      result.set(id, {
        continued: result.get(id)?.continued ?? false,
        continuation: true,
        footerMessage: {
          ...message,
          runMetrics,
          displayParts: [
            ...(source.displayParts ?? []),
            ...(message.displayParts ?? []),
          ],
        },
      });
      const leaderKey = leaderID ?? previous.id;
      const leader = result.get(leaderKey);
      if (leader)
        result.set(leaderKey, { ...leader, activityMessage: message });
    } else {
      leaderID = id;
    }
    previous = message;
    userCount = 0;
  }
  return result;
}
