import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import "@/shared/i18n";

import { WakerShowcaseCard } from "./waker-showcase-card";

describe("WakerShowcaseCard", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("展开途中渐入晃动，展开完成不重置姿态，结束时平滑归位", () => {
    vi.useFakeTimers();
    const props = { connectorCount: 0, name: "林可", skillCount: 0 };
    const { rerender } = render(
      <WakerShowcaseCard {...props} phase="opening" />,
    );
    const card = screen.getByRole("article");
    const angle = () =>
      Number.parseFloat(
        card
          .querySelector<HTMLElement>(".ally-holo-card__tilter")
          ?.style.transform.match(/rotateX\(([-.\d]+)/)?.[1] || "0",
      );

    act(() => vi.advanceTimersByTime(32));
    expect(Math.abs(angle())).toBeLessThan(2);
    act(() => vi.advanceTimersByTime(168));
    expect(Math.abs(angle())).toBeGreaterThan(0.1);
    act(() => vi.advanceTimersByTime(220));
    const beforeOpen = angle();
    rerender(<WakerShowcaseCard {...props} phase="open" />);
    expect(angle()).toBe(beforeOpen);
    act(() => vi.advanceTimersByTime(32));
    expect(Math.abs(angle() - beforeOpen)).toBeLessThan(2);
    expect(angle()).not.toBe(beforeOpen);
    act(() => vi.advanceTimersByTime(3600));
    expect(Math.abs(angle())).toBeLessThan(0.02);
  });

  it("收起保留当前倾斜且不翻背，由飞行动画结束通知关闭", () => {
    vi.useFakeTimers();
    const onMotionEnd = vi.fn();
    const props = {
      connectorCount: 0,
      name: "林可",
      skillCount: 0,
      onMotionEnd,
    };
    const { rerender } = render(<WakerShowcaseCard {...props} />);
    act(() => vi.advanceTimersByTime(900));
    const card = screen.getByRole("article");
    const angle = card.querySelector<HTMLElement>(".ally-holo-card__tilter")
      ?.style.transform;
    rerender(<WakerShowcaseCard {...props} phase="closing" />);
    fireEvent.pointerLeave(card);
    act(() => vi.advanceTimersByTime(200));
    expect(
      card.querySelector<HTMLElement>(".ally-holo-card__tilter")?.style
        .transform,
    ).toBe(angle);
    const rotator = card.querySelector(".ally-holo-card__rotator");
    const translater = card.querySelector(".ally-holo-card__translater");
    if (!rotator || !translater) throw new Error("卡片缺少动画层");
    expect(getComputedStyle(rotator).animation).not.toContain("flip-out");
    // jsdom 没有 AnimationEvent，React 在此环境监听带前缀的事件。
    fireEvent(rotator, new Event("webkitAnimationEnd", { bubbles: true }));
    expect(onMotionEnd).not.toHaveBeenCalled();
    fireEvent(translater, new Event("webkitAnimationEnd", { bubbles: true }));
    expect(onMotionEnd).toHaveBeenCalledTimes(1);
  });

  it("展示形象、名称、擅长和技能连接器数量", () => {
    render(
      <WakerShowcaseCard
        avatar="companion:v1:0123456789abcdef0123456789abcdef"
        connectorCount={2}
        expertise={[
          { title: "前端实现" },
          { title: "设计系统", description: "不展示长说明" },
        ]}
        name="林可"
        skillCount={3}
      />,
    );

    expect(screen.getByRole("heading", { name: "林可" })).toBeVisible();
    expect(screen.getByRole("list", { name: "擅长" })).toBeVisible();
    expect(screen.getByText("前端实现")).toBeVisible();
    expect(screen.getByText("设计系统")).toBeVisible();
    expect(screen.getByText("3 个技能 · 2 个连接器")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "和 林可 互动" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("article")).toHaveAttribute("data-phase", "open");
  });

  it("没有擅长领域时给出空状态，载入中不抢先显示数量", () => {
    render(
      <WakerShowcaseCard
        connectorCount={0}
        countsPending
        name="林可"
        skillCount={0}
      />,
    );

    expect(screen.getByText("还没有填写擅长领域")).toBeVisible();
    expect(screen.getByText("正在清点技能和连接器")).toBeVisible();
    expect(screen.queryByText("0 个技能 · 0 个连接器")).not.toBeInTheDocument();
  });
});
