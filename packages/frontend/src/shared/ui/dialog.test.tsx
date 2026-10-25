import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Dialog, DialogContent, DialogFooter } from "./dialog";

describe("DialogContent", () => {
  it("把说明放在标题栏下方，关闭按钮与标题对齐", () => {
    render(
      <Dialog open>
        <DialogContent
          size="sm"
          title="启用 YOLO 模式？"
          description="这会把全部已发现 CLI 的默认权限改为完全访问。之后需要先切回手动，才能分别调整权限。"
        >
          <DialogFooter>操作</DialogFooter>
        </DialogContent>
      </Dialog>,
    );

    const title = screen.getByText("启用 YOLO 模式？");
    const close = screen.getByRole("button", { name: "关闭" });
    const description = screen.getByText(/这会把全部已发现 CLI/);
    expect(title.closest("header")).toContainElement(close);
    expect(title.closest("header")).not.toContainElement(description);
    expect(title.closest("header")).toHaveClass("items-start", "pt-5");
  });

  it("展示布局不显示关闭按钮，只保留可访问名称", () => {
    render(
      <Dialog open>
        <DialogContent layout="stage" title="伙伴卡片">
          卡片内容
        </DialogContent>
      </Dialog>,
    );

    expect(screen.getByRole("heading", { name: "伙伴卡片" })).toHaveClass(
      "sr-only",
    );
    expect(screen.getByRole("button", { name: "关闭" })).toHaveClass("sr-only");
    expect(screen.getByText("卡片内容")).toBeVisible();
  });
});
