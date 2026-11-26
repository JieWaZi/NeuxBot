import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { Dialog, DialogDrawerContent } from "./dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "./dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";
import { Select, SelectContent, SelectItem, SelectTrigger } from "./select";

afterEach(cleanup);

it("主菜单与子菜单都按剩余视口高度限制，并在内部滚动", () => {
  render(
    <DropdownMenu defaultOpen>
      <DropdownMenuTrigger>菜单</DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem>选项</DropdownMenuItem>
        <DropdownMenuSub open>
          <DropdownMenuSubTrigger>子菜单</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            <DropdownMenuItem>子选项</DropdownMenuItem>
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenu>,
  );
  const menus = screen.getAllByRole("menu", { hidden: true });
  expect(menus).toHaveLength(2);
  for (const menu of menus) {
    expect(menu).toHaveClass(
      "max-h-[min(320px,var(--radix-dropdown-menu-content-available-height))]",
      "overflow-y-auto",
      "overscroll-contain",
      "max-w-[calc(100vw-32px)]",
    );
  }
});

it.each(["default", "navigation"] as const)(
  "%s Popover 由共享层限制剩余高度，导航保留独立列表滚动",
  (variant) => {
    render(
      <Popover defaultOpen>
        <PopoverTrigger>浮层</PopoverTrigger>
        <PopoverContent variant={variant}>内容</PopoverContent>
      </Popover>,
    );
    expect(screen.getByRole("dialog")).toHaveClass(
      "max-h-[var(--radix-popover-content-available-height)]",
      variant === "navigation" ? "overflow-hidden" : "overflow-y-auto",
    );
  },
);

it("窗格抽屉为边缘留白并在剩余空间内滚动", () => {
  render(
    <Dialog defaultOpen modal={false}>
      <DialogDrawerContent container={document.body} title="摘要">
        内容
      </DialogDrawerContent>
    </Dialog>,
  );
  expect(screen.getByRole("dialog", { name: "摘要" })).toHaveClass(
    "top-2",
    "right-2",
    "max-h-[calc(100%-1rem)]",
    "max-w-[calc(100%-1rem)]",
    "overflow-y-auto",
    "overscroll-contain",
  );
});

it("Select 的固定高度上限仍受剩余视口约束", () => {
  render(
    <Select defaultOpen>
      <SelectTrigger aria-label="选项" />
      <SelectContent>
        <SelectItem value="one">第一项</SelectItem>
      </SelectContent>
    </Select>,
  );
  expect(screen.getByRole("listbox")).toHaveClass(
    "max-h-[min(320px,var(--radix-select-content-available-height,320px))]",
  );
});
