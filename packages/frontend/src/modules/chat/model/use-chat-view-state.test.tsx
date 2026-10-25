import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useChatViewState } from "./use-chat-view-state";

describe("useChatViewState", () => {
  it("切换对话时立即使用目标对话自己的视图状态", () => {
    const { result, rerender } = renderHook(
      ({ scopeKey }) => useChatViewState(scopeKey, ""),
      { initialProps: { scopeKey: "chat:a" } },
    );

    act(() => {
      result.current.update({
        query: "A 的搜索词",
        runtimeId: "runtime-a",
      });
    });

    rerender({ scopeKey: "chat:b" });
    expect(result.current.state.query).toBe("");
    expect(result.current.state.runtimeId).toBe("");

    act(() => {
      result.current.update({ query: "B 的搜索词" });
    });
    rerender({ scopeKey: "chat:a" });
    expect(result.current.state.query).toBe("A 的搜索词");
    expect(result.current.state.runtimeId).toBe("runtime-a");
  });
});
