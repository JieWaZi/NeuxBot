import {
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
  type UIEvent,
} from "react";

import type { ScrollSession } from "./chat-history-anchor";

const historyTopThreshold = 80;

/** 管理向前翻页的并发、滚动锚定和会话切换；不会在挂载时自动读完整历史。 */
export function useChatHistoryScroll({
  scope,
  viewport,
  hasMore,
  loading,
  load,
  onError,
}: {
  scope: string;
  viewport: RefObject<HTMLDivElement | null>;
  hasMore: boolean;
  loading: boolean;
  load: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const sessionRef = useRef<ScrollSession>({ scope, live: true });
  const [failure, setFailure] = useState<{ scope: string; error: unknown }>();
  const error = failure?.scope === scope ? failure.error : undefined;
  useLayoutEffect(() => {
    const session: ScrollSession = { scope, live: true };
    sessionRef.current = session;
    return () => {
      session.live = false;
      if (session.frame !== undefined) cancelAnimationFrame(session.frame);
    };
  }, [scope]);

  const loadEarlier = async () => {
    const session = sessionRef.current;
    if (session.pending) return session.pending;
    if (
      !session.live ||
      session.scope !== scope ||
      !hasMore ||
      loading ||
      session.frame !== undefined
    )
      return;
    const request = (async () => {
      try {
        const { loadChatHistoryWithAnchor } =
          await import("./chat-history-anchor");
        if (!session.live) return;
        await loadChatHistoryWithAnchor(session, viewport, load);
        if (session.live) setFailure(undefined);
      } catch (error) {
        if (!session.live) return;
        session.anchor = undefined;
        setFailure({ scope, error });
        onError(error);
      } finally {
        session.pending = undefined;
      }
    })();
    session.pending = request;
    return request;
  };

  const onScroll = (event: UIEvent<HTMLDivElement>) => {
    const container = event.currentTarget;
    const session = sessionRef.current;
    const previous = session.previousTop;
    session.previousTop = container.scrollTop;
    if (session.anchor) {
      // Viewport 已关闭原生锚定；只按实际滚动量移动目标，不把前插布局位移写回基线。
      session.anchor.offset -=
        container.scrollTop - (previous ?? container.scrollTop);
    }
    if (
      previous !== undefined &&
      container.scrollTop < previous &&
      container.scrollTop <= historyTopThreshold &&
      container.scrollHeight > container.clientHeight &&
      !error
    )
      void loadEarlier();
  };

  return {
    loadEarlier,
    onScroll,
    error,
    clearError: () => setFailure(undefined),
  };
}
