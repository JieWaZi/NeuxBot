import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useComposerScopeTransition } from "./use-composer-scope-transition";

describe("useComposerScopeTransition", () => {
  it("主动清空输入后，切换聊天继续按原策略清空输入", () => {
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
    expect(view.result.current.composerClearRevision).toBe(1);

    view.rerender({ chatKey: "chat:two", workspaceKey: "chat:two" });
    expect(view.result.current.composerClearRevision).toBe(2);
  });

  it("草稿首次持久化保留输入内容", () => {
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
    expect(view.result.current.composerClearRevision).toBe(0);
  });
});
