import { StrictMode, memo, useEffect, useState, type ReactNode } from "react";
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { i18n } from "@/shared/i18n";

import { useSceneWorkspace, WorkspaceProvider } from "./workspace-context";

it("更新一个成员工作区只通知该范围，不唤醒其他 39 个工作区", () => {
  const renders = vi.fn();
  const Probe = memo(function Probe({ scope }: { scope: string }) {
    const workspace = useSceneWorkspace(scope);
    renders(scope);
    return (
      <button
        onClick={() =>
          workspace.dispatch({
            type: "open-member",
            member: { wakerId: "member", label: "成员" },
          })
        }
      >
        {scope}
      </button>
    );
  });
  render(
    <WorkspaceProvider>
      {Array.from({ length: 40 }, (_, index) => (
        <Probe key={index} scope={`team:${index}`} />
      ))}
    </WorkspaceProvider>,
  );
  renders.mockClear();
  fireEvent.click(screen.getByRole("button", { name: "team:0" }));
  expect(renders.mock.calls.map(([scope]) => scope)).toEqual(["team:0"]);
  renders.mockClear();
  fireEvent.click(screen.getByRole("button", { name: "team:0" }));
  expect(renders).not.toHaveBeenCalled();
});

it("StrictMode 下切换范围后只订阅新范围，回到原范围保留成员选择", () => {
  const renders = vi.fn();
  const { result, rerender } = renderHook(
    ({ scope }) => {
      renders(scope);
      return useSceneWorkspace(scope);
    },
    {
      initialProps: { scope: "first" },
      wrapper: ({ children }: { children: ReactNode }) => (
        <StrictMode>
          <WorkspaceProvider>{children}</WorkspaceProvider>
        </StrictMode>
      ),
    },
  );
  const first = result.current;
  act(() =>
    first.dispatch({
      type: "open-member",
      member: { wakerId: "member", label: "成员" },
    }),
  );
  rerender({ scope: "second" });
  expect(result.current.snapshot.activeTabId).toBe("chat:second");
  renders.mockClear();
  act(() =>
    first.dispatch({
      type: "open-member",
      member: { wakerId: "second-member", label: "另一成员" },
    }),
  );
  expect(renders).not.toHaveBeenCalled();
  rerender({ scope: "first" });
  expect(result.current.snapshot.activeTabId).toBe(
    "member:first:second-member",
  );
  expect(result.current.snapshot.tabs.map((tab) => tab.label)).toEqual([
    "对话",
    "成员",
    "另一成员",
  ]);
  act(() => result.current.dispatch({ type: "clear" }));
  expect(result.current.snapshot.tabs).toHaveLength(1);
  expect(result.current.snapshot.activeTabId).toBe("chat:first");
});

it("父组件重渲染不改变同一工作区控制器身份或重启依赖它的 Effect", () => {
  const effect = vi.fn();
  function Probe() {
    const [, repaint] = useState(0);
    const workspace = useSceneWorkspace("one");
    useEffect(() => {
      effect();
    }, [workspace]);
    return (
      <button onClick={() => repaint((value) => value + 1)}>局部更新</button>
    );
  }
  render(
    <WorkspaceProvider>
      <Probe />
    </WorkspaceProvider>,
  );
  effect.mockClear();
  fireEvent.click(screen.getByRole("button", { name: "局部更新" }));
  expect(effect).not.toHaveBeenCalled();
});

it("稳定快照仍响应语言切换，并保留已经选择的成员身份", async () => {
  const { result } = renderHook(() => useSceneWorkspace("localized"), {
    wrapper: WorkspaceProvider,
  });
  const language = i18n.language;
  try {
    act(() =>
      result.current.dispatch({
        type: "open-member",
        member: { wakerId: "member", label: "作者命名" },
      }),
    );
    const active = result.current.snapshot.activeTabId;
    await act(async () => {
      await i18n.changeLanguage("en-US");
    });
    expect(result.current.snapshot.tabs[0]?.label).toBe(
      i18n.t("chat:conversationTab"),
    );
    expect(result.current.snapshot.tabs[1]?.label).toBe("作者命名");
    expect(result.current.snapshot.activeTabId).toBe(active);
  } finally {
    await act(async () => {
      await i18n.changeLanguage(language);
    });
  }
});
