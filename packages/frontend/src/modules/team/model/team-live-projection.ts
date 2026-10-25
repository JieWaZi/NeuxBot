import type {
  GithubComJieWaZiNeuxBotServerApisChannelsChannelThreadDetail as ChannelThreadDetail,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelEvent as ChannelEvent,
} from "@/shared/api/generated/types.gen";

const maximumRememberedEvents = 512;

export type TeamLiveProjection = {
  cursor: string;
  lastEventOrder?: readonly [createdAt: number, id: string];
  needsSnapshot: boolean;
  reason: "current" | "event" | "gap" | "reconnect";
  seenEventIds: readonly string[];
  view: ChannelThreadDetail;
};

export type TeamLiveProjectionAction =
  | { snapshot: ChannelThreadDetail; type: "snapshot" }
  | { event: ChannelEvent; type: "event" }
  | { type: "reconnect" };

export type TeamEventNotice = "leader-retry-exhausted" | "task-recovery-failed";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function eventPayloadOf(event: ChannelEvent): Record<string, unknown> {
  if (isRecord(event.payload)) return event.payload;
  if (typeof event.payload !== "string") return {};
  try {
    const parsed = JSON.parse(event.payload) as unknown;
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

/** 将后端失败事件收敛成不泄露底层诊断的稳定用户提示。 */
export function teamEventNoticeOf(
  event: ChannelEvent,
): TeamEventNotice | undefined {
  if (event.type === "task.recovery_failed") return "task-recovery-failed";
  if (
    event.type === "team.leader.processing" &&
    eventPayloadOf(event).retryExhausted === true
  ) {
    return "leader-retry-exhausted";
  }
  return undefined;
}

/** REST snapshot 是权威视图；持久事件只推进游标并请求一次增量对账。 */
export function createTeamLiveProjection(
  snapshot: ChannelThreadDetail,
): TeamLiveProjection {
  const cursor = snapshot.eventCursor ?? "";
  return {
    cursor,
    needsSnapshot: false,
    reason: "current",
    seenEventIds: cursor ? [cursor] : [],
    view: snapshot,
  };
}

function eventIsBefore(
  previous: readonly [number, string] | undefined,
  current: readonly [number, string],
) {
  if (!previous) return false;
  return (
    current[0] < previous[0] ||
    (current[0] === previous[0] && current[1] < previous[1])
  );
}

/** ordered event reducer；重复事件幂等，乱序事件强制重新获取 snapshot。 */
export function reduceTeamLiveProjection(
  current: TeamLiveProjection,
  action: TeamLiveProjectionAction,
): TeamLiveProjection {
  if (action.type === "snapshot") {
    return createTeamLiveProjection(action.snapshot);
  }
  if (action.type === "reconnect") {
    return current.needsSnapshot && current.reason === "reconnect"
      ? current
      : { ...current, needsSnapshot: true, reason: "reconnect" };
  }

  const id = action.event.id;
  if (!id || action.event.threadId !== current.view.thread?.id) return current;
  if (current.seenEventIds.includes(id)) return current;
  const order = [action.event.createdAt ?? 0, id] as const;
  const gap = eventIsBefore(current.lastEventOrder, order);
  const seenEventIds = [...current.seenEventIds, id].slice(
    -maximumRememberedEvents,
  );
  const payload = eventPayloadOf(action.event);
  let view = current.view;
  if (
    action.event.type === "team.leader.processing" &&
    typeof payload.active === "boolean"
  ) {
    view = {
      ...view,
      leadActiveExecutionId:
        payload.active && typeof payload.executionId === "string"
          ? payload.executionId
          : payload.active
            ? view.leadActiveExecutionId
            : undefined,
      leadProcessing: payload.active,
    };
  } else if (
    action.event.type === "thread.work_stopping" &&
    typeof payload.cancelingAt === "number" &&
    view.thread
  ) {
    view = {
      ...view,
      thread: { ...view.thread, cancelingAt: payload.cancelingAt },
    };
  } else if (action.event.type === "thread.work_stopped" && view.thread) {
    view = {
      ...view,
      thread: { ...view.thread, cancelingAt: undefined },
    };
  }
  return {
    ...current,
    cursor: gap ? current.cursor : id,
    lastEventOrder: gap ? current.lastEventOrder : order,
    needsSnapshot: true,
    reason: gap ? "gap" : "event",
    seenEventIds,
    view,
  };
}

/** React reducer 允许在首个 REST snapshot 到达前保持未初始化。 */
export function teamLiveProjectionReducer(
  current: TeamLiveProjection | undefined,
  action: TeamLiveProjectionAction,
): TeamLiveProjection | undefined {
  if (action.type === "snapshot")
    return createTeamLiveProjection(action.snapshot);
  if (!current) return current;
  return reduceTeamLiveProjection(current, action);
}
