import { useState } from "react";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import "@/shared/i18n";
import { CompanionPicker } from "./companion-picker";

const savedAvatar = "bloub:plush:nuage:curieux:38bdf8";

function Picker({
  initialValue = savedAvatar,
  inline = false,
}: {
  initialValue?: string;
  inline?: boolean;
}) {
  const [value, setValue] = useState(initialValue);
  return (
    <>
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value={value}
        onChange={setValue}
        inline={inline}
      />
      <output aria-label="已选头像">{value}</output>
    </>
  );
}

afterEach(() => vi.restoreAllMocks());

describe("伙伴头像选择", () => {
  it.each([false, true])(
    "双击形状循环表情，不重置已选表情 (inline=%s)",
    async (inline) => {
      render(<Picker inline={inline} />);
      const user = userEvent.setup();
      if (!inline)
        await user.click(screen.getByRole("button", { name: "编辑头像" }));
      const avatars = within(screen.getByRole("group", { name: "头像样式" }));
      await user.dblClick(avatars.getByRole("button", { name: "云朵" }));
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:plush:nuage:timide:38bdf8",
      );
      await user.click(avatars.getByRole("button", { name: "三角形" }));
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:plush:triangle:timide:38bdf8",
      );
      await user.dblClick(avatars.getByRole("button", { name: "三角形" }));
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:plush:triangle:blase:38bdf8",
      );
      avatars.getByRole("button", { name: "三角形" }).focus();
      await user.keyboard("{Shift>}{Enter}{/Shift}");
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:plush:triangle:gene:38bdf8",
      );
    },
  );

  it("每个形状保留默认表情，选择时保留材质与颜色", async () => {
    render(<Picker inline />);
    const user = userEvent.setup();
    const avatars = within(screen.getByRole("group", { name: "头像样式" }));
    expect(
      avatars
        .getAllByRole("button")
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual([
      "圆形",
      "卵石",
      "圆角方形",
      "胶囊",
      "三角形",
      "六边形",
      "云朵",
      "方形",
      "菱形",
      "五边形",
    ]);
    expect(
      avatars
        .getAllByRole("button")
        .map((button) =>
          button
            .querySelector("svg[data-companion-avatar]")
            ?.getAttribute("data-expression"),
        ),
    ).toEqual([
      "neutre",
      "attentif",
      "hilare",
      "curieux",
      "timide",
      "blase",
      "curieux",
      "reveur",
      "meditatif",
      "confus",
    ]);
    expect(screen.queryByRole("group", { name: "眼睛" })).toBeNull();
    expect(screen.queryByRole("group", { name: "形状" })).toBeNull();
    await user.click(avatars.getByRole("button", { name: "三角形" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:plush:triangle:timide:38bdf8",
    );
    expect(avatars.getByRole("button", { name: "三角形" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "软胶" }));
    await user.click(screen.getByRole("button", { name: "颜色 #ef4444" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:balloon:triangle:timide:ef4444",
    );
  });

  it("双击大预览切换表情，保留形状、材质与颜色", async () => {
    render(<Picker inline />);
    const user = userEvent.setup();
    await user.dblClick(
      screen.getByRole("button", { name: "双击切换表情（Shift+Enter）" }),
    );
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:plush:nuage:timide:38bdf8",
    );
  });

  it("只提供头像、材质和颜色，不把业务动画暴露为选择项", async () => {
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
    expect(screen.getAllByRole("group")).toHaveLength(3);
    for (const name of ["材质", "头像样式", "颜色"]) {
      expect(screen.getByRole("group", { name })).toBeInTheDocument();
    }
    expect(
      within(screen.getByRole("group", { name: "材质" }))
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["毛绒", "粒子", "软胶"]);
    expect(screen.queryByRole("button", { name: /预览/ })).toBeNull();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("调整材质与颜色保留表情，换形状使用对应默认表情", async () => {
    render(<Picker initialValue="bloub:plush:capsule:curieux:38bdf8" />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    const avatars = within(screen.getByRole("group", { name: "头像样式" }));
    expect(avatars.getByRole("button", { name: "胶囊" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:plush:capsule:curieux:38bdf8",
    );
    await user.click(screen.getByRole("button", { name: "粒子" }));
    await user.click(screen.getByRole("button", { name: "颜色 #ef4444" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:particles:capsule:curieux:ef4444",
    );
    await user.click(avatars.getByRole("button", { name: "云朵" }));
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:particles:nuage:gene:ef4444",
    );
  });

  it("重新打开时按形状匹配选中项，不叠加装饰勾", async () => {
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
    await user.keyboard("{Escape}");
    view.rerender(
      <CompanionPicker
        label="头像"
        name="NeuxBot"
        value="bloub:particles:triangle:timide:ef4444"
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "编辑头像" }));
    for (const name of ["粒子", "三角形", "颜色 #ef4444"]) {
      const selected = screen.getByRole("button", { name });
      expect(selected).toHaveAttribute("aria-pressed", "true");
      expect(
        selected.querySelector("svg:not([data-companion-avatar])"),
      ).toBeNull();
    }
  });

  it.each([false, true])(
    "随机与重置在 inline=%s 时都可使用，随机保留材质",
    async (inline) => {
      vi.spyOn(crypto, "getRandomValues").mockReturnValue(
        new Uint32Array([4, 0]),
      );
      render(
        <Picker
          initialValue="bloub:particles:nuage:curieux:38bdf8"
          inline={inline}
        />,
      );
      const user = userEvent.setup();
      if (!inline)
        await user.click(screen.getByRole("button", { name: "编辑头像" }));
      await user.click(screen.getByRole("button", { name: "随机" }));
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:particles:triangle:timide:ef4444",
      );
      await user.click(screen.getByRole("button", { name: "重置" }));
      expect(screen.getByLabelText("已选头像")).toHaveTextContent(
        "bloub:plush:cercle:neutre:8b5cf6",
      );
      expect(screen.getByRole("button", { name: "圆形" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    },
  );

  it("支持键盘打开、选择形状与关闭，并恢复触发按钮焦点", async () => {
    render(<Picker />);
    const user = userEvent.setup();
    await user.tab();
    expect(screen.getByRole("button", { name: "编辑头像" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(screen.getByRole("button", { name: "毛绒" })).toHaveFocus();
    await user.tab();
    await user.keyboard(" ");
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:particles:nuage:curieux:38bdf8",
    );
    for (let index = 0; index < 4; index++) await user.tab();
    expect(screen.getByRole("button", { name: "圆形" })).toHaveFocus();
    await user.keyboard(" ");
    expect(screen.getByLabelText("已选头像")).toHaveTextContent(
      "bloub:particles:cercle:neutre:38bdf8",
    );
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("button", { name: "粒子" })).toBeNull();
    expect(screen.getByRole("button", { name: "编辑头像" })).toHaveFocus();
  });
});
