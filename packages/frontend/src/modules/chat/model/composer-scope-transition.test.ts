import { describe, expect, it } from "vitest";

import { isDraftToChatScopeTransition } from "./composer-scope-transition";

describe("isDraftToChatScopeTransition", () => {
  it("识别伙伴草稿首条 create 后的工作区切换", () => {
    expect(isDraftToChatScopeTransition("draft:draft-1", "chat:chat-1")).toBe(
      true,
    );
  });

  it("识别团队草稿 thread 创建后的工作区切换", () => {
    expect(
      isDraftToChatScopeTransition(
        "team:channel-1:draft:draft-1",
        "team:channel-1:thread-1",
      ),
    ).toBe(true);
  });

  it("不把已有 chat 之间的切换当成 create 过渡", () => {
    expect(isDraftToChatScopeTransition("chat:chat-1", "chat:chat-2")).toBe(
      false,
    );
  });

  it("不把草稿切到另一个草稿当成 create 过渡", () => {
    expect(isDraftToChatScopeTransition("draft:draft-1", "draft:draft-2")).toBe(
      false,
    );
  });
});
