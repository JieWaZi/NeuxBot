import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { FieldCheckboxDropdown } from "./field-checkbox-dropdown";

describe("FieldCheckboxDropdown", () => {
  it("触发器使用与 Select 一致的 field 外观", () => {
    render(
      <FieldCheckboxDropdown
        ariaLabel="思考等级"
        options={["low", "high"]}
        value={["low"]}
        triggerLabel="low"
        onValueChange={() => undefined}
      />,
    );

    expect(screen.getByRole("button", { name: "思考等级" })).toHaveClass(
      "bg-field",
      "rounded-md",
      "h-control-sm",
    );
  });

  it("勾选选项时按 options 顺序回传", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <FieldCheckboxDropdown
        ariaLabel="思考等级"
        options={["none", "low", "high", "max"]}
        value={["low"]}
        triggerLabel="low"
        onValueChange={onValueChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: "思考等级" }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "high" }));

    expect(onValueChange).toHaveBeenCalledWith(["low", "high"]);
  });
});
