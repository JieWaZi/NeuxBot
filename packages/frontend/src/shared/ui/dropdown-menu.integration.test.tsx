import { render, screen } from "@testing-library/react";
import { Folder } from "lucide-react";
import { describe, expect, it } from "vitest";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

describe("DropdownMenuRadioItem", () => {
  it("选中项使用 selection-surface 背景", () => {
    render(
      <DropdownMenu defaultOpen>
        <DropdownMenuTrigger>打开</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuRadioGroup value="codex">
            <DropdownMenuRadioItem value="claude">Claude</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="codex">Codex</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    expect(screen.getByRole("menuitemradio", { name: "Codex" })).toHaveClass(
      "data-[state=checked]:bg-selection-surface",
    );
    expect(
      screen.getByRole("menuitemradio", { name: "Codex" }),
    ).toHaveAttribute("data-state", "checked");
  });
});

describe("DropdownMenuLabel", () => {
  it("菜单分区标题自带统一分割线", () => {
    render(
      <DropdownMenu defaultOpen>
        <DropdownMenuTrigger>打开</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuLabel>智能体</DropdownMenuLabel>
          <DropdownMenuRadioGroup value="claude">
            <DropdownMenuRadioItem value="claude">Claude</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    expect(screen.getByText("智能体")).toHaveClass("border-b");
  });
});

describe("DropdownMenuSubTrigger", () => {
  it("图标和子菜单标题保持在同一行", () => {
    render(
      <DropdownMenu defaultOpen>
        <DropdownMenuTrigger>打开</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger value="zddiv3">
              <Folder />
              项目
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>项目列表</DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    const title = screen.getByText("项目");
    expect(title).toHaveClass("flex", "items-center", "gap-2");
  });
});
