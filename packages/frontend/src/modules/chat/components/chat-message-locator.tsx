import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { useVisibleScene } from "@/shared/api/chat-reading";

export function useChatMessageLocator({
  viewport,
  active,
  routeKey,
  messageId,
  cancelReplyLocation,
  keyboardReplyTrigger,
  requestedInteractionId,
  requestedInteractionScope,
  hasEarlierMessages = false,
  onLoadEarlier,
  onProgrammaticScroll,
  inlineReplyFeedback = false,
}: {
  viewport: RefObject<HTMLDivElement | null>;
  active: boolean;
  routeKey?: string;
  messageId?: string;
  cancelReplyLocation?: () => void;
  keyboardReplyTrigger?: HTMLButtonElement;
  requestedInteractionId?: string;
  requestedInteractionScope?: string;
  hasEarlierMessages?: boolean;
  onLoadEarlier: () => Promise<boolean | "unavailable" | undefined>;
  onProgrammaticScroll: (top: number) => void;
  inlineReplyFeedback?: boolean;
}) {
  const sceneVisible = useVisibleScene();
  const { t } = useTranslation(["workBoard", "chat", "common"]);
  const locatingReply = Boolean(cancelReplyLocation);
  const [batch, setBatch] = useState(0);
  const key = messageId ? JSON.stringify([routeKey, messageId]) : undefined;
  const [progress, setProgress] = useState<{
    key: string;
    status: "loading" | "more" | "missing" | "error" | "positioned";
  }>();
  // Scene 保留挂载。离开后使旧定位失效，恢复前台时先定位再开放阅读。
  if ((!active || !sceneVisible) && progress) setProgress(undefined);
  const status = progress?.key === key ? progress?.status : undefined;
  const history = useRef({ hasEarlierMessages, onLoadEarlier });
  useLayoutEffect(() => {
    history.current = { hasEarlierMessages, onLoadEarlier };
  }, [hasEarlierMessages, onLoadEarlier]);
  useLayoutEffect(() => {
    const id = messageId ?? requestedInteractionId;
    const container = viewport.current;
    if (!active || !sceneVisible || !routeKey || !id || !container) return;
    let moved = false;
    let live = true;
    let loadingHistory = false;
    let highlighted: HTMLElement | undefined;
    let highlightTimer: ReturnType<typeof setTimeout> | undefined;
    const update = (status: NonNullable<typeof progress>["status"]) => {
      if (key && live) setProgress({ key, status });
    };
    const move = () => {
      if (moved || !live || loadingHistory) return moved;
      const target = messageId
        ? container.querySelector<HTMLElement>(
            `[data-message-id="${CSS.escape(id)}"]`,
          )
        : Array.from(
            container.querySelectorAll<HTMLElement>(
              "[data-chat-elicitation-card], [data-chat-interaction-id]",
            ),
          ).find(
            (card) =>
              (card.dataset.messageId === id ||
                card.dataset.chatInteractionId === id) &&
              (!requestedInteractionScope ||
                card.closest<HTMLElement>("[data-chat-interaction-scope]")
                  ?.dataset.chatInteractionScope === requestedInteractionScope),
          );
      if (target) {
        const previousTop = container.scrollTop;
        target.scrollIntoView({ block: locatingReply ? "center" : "start" });
        if (container.scrollTop !== previousTop)
          onProgrammaticScroll(container.scrollTop);
        const bubble = locatingReply
          ? target.querySelector<HTMLElement>("[data-chat-message-bubble]")
          : undefined;
        if (bubble) {
          if (keyboardReplyTrigger === target.ownerDocument.activeElement)
            bubble.focus({ preventScroll: true });
          highlighted = bubble;
          bubble.setAttribute("data-chat-message-located", "");
          highlightTimer = setTimeout(() => {
            bubble.removeAttribute("data-chat-message-located");
          }, 1200);
        }
        moved = true;
        update("positioned");
      }
      return moved;
    };
    const locate = async () => {
      if (move()) return;
      // 待办定位等待当前会话的待处理请求到达。审批保留在输入区。
      if (!messageId) return;
      update("loading");
      // 每次定位最多读取二十个原历史窗口，较远结果由用户继续，避免无界加载。
      for (let page = 0; live && !moved && page < 20; page++) {
        const current = history.current;
        if (!current.hasEarlierMessages) break;
        loadingHistory = true;
        const loaded = await current.onLoadEarlier();
        if (loaded) {
          await new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          );
        }
        loadingHistory = false;
        if (!live) return;
        if (loaded === "unavailable") {
          update("missing");
          return;
        }
        if (loaded === false) {
          update("error");
          return;
        }
        if (move()) return;
        if (!loaded) break;
      }
      if (!moved)
        update(history.current.hasEarlierMessages ? "more" : "missing");
    };
    void locate();
    const observer = new MutationObserver(move);
    observer.observe(container, {
      childList: true,
      subtree: true,
      attributeFilter: requestedInteractionId
        ? ["data-chat-interaction-id"]
        : undefined,
    });
    return () => {
      live = false;
      observer.disconnect();
      clearTimeout(highlightTimer);
      highlighted?.removeAttribute("data-chat-message-located");
    };
  }, [
    active,
    sceneVisible,
    routeKey,
    key,
    keyboardReplyTrigger,
    locatingReply,
    messageId,
    onProgrammaticScroll,
    viewport,
    batch,
    requestedInteractionId,
    requestedInteractionScope,
  ]);
  const feedback =
    active && sceneVisible && messageId && status && status !== "positioned" ? (
      <div
        role="status"
        className={
          inlineReplyFeedback
            ? "text-user-message-content/75 type-ui mb-1 flex flex-wrap items-center gap-x-2"
            : "text-content-muted type-ui flex shrink-0 items-center gap-3 px-6 py-2"
        }
      >
        {locatingReply
          ? t(
              {
                loading: "chat:locatingReply",
                missing: "chat:replyUnavailable",
                error: "chat:replyLocateFailed",
                more: "chat:replyFurther",
              }[status],
            )
          : t(
              {
                loading: "locatingResult",
                missing: "resultUnavailable",
                error: "locateFailed",
                more: "resultFurther",
              }[status],
            )}
        {status === "more" || status === "error" ? (
          <Button
            size="sm"
            variant={inlineReplyFeedback ? "plain" : "ghost"}
            className={
              inlineReplyFeedback
                ? "text-user-message-content hover:text-user-message-content active:text-user-message-content"
                : undefined
            }
            onClick={() => setBatch((value) => value + 1)}
          >
            {t(
              inlineReplyFeedback && status === "error"
                ? "common:retry"
                : "continueLocating",
            )}
          </Button>
        ) : null}
        {cancelReplyLocation &&
        (!inlineReplyFeedback || status === "loading") ? (
          <Button
            size="sm"
            variant={inlineReplyFeedback ? "plain" : "ghost"}
            className={
              inlineReplyFeedback
                ? "text-user-message-content hover:text-user-message-content active:text-user-message-content"
                : undefined
            }
            onClick={cancelReplyLocation}
          >
            {t(status === "loading" ? "common:cancel" : "common:close")}
          </Button>
        ) : null}
      </div>
    ) : null;
  return {
    positioning: Boolean(
      messageId &&
      (locatingReply
        ? !status || status === "loading"
        : status !== "positioned"),
    ),
    feedback,
  };
}
