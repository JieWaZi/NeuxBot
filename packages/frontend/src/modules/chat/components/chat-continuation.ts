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

function legacyTokens(
  metrics: ChatRunMetrics | undefined,
): NonNullable<NativeUsage["knownTokens"]> {
  return {
    input: metrics?.inputTokens,
    output: metrics?.outputTokens,
    cacheRead: metrics?.cacheReadTokens,
    cacheWrite: metrics?.cacheCreationTokens,
    reasoning: metrics?.reasoningOutputTokens,
  };
}

function combineUsage(
  executions: ReadonlyMap<string, ChatRunMetrics | undefined>,
  unidentified: boolean,
) {
  const native = new Map<string, NativeUsage>();
  const legacy: NonNullable<NativeUsage["knownTokens"]>[] = [];
  for (const metrics of executions.values()) {
    const usage = metrics?.tokenUsage;
    if (usage?.invocationId) native.set(usage.invocationId, usage);
    else legacy.push(legacyTokens(metrics));
  }
  const combined = combineNativeUsage([...native.values()]);
  if (!combined || (!legacy.length && !unidentified)) return combined;
  const knownTokens: NonNullable<NativeUsage["knownTokens"]> = {};
  for (const key of tokenKeys) {
    knownTokens[key] = sum(
      [combined.knownTokens?.[key], ...legacy.map((tokens) => tokens[key])],
      false,
    );
  }
  return {
    ...combined,
    invocationId: undefined,
    coverage: undefined,
    source: undefined,
    complete: false,
    tokens: {},
    knownTokens,
    totalWithCacheRead: undefined,
    totalWithoutCacheRead: undefined,
    knownTotalWithCacheRead: sum(
      [
        knownTokens.input,
        knownTokens.cacheRead,
        knownTokens.cacheWrite,
        knownTokens.output,
      ],
      false,
    ),
    knownTotalWithoutCacheRead: sum(
      [knownTokens.input, knownTokens.cacheWrite, knownTokens.output],
      false,
    ),
  };
}

/** 只连接相邻且身份匹配的 steer；真实执行状态、工具和文件成果仍归原消息。 */
export function chatContinuations(
  messages: readonly AllyChatMessage[],
): ReadonlyMap<string, ChatContinuation> {
  const result = new Map<string, ChatContinuation>();
  let previous: AllyChatMessage | undefined;
  let userCount = 0;
  let leaderID: string | undefined;
  const executions = new Map<string, ChatRunMetrics | undefined>();
  let unidentified = false;
  const rememberUsage = (message: AllyChatMessage) => {
    const id = message.executionId;
    if (!id) {
      unidentified = true;
      return;
    }
    // A revised view of the same execution replaces its prior compatibility
    // counters. Once known, the canonical result is the stronger evidence.
    if (
      !executions.get(id)?.tokenUsage?.invocationId ||
      message.runMetrics?.tokenUsage?.invocationId
    ) {
      executions.set(id, message.runMetrics);
    }
  };
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
      rememberUsage(message);
      const tokenUsage = combineUsage(executions, unidentified);
      if (tokenUsage) runMetrics.tokenUsage = tokenUsage;
      const measurements = [...executions.values()];
      if (!message.executionId) measurements.push(message.runMetrics);
      for (const key of cumulativeMetrics) {
        if (key === "durationMs") {
          runMetrics[key] = sum(
            [source.runMetrics?.[key], message.runMetrics?.[key]],
            false,
          );
        } else if (!tokenUsage) {
          runMetrics[key] = sum(
            measurements.map((metrics) => metrics?.[key]),
            false,
          );
        }
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
      executions.clear();
      unidentified = false;
      rememberUsage(message);
    }
    previous = message;
    userCount = 0;
  }
  return result;
}
