import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type RefObject,
} from "react";
import { useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import {
  getWorkResultOptions,
  getWorkResultQueryKey,
  getWorkBoardSummaryQueryKey,
  listWorkBoardQueryKey,
  viewWorkResultMutation,
} from "./generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkResultReceipt as Receipt } from "./generated/types.gen";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "./local-error-surface";
import { useQueryEvents } from "./use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { usePresentationReading } from "./presentation-reading";
import {
  useNotificationPresentation,
  type ChatResultOwner,
} from "./notification-presentation";

function subscribeVisibility(update: () => void) {
  document.addEventListener("visibilitychange", update);
  return () => document.removeEventListener("visibilitychange", update);
}

/** 只有可见 Scene 可以确认阅读，后台保活和预取不属于查看。 */
export function useVisibleScene() {
  const active = useRouteSceneActive();
  const visible = useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState === "visible",
    () => false,
  );
  return active && visible;
}

/** 原结果在实际可见区域渲染后，提交与该次渲染配套的观察版本。 */
export function useVisibleWorkResult({
  ownerKind,
  ownerId,
  root,
  selector,
  rendered,
  chatId,
}: {
  ownerKind: "chat" | "team_thread" | "workflow_run";
  ownerId: string;
  root: RefObject<HTMLElement | null>;
  selector: (receipt: Receipt) => string;
  rendered: boolean;
  chatId?: string;
}) {
  const active = useVisibleScene();
  const client = useQueryClient();
  const query = useQuery({
    ...getWorkResultOptions(
      withLocalErrorSurface({ path: { ownerKind, ownerId } }),
    ),
    enabled: active && rendered && /^[a-f0-9]{32}$/.test(ownerId),
  });
  useQueryEvents({
    topic: "work-results",
    resource: { kind: ownerKind, id: ownerId },
    queryKeys: [getWorkResultQueryKey({ path: { ownerKind, ownerId } })],
    active: active && rendered,
    enabled: /^[a-f0-9]{32}$/.test(ownerId),
  });
  const view = useMutation({
    ...withLocalErrorMutation(viewWorkResultMutation()),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: getWorkResultQueryKey({ path: { ownerKind, ownerId } }),
      });
      void client.invalidateQueries({
        queryKey: getWorkBoardSummaryQueryKey(),
      });
      void client.invalidateQueries({ queryKey: listWorkBoardQueryKey() });
    },
  });
  const receipt = query.data?.result;
  useNotificationPresentation({
    root,
    active:
      active &&
      rendered &&
      (!receipt?.reference || !chatId || receipt.reference.chatId === chatId),
    target: {
      kind: "result",
      ownerKind,
      ownerId,
      chatId,
      version: receipt?.latestVersion ?? 0,
    },
  });
  const readable =
    active &&
    rendered &&
    receipt?.reference &&
    receipt.latestVersion &&
    receipt.latestVersion > (receipt.viewedVersion ?? 0) &&
    (!chatId || receipt.reference.chatId === chatId);
  const targetSelector = receipt ? selector(receipt) : "";
  usePresentationReading({
    root,
    selector: targetSelector,
    identity:
      readable && targetSelector
        ? `result:${ownerKind}:${ownerId}:${receipt.latestVersion}`
        : undefined,
    revision: query.dataUpdatedAt,
    read: () =>
      receipt?.reference
        ? view.mutateAsync({
            path: { ownerKind, ownerId },
            body: {
              version: receipt?.latestVersion ?? 0,
              reference: receipt.reference,
            },
          })
        : Promise.resolve(),
  });
}

const messageSelector = (receipt: Receipt) =>
  receipt.reference?.messageId
    ? `[data-message-id="${CSS.escape(receipt.reference.messageId)}"] [data-work-result-body]`
    : "";

/** Chat 只有真实正文所在的滚动区域可以产生阅读事实。 */
export function ChatWorkResultReader({
  chatId,
  owner = { kind: "chat", id: chatId },
  viewport,
  hasEarlierMessages = false,
  onLoadEarlier,
}: {
  chatId: string;
  owner?: ChatResultOwner;
  viewport: RefObject<HTMLDivElement | null>;
  hasEarlierMessages?: boolean;
  onLoadEarlier?: () => Promise<void>;
}) {
  const location = useLocation();
  const active = useVisibleScene();
  const { t } = useTranslation("workBoard");
  const [batch, setBatch] = useState(0);
  const [locating, setLocating] = useState<
    "" | "loading" | "more" | "missing" | "error"
  >("");
  const history = useRef({ hasEarlierMessages, onLoadEarlier });
  useEffect(() => {
    history.current = { hasEarlierMessages, onLoadEarlier };
  }, [hasEarlierMessages, onLoadEarlier]);
  useVisibleWorkResult({
    ownerKind: owner.kind,
    ownerId: owner.id,
    root: viewport,
    selector: messageSelector,
    rendered: true,
    chatId,
  });
  useEffect(() => {
    const id = new URLSearchParams(location.search).get("resultMessage");
    const container = viewport.current;
    if (!active || !id || !container) return;
    let moved = false;
    let live = true;
    const move = () => {
      if (moved || !live) return moved;
      const target = container.querySelector<HTMLElement>(
        `[data-message-id="${CSS.escape(id)}"]`,
      );
      if (target) {
        target.scrollIntoView({ block: "start" });
        moved = true;
        setLocating("");
      }
      return moved;
    };
    const locate = async () => {
      if (move()) return;
      setLocating("loading");
      // 每次定位最多读取二十个原历史窗口，较远结果由用户继续，避免无界加载。
      for (let page = 0; live && !moved && page < 20; page++) {
        const current = history.current;
        if (!current.hasEarlierMessages || !current.onLoadEarlier) break;
        try {
          await current.onLoadEarlier();
          await new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          );
        } catch {
          if (live) setLocating("error");
          return;
        }
        if (move()) return;
      }
      if (live && !moved)
        setLocating(history.current.hasEarlierMessages ? "more" : "missing");
    };
    void locate();
    const observer = new MutationObserver(move);
    observer.observe(container, { childList: true, subtree: true });
    return () => {
      live = false;
      observer.disconnect();
    };
  }, [active, location.search, viewport, batch]);
  if (!active || !locating) return null;
  return (
    <div
      role="status"
      className="text-content-muted type-ui flex shrink-0 items-center gap-3 px-6 py-2"
    >
      {t(
        locating === "loading"
          ? "locatingResult"
          : locating === "missing"
            ? "resultUnavailable"
            : locating === "error"
              ? "locateFailed"
              : "resultFurther",
      )}
      {locating === "more" || locating === "error" ? (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => setBatch((value) => value + 1)}
        >
          {t("continueLocating")}
        </Button>
      ) : null}
    </div>
  );
}
