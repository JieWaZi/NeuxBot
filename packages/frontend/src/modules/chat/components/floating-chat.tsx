import { createPortal } from "react-dom";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { Minus, Move } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { StreamState } from "@/shared/api/events";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { useRegisterFloatingChatLauncher } from "@/shared/workspace/floating-chat-launcher";

export type FloatingChatSnapshot = {
  isRunning: boolean;
  pendingApprovalCount: number;
  pendingElicitationCount: number;
  streamState: StreamState;
};

type FloatingChatProps = {
  chatId?: string;
  /** 外部整理动作递增请求号，展开已存在的同一浮窗。 */
  openRequest?: number;
  children: ReactNode;
  fullView: boolean;
  scopeKey: string;
  snapshot: FloatingChatSnapshot;
  title: string;
};

type Position = { left: number; top: number };

function clampPosition(position: Position, width: number, height: number) {
  return {
    left: Math.min(
      Math.max(8, position.left),
      Math.max(8, window.innerWidth - width - 8),
    ),
    top: Math.min(
      Math.max(8, position.top),
      Math.max(8, window.innerHeight - height - 8),
    ),
  };
}

function focusFloatingChatShellLauncher() {
  window.requestAnimationFrame(() =>
    document
      .querySelector<HTMLButtonElement>("[data-floating-chat-shell-launcher]")
      ?.focus(),
  );
}

