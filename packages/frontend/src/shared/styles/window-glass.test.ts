import { describe, expect, it } from "vitest";
import glassStyles from "./window-glass.css?raw";
import { resolveColor, themeTokens } from "@/test/theme-colors";

function declarations(rule: string): Record<string, string> {
  return Object.fromEntries(
    [...rule.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((match) => [
      match[1],
      match[2]?.replace(/var\(\s*(--[\w-]+)\s*\)/g, "var($1)").trim() ?? "",
    ]),
  );
}

describe("透明窗口表面", () => {
  it.each(["light", "dark"] as const)(
    "%s 主题只转换工作台，正文与控件透出材质而浮层保持实色",
    (theme) => {
      const rules = [...glassStyles.matchAll(/([^{}]+)\{([^{}]*)\}/g)];
      const rootRule = rules.find((match) =>
        match[1]?.trim().endsWith(':root[data-window-transparent="true"]'),
      );
      const windowRule = rules.find(
        (match) =>
          match[1]?.trim() ===
          ':root[data-window-transparent="true"] .app-window-shell',
      );
      const floatingRule = rules.find((match) =>
        match[1]?.includes('[role="dialog"]'),
      );
      expect(rootRule).toBeDefined();
      expect(windowRule).toBeDefined();
      expect(floatingRule?.[1]).toContain('[role="listbox"]');
      expect(floatingRule?.[1]).toContain("[data-workflow-node]");
      const base = themeTokens(theme);
      const root = { ...base, ...declarations(rootRule?.[2] ?? "") };
      // CSS 自定义属性在定义元素上解析后再继承；先解析根快照，避免把子级覆盖误算成自引用。
      const snapshots: Record<string, string> = {};
      for (const key of Object.keys(root).filter((key) =>
        key.startsWith("--window-"),
      )) {
        const color = resolveColor(key, root);
        snapshots[key] =
          `rgba(${Math.round(color.r)}, ${Math.round(color.g)}, ${Math.round(color.b)}, ${color.a})`;
      }
      const window = {
        ...base,
        ...snapshots,
        ...declarations(windowRule?.[2] ?? ""),
      };
      const floating = { ...window, ...declarations(floatingRule?.[2] ?? "") };
      for (const [token, expectedAlpha] of [
        ["--shell-surface-frame", 0.14],
        ["--shell-surface-canvas", 0.76],
        ["--surface-panel", 0.92],
        ["--card-surface", 0.92],
        ["--resource-surface", 0.92],
        ["--field-background", 0.9],
      ] as const) {
        expect(resolveColor(token, base).a, token).toBe(1);
        const alpha = resolveColor(token, window).a;
        expect(alpha, token).toBeCloseTo(expectedAlpha);
        expect(resolveColor(token, floating).a, token).toBe(1);
      }
      expect(resolveColor("--surface-scene", window).a).toBe(0);
      expect(resolveColor("--canvas", window).a).toBe(0);
      expect(resolveColor("--resource-canvas", window).a).toBe(0);
      const frameAlpha = resolveColor("--shell-surface-frame", window).a;
      const sidebarAlpha = resolveColor("--window-glass-sidebar", window).a;
      const contentAlpha = resolveColor("--window-glass-card", window).a;
      expect(1 - (1 - frameAlpha) * (1 - sidebarAlpha)).toBeCloseTo(0.312);
      expect(1 - (1 - frameAlpha) * (1 - contentAlpha)).toBeCloseTo(0.7936);
      expect(resolveColor("--content-primary", window)).toEqual(
        resolveColor("--content-primary", base),
      );
    },
  );
});
