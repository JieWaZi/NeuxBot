import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { highlightSelectableCode } from "./selectable-code-highlighter";

vi.mock("./selectable-code-highlighter", () => ({
  highlightSelectableCode: vi.fn(),
}));

import {
  SelectableCodeViewer,
  shouldHighlightCode,
  languageFromFileName,
} from "./selectable-code-viewer";

describe("SelectableCodeViewer", () => {
  it.each([
    ["Dockerfile", "dockerfile"],
    ["Makefile", "makefile"],
    ["code.c", "c"],
    ["code.cc", "cpp"],
    ["code.cpp", "cpp"],
    ["code.cs", "csharp"],
    ["code.css", "css"],
    ["code.go", "go"],
    ["code.h", "c"],
    ["code.hpp", "cpp"],
    ["code.html", "html"],
    ["code.java", "java"],
    ["code.js", "javascript"],
    ["code.jsx", "jsx"],
    ["code.json", "json"],
    ["code.jsonc", "jsonc"],
    ["code.kt", "kotlin"],
    ["code.less", "less"],
    ["code.lua", "lua"],
    ["code.py", "python"],
    ["code.rb", "ruby"],
    ["code.rs", "rust"],
    ["code.scss", "scss"],
    ["code.sh", "bash"],
    ["code.sql", "sql"],
    ["code.svelte", "svelte"],
    ["code.toml", "toml"],
    ["code.ts", "typescript"],
    ["code.tsx", "tsx"],
    ["code.vue", "vue"],
    ["code.xml", "xml"],
    ["code.yaml", "yaml"],
    ["code.yml", "yaml"],
    ["code.zsh", "bash"],
    ["/path/COMPONENT.TSX", "tsx"],
    ["code.unknown", "text"],
  ])("保留 %s 的文件语法 %s", (fileName, language) => {
    expect(languageFromFileName(fileName)).toBe(language);
  });
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

describe("SelectableCodeViewer 异步高亮", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetAllMocks();
  });

  it("高亮失败保持可选文本，后续主题变化能重新高亮", async () => {
    vi.stubEnv("MODE", "production");
    vi.mocked(highlightSelectableCode).mockRejectedValueOnce(
      new Error("load failed"),
    );
    const { container, rerender } = render(
      <SelectableCodeViewer
        content="retryHighlight"
        fileName="retry.ts"
        theme="light"
        wrap={false}
      />,
    );
    await waitFor(() =>
      expect(highlightSelectableCode).toHaveBeenCalledTimes(1),
    );
    expect(screen.getByText("retryHighlight")).toBeInTheDocument();
    expect(
      container.querySelector("[data-code-line] span"),
    ).not.toHaveAttribute("style");
    vi.mocked(highlightSelectableCode).mockResolvedValueOnce([
      [{ content: "retryHighlight", color: "#ff0000", offset: 0 }],
    ]);
    rerender(
      <SelectableCodeViewer
        content="retryHighlight"
        fileName="retry.ts"
        theme="dark"
        wrap={false}
      />,
    );
    await waitFor(() =>
      expect(container.querySelector("[data-code-line] span")).toHaveStyle({
        color: "#ff0000",
      }),
    );
  });

  it("切换文档后忽略旧文档延迟完成的高亮", async () => {
    vi.stubEnv("MODE", "production");
    let finishOld:
      | ((value: Awaited<ReturnType<typeof highlightSelectableCode>>) => void)
      | undefined;
    vi.mocked(highlightSelectableCode).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve;
        }),
    );
    vi.mocked(highlightSelectableCode).mockResolvedValueOnce([
      [{ content: "newDocument", color: "#0000ff", offset: 0 }],
    ]);
    const { container, rerender } = render(
      <SelectableCodeViewer
        content="oldDocument"
        fileName="old.ts"
        theme="light"
        wrap={false}
      />,
    );
    await waitFor(() => expect(finishOld).toBeDefined());
    rerender(
      <SelectableCodeViewer
        content="newDocument"
        fileName="new.ts"
        theme="light"
        wrap={false}
      />,
    );
    await waitFor(() =>
      expect(container.querySelector("[data-code-line] span")).toHaveStyle({
        color: "#0000ff",
      }),
    );
    await act(async () => {
      finishOld?.([[{ content: "oldDocument", color: "#ff0000", offset: 0 }]]);
    });
    expect(container.querySelector("[data-code-line]")).toHaveTextContent(
      "newDocument",
    );
    expect(container.querySelector("[data-code-line] span")).toHaveStyle({
      color: "#0000ff",
    });
  });
});
