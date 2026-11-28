import { useContext, useLayoutEffect, useRef } from "react";

import { ContentVisibility } from "@/shared/lib/content-visibility";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";

function measureHighlight(track: HTMLDivElement) {
  const highlight = track.querySelector<HTMLElement>(
    "[data-scene-tab-highlight]",
  );
  const selected = track.querySelector<HTMLElement>(
    "[data-scene-tab-item][data-selected]:not([data-invalid])",
  );
  if (!highlight) return;
  const width = selected?.offsetWidth ?? 0;
  if (selected && width) {
    highlight.style.translate = `${selected.offsetLeft}px`;
    highlight.style.width = `${width}px`;
  }
  track.toggleAttribute("data-tab-highlight-ready", width > 0);
}

function measureTabs(track: HTMLDivElement) {
  return new Map(
    Array.from(
      track.querySelectorAll<HTMLElement>(
        "[data-scene-tab-item]:not([data-fixed]), [data-scene-tab-trailing]",
      ),
      (item) => [item, item.offsetLeft],
    ),
  );
}

/** 在已提交的页签布局上补间位置，不延长关闭项的交互或内容生命周期。 */
export function useSceneTabMotion(enabled = true) {
  const visible = useContext(ContentVisibility) && enabled;
  const trackRef = useRef<HTMLDivElement>(null);
  const previous = useRef(new Map<HTMLElement, number>());
  const running = useRef(new Map<HTMLElement, Animation>());

  useLayoutEffect(() => {
    const track = trackRef.current;
    if (!track || !visible) return;
    const animations = running.current;
    const preference = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const updateMotion = () => {
      const paused = document.hidden || Boolean(preference?.matches);
      track.toggleAttribute("data-tab-motion-paused", paused);
      if (paused) {
        for (const animation of animations.values()) animation.cancel();
        animations.clear();
      }
    };
    const onScroll = () => {
      track.toggleAttribute("data-tab-track-scrolled", track.scrollLeft !== 0);
      if (
        track.querySelector("[data-scene-tab-item][data-fixed][data-selected]")
      )
        measureHighlight(track);
    };
    const resize = () => {
      measureHighlight(track);
      previous.current = measureTabs(track);
    };
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(resize);
    observer?.observe(track);
    const list = track.querySelector("[role=tablist]");
    if (list) observer?.observe(list);
    track.addEventListener("scroll", onScroll, { passive: true });
    document.addEventListener("visibilitychange", updateMotion);
    preference?.addEventListener?.("change", updateMotion);
    updateMotion();
    onScroll();
    return () => {
      observer?.disconnect();
      track.removeEventListener("scroll", onScroll);
      document.removeEventListener("visibilitychange", updateMotion);
      preference?.removeEventListener?.("change", updateMotion);
      previous.current.clear();
      for (const animation of animations.values()) animation.cancel();
      animations.clear();
    };
  }, [visible]);

  useLayoutEffect(() => {
    const track = trackRef.current;
    if (!track || !visible) return;
    measureHighlight(track);
    const before = previous.current;
    const after = measureTabs(track);
    previous.current = after;
    if (track.hasAttribute("data-tab-motion-paused")) return;
    if (![...before.keys()].some((item) => !after.has(item))) return;

    const { duration, ease } = readLayoutMotionTokens("--duration-standard");
    if (duration === 0) return;
    const easing = ease ? `cubic-bezier(${ease.join(", ")})` : undefined;
    for (const [item, from] of before) {
      const to = after.get(item);
      const animation = running.current.get(item);
      if (to === from) continue;
      const displacement = animation
        ? Number.parseFloat(getComputedStyle(item).translate) || 0
        : 0;
      animation?.cancel();
      running.current.delete(item);
      if (to === undefined || !item.animate) continue;
      const slide = item.animate(
        [{ translate: `${from - to + displacement}px` }, { translate: "0px" }],
        { duration: duration * 1000, easing },
      );
      running.current.set(item, slide);
      slide.onfinish = () => {
        if (running.current.get(item) === slide) running.current.delete(item);
      };
    }
  });
  return trackRef;
}
