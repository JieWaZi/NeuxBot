import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { GitPatchBody } from "./git-patch-preview";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: { page: number; total: number }) =>
      options ? `${key} ${options.page}/${options.total}` : key,
  }),
}));

beforeEach(() => {
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    measureText: (text: string) => ({ width: text.length * 8 }),
  } as CanvasRenderingContext2D);
});

afterEach(() => vi.restoreAllMocks());

function findDiffRow(container: HTMLElement, content: string) {
  return Array.from(
    container.querySelectorAll(".diff-line-content-raw, .diff-line-syntax-raw"),
  )
    .find((line) => line.textContent === content)
    ?.closest("tr");
}

it("真实 Diff 引擎统一和并排均渲染完整文件补丁", () => {
  const path = "src/changed.ts";
  const patch =
    "diff --git a/src/changed.ts b/src/changed.ts\n" +
    "--- a/src/changed.ts\n+++ b/src/changed.ts\n" +
    "@@ -1 +1 @@\n-old content\n+new content\n";
  const { container, rerender } = render(
    <GitPatchBody mode="unified" patch={patch} path={path} wrap={false} />,
  );
  expect(container).toHaveTextContent("old content");
  expect(container).toHaveTextContent("new content");

  rerender(<GitPatchBody mode="split" patch={patch} path={path} wrap />);
  expect(container).toHaveTextContent("old content");
  expect(container).toHaveTextContent("new content");
});

it("上万行变更只渲染当前区段并保留两侧的原始行号", () => {
  const path = "src/large.txt";
  const rows = Array.from({ length: 5000 }, (_, index) => index + 1);
  const patch = [
    "diff --git a/src/large.txt b/src/large.txt",
    "--- a/src/large.txt",
    "+++ b/src/large.txt",
    "@@ -17,5000 +31,5000 @@ section",
    ...rows.map((line) => `-old-line-${line}`),
    ...rows.map((line) => `+new-line-${line}`),
    "",
  ].join("\n");
  const { container, rerender } = render(
    <GitPatchBody mode="unified" patch={patch} path={path} wrap={false} />,
  );

  expect(
    container.querySelectorAll('tr[data-state="diff"]').length,
  ).toBeLessThanOrEqual(300);
  expect(
    findDiffRow(container, "old-line-1")?.querySelector("[data-line-old-num]"),
  ).toHaveAttribute("data-line-old-num", "17");
  expect(
    findDiffRow(container, "new-line-1")?.querySelector("[data-line-new-num]"),
  ).toHaveAttribute("data-line-new-num", "31");
  expect(findDiffRow(container, "new-line-5000")).toBeUndefined();

  fireEvent.click(
    screen.getByRole("button", { name: "workspace.git.patchNext" }),
  );
  expect(screen.getByRole("status")).toHaveTextContent("2/34");
  expect(
    findDiffRow(container, "old-line-151")?.querySelector(
      "[data-line-old-num]",
    ),
  ).toHaveAttribute("data-line-old-num", "167");
  expect(
    findDiffRow(container, "new-line-151")?.querySelector(
      "[data-line-new-num]",
    ),
  ).toHaveAttribute("data-line-new-num", "181");
  expect(findDiffRow(container, "new-line-1")).toBeUndefined();

  rerender(<GitPatchBody mode="split" patch={patch} path={path} wrap />);
  expect(
    findDiffRow(container, "new-line-151")?.querySelector("[data-line-num]"),
  ).toHaveAttribute("data-line-num", "181");
  expect(findDiffRow(container, "new-line-1")).toBeUndefined();
  fireEvent.click(
    screen.getByRole("button", { name: "workspace.git.patchPrevious" }),
  );
  expect(findDiffRow(container, "new-line-1")).toBeInTheDocument();
});

it("增删单侧空范围和跨分段的后续补丁块保留正确行号", () => {
  const path = "src/changed.txt";
  const patch = [
    "diff --git a/src/changed.txt b/src/changed.txt",
    "--- a/src/changed.txt",
    "+++ b/src/changed.txt",
    "@@ -70,0 +71,301 @@",
    ...Array.from({ length: 301 }, (_, index) => `+insert-${index + 1}`),
    "@@ -100,2 +401 @@",
    " retained context",
    "-deleted end",
    "\\ No newline at end of file",
    "",
  ].join("\n");
  const { container } = render(
    <GitPatchBody mode="unified" patch={patch} path={path} wrap={false} />,
  );
  expect(findDiffRow(container, "insert-301")).toBeUndefined();
  const next = screen.getByRole("button", { name: "workspace.git.patchNext" });
  fireEvent.click(next);
  expect(screen.getByRole("status")).toHaveTextContent("2/2");
  expect(
    findDiffRow(container, "insert-301")?.querySelector("[data-line-new-num]"),
  ).toHaveAttribute("data-line-new-num", "371");
  expect(
    findDiffRow(container, "retained context")?.querySelector(
      "[data-line-new-num]",
    ),
  ).toHaveAttribute("data-line-new-num", "401");
  expect(
    findDiffRow(container, "deleted end")?.querySelector("[data-line-old-num]"),
  ).toHaveAttribute("data-line-old-num", "101");
  expect(next).toBeDisabled();
});
