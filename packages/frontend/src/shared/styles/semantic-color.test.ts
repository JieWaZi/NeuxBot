import { describe, expect, it } from "vitest";

import indexStyles from "./index.css?raw";
import themeStyles from "./theme.css?raw";

import { contrast, themeTokens } from "@/test/theme-colors";

describe("语义主题", () => {
  it.each(["light", "dark"] as const)(
    "%s 主题的十种分类底色都能辨认白色图标",
    (theme) => {
      const tokens = themeTokens(theme);
      const categories = [
        "code",
        "design",
        "data",
        "writing",
        "research",
        "web",
        "automation",
        "business",
        "communication",
        "tools",
      ];
      for (const category of categories) {
        expect(
          contrast(
            "--resource-category-content",
            `--resource-category-${category}`,
            tokens,
          ),
        ).toBeGreaterThanOrEqual(3);
      }
    },
  );

  it("使用 NeuxBot 主题绿统一亮暗主题与控件交互强调色", () => {
    expect(themeStyles).toContain(
      "--ref-ally-green-light: oklch(84.92% 0.127 150);",
    );
    expect(themeStyles).toContain(
      "--ref-ally-green-dark: oklch(72% 0.13 150);",
    );
    expect(themeStyles).toMatch(
      /\[data-theme="light"\][\s\S]*--accent-default: var\(--ref-ally-green-light\)/,
    );
    expect(themeStyles).toMatch(
      /\[data-theme="dark"\][\s\S]*--accent-default: var\(--ref-ally-green-dark\)/,
    );
    expect(
      themeStyles.match(
        /^\s+--control-highlight-background: var\(--accent-default\);/gm,
      ),
    ).toHaveLength(2);
    expect(indexStyles).toContain(
      "--color-control-switch-track-checked: var(--control-switch-track-checked);",
    );
  });

  it("同时定义 Scene、内容、边框、状态和组件语义接口", () => {
    for (const token of [
      "--surface-scene",
      "--content-primary",
      "--border-default",
      "--status-warning-emphasis",
      "--selection-surface",
      "--field-border-focus",
    ]) {
      expect(
        themeStyles.match(new RegExp(`^\\s+${token}:`, "gm")),
      ).toHaveLength(2);
    }

    expect(indexStyles).toContain(
      "--color-surface-scene: var(--surface-scene);",
    );
    expect(indexStyles).toContain("--color-warning: var(--warning);");
    expect(themeStyles).toContain("--warning: var(--status-warning-content);");
  });

  it("折叠导航时为场景顶栏控制保留鼠标命中区域", () => {
    expect(indexStyles).toMatch(
      /\[data-navigation-collapsed\]\s+\[data-scene-tab-strip\]\s*\{[^}]*-webkit-app-region:\s*no-drag;/,
    );
  });

  it.each(["light", "dark"] as const)(
    "%s 主题的正文、辅助文案和操作文字达到 WCAG AA 对比度",
    (theme) => {
      const tokens = themeTokens(theme);
      const pairs = [
        ["--content-primary", "--surface-scene"],
        ["--content-secondary", "--surface-scene"],
        ["--content-muted", "--surface-scene"],
        ["--content-caption", "--surface-scene"],
        ["--field-placeholder", "--field-background"],
        ["--link-default", "--surface-scene"],
        ["--destructive", "--surface-scene"],
        ["--success", "--surface-scene"],
        ["--warning", "--surface-scene"],
        ["--action-primary-content", "--action-primary"],
        ["--destructive-foreground", "--destructive"],
        ["--success-foreground", "--success"],
        ["--warning-foreground", "--warning"],
      ] as const;
      for (const [foreground, background] of pairs) {
        expect(
          contrast(foreground, background, tokens),
          `${theme}: ${foreground} / ${background}`,
        ).toBeGreaterThanOrEqual(4.5);
      }
      expect(
        contrast("--focus-ring", "--field-background", tokens),
      ).toBeGreaterThanOrEqual(3);
    },
  );
});
