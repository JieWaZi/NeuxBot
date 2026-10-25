import { isElementVisible } from "./element-visibility";
import { useEffect, useState, type RefObject } from "react";

type PresentationObserver = {
  root: HTMLElement;
  selector?: string;
  onVisible?: (element: HTMLElement) => void;
  onChange?: () => void;
};

// 只保存 DOM 观察器，不缓存通知、计数或阅读事实。全部消费者共用一组环境监听。
const observers = new Set<() => void>();
let disconnect: (() => void) | undefined;
let frame: number | undefined;

function refresh() {
  for (const observer of [...observers]) observer();
}

function schedule() {
  if (frame !== undefined) return;
  frame = requestAnimationFrame(() => {
    frame = undefined;
    refresh();
  });
}

function connect() {
  const mutation = new MutationObserver((records) => {
    // HTML 属性可能通过任意 aria/data 选择器改变呈现；SVG 动画帧不触发全局扫描。
    if (
      records.some(
        (record) =>
          record.type === "childList" || record.target instanceof HTMLElement,
      )
    )
      schedule();
  });
  mutation.observe(document.documentElement, {
    childList: true,
    subtree: true,
    attributes: true,
  });
  window.addEventListener("focus", refresh);
  window.addEventListener("blur", refresh);
  document.addEventListener("visibilitychange", refresh);
  document.addEventListener("scroll", schedule, true);
  window.addEventListener("resize", schedule);
  document.addEventListener("transitionend", schedule, true);
  document.addEventListener("animationend", schedule, true);
  disconnect = () => {
    mutation.disconnect();
    window.removeEventListener("focus", refresh);
    window.removeEventListener("blur", refresh);
    document.removeEventListener("visibilitychange", refresh);
    document.removeEventListener("scroll", schedule, true);
    window.removeEventListener("resize", schedule);
    document.removeEventListener("transitionend", schedule, true);
    document.removeEventListener("animationend", schedule, true);
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
  };
}

/** 内容进入真实前台视区的统一观察；延迟渲染、祖先折叠、滚动和回焦共用相同资格。 */
export function observeContentPresentation({
  root,
  selector,
  onVisible,
  onChange,
}: PresentationObserver): () => void {
  let alive = true;
  const intersections = new Map<HTMLElement, boolean>();
  const visible = new Set<HTMLElement>();
  let intersection: IntersectionObserver | undefined;
  const resize =
    typeof ResizeObserver === "undefined"
      ? undefined
      : new ResizeObserver(schedule);
  resize?.observe(root);
  const targets = () =>
    selector ? [...root.querySelectorAll<HTMLElement>(selector)] : [root];
  function update() {
    if (!alive) return;
    const current = new Set(targets());
    if (
      (onVisible || (selector && onChange)) &&
      current.size &&
      !intersection &&
      typeof IntersectionObserver !== "undefined"
    ) {
      intersection = new IntersectionObserver(
        (entries) => {
          if (!alive) return;
          for (const entry of entries) {
            if (
              entry.target instanceof HTMLElement &&
              intersections.has(entry.target)
            ) {
              intersections.set(
                entry.target,
                entry.isIntersecting &&
                  entry.intersectionRect.width > 0 &&
                  entry.intersectionRect.height > 0,
              );
            }
          }
          update();
        },
        { root: selector ? root : null, threshold: 0 },
      );
    }
    for (const element of intersections.keys()) {
      if (!current.has(element)) {
        intersection?.unobserve(element);
        resize?.unobserve(element);
        intersections.delete(element);
        visible.delete(element);
      }
    }
    for (const element of current) {
      if (intersection && !intersections.has(element)) {
        intersections.set(element, false);
        intersection.observe(element);
        resize?.observe(element);
      }
      const presented =
        intersections.get(element) === true && isElementVisible(element);
      if (presented && !visible.has(element)) {
        visible.add(element);
        onVisible?.(element);
      } else if (!presented) visible.delete(element);
    }
    onChange?.();
  }
  if (!observers.size) connect();
  observers.add(update);
  update();
  return () => {
    alive = false;
    intersection?.disconnect();
    resize?.disconnect();
    observers.delete(update);
    if (!observers.size) {
      disconnect?.();
      disconnect = undefined;
    }
  };
}

/** 可见资格只决定是否需要获取阅读凭据，本身不构成阅读或业务完成。 */
export function useContentVisibility(
  root: RefObject<HTMLElement | null>,
  enabled: boolean,
) {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const element = root.current;
    if (!enabled || !element) return;
    return observeContentPresentation({
      root: element,
      onChange: () => setVisible(isElementVisible(element)),
    });
  }, [root, enabled]);
  return enabled && visible;
}
