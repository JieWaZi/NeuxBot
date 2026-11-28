import { useCallback, useLayoutEffect, useRef, type RefObject } from "react";
import type { QueryKey } from "@tanstack/react-query";
import type { QueryEventRefreshCommit } from "@/shared/api/use-query-events";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import type {
  PendingMainConversationInput,
  PublicMessage,
} from "../api/main-conversation";

const reducedMotionQuery = "(prefers-reduced-motion: reduce)";
const bubbleTravel = 12;

/** 只对可见主会话中实际到达的消息播放入场。 */
export function useMainConversationMotion({
  viewport,
  messages,
  localInputs,
  acceptedInputs,
  historyKey,
  historyUpdates,
  enabled,
}: {
  viewport: RefObject<HTMLDivElement | null>;
  messages: readonly PublicMessage[];
  localInputs?: ReadonlySet<string>;
  acceptedInputs?: readonly PendingMainConversationInput[];
  historyKey: QueryKey;
  historyUpdates: number;
  enabled: boolean;
}) {
  const baseline = useRef<
    | { sequence: number; ids: Set<string>; localInputs?: ReadonlySet<string> }
    | undefined
  >(undefined);
  const historyRefresh = useRef<QueryEventRefreshCommit | undefined>(undefined);
  const localPreviews = useRef(new Set<string>());
  const batch = useRef<
    { entering: Set<string>; previous: Set<string> } | undefined
  >(undefined);
  const animations = useRef(new Set<Animation>());
  const cancel = useCallback(() => {
    for (const animation of animations.current) animation.cancel();
    animations.current.clear();
  }, []);
  const onRefreshCommit = useCallback(
    (commit: QueryEventRefreshCommit) => {
      const { queryKey } = commit;
      if (
        queryKey.length !== historyKey.length ||
        queryKey.some((part, index) => part !== historyKey[index])
      )
        return;
      historyRefresh.current = commit;
    },
    [historyKey],
  );

  useLayoutEffect(() => {
    if (!enabled) return;
    const preference = window.matchMedia?.(reducedMotionQuery);
    const onPreferenceChange = (event: MediaQueryListEvent) => {
      if (event.matches) cancel();
    };
    const onVisibilityChange = () => {
      if (!document.hidden) return;
      batch.current = undefined;
      historyRefresh.current = undefined;
      cancel();
    };
    preference?.addEventListener?.("change", onPreferenceChange);
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      preference?.removeEventListener?.("change", onPreferenceChange);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      baseline.current = undefined;
      batch.current = undefined;
      historyRefresh.current = undefined;
      cancel();
    };
  }, [enabled, cancel]);

  useLayoutEffect(() => {
    batch.current = undefined;
    if (!enabled) return;
    const previous = baseline.current;
    const refresh = historyRefresh.current;
    const source =
      refresh?.dataUpdateCount === historyUpdates ? refresh.source : undefined;
    if (source) historyRefresh.current = undefined;
    const live = source === "change";
    const accepted = live
      ? new Set(acceptedInputs?.map(({ id }) => id))
      : undefined;
    const ids = new Set<string>();
    const entering = new Set<string>();
    let sequence = previous?.sequence ?? 0;
    for (const row of messages) {
      if (!row.id) continue;
      const id = row.id;
      ids.add(id);
      sequence = Math.max(sequence, row.sequence ?? 0);
      const local = localInputs?.has(id) && !localPreviews.current.has(id);
      if (local) localPreviews.current.add(id);
      // 发送回执的对账可能先于推送读到本次提交产生的回复。
      const replyToLocalInput =
        source !== "recovery" &&
        row.role === "assistant" &&
        row.replyToMessageId &&
        previous?.localInputs?.has(row.replyToMessageId);
      const appended =
        !localPreviews.current.has(id) &&
        (accepted?.has(id) ||
          ((live || replyToLocalInput) &&
            row.sequence !== undefined &&
            previous &&
            row.sequence > previous.sequence));
      if (previous && !previous.ids.has(id) && (local || appended))
        entering.add(id);
    }
    baseline.current = { sequence, ids, localInputs };
    if (!document.hidden && entering.size && previous)
      batch.current = { entering, previous: previous.ids };
  }, [acceptedInputs, enabled, historyUpdates, localInputs, messages]);

  const onFollowRevision = useCallback(
    (distance: number) => {
      const arrivals = batch.current;
      batch.current = undefined;
      const element = viewport.current;
      if (
        !arrivals ||
        !element?.clientHeight ||
        window.matchMedia?.(reducedMotionQuery).matches
      )
        return;
      const column = element.querySelector("[data-chat-message-column]");
      if (!column) return;
      const { duration, ease } = readLayoutMotionTokens("--duration-standard");
      if (!duration) return;
      const options = {
        duration: duration * 1000,
        easing: ease ? `cubic-bezier(${ease.join(",")})` : "ease-out",
      };
      const animate = (
        node: HTMLElement,
        frames: Keyframe[],
        timing: KeyframeAnimationOptions,
      ) => {
        const animation = node.animate?.(frames, timing);
        if (!animation) return;
        animations.current.add(animation);
        const release = () => animations.current.delete(animation);
        void animation.finished.then(release, release);
      };
      const push = distance > 0;
      const bounds = element.getBoundingClientRect();
      const maxTravel = push ? Math.max(distance, bubbleTravel) : bubbleTravel;
      for (let index = column.children.length - 1; index >= 0; index--) {
        const row = column.children.item(index);
        if (!(row instanceof HTMLElement)) continue;
        const { messageId, chatTimeDivider } = row.dataset;
        const id = messageId ?? chatTimeDivider;
        if (!id) continue;
        const rect = row.getBoundingClientRect();
        if (rect.bottom + maxTravel <= bounds.top) break;
        if (rect.top >= bounds.bottom) continue;
        const entering = arrivals.entering.has(id);
        if (push && (entering || arrivals.previous.has(id))) {
          // 滚动坐标已贴底，旧消息和新消息一起从原视口位置向上归位。
          animate(
            row,
            [
              { transform: `translateY(${distance}px)` },
              { transform: "translateY(0)" },
            ],
            { ...options, composite: "add" },
          );
        }
        if (!entering || !messageId || rect.bottom <= bounds.top) continue;
        const transformOrigin =
          row.dataset.role === "user" ? "right bottom" : "left bottom";
        const bubbles = row.querySelectorAll<HTMLElement>(
          "[data-chat-message-bubble]",
        );
        for (const bubble of bubbles)
          animate(
            bubble,
            [
              {
                transform: `translateY(${bubbleTravel}px) scale(0.94)`,
                opacity: 0,
                transformOrigin,
                easing: options.easing,
              },
              {
                offset: 0.7,
                transform: "translateY(0) scale(1.01)",
                opacity: 1,
                transformOrigin,
                easing: "ease-out",
              },
              {
                transform: "translateY(0) scale(1)",
                opacity: 1,
                transformOrigin,
              },
            ],
            { duration: options.duration, fill: "backwards" },
          );
      }
    },
    [viewport],
  );
  return { onRefreshCommit, onFollowRevision, onUserScroll: cancel };
}
