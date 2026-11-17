import { describe, expect, it } from "vitest";
import type { BotChatMessage } from "../runtime/chat-projection";

import {
  teamChatPresentation,
  teamMainChatMessageVisible,
  teamDelegationPosition,
  groupTeamDelegations,
} from "./team-chat-presentation";

describe("teamChatPresentation", () => {
  const replies: BotChatMessage[] = [
    { id: "reply-1", role: "assistant", createdAt: 100, updatedAt: 200 },
    { id: "reply-2", role: "assistant", createdAt: 300, updatedAt: 400 },
  ];
  const delegation = (id: string, assigneeWakerId: string, sentAt: number) => ({
    id,
    senderWakerId: "lead",
    assigneeWakerId,
    sentAt,
    createdAt: sentAt + 500,
  });

  it("同一回复的委派合为一行，重复伙伴去重，不同伙伴保留首次出现顺序", () => {
    const groups = groupTeamDelegations(
      [
        delegation("a-1", "a", 120),
        delegation("b", "b", 130),
        delegation("a-2", "a", 140),
        delegation("c", "c", 150),
        delegation("d", "d", 160),
      ],
      "lead",
      replies,
    );
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({
      direction: "outgoing",
      beforeMessageId: "reply-1",
      partnerIds: ["a", "b", "c", "d"],
      createdAt: 120,
      latestAt: 660,
    });
  });

  it("同一个伙伴在不同回复里的委派分别保留，不跨回合去重", () => {
    const groups = groupTeamDelegations(
      [delegation("a-1", "a", 120), delegation("a-2", "a", 320)],
      "lead",
      replies,
    );
    expect(
      groups.map((group) => [group.beforeMessageId, group.partnerIds]),
    ).toEqual([
      ["reply-1", ["a"]],
      ["reply-2", ["a"]],
    ]);
  });

  it("无法定位来源回复的委派不合并，避免把不同回合误聚合", () => {
    expect(
      groupTeamDelegations(
        [delegation("a-1", "a", 10), delegation("a-2", "a", 20)],
        "lead",
        replies,
      ),
    ).toHaveLength(2);
  });

  it("收到委派按同一执行输入合并发起伙伴，忽略无关记录", () => {
    const groups = groupTeamDelegations(
      [
        { ...delegation("a-1", "member", 120), executionId: "run" },
        { ...delegation("a-2", "member", 130), executionId: "run" },
        {
          ...delegation("b", "member", 140),
          senderWakerId: "other",
          executionId: "run",
        },
        delegation("unrelated", "a", 150),
      ],
      "member",
      [
        { id: "input", role: "user", createdAt: 150 },
        { id: "answer", role: "assistant", executionId: "run", createdAt: 160 },
      ],
    );
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({
      direction: "incoming",
      beforeMessageId: "input",
      partnerIds: ["lead", "other"],
    });
  });
  it("收到委派放在对应执行的输入前，即使接纳回执时间晚于回复创建时间", () => {
    const messages: BotChatMessage[] = [
      { id: "old", role: "assistant", executionId: "old-run", createdAt: 100 },
      { id: "input", role: "user", createdAt: 210 },
      {
        id: "reply",
        role: "assistant",
        executionId: "member-run",
        createdAt: 220,
      },
    ];
    expect(
      teamDelegationPosition(
        "incoming",
        { executionId: "member-run", queuedAt: 200, createdAt: 300 },
        messages,
      ),
    ).toEqual({ beforeMessageId: "input", createdAt: 200 });
  });
  it("发送委派放在发起这次任务的回复前，不跟随稍后才开始的成员执行", () => {
    const messages: BotChatMessage[] = [
      { id: "input", role: "user", createdAt: 100 },
      {
        id: "delegating-reply",
        role: "assistant",
        createdAt: 110,
        updatedAt: 190,
      },
      { id: "unrelated", role: "assistant", createdAt: 200, updatedAt: 250 },
    ];
    expect(
      teamDelegationPosition(
        "outgoing",
        { sentAt: 150, queuedAt: 300, createdAt: 350 },
        messages,
      ),
    ).toEqual({ beforeMessageId: "delegating-reply", createdAt: 150 });
  });
  it("延期派发在负责人回复结束后才开始时，仍定位到前一轮回复之前", () => {
    const messages: BotChatMessage[] = [
      { id: "input", role: "user", createdAt: 100 },
      {
        id: "delegating-reply",
        role: "assistant",
        createdAt: 110,
        updatedAt: 190,
      },
    ];
    expect(
      teamDelegationPosition(
        "outgoing",
        { sentAt: 250, queuedAt: 250, createdAt: 300 },
        messages,
      ),
    ).toEqual({ beforeMessageId: "delegating-reply", createdAt: 250 });
  });
  it("团队主会话展示负责人自己的 token/context 用量", () => {
    const result = teamChatPresentation();

    expect(result.showUsageMetrics).toBe(true);
  });

  it("成员 Tab 保留该成员 Chat 用量", () => {
    const result = teamChatPresentation();

    expect(result.showUsageMetrics).toBe(true);
  });

  it("团队主会话隐藏内部协调输入但保留真实用户输入和成员报告", () => {
    expect(teamMainChatMessageVisible({ role: "user", actor: "system" })).toBe(
      false,
    );
    expect(
      teamMainChatMessageVisible({ role: "user", actor: "team_lead" }),
    ).toBe(false);
    expect(teamMainChatMessageVisible({ role: "user", actor: "user" })).toBe(
      true,
    );
    expect(
      teamMainChatMessageVisible({ role: "user", actor: "team_member" }),
    ).toBe(true);
    expect(teamMainChatMessageVisible({ role: "assistant" })).toBe(true);
  });
});
