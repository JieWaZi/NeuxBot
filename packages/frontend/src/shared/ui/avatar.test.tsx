import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { Avatar, StaticAvatar } from "./avatar";
import "@/shared/i18n";

it("悬停临时换表情，点击不重复切换，移开恢复保存的眼睛", async () => {
  vi.spyOn(Math, "random").mockReturnValue(0.6);
  const user = userEvent.setup();
  const view = render(
    <Avatar name="伙伴" src="bloub:plush:nuage:curieux:38bdf8" />,
  );
  const trigger = screen.getByRole("button", { name: "与 伙伴 互动" });
  const avatar = view.container.querySelector("svg[data-companion-avatar]");
  await user.hover(trigger);
  const expression = avatar?.getAttribute("data-expression");
  expect(expression).not.toBe("curieux");
  for (let count = 0; count < 3; count++) {
    await user.click(trigger);
    expect(avatar).toHaveAttribute("data-expression", expression);
  }
  await user.unhover(trigger);
  expect(avatar).toHaveAttribute("data-expression", "curieux");
  fireEvent.keyDown(trigger, { key: "ArrowUp" });
  expect(screen.queryByRole("region")).toBeNull();
  vi.restoreAllMocks();
});

it("关闭头像互动后，外部按钮的悬停与焦点不切换头像表情", async () => {
  const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
  const user = userEvent.setup();
  const { container } = render(
    <button type="button">
      选择伙伴
      <Avatar
        name="伙伴"
        src="bloub:plush:nuage:curieux:38bdf8"
        interactive={false}
      />
    </button>,
  );
  const trigger = screen.getByRole("button", { name: "选择伙伴" });
  const avatar = container.querySelector("svg[data-companion-avatar]");
  expect(screen.getAllByRole("button")).toHaveLength(1);
  await user.hover(trigger);
  await user.tab();
  expect(avatar).toHaveAttribute("data-expression", "curieux");
  random.mockRestore();
});

it("指针移开后保留键盘焦点反馈，失焦后恢复保存的表情", async () => {
  const random = vi.spyOn(Math, "random").mockReturnValue(0.6);
  const user = userEvent.setup();
  const { container } = render(
    <Avatar name="伙伴" src="bloub:plush:nuage:curieux:38bdf8" />,
  );
  const trigger = screen.getByRole("button");
  const avatar = container.querySelector("svg[data-companion-avatar]");
  await user.tab();
  expect(trigger).toHaveFocus();
  expect(avatar).not.toHaveAttribute("data-expression", "curieux");
  await user.hover(trigger);
  await user.unhover(trigger);
  expect(avatar).not.toHaveAttribute("data-expression", "curieux");
  await user.tab();
  expect(avatar).toHaveAttribute("data-expression", "curieux");
  random.mockRestore();
});

it("静态头像使用同一渲染层的固定采样，不保留旧图片头像", () => {
  const raf = vi.spyOn(window, "requestAnimationFrame");
  const view = render(<StaticAvatar name="伙伴" size="sm" />);
  expect(raf).not.toHaveBeenCalled();
  raf.mockRestore();
  expect(view.container.querySelector("img")).toBeNull();
  expect(screen.queryByRole("button")).toBeNull();
  expect(view.container.firstElementChild).toHaveClass("size-avatar-sm");
});

it("明确提供资料回调时，头像双击和键盘打开资料，单击保持行内展示", async () => {
  const user = userEvent.setup();
  const onDoubleInteract = vi.fn();
  render(
    <Avatar
      name="伙伴"
      interactive={false}
      onDoubleInteract={onDoubleInteract}
    />,
  );
  const trigger = screen.getByRole("button");
  await user.click(trigger);
  expect(onDoubleInteract).not.toHaveBeenCalled();
  await user.dblClick(trigger);
  expect(onDoubleInteract).toHaveBeenCalledExactlyOnceWith(trigger);
  trigger.focus();
  await user.keyboard("{Enter}");
  expect(onDoubleInteract).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("region")).toBeNull();
});

it("关闭动画时不创建资料卡或表情互动按钮", () => {
  const onDoubleInteract = vi.fn();
  render(
    <Avatar name="伙伴" animated={false} onDoubleInteract={onDoubleInteract} />,
  );
  expect(screen.queryByRole("button")).toBeNull();
});
