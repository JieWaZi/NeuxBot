import { describe, expect, it } from "vitest";

import { obsoleteTeamMemberTabIds } from "./team-member-tabs";

describe("obsoleteTeamMemberTabIds", () => {
  it("只清理被删除或 Chat 身份已变化的成员 Tab", () => {
    expect(
      obsoleteTeamMemberTabIds(
        [
          { id: "main", kind: "chat", label: "负责人" },
          {
            chatId: "chat-keep",
            id: "member-keep",
            kind: "member",
            label: "保留成员",
            wakerId: "waker-keep",
          },
          {
            chatId: "chat-removed",
            id: "member-removed",
            kind: "member",
            label: "删除成员",
            wakerId: "waker-removed",
          },
          {
            chatId: "chat-old",
            id: "member-recreated",
            kind: "member",
            label: "重建成员",
            wakerId: "waker-recreated",
          },
        ],
        [
          { chatId: "chat-keep", id: "waker-keep" },
          { chatId: "chat-new", id: "waker-recreated" },
        ],
      ),
    ).toEqual(["member-removed", "member-recreated"]);
  });
});
