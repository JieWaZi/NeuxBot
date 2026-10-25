import { describe, expect, it } from "vitest";

import {
  teamChatPresentation,
  teamMainChatMessageVisible,
} from "./team-chat-presentation";

describe("teamChatPresentation", () => {
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
