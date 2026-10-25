import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GitPatchPreview } from "./git-patch-preview";

const { init } = vi.hoisted(() => ({ init: vi.fn() }));

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));
vi.mock("@git-diff-view/file", () => ({
  DiffFile: class {
    initTheme() {}
    init() {
      init();
    }
    buildUnifiedDiffLines() {}
    buildSplitDiffLines() {}
  },
}));
vi.mock("@git-diff-view/react", () => ({
  DiffModeEnum: { Unified: "unified", Split: "split" },
  DiffView: () => <div data-testid="rendered-diff" />,
}));

describe("GitPatchPreview", () => {
  beforeEach(() => init.mockClear());

  it("多文件提交只在展开对应文件后初始化 Diff", async () => {
    const user = userEvent.setup();
    const patch = ["src/a.ts", "src/b.ts"]
      .map(
        (path) =>
          `diff --git a/${path} b/${path}\n--- a/${path}\n+++ b/${path}\n@@ -1 +1 @@\n-old\n+new\n`,
      )
      .join("");
    render(<GitPatchPreview path="commit" patch={patch} />);

    expect(init).not.toHaveBeenCalled();
    expect(screen.queryByTestId("rendered-diff")).toBeNull();
    await user.click(screen.getByRole("button", { name: /src\/a.ts/ }));
    expect(init).toHaveBeenCalledTimes(1);
    expect(screen.getAllByTestId("rendered-diff")).toHaveLength(1);
  });

  it("单个超大文件默认折叠，明确展开后才渲染", async () => {
    const user = userEvent.setup();
    const patch = `diff --git a/src/large.ts b/src/large.ts\n--- a/src/large.ts\n+++ b/src/large.ts\n@@ -1 +1 @@\n-old\n+${"a".repeat(75_000)}\n`;
    render(<GitPatchPreview path="src/large.ts" patch={patch} />);

    expect(init).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: /src\/large.ts/ }));
    expect(init).toHaveBeenCalledTimes(1);
  });
});
