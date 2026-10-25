import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { companionAppearance } from "@/shared/companion/appearance";
import styles from "../../../../../vendor/companion/styles.json";
import { CompanionPicker } from "./companion-picker";

vi.mock("./avatar", () => ({ Avatar: () => null }));

describe("伙伴形象切换", () => {
  it("每次点击下一款顺序前进，机甲之后回到原始，不连续重复", async () => {
    const changed = vi.fn();
    function Picker() {
      const [value, setValue] = useState(styles.plush.avatar);
      return (
        <CompanionPicker
          label="头像"
          name="NeuxBot"
          value={value}
          onChange={(avatar) => {
            changed(avatar);
            setValue(avatar);
          }}
        />
      );
    }
    render(<Picker />);
    const user = userEvent.setup();
    const button = screen.getByRole("button", { name: "下一款" });
    for (let index = 0; index < 10; index++) await user.click(button);
    expect(
      changed.mock.calls.map(([avatar]) => companionAppearance(avatar).style),
    ).toEqual([
      "metal",
      "particles",
      "oil",
      "comic",
      "jelly",
      "grass",
      "mecha",
      "original",
      "lava",
      "plush",
    ]);
  });
});
