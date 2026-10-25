import { describe, expect, it } from "vitest";

import { resolveChatRecentTarget } from "./resolve-chat-recent-target";

describe("resolveChatRecentTarget", () => {
  it("prefers the server latest chat", () => {
    expect(
      resolveChatRecentTarget({
        wakers: [{ id: "waker-a" }],
        latestChat: {
          id: "chat-new",
          wakerId: "waker-a",
        },
      }),
    ).toEqual({
      kind: "chat",
      chatId: "chat-new",
      wakerId: "waker-a",
    });
  });

  it("prefers the validated last active chat", () => {
    expect(
      resolveChatRecentTarget({
        wakers: [{ id: "waker-a" }],
        lastActive: {
          id: "chat-b",
          wakerId: "waker-a",
        },
        latestChat: {
          id: "chat-a",
          wakerId: "waker-a",
        },
      }),
    ).toEqual({
      kind: "chat",
      chatId: "chat-b",
      wakerId: "waker-a",
    });
  });

  it("falls back to a draft when the list has no recent chat", () => {
    expect(
      resolveChatRecentTarget({
        wakers: [{ id: "waker-a" }],
        lastWakerId: "waker-a",
      }),
    ).toEqual({
      kind: "draft",
      wakerId: "waker-a",
    });
  });

  it("returns draft when the resolved waker has no recent chat", () => {
    expect(
      resolveChatRecentTarget({
        requestedWakerId: "waker-b",
        wakers: [{ id: "waker-a" }, { id: "waker-b" }],
      }),
    ).toEqual({
      kind: "draft",
      wakerId: "waker-b",
    });
  });
});
