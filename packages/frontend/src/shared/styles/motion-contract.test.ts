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
    for (const category of [
      "presence",
      "popup",
      "side-popup",
      "view",
      "overlay",
      "none",
    ]) {
      expect(styles).toContain(`[data-motion="${category}"]`);
    }
  });

  it("遵循 reduced-motion 且不从零缩放", () => {
    expect(styles).toContain("@media (prefers-reduced-motion: reduce)");
    expect(styles).not.toMatch(/scale(?:3d)?\(\s*0(?:\s*[,)]|\.0+\s*[,)]?)/);
  });

  it("等待文字的流光裁剪到字形，系统需要静态高对比文字时退出", () => {
    const sheet = new CSSStyleSheet();
    sheet.replaceSync(styles);
    const support = [...sheet.cssRules].find(
      (rule): rule is CSSSupportsRule =>
        rule instanceof CSSSupportsRule &&
        rule.conditionText.includes("background-clip: text"),
    );
    const media = [...(support?.cssRules ?? [])].find(
      (rule): rule is CSSMediaRule =>
        rule instanceof CSSMediaRule &&
        rule.conditionText.includes("prefers-reduced-motion: no-preference") &&
        rule.conditionText.includes("prefers-contrast: no-preference") &&
        rule.conditionText.includes("forced-colors: none"),
    );
    const clip = [...(media?.cssRules ?? [])].find(
      (rule): rule is CSSStyleRule =>
        rule instanceof CSSStyleRule &&
        rule.selectorText ===
          '[data-text-shimmer="true"][data-shimmer-visible]',
    );
    expect(clip?.style.getPropertyValue("background-clip")).toBe("text");
  });
});
