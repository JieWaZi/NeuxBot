import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createTeamThreadRefreshScheduler,
  teamLeadActivityOf,
  teamWorkStateOf,
} from "./team-thread-refresh";

afterEach(() => {
  vi.useRealTimers();
});

describe("team thread busy snapshot", () => {
  it("负责人处理结果的间隙仍保持团队执行中", () => {
    const activity = teamLeadActivityOf({
      leadActiveExecutionId: "execution-1",
      leadProcessing: true,
    });

    expect(teamWorkStateOf([], activity)).toBe("running");
  });

  it("活跃任务优先于人工验收，全部停止后才进入验收态", () => {
    expect(
      teamWorkStateOf([{ status: "in_review" }, { status: "in_progress" }], {
        summarizing: false,
      }),
    ).toBe("running");
    expect(
      teamWorkStateOf([{ status: "in_review" }], { summarizing: false }),
    ).toBe("review");
  });
});

describe("team thread event refresh scheduler", () => {
  it("按有界时间窗合并高频持久事件", () => {
    vi.useFakeTimers();
    const refreshDetail = vi.fn();
    const refreshNavigation = vi.fn();
    const scheduler = createTeamThreadRefreshScheduler({
      refreshDetail,
      refreshNavigation,
    });

    scheduler.push("task.updated");
    vi.advanceTimersByTime(200);
    scheduler.push("task.updated");
    vi.advanceTimersByTime(299);
    expect(refreshDetail).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);

    expect(refreshDetail).toHaveBeenCalledTimes(1);
    expect(refreshNavigation).toHaveBeenCalledTimes(1);
  });

  it("任务生命周期事件合并刷新详情和导航", () => {
    vi.useFakeTimers();
    const refreshDetail = vi.fn();
    const refreshNavigation = vi.fn();
    const scheduler = createTeamThreadRefreshScheduler({
      refreshDetail,
      refreshNavigation,
    });

    for (let index = 0; index < 50; index += 1) {
      scheduler.push(index % 2 === 0 ? "task.started" : "task.finished");
    }
    vi.advanceTimersByTime(299);
    expect(refreshDetail).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);

    expect(refreshDetail).toHaveBeenCalledTimes(1);
    expect(refreshNavigation).toHaveBeenCalledTimes(1);
  });

  it("负责人处理事件立即拉取持久快照，不把瞬时事件当长期事实", () => {
    vi.useFakeTimers();
    const refreshDetail = vi.fn();
    const scheduler = createTeamThreadRefreshScheduler({
      refreshDetail,
      refreshNavigation: vi.fn(),
    });

    scheduler.push("team.leader.processing");
    expect(refreshDetail).not.toHaveBeenCalled();
    vi.advanceTimersByTime(0);
    expect(refreshDetail).toHaveBeenCalledTimes(1);
  });

  it("成员结果直达后立即刷新消息样式", () => {
    vi.useFakeTimers();
    const refreshDetail = vi.fn();
    const scheduler = createTeamThreadRefreshScheduler({
      refreshDetail,
      refreshNavigation: vi.fn(),
    });

    scheduler.push("team.member.direct_delivered");
    vi.advanceTimersByTime(0);

    expect(refreshDetail).toHaveBeenCalledTimes(1);
  });
});
