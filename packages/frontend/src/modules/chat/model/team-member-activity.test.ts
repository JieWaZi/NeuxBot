import { describe, expect, it } from "vitest";

import { teamMemberActivityOf } from "./team-member-activity";

describe("teamMemberActivityOf", () => {
  it("用户待处理交互优先于运行和任务状态", () => {
    expect(
      teamMemberActivityOf({
        activeExecutionId: "execution-1",
        taskStatus: "in_progress",
        waitingApproval: true,
      }),
    ).toBe("waiting_approval");
    expect(
      teamMemberActivityOf({
        activeExecutionId: "execution-1",
        waitingApproval: true,
        waitingElicitation: true,
      }),
    ).toBe("waiting_input");
  });

  it("区分负责人规划、成员执行、排队、验收和失败", () => {
    expect(teamMemberActivityOf({ leadCoordinating: true })).toBe("planning");
    expect(teamMemberActivityOf({ taskStatus: "in_progress" })).toBe("running");
    expect(teamMemberActivityOf({ taskStatus: "pending" })).toBe("queued");
    expect(teamMemberActivityOf({ taskStatus: "in_review" })).toBe("reviewing");
    expect(teamMemberActivityOf({ taskStatus: "failed" })).toBe("failed");
    expect(teamMemberActivityOf({ taskStatus: "completed" })).toBe("idle");
  });
});
