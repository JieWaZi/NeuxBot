import { describe, expect, it } from "vitest";

import styles from "./motion.css?raw";

describe("Dialog 动画", () => {
  it("不重复覆盖 Dialog 的居中位移", () => {
    const dialogKeyframes = [
      extractKeyframes(styles, "dialog-in"),
      extractKeyframes(styles, "dialog-out"),
    ];

    for (const keyframes of dialogKeyframes) {
      expect(keyframes).toContain("scale(");
      expect(keyframes).not.toContain("translate(");
    }
  });
});

function extractKeyframes(stylesheet: string, name: string): string {
  const start = stylesheet.indexOf(`@keyframes ${name}`);
  const nextKeyframes = stylesheet.indexOf("@keyframes", start + 1);

  expect(start).toBeGreaterThanOrEqual(0);
  return stylesheet.slice(
    start,
    nextKeyframes === -1 ? stylesheet.length : nextKeyframes,
  );
}
