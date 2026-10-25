import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import "@/shared/i18n";
import { TooltipProvider } from "@/shared/ui/tooltip";
import { ExecutionPermissionPicker } from "./execution-permission-picker";

describe("ExecutionPermissionPicker", () => {
  it("按钮本身不展示说明，展开后直接显示各档副文案", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <TooltipProvider delayDuration={0}>
        <ExecutionPermissionPicker
          value="default"
          supported={new Set(["default", "auto", "full-access"])}
          onChange={vi.fn()}
        />
      </TooltipProvider>,
    );

    const trigger = screen.getByRole("button", {
      name: "执行权限：默认权限",
    });
    const selectedIcon = container.querySelector(".lucide-shield-keyhole");
    expect(selectedIcon).toBeInTheDocument();
    expect(selectedIcon).toHaveClass("size-icon-md");
    expect(
      screen.queryByText(/允许读取和分析工作区内容/),
    ).not.toBeInTheDocument();
    await user.hover(trigger);
    expect(
      screen.queryByText(/允许读取和分析工作区内容/),
    ).not.toBeInTheDocument();

    await user.click(trigger);
    expect(screen.getByText("默认权限")).toBeInTheDocument();
    expect(screen.getByText("自动审批")).toBeInTheDocument();
    expect(screen.getByText("完全访问")).toBeInTheDocument();
    expect(screen.getByText(/允许读取和分析工作区内容/)).toBeInTheDocument();
    expect(screen.getByText(/结合当前任务/)).toBeInTheDocument();
    expect(screen.getByText(/跳过大部分权限询问/)).toBeInTheDocument();
    expect(
      document.querySelector(".lucide-shield-keyhole"),
    ).toBeInTheDocument();
    expect(document.querySelector(".lucide-sparkles")).toBeInTheDocument();
    expect(document.querySelector(".lucide-shield-alert")).toBeInTheDocument();
  });

  it("不支持的权限档展示不可用说明", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider delayDuration={0}>
        <ExecutionPermissionPicker
          value="default"
          supported={new Set(["default"])}
          onChange={vi.fn()}
        />
      </TooltipProvider>,
    );

    await user.click(
      screen.getByRole("button", { name: "执行权限：默认权限" }),
    );
    expect(screen.getAllByText("当前智能体或模型不支持此权限。")).toHaveLength(
      2,
    );
  });
});
