import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { Sheet, SheetContent } from "./sheet";
it("预览侧栏使用语义wide尺寸，默认窄侧栏保持原尺寸", () => {
  const view = render(
    <Sheet open>
      <SheetContent title="预览" size="wide">
        正文
      </SheetContent>
    </Sheet>,
  );
  expect(screen.getByRole("dialog")).toHaveClass("max-w-3xl");
  view.unmount();
  render(
    <Sheet open>
      <SheetContent title="原侧栏">正文</SheetContent>
    </Sheet>,
  );
  expect(screen.getByRole("dialog")).toHaveClass("max-w-[400px]");
});