/** 同一 ChatThread 根节点在聊天主区和浮窗之间移动，不重建运行时视图。 */
export function FloatingChat({
  chatId,
  openRequest = 0,
  children,
  fullView,
  scopeKey,
  snapshot,
  title,
}: FloatingChatProps) {
  const { t } = useTranslation("chat");
  const [openScopeKey, setOpenScopeKey] = useState<string | null>(null);
  const [dismissedRequest, setDismissedRequest] = useState(0);
  const [position, setPosition] = useState<Position | null>(null);
  const [contentNode] = useState(() => {
    const node = document.createElement("div");
    node.className = "h-full min-h-0 min-w-0";
    return node;
  });
  const normalHost = useRef<HTMLDivElement>(null);
  const floatingHost = useRef<HTMLDivElement>(null);
  const floatingPanel = useRef<HTMLDivElement>(null);
  const floatingLayer = useRef<HTMLDivElement>(null);
  const dragCleanup = useRef<(() => void) | undefined>(undefined);
  const active =
    fullView && (openScopeKey === scopeKey || openRequest > dismissedRequest);
  const showProgress = Boolean(
    chatId &&
    (snapshot.isRunning ||
      snapshot.pendingApprovalCount > 0 ||
      snapshot.pendingElicitationCount > 0),
  );
  const status = !showProgress
    ? t("floatingChat.open")
    : snapshot.pendingElicitationCount > 0
      ? t("waitingElicitation")
      : snapshot.pendingApprovalCount > 0
        ? t("waitingApproval")
        : snapshot.streamState === "reconnecting"
          ? t("reconnecting")
          : snapshot.streamState === "unavailable"
            ? t("streamUnavailable")
            : t("working");
  const openChat = useCallback(() => setOpenScopeKey(scopeKey), [scopeKey]);
  const launcherEntry = useMemo(
    () =>
      fullView
        ? {
            ariaLabel: showProgress
              ? t("floatingChat.openStatus", { status })
              : t("floatingChat.open"),
            label: status,
            onActivate: openChat,
            state: !showProgress
              ? ("idle" as const)
              : snapshot.pendingApprovalCount > 0 ||
                  snapshot.pendingElicitationCount > 0
                ? ("waiting" as const)
                : ("running" as const),
          }
        : null,
    [
      fullView,
      openChat,
      showProgress,
      snapshot.pendingApprovalCount,
      snapshot.pendingElicitationCount,
      status,
      t,
    ],
  );
  useRegisterFloatingChatLauncher(launcherEntry);

  useLayoutEffect(() => {
    const host = active ? floatingHost.current : normalHost.current;
    host?.appendChild(contentNode);
  }, [active, contentNode]);

  useLayoutEffect(() => {
    const panel = floatingPanel.current;
    if (!active || !panel) return;
    const update = () => {
      const { width, height } = panel.getBoundingClientRect();
      if (width <= 0 || height <= 0) return;
      setPosition((current) => {
        // 未拖动时保留 CSS 贴边；手动位置在缩放与重新打开时限制到可见区域。
        if (!current) return current;
        const next = clampPosition(current, width, height);
        return next.left === current.left && next.top === current.top
          ? current
          : next;
      });
    };
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(update);
    observer?.observe(panel);
    window.addEventListener("resize", update);
    update();
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [active, openRequest]);

  useEffect(() => {
    if (!active) return;
    floatingLayer.current
      ?.querySelector<HTMLButtonElement>("[data-floating-chat-minimize]")
      ?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpenScopeKey(null);
      setDismissedRequest(openRequest);
      focusFloatingChatShellLauncher();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active, openRequest]);

  useEffect(() => () => dragCleanup.current?.(), []);

  const startDrag = (event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0 || (event.target as HTMLElement).closest("button"))
      return;
    const panel = floatingPanel.current;
    if (!panel) return;
    event.preventDefault();
    dragCleanup.current?.();
    const rect = panel.getBoundingClientRect();
    const startX = event.clientX;
    const startY = event.clientY;
    const move = (moveEvent: PointerEvent) => {
      setPosition(
        clampPosition(
          {
            left: rect.left + moveEvent.clientX - startX,
            top: rect.top + moveEvent.clientY - startY,
          },
          rect.width,
          rect.height,
        ),
      );
    };
    const finish = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", finish);
      dragCleanup.current = undefined;
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", finish, { once: true });
    dragCleanup.current = finish;
  };

  return (
    <>
      <div
        ref={normalHost}
        className={cn(
          "h-full min-h-0",
          fullView && "pointer-events-none invisible",
        )}
        aria-hidden={fullView || undefined}
        inert={fullView}
        data-floating-chat-normal-host
      />
      {createPortal(children, contentNode)}
      {active
        ? createPortal(
            <div
              ref={floatingLayer}
              className="pointer-events-none fixed inset-0 z-50"
              data-floating-chat-layer
            >
              <div
                ref={floatingPanel}
                id="floating-chat-panel"
                className="bg-surface-scene border-border pointer-events-auto absolute right-4 bottom-4 flex h-[min(720px,calc(100dvh-96px))] max-h-[calc(100dvh-16px)] w-[min(400px,calc(100vw-32px))] min-w-0 flex-col overflow-hidden rounded-xl border shadow-lg"
                style={
                  position
                    ? {
                        left: position.left,
                        top: position.top,
                        right: "auto",
                        bottom: "auto",
                      }
                    : undefined
                }
                role="region"
                aria-label={t("floatingChat.window")}
                data-floating-chat-panel
              >
                <header
                  className="border-border h-row-navigation flex shrink-0 cursor-move items-center gap-2 border-b px-3 select-none"
                  onPointerDown={startDrag}
                  title={t("floatingChat.move")}
                >
                  <Move
                    className="text-muted-foreground size-icon-sm shrink-0"
                    aria-hidden="true"
                  />
                  <span className="type-ui min-w-0 flex-1 truncate font-medium">
                    {title}
                  </span>
                  {showProgress ? (
                    <span className="text-muted-foreground type-caption shrink-0">
                      {status}
                    </span>
                  ) : null}
                  <Button
                    type="button"
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t("floatingChat.minimize")}
                    title={t("floatingChat.minimize")}
                    data-floating-chat-minimize
                    onClick={() => {
                      setOpenScopeKey(null);
                      setDismissedRequest(openRequest);
                      focusFloatingChatShellLauncher();
                    }}
                  >
                    <Minus />
                  </Button>
                </header>
                <div ref={floatingHost} className="min-h-0 flex-1" />
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
