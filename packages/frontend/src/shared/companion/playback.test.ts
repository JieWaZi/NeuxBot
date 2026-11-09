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
  it("连接使用环绕，思考带点，等待和失败不被伪装成空闲", () => {
    expect(companionPresentation("connecting").state).toBe("swirl");
    expect(companionPresentation("thinking").motion).toBe("thinking");
    expect(companionPresentation("waiting").state).toBe("wide");
    expect(companionPresentation("failed").motion).toBe("failed");
    expect(companionPresentation("resting")).toEqual(
      companionPresentation("idle"),
    );
  });
  it("持续工作在表情段间依次播放原生思考、轨道和彗星，等待和完成拥有完整动作顺序", () => {
    expect(
      [0, 2.5, 5.1, 7.5, 10.9, 13.3, 15.7].map(
        (time) => companionPlayback("thinking", time).state,
      ),
    ).toEqual(["idle", "thinking", "idle", "orbit", "idle", "comet", "idle"]);
    expect(
      [0, 1.9, 4.3, 6.5].map(
        (time) => companionPlayback("waiting", time).state,
      ),
    ).toEqual(["wide", "alert", "notify", "wide"]);
    expect(
      [0, 2.1, 4.3, 6.9].map(
        (time) => companionPlayback("completed", time).state,
      ),
    ).toEqual(["exclaim", "notify", "burst", "idle"]);
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
