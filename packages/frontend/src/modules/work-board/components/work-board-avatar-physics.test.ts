import { fireEvent } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { dropWorkBoardAvatars } from "./work-board-avatar-physics";

afterEach(() => vi.restoreAllMocks());

function animationClock() {
  const frames = new Map<number, FrameRequestCallback>();
  let nextFrame = 0;
  let time = 0;
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    frames.set(++nextFrame, callback);
    return nextFrame;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
    frames.delete(id);
  });
  function advance(count: number) {
    for (let frame = 0; frame < count && frames.size; frame++) {
      time += 1000 / 60;
      const callbacks = [...frames.values()];
      frames.clear();
      callbacks.forEach((callback) => callback(time));
    }
  }

  return { advance, frames };
}

function avatarStage() {
  const stage = document.createElement("div");
  Object.defineProperties(stage, {
    clientWidth: { value: 420 },
    clientHeight: { value: 200 },
  });
  const avatar = document.createElement("div");
  avatar.dataset.pileAvatar = "";
  avatar.style.setProperty("--pile-rest-x", "50%");
  avatar.style.setProperty("--pile-rest-angle", "0deg");
  Object.defineProperty(avatar, "offsetWidth", { value: 80 });
  stage.append(avatar);
  vi.spyOn(stage, "getBoundingClientRect").mockImplementation(
    () => new DOMRect(0, 0, 420, 200),
  );
  vi.spyOn(avatar, "getBoundingClientRect").mockImplementation(() => {
    const x =
      (Number.parseFloat(avatar.style.getPropertyValue("--pile-live-x")) ||
        50) * 4.2;
    const lift = avatar.style
      .getPropertyValue("--pile-live-y")
      .match(/\* ([\d.-]+)/)?.[1];
    const y = 188 - (lift ? Number(lift) * 80 : 28.8);
    return new DOMRect(x - 40, y - 40, 80, 80);
  });
  return { stage, avatar };
}

it("拨动后自然停在新的位置，不再拉回旧落点", () => {
  const { advance, frames } = animationClock();
  const { stage, avatar } = avatarStage();

  const dispose = dropWorkBoardAvatars(stage);
  try {
    advance(240);
    expect(frames.size).toBe(0);
    const restX = Number.parseFloat(
      avatar.style.getPropertyValue("--pile-live-x"),
    );
    fireEvent.pointerMove(stage, {
      pointerType: "mouse",
      clientX: 50,
      clientY: 100,
    });
    expect(frames.size).toBe(0);
    fireEvent.pointerMove(stage, {
      pointerType: "mouse",
      clientX: 190,
      clientY: 159,
    });
    fireEvent.pointerMove(stage, {
      pointerType: "mouse",
      clientX: 206,
      clientY: 159,
    });
    advance(8);
    expect(
      Number.parseFloat(avatar.style.getPropertyValue("--pile-live-x")),
    ).toBeGreaterThan(restX + 1);
    advance(180);
    expect(frames.size).toBe(0);
    expect(
      Number.parseFloat(avatar.style.getPropertyValue("--pile-live-x")),
    ).toBeGreaterThan(restX + 1);
    const settled = avatar.style.cssText;
    advance(120);
    expect(avatar.style.cssText).toBe(settled);
  } finally {
    dispose?.();
  }
  expect(frames.size).toBe(0);
});

it("落下途中暂停保留当前位置，恢复后继续落稳", () => {
  const { advance, frames } = animationClock();
  const { stage, avatar } = avatarStage();
  const pause = dropWorkBoardAvatars(stage);
  advance(12);
  const paused = avatar.style.cssText;
  pause?.();
  expect(avatar.style.cssText).toBe(paused);
  expect(avatar).toHaveAttribute("data-falling");
  expect(frames.size).toBe(0);

  const resume = dropWorkBoardAvatars(stage, false);
  try {
    const position = avatar.getBoundingClientRect();
    advance(1);
    const resumed = avatar.getBoundingClientRect();
    expect(Math.abs(resumed.y - position.y)).toBeLessThan(1);
    advance(360);
    expect(avatar.getBoundingClientRect().y).toBeGreaterThan(position.y + 20);
    expect(frames.size).toBe(0);
  } finally {
    const lastFrame = avatar.style.cssText;
    resume?.();
    expect(avatar.style.cssText).toBe(lastFrame);
  }
  expect(frames.size).toBe(0);
});

it("首个动画帧之前已定位到舞台上方", () => {
  animationClock();
  const { stage, avatar } = avatarStage();
  const dispose = dropWorkBoardAvatars(stage);
  try {
    expect(avatar).toHaveAttribute("data-falling");
    expect(avatar.getBoundingClientRect().bottom).toBeLessThanOrEqual(0);
  } finally {
    dispose?.();
  }
});
