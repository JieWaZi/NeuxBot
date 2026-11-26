import { useEffect, useState, type RefObject } from "react";

const listeners = new Map<Element, (visible: boolean) => void>();
let observer: IntersectionObserver | undefined;

function observePatch(target: Element, onChange: (visible: boolean) => void) {
  observer ??= new IntersectionObserver(
    (entries, current) => {
      if (current !== observer) return;
      for (const { target, isIntersecting } of entries) {
        listeners.get(target)?.(isIntersecting);
      }
    },
    { scrollMargin: "240px 0px" },
  );
  listeners.set(target, onChange);
  observer.observe(target);
  return () => {
    listeners.delete(target);
    observer?.unobserve(target);
    if (listeners.size === 0) {
      observer?.disconnect();
      observer = undefined;
    }
  };
}

/** 只有视区及邻近的补丁参与自动重读，已缓存内容保留原来的滚动高度。 */
export function useDiffPatchViewport(
  content: RefObject<HTMLElement | null>,
  active: boolean,
): boolean {
  const [visible, setVisible] = useState(false);
  if (!active && visible) setVisible(false);

  useEffect(() => {
    if (
      !active ||
      !content.current ||
      typeof IntersectionObserver === "undefined"
    )
      return;
    return observePatch(content.current, setVisible);
  }, [active, content]);

  return active && (visible || typeof IntersectionObserver === "undefined");
}
