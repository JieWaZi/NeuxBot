import { describe, expect, it } from "vitest";

import { projectTeamMembers } from "./team-member-projection";

describe("projectTeamMembers", () => {
  it("execution 在已有会话中只使用冻结成员", () => {
    const members = projectTeamMembers({
      channelMembers: [
        { waker: { id: "lead-1", name: "新负责人" } },
        { waker: { id: "member-2", name: "执行成员" } },
        { waker: { id: "member-3", name: "新成员" } },
      ],
      fallbackName: "成员",
      leadWakerId: "lead-1",
      roster: "execution",
      threadDetail: {
        tasks: [
          {
            assigneeWakerId: "member-2",
            status: "in_progress",
            subject: "当前执行任务",
          },
          {
            assigneeWakerId: "member-2",
            status: "pending",
            subject: "排队任务",
          },
          {
            assigneeWakerId: "lead-1",
            status: "completed",
            subject: "已完成任务",
          },
        ],
        members: [
          {
            chatId: "lead-chat",
            displayName: "原负责人",
            isLead: true,
            position: 0,
            wakerId: "lead-1",
          },
          {
            chatId: "member-chat",
            displayName: "执行成员",
            position: 1,
            wakerId: "member-2",
          },
        ],
      },
    });

    expect(members.map((member) => member.id)).toEqual(["lead-1", "member-2"]);
    expect(members[0]?.name).toBe("原负责人");
    expect(members[0]?.activeTaskSubject).toBeUndefined();
    expect(members[1]?.activeTaskSubject).toBe("当前执行任务");
  });

  it("navigation 在已有会话中展示当前团队成员并保留历史成员", () => {
    const members = projectTeamMembers({
      channelMembers: [
        { waker: { id: "lead-1", name: "新负责人" } },
        { waker: { id: "member-2", name: "执行成员" } },
        { waker: { id: "member-3", name: "新成员" } },
      ],
      fallbackName: "成员",
      leadWakerId: "lead-1",
      roster: "navigation",
      threadDetail: {
        members: [
          {
            chatId: "lead-chat",
            displayName: "原负责人",
            isLead: true,
            position: 0,
            wakerId: "lead-1",
          },
          {
            chatId: "member-chat",
            displayName: "执行成员",
            position: 1,
            wakerId: "member-2",
          },
          {
            chatId: "removed-chat",
            displayName: "已移除成员",
            position: 2,
            wakerId: "removed-1",
          },
        ],
      },
    });

    expect(members.map((member) => member.id)).toEqual([
      "lead-1",
      "member-2",
      "member-3",
      "removed-1",
    ]);
    expect(members[0]?.name).toBe("原负责人");
    expect(members[2]?.chatId).toBeUndefined();
    expect(members[3]?.name).toBe("已移除成员");
  });
});
