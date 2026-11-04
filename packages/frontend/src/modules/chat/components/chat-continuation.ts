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

type NativeUsage = NonNullable<ChatRunMetrics["tokenUsage"]>;

const tokenKeys = [
  "input",
  "output",
  "cacheRead",
  "cacheWrite",
  "reasoning",
] as const;
const usageTotals = [
  "totalWithCacheRead",
  "totalWithoutCacheRead",
  "knownTotalWithCacheRead",
  "knownTotalWithoutCacheRead",
] as const;

function sum(values: (number | null | undefined)[], complete: boolean) {
  const known = values.filter((value): value is number => value != null);
  if (!known.length || (complete && known.length !== values.length))
    return undefined;
  return known.reduce((total, value) => total + value, 0);
}

function combineNativeUsage(values: readonly NativeUsage[]) {
  const [first] = values;
  if (values.length < 2) return first;
  const tokens: NonNullable<NativeUsage["tokens"]> = {};
  const knownTokens: NonNullable<NativeUsage["knownTokens"]> = {};
  const result: NativeUsage = {
    complete: values.every((value) => value.complete),
    tokens,
    knownTokens,
    invocations: values.length,
    missingUsageInvocations: sum(
      values.map((value) => value.missingUsageInvocations),
      false,
    ),
    incompleteUsageInvocations: sum(
      values.map((value) => value.incompleteUsageInvocations),
      false,
    ),
  };
  for (const key of tokenKeys) {
    tokens[key] = sum(
      values.map((value) => value.tokens?.[key]),
      true,
    );
    knownTokens[key] = sum(
      values.map((value) => value.knownTokens?.[key]),
      false,
    );
  }
  for (const key of usageTotals) {
    result[key] = sum(
      values.map((value) => value[key]),
      key.startsWith("total"),
    );
  }
  return result;
}

/** 只连接相邻且身份匹配的 steer；真实执行状态、工具和文件成果仍归原消息。 */
export function chatContinuations(
  messages: readonly AllyChatMessage[],
): ReadonlyMap<string, ChatContinuation> {
  const result = new Map<string, ChatContinuation>();
  let previous: AllyChatMessage | undefined;
  let userCount = 0;
  let leaderID: string | undefined;
  const native = new Map<string, NativeUsage>();
  let unattributed = false;
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
    const usage = message.runMetrics?.tokenUsage;
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
      if (usage?.invocationId) native.set(usage.invocationId, usage);
      else unattributed = true;
      const tokenUsage = combineNativeUsage([...native.values()]);
      if (tokenUsage) {
        runMetrics.tokenUsage = unattributed
          ? {
              ...tokenUsage,
              complete: false,
              tokens: {},
              totalWithCacheRead: undefined,
              totalWithoutCacheRead: undefined,
            }
          : tokenUsage;
      }
      for (const key of cumulativeMetrics) {
        if (tokenUsage && key !== "durationMs") continue;
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
      native.clear();
      unattributed = !usage?.invocationId;
      if (usage?.invocationId) native.set(usage.invocationId, usage);
    }
    previous = message;
    userCount = 0;
  }
  return result;
}
