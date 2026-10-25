import { describe, expect, it } from "vitest";

import { teamMemberTaskStatusOf } from "./team-member-task-status";

describe("teamMemberTaskStatusOf", () => {
  it("成员存在多轮任务时优先展示当前活跃状态", () => {
    expect(
      teamMemberTaskStatusOf(["completed", "failed", "pending", "in_progress"]),
    ).toBe("in_progress");
    expect(teamMemberTaskStatusOf(["completed", "in_review"])).toBe(
      "in_review",
    );
  });
});
