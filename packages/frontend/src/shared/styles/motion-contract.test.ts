import { describe, expect, it } from "vitest";

import styles from "./motion.css?raw";

describe("共享动效合同", () => {
  it("提供统一的五档时长和标准缓动", () => {
    expect(styles).toContain("--duration-instant: 80ms;");
    expect(styles).toContain("--duration-quick: 140ms;");
    expect(styles).toContain("--duration-standard: 220ms;");
    expect(styles).toContain("--duration-content-swap: 320ms;");
    expect(styles).toContain("--duration-slow: 420ms;");
    expect(styles).toContain("--duration-deliberate: 420ms;");
    expect(styles).toContain("--ease-ui: cubic-bezier(0.23, 1, 0.32, 1);");
  });

  it("覆盖所有共享 data-motion 类别", () => {
    for (const category of ["presence", "popup", "view", "overlay", "none"]) {
      expect(styles).toContain(`[data-motion="${category}"]`);
    }
  });

  it("遵循 reduced-motion 且不从零缩放", () => {
    expect(styles).toContain("@media (prefers-reduced-motion: reduce)");
    expect(styles).not.toMatch(/scale(?:3d)?\(\s*0(?:\s*[,)]|\.0+\s*[,)]?)/);
  });
});
