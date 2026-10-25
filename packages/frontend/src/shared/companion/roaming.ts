export type CompanionMotion = {
  move: (action: "walk" | "jump") => boolean;
  stop: () => void;
};

/** 横向位移交给浏览器合成；身体走路和跳跃仍使用形象原有的动作时钟。 */
export function companionRoaming(track: HTMLElement, actor: HTMLElement) {
  let animation: Animation | undefined;
  let position = 0;
  let hovering = false;
  const limit = () => Math.max(0, track.clientWidth - actor.offsetWidth);
  const place = (value: number) => {
    position = Math.max(0, Math.min(limit(), value));
    actor.style.transform = `translateX(${position}px)`;
  };
  const stop = () => {
    if (animation) {
      const current =
        actor.getBoundingClientRect().left - track.getBoundingClientRect().left;
      animation.cancel();
      animation = undefined;
      place(current);
    } else place(position);
  };
  const enter = () => {
    hovering = true;
    stop();
  };
  const leave = () => {
    hovering = false;
  };
  const observer =
    typeof ResizeObserver === "undefined"
      ? undefined
      : new ResizeObserver(stop);
  observer?.observe(track);
  observer?.observe(actor);
  actor.addEventListener("pointerenter", enter);
  actor.addEventListener("pointerleave", leave);
  actor.addEventListener("focusin", stop);
  return {
    move(action: "walk" | "jump") {
      if (hovering || actor.contains(document.activeElement)) return false;
      stop();
      const available = limit();
      // 保证有可见位移，但不让伙伴越过输入框边缘；窄布局原地跳跃。
      const direction = position < available / 2 ? 1 : -1;
      const distance =
        available *
        (0.25 + Math.random() * 0.35) *
        (action === "jump" ? 0.5 : 1);
      const target = Math.max(
        0,
        Math.min(available, position + direction * distance),
      );
      if (target !== position) {
        animation = actor.animate(
          [
            { transform: `translateX(${position}px)` },
            { transform: `translateX(${target}px)` },
          ],
          { duration: action === "walk" ? 3400 : 1550, easing: "ease-in-out" },
        );
        // 动画覆盖期间预置终点，完成后不用额外计时器或 React 每帧重绘。
        place(target);
      }
      return true;
    },
    stop,
    destroy() {
      stop();
      observer?.disconnect();
      actor.removeEventListener("pointerenter", enter);
      actor.removeEventListener("pointerleave", leave);
      actor.removeEventListener("focusin", stop);
    },
  } satisfies CompanionMotion & { destroy: () => void };
}
