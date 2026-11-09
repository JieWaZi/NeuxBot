import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { Avatar, StaticAvatar } from "./avatar";
import "@/shared/i18n";

it("单击头像和键盘输入可切换表情，连续点击及上箭头不展开悬浮角色", async () => {
  const user = userEvent.setup();
  const view = render(<Avatar name="伙伴" />);
  const trigger = screen.getByRole("button", { name: "与 伙伴 互动" });
  const avatar = view.container.querySelector("svg[data-companion-avatar]");
  let expression = avatar?.getAttribute("data-expression");
  expect(expression).toBeTruthy();
  for (let count = 0; count < 3; count++) {
    await user.click(trigger);
    await waitFor(() =>
      expect(avatar?.getAttribute("data-expression")).not.toBe(expression),
    );
    expression = avatar?.getAttribute("data-expression");
  }
  trigger.focus();
  for (const key of ["{Enter}", " "]) {
    await user.keyboard(key);
    await waitFor(() =>
      expect(avatar?.getAttribute("data-expression")).not.toBe(expression),
    );
    expression = avatar?.getAttribute("data-expression");
  }
  fireEvent.keyDown(trigger, { key: "ArrowUp" });
  expect(screen.queryByRole("region")).toBeNull();
});

it("明确关闭互动时保留普通行内头像", () => {
  render(<Avatar name="伙伴" interactive={false} />);
  expect(screen.queryByRole("button")).toBeNull();
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
