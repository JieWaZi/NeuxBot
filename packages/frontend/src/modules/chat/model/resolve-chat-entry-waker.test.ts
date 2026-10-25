import { describe, expect, it } from "vitest";

import { resolveChatEntryWakerId } from "./resolve-chat-entry-waker";

describe("resolveChatEntryWakerId", () => {
  it("prefers the requested waker, then the last active waker, then the first listed waker", () => {
    const wakers = [{ id: "waker-a" }, { id: "waker-b" }];

    expect(
      resolveChatEntryWakerId({
        requestedWakerId: " waker-b ",
        lastWakerId: "waker-a",
        wakers,
      }),
    ).toBe("waker-b");

    expect(
      resolveChatEntryWakerId({
        lastWakerId: "waker-a",
        wakers,
      }),
    ).toBe("waker-a");

    expect(resolveChatEntryWakerId({ wakers })).toBe("waker-a");
    expect(resolveChatEntryWakerId({ wakers: [] })).toBe("");
  });

  it("ignores pointers to a waker that no longer exists", () => {
    const wakers = [{ id: "waker-a" }, { id: "waker-b" }];

    expect(
      resolveChatEntryWakerId({
        requestedWakerId: "deleted-waker",
        lastWakerId: "also-deleted-waker",
        wakers,
      }),
    ).toBe("waker-a");
  });
});
