import { describe, expect, it } from "vitest";

import { findSelectedTextLineRange } from "./workspace-text-selection";

describe("findSelectedTextLineRange", () => {
  it("把渲染视图选中文字映射回原文行号", () => {
    expect(
      findSelectedTextLineRange(
        "# AGENTS.md\n\n## 仓库运行时入口\n\n关联 DevWiki",
        "仓库运行时入口",
      ),
    ).toEqual({ startLine: 3, endLine: 3 });
  });
});

import { readWorkspaceTextSelection } from "./workspace-text-selection";

describe("readWorkspaceTextSelection", () => {
  function preview() {
    const container = document.createElement("div");
    for (const [index, text] of ["same\n", "same\n", "last"].entries()) {
      const row = document.createElement("div");
      row.dataset.line = String(index + 1);
      const gutter = document.createElement("span");
      gutter.textContent = String(index + 1);
      const code = document.createElement("span");
      code.dataset.codeLine = "";
      code.textContent = text;
      row.append(gutter, code);
      container.append(row);
    }
    return container;
  }

  it("uses DOM line positions for repeated text and excludes gutters", () => {
    const container = preview();
    const range = document.createRange();
    const row = container.children[1];
    if (!row) throw new Error("Missing second row");
    range.selectNodeContents(row);
    expect(
      readWorkspaceTextSelection(container, range, "same\nsame\nlast"),
    ).toEqual({ content: "same\n", startLine: 2, endLine: 2 });
    range.selectNodeContents(container);
    expect(
      readWorkspaceTextSelection(container, range, "same\nsame\nlast"),
    ).toEqual({ content: "same\nsame\nlast", startLine: 1, endLine: 3 });
  });

  it("preserves partial text without including an untouched end line", () => {
    const container = preview();
    const lines = container.querySelectorAll("[data-code-line]");
    const range = document.createRange();
    const first = lines[0]?.firstChild;
    const last = lines[2]?.firstChild;
    if (!first || !last) throw new Error("Missing code lines");
    range.setStart(first, 2);
    range.setEnd(last, 0);
    expect(
      readWorkspaceTextSelection(container, range, "same\nsame\nlast"),
    ).toEqual({ content: "me\nsame\n", startLine: 1, endLine: 2 });
  });

  it("does not invent source lines for missing or ambiguous rendered text", () => {
    expect(findSelectedTextLineRange("same\nsame", "same")).toBeUndefined();
    expect(findSelectedTextLineRange("source", "missing")).toBeUndefined();
    expect(findSelectedTextLineRange("source", "  ")).toBeUndefined();
  });
});
