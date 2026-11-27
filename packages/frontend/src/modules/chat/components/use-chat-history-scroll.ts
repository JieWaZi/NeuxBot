import {
  useLayoutEffect,
  useCallback,
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
  loading = false,
  enabled = true,
  load,
  onError,
}: {
  scope: string;
  viewport: RefObject<HTMLDivElement | null>;
  hasMore: boolean;
  loading?: boolean;
  enabled?: boolean;
  load: (signal: AbortSignal) => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const sessionRef = useRef<ScrollSession>(null);
  const [failure, setFailure] = useState<{ scope: string; error: unknown }>();
  const error = failure?.scope === scope ? failure.error : undefined;
  useLayoutEffect(() => {
    if (!enabled) return;
    const lifetime = new AbortController();
    const session: ScrollSession = { scope, signal: lifetime.signal };
    sessionRef.current = session;
    return () => {
      lifetime.abort();
      if (session.frame !== undefined) cancelAnimationFrame(session.frame);
    };
  }, [enabled, scope]);

  const loadEarlier = async () => {
    const session = sessionRef.current;
    if (
      !enabled ||
      !session ||
      session.signal.aborted ||
      session.scope !== scope
    )
      return;
    if (session.pending) return session.pending;
    if (!hasMore || loading || session.frame !== undefined) return;
    const request = (async () => {
      try {
        const { loadChatHistoryWithAnchor } =
          await import("./chat-history-anchor");
        if (session.signal.aborted) return;
        await loadChatHistoryWithAnchor(session, viewport, () =>
          load(session.signal),
        );
        if (session.signal.aborted) return;
        setFailure(undefined);
        return true;
      } catch (error) {
        if (session.signal.aborted) return;
        session.anchor = undefined;
        setFailure({ scope, error });
        onError(error);
        return false;
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
    if (!enabled || !session || session.signal.aborted) return;
    const previous = session.previousTop;
    session.previousTop = container.scrollTop;
    const programmaticTop = session.programmaticTop;
    session.programmaticTop = undefined;
    if (
      programmaticTop !== undefined &&
      Math.abs(container.scrollTop - programmaticTop) < 1
    )
      return;
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

  const isAnchoring = useCallback(
    () =>
      Boolean(sessionRef.current?.anchor && !sessionRef.current.signal.aborted),
    [],
  );
  const ignoreProgrammaticScroll = useCallback((top: number) => {
    if (sessionRef.current) sessionRef.current.programmaticTop = top;
  }, []);
  return {
    isAnchoring,
    ignoreProgrammaticScroll,
    loadEarlier,
    onScroll,
    error,
    clearError: () => setFailure(undefined),
  };
}
