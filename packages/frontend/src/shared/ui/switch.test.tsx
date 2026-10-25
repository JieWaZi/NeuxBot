import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Switch } from "./switch";

describe("Switch", () => {
  it("exposes checked state and delegates activation", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();

    render(<Switch checked aria-label="启用技能" onClick={onClick} />);

    const control = screen.getByRole("switch", { name: "启用技能" });
    expect(control).toBeChecked();
    expect(control).toHaveClass("bg-control-switch-track-checked");
    await user.click(control);
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
