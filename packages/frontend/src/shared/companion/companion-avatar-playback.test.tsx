import { act, fireEvent, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { BotEngine } from "@neuxbot/bloub/engine";
import { enlargedEyes } from "@neuxbot/bloub/eyes";
import { REST_LOOK } from "@neuxbot/bloub/motion";
import { SHAPE_BY_ID } from "@neuxbot/bloub/skins";
import { CompanionAvatar } from "./companion-avatar";
import { defaultCompanionAvatar } from "./appearance";
import { avatarExpressionById } from "./avatar-expressions";

vi.mock("./prepared-appearance", async () => {
  const { companionAppearance } = await import("./appearance");
  return {
    usePreparedAppearance: (request: string) => ({
      request,
      appearance: companionAppearance(request),
      status: "ready",
      texture: "data:image/png;base64,test",
    }),
  };
});

afterEach(() => {
  vi.restoreAllMocks();
});

function frames(hz = 60) {
  let now = 0;
  let sequence = 0;
  const pending = new Map<number, FrameRequestCallback>();
  vi.spyOn(performance, "now").mockImplementation(() => now);
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    pending.set(++sequence, callback);
    return sequence;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
    pending.delete(id);
  });
  return {
    pending,
    advance(milliseconds: number) {
      const count = Math.round((milliseconds * hz) / 1000);
      for (let index = 0; index < count; index++) {
        now += 1000 / hz;
        const callbacks = [...pending.values()];
        pending.clear();
        act(() => callbacks.forEach((callback) => callback(now)));
      }
    },
  };
}

function eyes(container: HTMLElement) {
  return [...container.querySelectorAll("[data-eye]")].map((eye) => [
    eye.getAttribute("d"),
    eye.getAttribute("transform"),
  ]);
}

it.each([60, 75, 90, 120])(
  "%sHz 的连续预览稳定采样30fps，不丢弃时间余量",
  (hz) => {
    const clock = frames(hz);
    const sample = vi.spyOn(BotEngine.prototype, "sample");
    render(<CompanionAvatar avatar={defaultCompanionAvatar} preview />);
    sample.mockClear();
    clock.advance(6000);
    expect(sample.mock.calls.length).toBeGreaterThanOrEqual(179);
    expect(sample.mock.calls.length).toBeLessThanOrEqual(180);
  },
);

it("空闲完成过渡后停止回调，暂停期间保留已显示的眼睛", () => {
  const clock = frames();
  const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
  clock.advance(2000);
  expect(clock.pending.size).toBe(0);
  const frozen = eyes(view.container);
  clock.advance(5000);
  expect(eyes(view.container)).toEqual(frozen);
});

it("空闲暂停后更换表情继续原生过渡，中途再次切换保持连续并最终停稳", () => {
  const clock = frames();
  const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
  clock.advance(2000);
  const resting = eyes(view.container);
  view.rerender(
    <CompanionAvatar avatar={defaultCompanionAvatar} expression="surpris" />,
  );
  expect(eyes(view.container)).toEqual(resting);
  clock.advance(100);
  const changing = eyes(view.container);
  expect(changing).not.toEqual(resting);
  view.rerender(
    <CompanionAvatar avatar={defaultCompanionAvatar} expression="complice" />,
  );
  expect(eyes(view.container)).toEqual(changing);
  clock.advance(2000);
  expect(clock.pending.size).toBe(0);
  expect(eyes(view.container)).not.toEqual(resting);
});

it("悬停唤醒，移开完成回程后停稳，不跳过中途表情", () => {
  const clock = frames();
  vi.spyOn(Math, "random").mockReturnValue(0.6);
  const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
  clock.advance(2000);
  const resting = eyes(view.container);
  const host = view.container.firstElementChild;
  if (!host) throw new Error("缺少头像容器");
  fireEvent.pointerEnter(host);
  expect(eyes(view.container)).toEqual(resting);
  clock.advance(100);
  expect(clock.pending.size).toBe(1);
  const changing = eyes(view.container);
  fireEvent.pointerLeave(host);
  expect(eyes(view.container)).toEqual(changing);
  clock.advance(2000);
  expect(clock.pending.size).toBe(0);
  expect(view.container.querySelector("svg")).toHaveAttribute(
    "data-expression",
    "neutre",
  );
});

it("隐藏和恢复保留悬停中途姿态，隐藏时间不计入头像时钟", () => {
  const clock = frames();
  const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  vi.spyOn(Math, "random").mockReturnValue(0.6);
  const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
  clock.advance(2000);
  const host = view.container.firstElementChild;
  if (!host) throw new Error("缺少头像容器");
  fireEvent.pointerEnter(host);
  clock.advance(83);
  const changing = eyes(view.container);
  act(() => {
    hidden.mockReturnValue(true);
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(eyes(view.container)).toEqual(changing);
  clock.advance(5000);
  expect(clock.pending.size).toBe(0);
  act(() => {
    hidden.mockReturnValue(false);
    document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(eyes(view.container)).toEqual(changing);
  clock.advance(100);
  expect(eyes(view.container)).not.toEqual(changing);
});

it("空闲停稳后切换到持续预览能唤醒原时钟", () => {
  const clock = frames();
  const view = render(<CompanionAvatar avatar={defaultCompanionAvatar} />);
  clock.advance(2000);
  expect(clock.pending.size).toBe(0);
  const frozen = eyes(view.container);
  view.rerender(<CompanionAvatar avatar={defaultCompanionAvatar} preview />);
  clock.advance(300);
  expect(clock.pending.size).toBe(1);
  expect(eyes(view.container)).not.toEqual(frozen);
});

it("普通空闲眼睛沿用毛绒REST_LOOK的小幅游移", () => {
  const clock = frames();
  const sample = vi.spyOn(BotEngine.prototype, "sample");
  const view = render(
    <CompanionAvatar avatar={defaultCompanionAvatar} preview />,
  );
  clock.advance(2000);
  const time = sample.mock.calls.at(-1)?.[0] ?? 0;
  const reference = new BotEngine(
    100,
    "idle",
    SHAPE_BY_ID.get("cercle")?.radii,
    avatarExpressionById.get("neutre"),
  );
  reference.setLook(REST_LOOK, 0);
  const expected = enlargedEyes(
    enlargedEyes(reference.sample(time).eyes, 1.3),
  ).map((eye) => eye.matrix);
  expect(
    [...view.container.querySelectorAll("[data-eye]")].map((eye) =>
      eye.getAttribute("transform"),
    ),
  ).toEqual(expected);
});
