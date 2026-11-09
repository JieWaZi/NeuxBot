import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { CompanionPicker } from "./companion-picker";

// 材质纹理和动画生命周期由渲染层测试覆盖；这里验证选择和表单值。
vi.mock("./avatar", () => ({ Avatar: () => null }));

const savedAvatar = "bloub:v1:plush:nuage:curieux:38bdf8";

function Picker({ initialValue = savedAvatar }: { initialValue?: string }) {
  const [value, setValue] = useState(initialValue);
  return (
    <>
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value={value}
        onChange={setValue}
      />
      <output aria-label="已选头像">{value}</output>
    </>
  );
}

afterEach(() => vi.restoreAllMocks());

describe("伙伴头像选择", () => {
  it("只提供外观配置，不把业务动画暴露为选择项", async () => {
    const onChange = vi.fn();
    render(
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value={savedAvatar}
        onChange={onChange}
      />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    expect(screen.getAllByRole("group")).toHaveLength(4);
    for (const name of ["材质", "形状", "眼睛", "颜色"]) {
      expect(screen.getByRole("group", { name })).toBeInTheDocument();
    }
    expect(
      within(screen.getByRole("group", { name: "材质" }))
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["毛绒", "粒子"]);
    expect(screen.queryByRole("button", { name: /预览/ })).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });
  it("分别修改材质、形状、眼睛和颜色时保留其他配置", async () => {
    render(<Picker />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    expect(screen.getByRole("button", { name: "毛绒" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "粒子" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:nuage:curieux:38bdf8",
    );
    await user.click(screen.getByRole("button", { name: "三角形" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:triangle:curieux:38bdf8",
    );
    await user.click(screen.getByRole("button", { name: "开心" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:triangle:heureux:38bdf8",
    );
    await user.click(screen.getByRole("button", { name: "颜色 #ef4444" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:triangle:heureux:ef4444",
    );
    expect(
      screen.getByRole("button", { name: "颜色 #ef4444" }),
    ).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "毛绒" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:plush:triangle:heureux:ef4444",
    );
  });

  it("重新打开时按父表单配置显示选中项，不叠加装饰勾", async () => {
    const onChange = vi.fn();
    const view = render(
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value={savedAvatar}
        onChange={onChange}
      />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    await user.click(screen.getByRole("button", { name: "粒子" }));
    await user.keyboard("{Escape}");
    view.rerender(
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value="bloub:v1:particles:triangle:heureux:ef4444"
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    for (const name of ["粒子", "三角形", "开心", "颜色 #ef4444"]) {
      const selected = screen.getByRole("button", { name });
      expect(selected).toHaveAttribute("aria-pressed", "true");
      expect(
        selected.querySelector("svg:not([data-companion-avatar])"),
      ).toBeNull();
    }
  });

  it("随机生成完整组合，重置回默认头像", async () => {
    vi.spyOn(crypto, "getRandomValues").mockReturnValue(
      new Uint32Array([2, 4, 4, 0]),
    );
    render(<Picker initialValue="bloub:v1:particles:nuage:curieux:38bdf8" />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    await user.click(screen.getByRole("button", { name: "随机" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:triangle:heureux:ef4444",
    );
    await user.click(screen.getByRole("button", { name: "重置" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:plush:cercle:neutre:8b5cf6",
    );
  });

  it("支持键盘打开、选择与关闭，并恢复触发按钮焦点", async () => {
    render(<Picker />);
    const user = userEvent.setup();
    await user.tab();
    expect(screen.getByRole("button", { name: "编辑头像" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "毛绒" })).toHaveFocus();
    await user.tab();
    expect(screen.getByRole("button", { name: "粒子" })).toHaveFocus();
    await user.keyboard(" ");
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:v1:particles:nuage:curieux:38bdf8",
    );
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("button", { name: "粒子" })).toBeNull();
    expect(screen.getByRole("button", { name: "编辑头像" })).toHaveFocus();
  });
});
