import {
  createContext,
  useContext,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";

type AnchorIdentity = {
  attribute: "data-chat-node-id" | "data-message-id";
  id: string;
  offset: number;
};
type ReadingAnchor = {
  element: HTMLElement;
  offset: number;
  identities: readonly AnchorIdentity[];
};
export type ChatViewportPositions = Map<
  string,
  { following: boolean; identities: readonly AnchorIdentity[]; top: number }
>;
export const ChatViewportPositionContext = createContext<
  ChatViewportPositions | undefined
>(undefined);

/** canonical 消息、前插历史及异步 Markdown 增高共用一个阅读锚点。 */
export function useChatViewportScroll({
  viewport,
  scope,
  revision,
  enabled = true,
  followInitially = true,
  isHistoryAnchoring,
  onProgrammaticScroll,
  onFollowRevision,
  onUserScroll,
}: {
  viewport: RefObject<HTMLDivElement | null>;
  scope: string;
  revision: unknown;
  enabled?: boolean;
  followInitially?: boolean;
  isHistoryAnchoring?: () => boolean;
  onProgrammaticScroll?: (top: number) => void;
  onFollowRevision?: (distance: number) => void;
  onUserScroll?: () => void;
}) {
  const refresh = useRef<(() => number | undefined) | undefined>(undefined);
  const notifyFollowRevision = useEffectEvent((distance: number) => {
    onFollowRevision?.(distance);
  });
  const notifyUserScroll = useEffectEvent(() => {
    onUserScroll?.();
  });
  const [localPositions] = useState<ChatViewportPositions>(() => new Map());
  const positions = useContext(ChatViewportPositionContext) ?? localPositions;
  useLayoutEffect(() => {
    const element = viewport.current;
    if (!enabled || !element) return;
    const previous = followInitially ? positions.get(scope) : undefined;
    let following = previous?.following ?? followInitially;
    let anchor: ReadingAnchor | undefined;
    let restore = previous;
    let latestTop = previous?.top ?? element.scrollTop;
    // 重新启用时，定位的原生滚动事件可能还未送达。
    let programmaticTop = followInitially ? undefined : element.scrollTop;
    const remember = (target: HTMLElement, viewportTop: number) => {
      const identities: AnchorIdentity[] = [];
      for (const attribute of [
        "data-chat-node-id",
        "data-message-id",
      ] as const) {
        const owner = target.closest<HTMLElement>(`[${attribute}]`);
        const id = owner?.getAttribute(attribute);
        if (owner && id && element.contains(owner)) {
          identities.push({
            attribute,
            id,
            offset: owner.getBoundingClientRect().top - viewportTop,
          });
        }
      }
      anchor = {
        element: target,
        offset: target.getBoundingClientRect().top - viewportTop,
        identities,
      };
    };
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
          remember(target, box.top);
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
      remember(target, box.top);
    };
    const scroll = (top: number) => {
      const before = element.scrollTop;
      if (Math.abs(top - before) > 0.5)
        element.scrollTo({ top, behavior: "instant" });
      latestTop = element.scrollTop;
      const distance = latestTop - before;
      if (distance !== 0) {
        programmaticTop = latestTop;
        onProgrammaticScroll?.(latestTop);
      }
      return distance;
    };
    const update = () => {
      if (document.hidden || element.clientHeight <= 0) return;
      if (following) {
        restore = undefined;
        return scroll(Math.max(0, element.scrollHeight - element.clientHeight));
      }
      // 翻页请求由既有历史锚点负责一次补偿，避免两个滚动所有者重复移动。
      if (isHistoryAnchoring?.()) return;
      if (anchor && !element.contains(anchor.element)) {
        restore = {
          following: false,
          identities: anchor.identities,
          top: latestTop,
        };
        anchor = undefined;
      }
      if (restore) {
        const position = restore;
        // 只在重新进入或原节点消失时按稳定身份查找，常规增量直接复用已测量的节点。
        for (const { attribute, id, offset } of position.identities) {
          const restored = Array.from(
            element.querySelectorAll<HTMLElement>(`[${attribute}]`),
          ).find((candidate) => candidate.getAttribute(attribute) === id);
          if (restored) {
            anchor = {
              element: restored,
              offset,
              identities: position.identities,
            };
            break;
          }
        }
        // 历史恢复前可能只有占位内容，保留锚点给首批真实消息。
        if (
          !anchor &&
          !element.querySelector("[data-chat-node-id],[data-message-id]")
        )
          return;
        restore = undefined;
        if (!anchor) scroll(position.top);
      }
      if (anchor) {
        const delta =
          anchor.element.getBoundingClientRect().top -
          element.getBoundingClientRect().top -
          anchor.offset;
        if (Math.abs(delta) > 0.5) scroll(element.scrollTop + delta);
        return;
      }
      capture();
    };
    const onScroll = () => {
      if (restore && element.scrollHeight <= element.clientHeight) return;
      if (
        programmaticTop !== undefined &&
        Math.abs(element.scrollTop - programmaticTop) < 1
      ) {
        programmaticTop = undefined;
        return;
      }
      programmaticTop = undefined;
      restore = undefined;
      if (element.scrollTop !== latestTop) notifyUserScroll();
      latestTop = element.scrollTop;
      following =
        element.scrollHeight - element.scrollTop - element.clientHeight <= 24;
      if (following) anchor = undefined;
      else capture();
    };
    const observer = new ResizeObserver(update);
    const observe = () => {
      observer.observe(element);
      const content = element.querySelector("[data-chat-message-column]");
      if (content) observer.observe(content);
    };
    const visibility = () => {
      if (document.hidden) {
        observer.disconnect();
      } else {
        observe();
        update();
      }
    };
    refresh.current = update;
    element.addEventListener("scroll", onScroll, { passive: true });
    document.addEventListener("visibilitychange", visibility);
    observe();
    update();
    return () => {
      positions.delete(scope);
      positions.set(scope, {
        following,
        identities: anchor?.identities ?? restore?.identities ?? [],
        top: latestTop,
      });
      if (positions.size > 10) {
        const oldest = positions.keys().next().value;
        if (oldest !== undefined) positions.delete(oldest);
      }
      refresh.current = undefined;
      observer.disconnect();
      element.removeEventListener("scroll", onScroll);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [
    viewport,
    scope,
    enabled,
    followInitially,
    isHistoryAnchoring,
    onProgrammaticScroll,
    positions,
  ]);
  useLayoutEffect(() => {
    notifyFollowRevision(refresh.current?.() ?? 0);
  }, [revision]);
}
