import type { RefObject } from "react";

export type ScrollSession = {
  scope: string;
  live: boolean;
  pending?: Promise<void>;
  frame?: number;
  previousTop?: number;
  anchor?: { element: HTMLElement; offset: number };
};

/** 前插历史时维护可见消息锚点；只在真正翻页时加载 DOM 协调逻辑。 */
export async function loadChatHistoryWithAnchor(
  session: ScrollSession,
  viewport: RefObject<HTMLDivElement | null>,
  load: () => Promise<void>,
) {
  const container = viewport.current;
  const previousHeight = container?.scrollHeight ?? 0;
  const previousTop = container?.scrollTop ?? 0;
  session.previousTop = previousTop;
  const viewportTop = container?.getBoundingClientRect().top ?? 0;
  const anchor =
    container &&
    Array.from(
      container.querySelectorAll<HTMLElement>("[data-message-id]"),
    ).find((element) => element.getBoundingClientRect().bottom > viewportTop);
  session.anchor = anchor
    ? {
        element: anchor,
        offset: anchor.getBoundingClientRect().top - viewportTop,
      }
    : undefined;
  await load();
  if (!session.live || viewport.current !== container) return;
  session.frame = requestAnimationFrame(() => {
    if (!session.live || !container || viewport.current !== container) return;
    // 按可见消息的实际位移补偿，浏览器已完成原生锚定时位移为零。
    if (session.anchor && container.contains(session.anchor.element)) {
      container.scrollTop +=
        session.anchor.element.getBoundingClientRect().top -
        container.getBoundingClientRect().top -
        session.anchor.offset;
    } else {
      container.scrollTop =
        (session.previousTop ?? previousTop) +
        Math.max(0, container.scrollHeight - previousHeight);
    }
    session.previousTop = container.scrollTop;
    session.anchor = undefined;
    session.frame = undefined;
  });
}
