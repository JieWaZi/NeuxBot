import { describe, expect, it } from "vitest";
import { companionPlayback, companionPresentation } from "./playback";
import { SEQUENCE } from "@neuxbot/bloub/states";
import type { CompanionActivity } from "./activity";

describe("Bloub 工作状态", () => {
  it.each<CompanionActivity>([
    "resting",
    "idle",
    "starting",
    "thinking",
    "searching",
    "working",
    "connecting",
    "responding",
    "waiting",
    "completed",
    "failed",
    "stopped",
  ])("%s 保留可绘制的身体和有意义的状态", (activity) => {
    const result = companionPresentation(activity);
    expect([...SEQUENCE, "swirl"]).toContain(result.state);
    expect(result).toHaveProperty("motion");
  });
  it.each([
    ["thinking", "thinking"],
    ["working", "thinking"],
    ["searching", "thinking"],
    ["connecting", "thinking"],
    ["responding", "wink"],
    ["completed", "notify"],
    ["failed", "alert"],
  ] as const)(
    "%s 只选择思考、眨眼、警示或通知，持续状态不轮播",
    (activity, state) => {
      for (let time = 0; time < 60; time += 0.1)
        expect(companionPlayback(activity, time).state).toBe(state);
    },
  );
  it("完成通知持续保持，不回空闲或重复弹入", () => {
    expect(companionPlayback("completed", 0)).toEqual({
      state: "notify",
      local: 0,
      block: "steady",
    });
    expect(companionPlayback("completed", 120)).toEqual({
      state: "notify",
      local: 120,
      block: "steady",
    });
  });
  it("14种原生预览独立循环，局部时钟不随循环累计", () => {
    for (const animation of SEQUENCE) {
      const first = companionPlayback("idle", 0.1, animation);
      const later = companionPlayback("idle", 100, animation);
      expect(first.state).toBe(animation === "sleep" ? "idle" : animation);
      expect(later.state).toBe(animation === "sleep" ? "idle" : animation);
      expect(later.block).not.toBe(first.block);
      expect(later.local).toBeGreaterThanOrEqual(0);
      expect(later.local).toBeLessThan(3.5);
    }
  });
});
