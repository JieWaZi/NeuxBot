import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "./select";

it("Select 使用标准触发器并通过键盘菜单选择值", async () => {
  const user = userEvent.setup();
  const onValueChange = vi.fn();
  render(
    <Select value="default" onValueChange={onValueChange}>
      <SelectTrigger aria-label="默认模型">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="default">Default</SelectItem>
        <SelectItem value="high">High</SelectItem>
      </SelectContent>
    </Select>,
  );

  const trigger = screen.getByRole("combobox", { name: "默认模型" });
  expect(trigger).toHaveClass("h-control-md", "rounded-md");

  trigger.focus();
  await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");

  expect(onValueChange).toHaveBeenCalledWith("high");
});

it("SelectItem 选中项使用 selection-surface 背景", () => {
  render(
    <Select defaultOpen value="default" onValueChange={() => undefined}>
      <SelectTrigger aria-label="默认模型">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="default">Default</SelectItem>
        <SelectItem value="high">High</SelectItem>
      </SelectContent>
    </Select>,
  );

  expect(screen.getByRole("option", { name: "Default" })).toHaveClass(
    "data-[state=checked]:bg-selection-surface",
  );
  expect(screen.getByRole("option", { name: "Default" })).toHaveAttribute(
    "data-state",
    "checked",
  );
  expect(screen.getByRole("listbox")).toHaveClass("max-w-[calc(100vw-32px)]");
});
