import { describe, expect, it } from "vitest";
import {
  companionPlayback,
  companionPresentation,
  COMPLETION_DURATION,
} from "./playback";
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
  it("连接使用原生彗星，思考带点，等待和失败不被伪装成空闲", () => {
    expect(companionPresentation("connecting").state).toBe("comet");
    expect(companionPresentation("thinking").motion).toBe("thinking");
    expect(companionPresentation("waiting").state).toBe("wide");
    expect(companionPresentation("failed").motion).toBe("failed");
    expect(companionPresentation("resting")).toEqual(
      companionPresentation("idle"),
    );
  });
  it.each([
    ["thinking", ["idle", "thinking", "orbit"]],
    ["working", ["idle", "thinking", "orbit"]],
    ["searching", ["idle", "egg", "hexagon", "orbit"]],
    ["responding", ["play", "idle"]],
  ] as const)(
    "%s 保留多动作及可见表情，不混入连接彗星",
    (activity, expected) => {
      const states = new Set(
        Array.from(
          { length: 600 },
          (_, i) => companionPlayback(activity, i / 10).state,
        ),
      );
      expect([...states].sort()).toEqual([...expected].sort());
    },
  );
  it("连接持续循环彗星中段，避开恢复头像的收尾", () => {
    for (const time of [0, 0.7, 2.5, 13.3, 60]) {
      const result = companionPlayback("connecting", time);
      expect(result.state).toBe("comet");
      expect(result.local).toBeGreaterThanOrEqual(0.55);
      expect(result.local).toBeLessThan(1.55);
    }
  });
  it("等待继续使用等待提示动作", () => {
    expect(
      [0, 1.9, 4.3, 6.5].map(
        (time) => companionPlayback("waiting", time).state,
      ),
    ).toEqual(["wide", "alert", "notify", "wide"]);
  });
  it("完成只播放一次粒子散开聚合，2.6秒后保持空闲", () => {
    expect(companionPresentation("completed").state).toBe("burst");
    expect(
      [0, 0.7, 1.7, 2.5, 2.6, 10].map(
        (time) => companionPlayback("completed", time).state,
      ),
    ).toEqual(["burst", "burst", "burst", "burst", "idle", "idle"]);
    expect(COMPLETION_DURATION).toBe(2.6);
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
