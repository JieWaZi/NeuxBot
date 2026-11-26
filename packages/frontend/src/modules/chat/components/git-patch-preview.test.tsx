import { render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { GitPatchBody } from "./git-patch-preview";

vi.mock("@/shared/theme", () => ({
  useTheme: () => ({ resolvedTheme: "light" }),
}));

afterEach(() => vi.restoreAllMocks());

it("真实 Diff 引擎统一和并排均渲染完整文件补丁", () => {
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
    measureText: (text: string) => ({ width: text.length * 8 }),
  } as CanvasRenderingContext2D);
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
