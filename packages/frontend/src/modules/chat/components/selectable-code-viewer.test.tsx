import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  SelectableCodeViewer,
  shouldHighlightCode,
} from "./selectable-code-viewer";

describe("SelectableCodeViewer", () => {
  it("以 Light DOM 渲染任意文本文件并保留行号", () => {
    const { container } = render(
      <SelectableCodeViewer
        content={"#!/bin/sh\necho ally"}
        fileName="release-install.unknown"
        theme="light"
        wrap={false}
      />,
    );

    const viewer = container.querySelector("[data-selectable-code-viewer]");
    expect(viewer?.shadowRoot).toBeNull();
    expect(viewer?.querySelector('[data-line="2"]')).toHaveTextContent(
      "echo ally",
    );
    expect(screen.getByText("echo ally")).toBeInTheDocument();
  });

  it("大文件跳过语法高亮以保护主线程", () => {
    expect(shouldHighlightCode("a".repeat(200_001))).toBe(false);
    expect(shouldHighlightCode("line\n".repeat(5_001))).toBe(false);
    expect(shouldHighlightCode("const ally = true")).toBe(true);
  });
});
