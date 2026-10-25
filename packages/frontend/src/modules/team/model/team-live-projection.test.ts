import { describe, expect, it } from "vitest";

import {
  createTeamLiveProjection,
  reduceTeamLiveProjection,
  teamEventNoticeOf,
} from "./team-live-projection";

const snapshot = {
  eventCursor: "event-1",
  tasks: [{ id: "task-1", status: "pending" as const }],
  thread: { id: "thread-1", title: "会话" },
};

describe("TeamLiveProjection", () => {
  it("以 REST snapshot 为权威视图并按顺序推进事件游标", () => {
    const initial = createTeamLiveProjection(snapshot);
    const projected = reduceTeamLiveProjection(initial, {
      type: "event",
      event: {
        id: "event-2",
        threadId: "thread-1",
        type: "task.started",
        createdAt: 2,
      },
    });

    expect(projected.view).toBe(snapshot);
    expect(projected.cursor).toBe("event-2");
    expect(projected).toMatchObject({ needsSnapshot: true, reason: "event" });
  });

  it("非相邻重复事件保持幂等", () => {
    const first = reduceTeamLiveProjection(createTeamLiveProjection(snapshot), {
      type: "event",
      event: { id: "event-2", threadId: "thread-1", createdAt: 2 },
    });
    const second = reduceTeamLiveProjection(first, {
      type: "event",
      event: { id: "event-3", threadId: "thread-1", createdAt: 3 },
    });

    expect(
      reduceTeamLiveProjection(second, {
        type: "event",
        event: { id: "event-2", threadId: "thread-1", createdAt: 2 },
      }),
    ).toBe(second);
  });

  it("乱序事件标记 gap，新的 snapshot 重新建立权威游标", () => {
    const first = reduceTeamLiveProjection(createTeamLiveProjection(snapshot), {
      type: "event",
      event: { id: "event-3", threadId: "thread-1", createdAt: 3 },
    });
    const gap = reduceTeamLiveProjection(first, {
      type: "event",
      event: { id: "event-2", threadId: "thread-1", createdAt: 2 },
    });
    expect(gap).toMatchObject({
      cursor: "event-3",
      needsSnapshot: true,
      reason: "gap",
    });

    const reconciled = reduceTeamLiveProjection(gap, {
      type: "snapshot",
      snapshot: { ...snapshot, eventCursor: "event-4" },
    });
    expect(reconciled).toMatchObject({
      cursor: "event-4",
      needsSnapshot: false,
      reason: "current",
    });
  });

  it("立即投影停止中和停止完成状态", () => {
    const stopping = reduceTeamLiveProjection(
      createTeamLiveProjection(snapshot),
      {
        type: "event",
        event: {
          id: "event-2",
          threadId: "thread-1",
          type: "thread.work_stopping",
          createdAt: 2,
          payload: { cancelingAt: 42 },
        },
      },
    );
    expect(stopping.view.thread?.cancelingAt).toBe(42);

    const stopped = reduceTeamLiveProjection(stopping, {
      type: "event",
      event: {
        id: "event-3",
        threadId: "thread-1",
        type: "thread.work_stopped",
        createdAt: 3,
      },
    });
    expect(stopped.view.thread?.cancelingAt).toBeUndefined();
  });

  it("负责人 watcher 耗尽时立即清除汇总状态并返回安全提示类型", () => {
    const initial = createTeamLiveProjection({
      ...snapshot,
      leadActiveExecutionId: "lead-execution",
      leadProcessing: true,
    });
    const event = {
      id: "event-2",
      threadId: "thread-1",
      type: "team.leader.processing" as const,
      createdAt: 2,
      payload: {
        active: false,
        reason: "delivery_watcher_exhausted",
        retryExhausted: true,
      },
    };
    const projected = reduceTeamLiveProjection(initial, {
      type: "event",
      event,
    });

    expect(projected.view.leadProcessing).toBe(false);
    expect(projected.view.leadActiveExecutionId).toBeUndefined();
    expect(teamEventNoticeOf(event)).toBe("leader-retry-exhausted");
    expect(
      teamEventNoticeOf({
        id: "event-3",
        threadId: "thread-1",
        type: "task.recovery_failed",
      }),
    ).toBe("task-recovery-failed");
  });
});
