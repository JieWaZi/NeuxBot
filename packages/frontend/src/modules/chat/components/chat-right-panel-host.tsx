import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";

export type ChatRightPanel = "workspace";

type ChatRightPanelHostProps = {
  activePanel?: ChatRightPanel;
  fullscreen?: boolean;
  overlay?: boolean;
  hidden?: boolean;
  onDismiss?: () => void;
  resizing?: boolean;
  workspaceLeading?: ReactNode;
  workspacePanel: ReactNode;
  side?: "left" | "right";
  persistent?: boolean;
};

/**
 * 工作台窗格随布局方向移动；会话变化由面板 props 和 scope state 驱动。
 */
export function ChatRightPanelHost({
  activePanel,
  fullscreen = false,
  overlay,
  hidden = false,
  onDismiss,
  resizing = false,
  workspaceLeading,
  workspacePanel,
  side = "right",
  persistent = false,
}: ChatRightPanelHostProps) {
  const { t } = useTranslation("chat");

  return (
    <div
      className={cn(
        "duration-layout ease-ui relative h-full min-h-0 min-w-0 overflow-visible transition-[width]",
        persistent || overlay === false
          ? "w-[var(--chat-right-panel-width)] shrink-0"
          : overlay
            ? "absolute inset-y-0 z-30 w-[var(--chat-right-panel-width)]"
            : "max-lg:absolute max-lg:inset-y-0 max-lg:z-30 max-lg:w-[min(88vw,var(--chat-right-panel-width))] lg:w-[var(--chat-right-panel-width)] lg:shrink-0",
        !persistent &&
          (overlay
            ? side === "left"
              ? "left-0"
              : "right-0"
            : overlay === undefined &&
              (side === "left" ? "max-lg:left-0" : "max-lg:right-0")),
        resizing && "transition-none",
        fullscreen && "absolute inset-0 z-40 w-full max-lg:w-full lg:w-full",
        (!activePanel || hidden) && "pointer-events-none invisible",
      )}
      aria-hidden={!activePanel || hidden}
      inert={!activePanel || hidden}
      data-chat-right-panel-host
      data-testid="chat-right-panel-host"
    >
      {onDismiss && overlay !== false && !fullscreen ? (
        <button
          type="button"
          aria-label={t("closeRightPanel")}
          className={cn(
            "bg-overlay absolute inset-y-0 w-screen backdrop-blur-sm",
            overlay ? "block" : "hidden max-lg:block",
            side === "left" ? "left-full" : "right-full",
          )}
          onClick={onDismiss}
        />
      ) : null}
      <div className="relative z-10 h-full min-h-0">
        {activePanel && !fullscreen ? workspaceLeading : null}
        <div
          className={cn(
            "absolute inset-0",
            activePanel !== "workspace" && "pointer-events-none invisible",
          )}
          aria-hidden={activePanel !== "workspace"}
        >
          {workspacePanel}
        </div>
      </div>
    </div>
  );
}
