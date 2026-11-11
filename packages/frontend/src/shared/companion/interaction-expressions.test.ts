import { describe, expect, it } from "vitest";
import { nextHoverFace, type HoverFace } from "./interaction-expressions";

describe("悬停表情", () => {
  it("排除保存的眼睛与前一次悬停反馈", () => {
    let previous: HoverFace | null = null;
    for (const seed of [0, 0.2, 0.5, 0.8, 1, 1, 0]) {
      const face = nextHoverFace("curieux", previous, seed);
      expect(face.expression).not.toBe("curieux");
      expect(face).not.toBe(previous);
      previous = face;
    }
  });
  it("可选眨眼和不同友好表情，边界随机数仍有反馈", () => {
    expect(nextHoverFace("curieux", null, 0)).toEqual({ state: "wink" });
    expect(nextHoverFace("curieux", null, 1)).toEqual({
      state: "idle",
      expression: "attentif",
    });
    for (const seed of [-1, NaN, Infinity, -Infinity]) {
      expect(nextHoverFace("curieux", null, seed)).toEqual({ state: "wink" });
    }
  });
});
