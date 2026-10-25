import { fireEvent, render, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { CompanionAvatar } from "./companion-avatar";
import bodies from "../../../../../vendor/companion/styles.json";

it.each([false, true])(
  "冷缓存下身体图片未加载时不露出眼睛，也不提前通知欢迎入场（动态 %s）",
  async (animated) => {
    const ready = vi.fn();
    const view = render(
      <CompanionAvatar
        avatar={bodies.plush.avatar}
        animated={animated}
        onReady={ready}
      />,
    );
    await waitFor(() =>
      expect(
        view.container.querySelector("image.ally-body-image"),
      ).not.toBeNull(),
    );
    const body = view.container.querySelector("image.ally-body-image");
    const svg = view.container.querySelector("svg");
    if (!body || !svg) throw new Error("缺少真实形象图层");
    expect(ready).not.toHaveBeenCalled();
    expect(svg).toHaveAttribute("visibility", "hidden");
    fireEvent.load(body);
    await waitFor(() => expect(ready).toHaveBeenCalledOnce());
    expect(svg).not.toHaveAttribute("visibility", "hidden");
  },
);

it("身体图片失败会通知降级，移除不完整 SVG，不错误放行欢迎入场", async () => {
  const ready = vi.fn();
  const error = vi.fn();
  const view = render(
    <CompanionAvatar
      avatar={bodies.plush.avatar}
      onReady={ready}
      onError={error}
    />,
  );
  await waitFor(() =>
    expect(
      view.container.querySelector("image.ally-body-image"),
    ).not.toBeNull(),
  );
  const body = view.container.querySelector("image.ally-body-image");
  if (!body) throw new Error("缺少身体图层");
  fireEvent.error(body);
  await waitFor(() => expect(error).toHaveBeenCalledOnce());
  expect(ready).not.toHaveBeenCalled();
  expect(view.container.querySelector("svg")).toBeNull();
});

it("加载中卸载后，旧图片完成不会再通知或恢复已销毁的形象", async () => {
  const ready = vi.fn();
  const view = render(
    <CompanionAvatar avatar={bodies.plush.avatar} onReady={ready} />,
  );
  await waitFor(() =>
    expect(
      view.container.querySelector("image.ally-body-image"),
    ).not.toBeNull(),
  );
  const body = view.container.querySelector("image.ally-body-image");
  if (!body) throw new Error("缺少身体图层");
  view.unmount();
  fireEvent.load(body);
  expect(ready).not.toHaveBeenCalled();
});
