import { useLayoutEffect, useRef, type RefObject } from "react";

/** canonical 消息、前插历史及异步 Markdown 增高共用一个阅读锚点。 */
export function useChatViewportScroll({
  viewport,
  scope,
  revision,
  enabled = true,
  followInitially = true,
  isHistoryAnchoring,
}: {
  viewport: RefObject<HTMLDivElement | null>;
  scope: string;
  revision: unknown;
  enabled?: boolean;
  followInitially?: boolean;
  isHistoryAnchoring?: () => boolean;
}) {
  const refresh = useRef<(() => void) | undefined>(undefined);
  const saved = useRef<
    | {
        scope: string;
        following: boolean;
        anchor?: { element: HTMLElement; offset: number };
      }
    | undefined
  >(undefined);
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!enabled || !element) return;
    const previous =
      followInitially && saved.current?.scope === scope
        ? saved.current
        : undefined;
    let following = previous?.following ?? followInitially;
    let frame: number | undefined;
    let anchor = previous?.anchor;
    // 重新启用时，定位的原生滚动事件可能还未送达。
    let programmaticTop = followInitially ? undefined : element.scrollTop;
    const capture = () => {
      const box = element.getBoundingClientRect();
      const pointTargets =
        typeof document.elementsFromPoint === "function"
          ? document.elementsFromPoint(
              box.left + Math.min(24, box.width / 2),
              box.top + Math.min(12, element.clientHeight / 2),
            )
          : [];
      for (const candidate of pointTargets) {
        if (!element.contains(candidate)) continue;
        const target = candidate.closest<HTMLElement>(
          "p,pre,table,[data-chat-node-id],[data-message-id]",
        );
        if (target && target !== element && element.contains(target)) {
          anchor = {
            element: target,
            offset: target.getBoundingClientRect().top - box.top,
          };
          return;
        }
      }
      // 无命中检测的宿主只二分顶层消息；单条消息内最多检查64个元素。
      const column =
        element.querySelector("[data-chat-message-column]") ?? element;
      const messages = column.children;
      let low = 0,
        high = messages.length;
      while (low < high) {
        const middle = Math.floor((low + high) / 2);
        const message = messages.item(middle);
        if (message && message.getBoundingClientRect().bottom <= box.top)
          low = middle + 1;
        else high = middle;
      }
      const message = messages.item(low);
      if (!(message instanceof HTMLElement)) {
        anchor = undefined;
        return;
      }
      let target = message;
      let cursor: Element | null = message;
      for (let visited = 0; cursor && visited < 64; visited += 1) {
        const bounds = cursor.getBoundingClientRect();
        const visible =
          bounds.height > 0 &&
          bounds.bottom > box.top &&
          bounds.top < box.top + element.clientHeight;
        if (
          visible &&
          cursor instanceof HTMLElement &&
          cursor.matches("p,pre,table")
        ) {
          target = cursor;
          break;
        }
        if (visible && cursor.firstElementChild) {
          cursor = cursor.firstElementChild;
          continue;
        }
        while (cursor && cursor !== message && !cursor.nextElementSibling)
          cursor = cursor.parentElement;
        cursor =
          cursor && cursor !== message ? cursor.nextElementSibling : null;
      }
      anchor = {
        element: target,
        offset: target.getBoundingClientRect().top - box.top,
      };
    };
    const scroll = (top: number) => {
      const before = element.scrollTop;
      element.scrollTo({ top, behavior: "instant" });
      if (element.scrollTop !== before) programmaticTop = element.scrollTop;
    };
    const update = () => {
      if (document.hidden || element.clientHeight <= 0) return;
      if (following) {
        scroll(element.scrollHeight);
        return;
      }
      // 翻页请求由既有历史锚点负责一次补偿，避免两个滚动所有者重复移动。
      if (isHistoryAnchoring?.()) return;
      if (anchor && element.contains(anchor.element)) {
        const delta =
          anchor.element.getBoundingClientRect().top -
          element.getBoundingClientRect().top -
          anchor.offset;
        if (Math.abs(delta) > 0.5) scroll(element.scrollTop + delta);
        return;
      }
      capture();
    };
    const schedule = () => {
      if (frame !== undefined || document.hidden) return;
      frame = requestAnimationFrame(() => {
        frame = undefined;
        update();
      });
    };
    const onScroll = () => {
      if (
        programmaticTop !== undefined &&
        Math.abs(element.scrollTop - programmaticTop) < 1
      ) {
        programmaticTop = undefined;
        return;
      }
      programmaticTop = undefined;
      following =
        element.scrollHeight - element.scrollTop - element.clientHeight <= 24;
      if (following) anchor = undefined;
      else capture();
    };
    const observer = new ResizeObserver(schedule);
    const observe = () => {
      observer.observe(element);
      const content = element.querySelector("[data-chat-message-column]");
      if (content) observer.observe(content);
    };
    const visibility = () => {
      if (document.hidden) {
        observer.disconnect();
        if (frame !== undefined) cancelAnimationFrame(frame);
        frame = undefined;
      } else {
        observe();
        schedule();
      }
    };
    refresh.current = schedule;
    element.addEventListener("scroll", onScroll, { passive: true });
    document.addEventListener("visibilitychange", visibility);
    observe();
    update();
    return () => {
      saved.current = { scope, following, anchor };
      refresh.current = undefined;
      observer.disconnect();
      element.removeEventListener("scroll", onScroll);
      document.removeEventListener("visibilitychange", visibility);
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, [viewport, scope, enabled, followInitially, isHistoryAnchoring]);
  useLayoutEffect(() => {
    refresh.current?.();
  }, [revision]);
}
