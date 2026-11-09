import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useWorkingExpression } from "./working-expression";
import type { CompanionActivity } from "./activity";
import type { ExpressionId } from "@neuxbot/bloub/expressions";
beforeEach(() => {
  vi.spyOn(Math, "random").mockReturnValue(0.6);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});
it("空闲从保存的表情开始轮换，暂停和静态缩略图不启动轮换", () => {
  vi.useFakeTimers();
  const hook = renderHook(
    ({ playing }) => useWorkingExpression("idle", playing, "curieux"),
    { initialProps: { playing: false } },
  );
  expect(hook.result.current).toBe("curieux");
  expect(vi.getTimerCount()).toBe(0);
  hook.rerender({ playing: true });
  act(() => vi.advanceTimersByTime(4800));
  expect(hook.result.current).not.toBe("curieux");
  hook.rerender({ playing: false });
  const paused = hook.result.current;
  act(() => vi.advanceTimersByTime(20_000));
  expect(hook.result.current).toBe(paused);
  expect(vi.getTimerCount()).toBe(0);
});
it("修改保存表情或重新进入空闲时立即回到当前保存基准", () => {
  vi.useFakeTimers();
  const hook = renderHook(
    ({
      activity,
      resting,
    }: {
      activity: CompanionActivity;
      resting: ExpressionId;
    }) => useWorkingExpression(activity, true, resting),
    {
      initialProps: {
        activity: "idle" as CompanionActivity,
        resting: "curieux" as ExpressionId,
      },
    },
  );
  act(() => vi.advanceTimersByTime(3700));
  expect(hook.result.current).toBe("reveur");
  hook.rerender({ activity: "idle", resting: "timide" });
  expect(hook.result.current).toBe("timide");
  act(() => vi.advanceTimersByTime(3700));
  expect(hook.result.current).not.toBe("timide");
  hook.rerender({ activity: "working", resting: "timide" });
  hook.rerender({ activity: "idle", resting: "timide" });
  expect(hook.result.current).toBe("timide");
});
it("工作期间持续变化表情，暂停不继续，等待不受工作表情覆盖", () => {
  vi.useFakeTimers();
  const hook = renderHook(
    ({ activity, playing }) => useWorkingExpression(activity, playing),
    {
      initialProps: {
        activity: "working" as "working" | "waiting",
        playing: true,
      },
    },
  );
  const first = hook.result.current;
  act(() => vi.advanceTimersByTime(2300));
  expect(hook.result.current).not.toBe(first);
  hook.rerender({ activity: "working", playing: false });
  const paused = hook.result.current;
  act(() => vi.advanceTimersByTime(10_000));
  expect(hook.result.current).toBe(paused);
  hook.rerender({ activity: "waiting", playing: true });
  expect(hook.result.current).toBeUndefined();
});

it.each([
  ["thinking", "reveur"],
  ["working", "determine"],
  ["responding", "complice"],
] as const)("%s 轮换包含扩充表情，且相邻轮换不重复", (activity, added) => {
  vi.useFakeTimers();
  const hook = renderHook(() => useWorkingExpression(activity, true));
  const reached = new Set([hook.result.current]);
  for (let index = 0; index < 4; index++) {
    const previous = hook.result.current;
    act(() => vi.advanceTimersByTime(2200));
    expect(hook.result.current).not.toBe(previous);
    reached.add(hook.result.current);
  }
  expect(reached.has(added)).toBe(true);
  hook.unmount();
  expect(vi.getTimerCount()).toBe(0);
});

it.each(["waiting", "failed", "stopped"] as const)(
  "进入 %s 立即交还真实状态表情并清理轮换",
  (activity) => {
    vi.useFakeTimers();
    const hook = renderHook(
      ({ value }: { value: CompanionActivity }) =>
        useWorkingExpression(value, true),
      { initialProps: { value: "working" as CompanionActivity } },
    );
    act(() => vi.advanceTimersByTime(2200));
    hook.rerender({ value: activity });
    expect(hook.result.current).toBeUndefined();
    expect(vi.getTimerCount()).toBe(0);
  },
);
