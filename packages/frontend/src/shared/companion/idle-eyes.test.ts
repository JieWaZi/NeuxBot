import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";
import { createCompanion } from "../../../../../vendor/companion/renderer";
import { companionAppearance } from "./appearance";
import source from "../../../../../vendor/companion/source/idle-eyes.js?raw";

type EyePose = Record<string, number> & {
  x: number;
  y: number;
  leftWide: number;
  rightWide: number;
  leftTall: number;
  rightTall: number;
  leftWink: number;
  rightWink: number;
};
type EyeController = {
  sample: (
    dt: number,
    enabled: boolean,
    attentive?: boolean,
    still?: boolean,
  ) => EyePose;
  play: (gesture: string) => boolean;
  cancel: () => void;
};
function createEyes() {
  const api: { createIdleEyes?: (random: () => number) => EyeController } = {};
  runInNewContext(source, { window: { NeuxBot: api } });
  if (!api.createIdleEyes) throw new Error("眼神控制器未注册");
  let seed = 27;
  return api.createIdleEyes(() => {
    seed = (seed * 1664525 + 1013904223) >>> 0;
    return seed / 4294967296;
  });
}
const neutral = (pose: EyePose) =>
  Object.values(pose).every((value) => Math.abs(value) < 0.001);

describe("八向张望与双眼独立形变", () => {
  it("睁眼、大小眼和轻眯眼保留双眼白色高光，闭眼时隐藏", () => {
    const clock = vi.spyOn(performance, "now").mockReturnValue(1000);
    const host = document.createElement("div");
    const engine = createCompanion(host, {
      character: "ally",
      emotion: "02",
      autostart: false,
      lite: false,
      idle: false,
    });
    try {
      engine.ball.setReduced(true);
      for (const gaze of [
        "wide",
        "soft",
        "curious-left",
        "curious-right",
        "up-left",
        "down-right",
      ]) {
        engine.ball.perform(`look:${gaze}`);
        engine.renderStatic();
        const highlights = host.querySelectorAll(".ally-eye ellipse");
        expect(highlights).toHaveLength(2);
        for (const highlight of highlights) {
          expect(
            Number(highlight.getAttribute("opacity")),
            gaze,
          ).toBeGreaterThan(0.8);
          expect(highlight.getAttribute("fill")).toBe("#fff");
        }
      }
      engine.ball.cancelAction();
      for (const [gaze, closedIndex] of [
        ["wink-left", 0],
        ["wink-right", 1],
      ] as const) {
        engine.ball.perform(`look:${gaze}`);
        engine.renderStatic();
        const highlights = host.querySelectorAll(".ally-eye ellipse");
        expect(Number(highlights[closedIndex]?.getAttribute("opacity"))).toBe(
          0,
        );
        expect(
          Number(highlights[1 - closedIndex]?.getAttribute("opacity")),
        ).toBeGreaterThan(0.8);
      }
      engine.ball.cancelAction();
      engine.setEmotion("21");
      engine.renderStatic();
      for (const highlight of host.querySelectorAll(".ally-eye ellipse")) {
        expect(
          Number(highlight.getAttribute("opacity")),
          "生气仍睁眼",
        ).toBeGreaterThan(0);
      }
      engine.setEmotion("00");
      engine.renderStatic();
      for (const highlight of host.querySelectorAll(".ally-eye ellipse")) {
        expect(Number(highlight.getAttribute("opacity"))).toBe(0);
      }
    } finally {
      engine.destroy();
      clock.mockRestore();
    }
  });
  it("空闲一轮覆盖八个方向、左右大小眼和双侧眨眼，数值始终有效", () => {
    const eyes = createEyes();
    const seen = new Set<string>();
    for (let frame = 0; frame < 120 * 60; frame++) {
      const pose = eyes.sample(1 / 60, true);
      expect(Object.values(pose).every(Number.isFinite)).toBe(true);
      if (Math.abs(pose.x) > 4 || Math.abs(pose.y) > 4) {
        seen.add(
          `${Math.abs(pose.x) > 4 ? Math.sign(pose.x) : 0},${Math.abs(pose.y) > 4 ? Math.sign(pose.y) : 0}`,
        );
      }
      if (pose.leftTall - pose.rightTall > 0.4) seen.add("left-large");
      if (pose.rightTall - pose.leftTall > 0.4) seen.add("right-large");
      if (pose.leftWink > 0.8) seen.add("left-wink");
      if (pose.rightWink > 0.8) seen.add("right-wink");
    }
    expect([...seen]).toEqual(
      expect.arrayContaining([
        "-1,0",
        "1,0",
        "0,-1",
        "0,1",
        "-1,-1",
        "1,-1",
        "-1,1",
        "1,1",
        "left-large",
        "right-large",
        "left-wink",
        "right-wink",
      ]),
    );
  });
  it("点按方向可打断旧眼神，工作禁用后清空，减少动态保留静态眼神", () => {
    const eyes = createEyes();
    expect(eyes.play("up-left")).toBe(true);
    for (let frame = 0; frame < 50; frame++) eyes.sample(1 / 60, true);
    const pose = eyes.sample(1 / 60, true);
    expect(pose.x).toBeLessThan(-5);
    expect(pose.y).toBeLessThan(-4);
    expect(neutral(eyes.sample(1 / 60, false))).toBe(true);
    expect(eyes.play("curious-right")).toBe(true);
    const still = eyes.sample(0, false, false, true);
    expect(still.rightTall).toBeGreaterThan(still.leftTall);
    eyes.cancel();
    expect(neutral(eyes.sample(0, false, false, true))).toBe(true);
    expect(eyes.play("missing")).toBe(false);
  });
  it("注意用户时平滑回正，停止注视后恢复自动张望", () => {
    const eyes = createEyes();
    for (let frame = 0; frame < 240; frame++) eyes.sample(1 / 60, true);
    for (let frame = 0; frame < 120; frame++) eyes.sample(1 / 60, true, true);
    expect(neutral(eyes.sample(1 / 60, true, true))).toBe(true);
    for (let frame = 0; frame < 30; frame++)
      expect(neutral(eyes.sample(1 / 60, true))).toBe(true);
    let resumed = false;
    for (let frame = 0; frame < 300; frame++)
      if (!neutral(eyes.sample(1 / 60, true))) resumed = true;
    expect(resumed).toBe(true);
  });
  it("真实 SVG 接收独立眼形缩放与八向位移，取消后不遗留静态眼神", () => {
    const clock = vi.spyOn(performance, "now").mockReturnValue(1000);
    const host = document.createElement("div");
    const engine = createCompanion(host, {
      character: "ally",
      emotion: "02",
      style: companionAppearance(
        "companion:v1:0123456789abcdef0123456789abcdef",
      ).style,
      autostart: false,
      lite: false,
      idle: false,
    });
    try {
      engine.ball.setReduced(true);
      const transforms = () =>
        Array.from(host.querySelectorAll(".ally-eye"), (eye) =>
          eye.getAttribute("transform"),
        );
      engine.renderStatic();
      const original = transforms();
      engine.ball.perform("look:curious-left");
      engine.renderStatic();
      const leftLarge = transforms();
      expect(leftLarge).not.toEqual(original);
      engine.ball.perform("look:curious-right");
      engine.renderStatic();
      expect(transforms()).not.toEqual(leftLarge);
      engine.ball.perform("look:down-right");
      engine.renderStatic();
      const origin = original[0]?.match(/^translate\(([^ ]+) ([^)]+)\)/);
      const moved = transforms()[0]?.match(/^translate\(([^ ]+) ([^)]+)\)/);
      expect(Number(moved?.[1]) - Number(origin?.[1])).toBeCloseTo(7);
      expect(Number(moved?.[2]) - Number(origin?.[2])).toBeCloseTo(5);
      engine.ball.cancelAction();
      engine.renderStatic();
      expect(transforms()).toEqual(original);
      expect(host.innerHTML).not.toContain("NaN");
    } finally {
      engine.destroy();
      clock.mockRestore();
    }
  });
});
