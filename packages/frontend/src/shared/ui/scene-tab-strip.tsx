import { CircleAlert, MoreHorizontal, X, type LucideIcon } from "lucide-react";
import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { OverflowText } from "@/shared/ui/overflow-text";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { useSceneChromeActions } from "@/shared/ui/scene-chrome-actions";
import {
  useSceneChromeAccessoryFullscreen,
  useSceneChromeAccessorySide,
  useSceneChromeAccessoryTargetSetter,
  useSceneChromeAccessoryWidth,
  useSceneChromePaneActionsTargetSetter,
} from "@/shared/ui/scene-chrome-portal";

export type SceneTab = {
  id: string;
  label: string;
  icon?: LucideIcon;
  autoFocus?: boolean;
  closeable?: boolean;
  draggable?: boolean;
  notFound?: boolean;
};

type SceneTabStripProps = {
  activeTabId: string;
  ariaLabel: string;
  tabs: readonly SceneTab[];
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onReorder?: (sourceTabId: string, targetTabId: string) => void;
  leading?: ReactNode;
  paneActions?: ReactNode;
  trailing?: ReactNode;
  className?: string;
};

export function SceneTabStrip({
  activeTabId,
  ariaLabel,
  className,
  leading,
  onClose,
  onReorder,
  onSelect,
  paneActions,
  tabs,
  trailing,
}: SceneTabStripProps) {
  const { t } = useTranslation("common");
  const draggedTabId = useRef("");
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabButtons = useRef(new Map<string, HTMLButtonElement>());
  const actionRef = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const [actionWidth, setActionWidth] = useState(0);
  const sceneActions = useSceneChromeActions();
  const setAccessoryTarget = useSceneChromeAccessoryTargetSetter();
  const setPaneActionsTarget = useSceneChromePaneActionsTargetSetter();
  const accessoryWidth = useSceneChromeAccessoryWidth();
  const accessoryFullscreen = useSceneChromeAccessoryFullscreen();
  const accessorySide = useSceneChromeAccessorySide();
  const accessoryAtLeft =
    accessorySide === "left" && Boolean(accessoryWidth) && !accessoryFullscreen;

  useLayoutEffect(() => {
    const tabList = tabListRef.current;
    if (!tabList) return;
    const measure = () => {
      setOverflowing(tabList.scrollWidth > tabList.clientWidth + 1);
    };
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(measure);
    observer?.observe(tabList);
    window.addEventListener("resize", measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [tabs]);

  useLayoutEffect(() => {
    const actions = actionRef.current;
    if (!actions) return;
    const measure = () => setActionWidth(actions.getBoundingClientRect().width);
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(measure);
    observer?.observe(actions);
    return () => observer?.disconnect();
  }, [sceneActions, accessoryAtLeft]);

  const focusTab = (index: number) => {
    const tab = tabs[index];
    if (!tab) return;
    selectTab(tab.id);
    tabButtons.current.get(tab.id)?.focus();
  };
  const moveFocus = (
    event: KeyboardEvent<HTMLButtonElement>,
    tabId: string,
  ) => {
    const currentIndex = tabs.findIndex((tab) => tab.id === tabId);
    if (currentIndex < 0) return;
    let nextIndex: number | undefined;
    if (event.key === "ArrowRight") {
      nextIndex = (currentIndex + 1) % tabs.length;
    } else if (event.key === "ArrowLeft") {
      nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    } else if (event.key === "Home") {
      nextIndex = 0;
    } else if (event.key === "End") {
      nextIndex = tabs.length - 1;
    }
    if (nextIndex === undefined) return;
    event.preventDefault();
    focusTab(nextIndex);
  };
  const dropTab = (event: DragEvent<HTMLDivElement>, tab: SceneTab) => {
    if (!onReorder || !tab.draggable) return;
    event.preventDefault();
    const sourceTabId =
      draggedTabId.current || event.dataTransfer.getData("text/plain");
    if (sourceTabId && sourceTabId !== tab.id) {
      onReorder(sourceTabId, tab.id);
    }
  };
  const selectTab = (tabId: string) => {
    onSelect(tabId);
  };
  const closeTab = (tabId: string) => {
    onClose(tabId);
  };

  const accessory = (
    <div
      className={cn(
        "relative flex h-full min-w-0 items-center",
        accessoryFullscreen ? "flex-1" : "shrink-0",
      )}
      style={
        {
          ...(accessoryFullscreen
            ? {
                marginLeft: "calc(-1 * var(--scene-chrome-left-inset, 8px))",
              }
            : {}),
          ...(!accessoryFullscreen
            ? { width: accessoryWidth ?? actionWidth }
            : {}),
          "--scene-chrome-actions-width": `${accessoryAtLeft ? 0 : actionWidth}px`,
        } as CSSProperties
      }
      data-scene-chrome-accessory-wrapper
    >
      <div
        ref={setAccessoryTarget}
        className="flex h-full min-w-0 flex-1 items-center overflow-hidden empty:hidden"
        data-scene-chrome-accessory
      />
      {accessoryWidth && !accessoryFullscreen ? (
        <span
          aria-hidden="true"
          className={cn(
            "bg-border pointer-events-none absolute top-1.5 bottom-1.5 z-10 w-px",
            accessoryAtLeft ? "right-0" : "left-0",
          )}
          data-scene-chrome-divider
        />
      ) : null}
      {sceneActions && !accessoryAtLeft ? (
        <div
          ref={actionRef}
          className="absolute inset-y-0 right-0 z-20 flex items-center gap-1 pr-2"
        >
          {sceneActions}
        </div>
      ) : null}
    </div>
  );

  return (
    <div
      data-desktop-drag-region
      data-scene-tab-strip
      className={cn(
        "border-border-subtle bg-surface-scene flex h-(--shell-titlebar-height) min-w-0 shrink-0 items-center border-b pr-0",
        accessoryAtLeft ? "pl-0" : "pl-2",
        className,
      )}
      data-accessory-left={accessoryAtLeft || undefined}
    >
      {accessoryAtLeft ? accessory : null}
      {leading && !accessoryFullscreen ? (
        <div
          className={cn(
            "mr-1 flex shrink-0 items-center",
            accessoryAtLeft && "pl-2",
          )}
        >
          {leading}
        </div>
      ) : null}
      <div
        ref={tabListRef}
        className={cn(
          "scrollbar-hidden flex min-w-0 items-center gap-2 overflow-x-auto",
          accessoryAtLeft && !leading && "pl-2",
          accessoryFullscreen && "hidden",
        )}
        role="tablist"
        aria-label={ariaLabel}
      >
        {tabs.map((tab) => {
          const active = tab.id === activeTabId;
          const Icon = tab.icon;
          const content = (
            <div
              key={tab.id}
              data-motion="presence"
              data-state="open"
              draggable={tab.draggable}
              onDragEnd={() => {
                draggedTabId.current = "";
              }}
              onDragOver={(event) => {
                if (onReorder && tab.draggable) event.preventDefault();
              }}
              onDragStart={(event) => {
                if (!tab.draggable) return;
                draggedTabId.current = tab.id;
                event.dataTransfer.effectAllowed = "move";
                event.dataTransfer.setData("text/plain", tab.id);
              }}
              onDrop={(event) => dropTab(event, tab)}
              className={cn(
                "group/tab duration-quick ease-ui h-control-tab rounded-control relative flex max-w-56 min-w-0 shrink-0 items-center border transition-[background-color,border-color,color]",
                tab.notFound
                  ? "bg-destructive/10 text-destructive border-destructive/20 hover:bg-destructive/15"
                  : active
                    ? "bg-selection-surface text-content-primary border-transparent font-medium"
                    : "border-button-outline text-content-primary hover:bg-button-fill hover:border-button-outline",
              )}
              role="presentation"
            >
              <button
                ref={(node) => {
                  if (node) tabButtons.current.set(tab.id, node);
                  else tabButtons.current.delete(tab.id);
                }}
                type="button"
                role="tab"
                aria-selected={active}
                aria-description={
                  tab.notFound ? t("sceneTabs.notFound") : undefined
                }
                autoFocus={tab.autoFocus}
                tabIndex={active ? 0 : -1}
                onClick={() => selectTab(tab.id)}
                onKeyDown={(event) => moveFocus(event, tab.id)}
                className={cn(
                  "focus-visible:ring-ring type-ui rounded-control flex h-full min-w-0 flex-1 items-center gap-1.5 py-0 text-left outline-none focus-visible:ring-2 focus-visible:ring-offset-2",
                  tab.closeable ? "pr-8" : "pr-3",
                  tab.closeable && !Icon && !tab.notFound ? "pl-8" : "pl-3",
                )}
              >
                {tab.notFound ? (
                  <CircleAlert
                    className="size-icon-sm shrink-0"
                    strokeWidth={1.5}
                    aria-hidden="true"
                  />
                ) : Icon ? (
                  <Icon
                    className="size-icon-sm shrink-0"
                    strokeWidth={1.5}
                    aria-hidden="true"
                  />
                ) : null}
                {tab.notFound ? (
                  <span className="min-w-0 flex-1 truncate">{tab.label}</span>
                ) : (
                  <OverflowText className="min-w-0 flex-1">
                    {tab.label}
                  </OverflowText>
                )}
              </button>
              {tab.closeable ? (
                <button
                  type="button"
                  aria-label={t("closeTab", { label: tab.label })}
                  onClick={() => closeTab(tab.id)}
                  className="text-content-muted hover:bg-action-quiet-hover hover:text-content-primary focus-visible:ring-ring duration-quick size-icon-md absolute top-1/2 right-1 grid -translate-y-1/2 place-items-center rounded-full opacity-0 transition-[background-color,color,opacity] outline-none group-hover/tab:opacity-100 focus-visible:opacity-100 focus-visible:ring-2"
                >
                  <X className="size-icon-sm" strokeWidth={1.5} />
                </button>
              ) : null}
            </div>
          );
          return tab.notFound ? (
            <Tooltip key={tab.id}>
              <TooltipTrigger asChild>{content}</TooltipTrigger>
              <TooltipContent>{t("sceneTabs.notFound")}</TooltipContent>
            </Tooltip>
          ) : (
            content
          );
        })}
      </div>
      {trailing && !accessoryFullscreen ? (
        <div
          className="ml-1 flex shrink-0 items-center"
          data-scene-tab-trailing
        >
          {trailing}
        </div>
      ) : null}
      {overflowing && !accessoryFullscreen ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              aria-label={`${ariaLabel}中的其他标签`}
              className="ml-1 shrink-0"
              size="icon-sm"
              type="button"
              variant="ghost"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="max-w-64">
            <DropdownMenuRadioGroup
              value={activeTabId ?? ""}
              onValueChange={selectTab}
            >
              {tabs.map((tab) => (
                <DropdownMenuRadioItem key={tab.id} value={tab.id}>
                  {tab.notFound ? (
                    <CircleAlert
                      className="text-destructive size-icon-sm shrink-0"
                      aria-label={t("sceneTabs.notFound")}
                    />
                  ) : null}
                  <OverflowText className="min-w-0 flex-1">
                    {tab.label}
                  </OverflowText>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
      {!accessoryFullscreen ? <div className="min-w-0 flex-1" /> : null}
      <div
        ref={setPaneActionsTarget}
        hidden={accessoryFullscreen}
        className={cn(
          "flex h-full shrink-0 items-center gap-1 px-2 empty:hidden",
          accessoryFullscreen && "hidden",
        )}
        data-scene-pane-actions
      >
        {paneActions}
      </div>
      {!accessoryAtLeft ? accessory : null}
      {sceneActions && accessoryAtLeft ? (
        <div
          ref={actionRef}
          className="flex h-full shrink-0 items-center gap-1 pr-2"
        >
          {sceneActions}
        </div>
      ) : null}
    </div>
  );
}
