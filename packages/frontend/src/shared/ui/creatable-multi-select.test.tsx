import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { CreatableMultiSelect } from "./creatable-multi-select";

function Field({
  onChange = () => undefined,
}: {
  onChange?: (value: string[]) => void;
}) {
  const [value, setValue] = useState<string[]>([]);
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        throw new Error("Enter submitted the form");
      }}
    >
      <CreatableMultiSelect
        label="分类"
        placeholder="选择或输入分类"
        options={["开发", "开发工具", "写作"]}
        value={value}
        onValueChange={(next) => {
          setValue(next);
          onChange(next);
        }}
        createLabel={(name) => `新建 ${name}`}
        removeLabel={(name) => `移除 ${name}`}
        emptyLabel="输入分类后回车"
      />
    </form>
  );
}

describe("CreatableMultiSelect", () => {
  it("filters existing options, selects several, and removes tags", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    render(<Field onChange={changed} />);
    const input = screen.getByRole("combobox", { name: "分类" });
    await user.type(input, "开发");
    expect(
      screen.getByRole("option", { name: "开发工具" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "写作" }),
    ).not.toBeInTheDocument();
    await user.keyboard("{Enter}");
    expect(changed).toHaveBeenLastCalledWith(["开发"]);
    await user.type(input, "写作{Enter}");
    expect(changed).toHaveBeenLastCalledWith(["开发", "写作"]);
    await user.click(screen.getByRole("button", { name: "移除 开发" }));
    expect(changed).toHaveBeenLastCalledWith(["写作"]);
  });

  it("creates only on explicit commit and does not duplicate an existing selection", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    render(<Field onChange={changed} />);
    const input = screen.getByRole("combobox", { name: "分类" });
    await user.type(input, "  资料整理  ");
    expect(changed).not.toHaveBeenCalled();
    await user.keyboard("{Enter}");
    expect(changed).toHaveBeenLastCalledWith(["资料整理"]);
    await user.type(input, "资料整理{Enter}");
    expect(changed).toHaveBeenCalledTimes(1);
    await user.type(input, "研究");
    await user.click(screen.getByRole("option", { name: "新建 研究" }));
    expect(changed).toHaveBeenLastCalledWith(["资料整理", "研究"]);
  });

  it("supports arrow navigation and ignores IME confirmation", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    render(<Field onChange={changed} />);
    const input = screen.getByRole("combobox", { name: "分类" });
    await user.click(input);
    await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(changed).toHaveBeenLastCalledWith(["开发工具"]);
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "中文" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });
    expect(changed).toHaveBeenCalledTimes(1);
    fireEvent.compositionEnd(input);
    await user.keyboard("{Enter}");
    expect(changed).toHaveBeenLastCalledWith(["开发工具", "中文"]);
  });
});
