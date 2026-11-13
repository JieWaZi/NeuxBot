import { fireEvent } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { dropWorkBoardAvatars } from "./work-board-avatar-physics";

afterEach(() => vi.restoreAllMocks());

it("落定后可以局部拨动头像，回稳后停止绘制", () => {
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

  const stage = document.createElement("div");
  Object.defineProperties(stage, {
    clientWidth: { value: 420 },
    clientHeight: { value: 200 },
  });
  const avatar = document.createElement("div");
  avatar.dataset.pileAvatar = "";
  avatar.style.setProperty("--pile-rest-x", "50%");
  Object.defineProperty(avatar, "offsetWidth", { value: 80 });
  stage.append(avatar);

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
    ).toBeCloseTo(restX, 0);
  } finally {
    dispose?.();
  }
  expect(frames.size).toBe(0);
});
