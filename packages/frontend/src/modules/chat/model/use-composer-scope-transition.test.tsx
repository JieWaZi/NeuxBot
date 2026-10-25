import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useComposerScopeTransition } from "./use-composer-scope-transition";

describe("useComposerScopeTransition", () => {
  it("发送清空输入框不释放形象，切换聊天才释放", () => {
    const onScopeChange = vi.fn();
    const view = renderHook(
      ({ chatKey, workspaceKey }) =>
        useComposerScopeTransition(workspaceKey, chatKey, onScopeChange),
      {
        initialProps: {
          chatKey: "chat:one",
          workspaceKey: "chat:one",
        },
      },
    );

    act(() => view.result.current.clearComposer());
    expect(view.result.current.companionScopeRevision).toBe(0);

    view.rerender({ chatKey: "chat:two", workspaceKey: "chat:two" });
    expect(view.result.current.companionScopeRevision).toBe(1);
  });

  it("草稿首次持久化保持已展开的形象", () => {
    const view = renderHook(
      ({ chatKey, workspaceKey }) =>
        useComposerScopeTransition(workspaceKey, chatKey, vi.fn()),
      {
        initialProps: {
          chatKey: "draft:one",
          workspaceKey: "draft:one",
        },
      },
    );

    act(() => view.result.current.markSkipComposerClearOnCreate());
    view.rerender({ chatKey: "chat:created", workspaceKey: "chat:created" });
    expect(view.result.current.companionScopeRevision).toBe(0);
  });
});
