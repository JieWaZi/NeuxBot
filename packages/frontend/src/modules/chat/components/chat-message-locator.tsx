import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { useVisibleScene } from "@/shared/api/chat-reading";

export function useChatMessageLocator({
  viewport,
  active,
  routeKey,
  messageId,
  requestedInteractionId,
  hasEarlierMessages = false,
  onLoadEarlier,
}: {
  viewport: RefObject<HTMLDivElement | null>;
  active: boolean;
  routeKey?: string;
  messageId?: string;
  requestedInteractionId?: string;
  hasEarlierMessages?: boolean;
  onLoadEarlier: () => Promise<boolean | undefined>;
}) {
  const sceneVisible = useVisibleScene();
  const { t } = useTranslation("workBoard");
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
  useEffect(() => {
    const id = messageId ?? requestedInteractionId;
    const container = viewport.current;
    if (!active || !sceneVisible || !routeKey || !id || !container) return;
    let moved = false;
    let live = true;
    let loadingHistory = false;
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
              "[data-chat-elicitation-card]",
            ),
          ).find((card) => card.dataset.messageId === id);
      if (target) {
        target.scrollIntoView({ block: "start" });
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
    observer.observe(container, { childList: true, subtree: true });
    return () => {
      live = false;
      observer.disconnect();
    };
  }, [
    active,
    sceneVisible,
    routeKey,
    key,
    messageId,
    viewport,
    batch,
    requestedInteractionId,
  ]);
  const feedback =
    active && sceneVisible && messageId && status && status !== "positioned" ? (
      <div
        role="status"
        className="text-content-muted type-ui flex shrink-0 items-center gap-3 px-6 py-2"
      >
        {t(
          status === "loading"
            ? "locatingResult"
            : status === "missing"
              ? "resultUnavailable"
              : status === "error"
                ? "locateFailed"
                : "resultFurther",
        )}
        {status === "more" || status === "error" ? (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setBatch((value) => value + 1)}
          >
            {t("continueLocating")}
          </Button>
        ) : null}
      </div>
    ) : null;
  return {
    positioning: Boolean(messageId && status !== "positioned"),
    feedback,
  };
}
