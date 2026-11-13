import { CircleAlert, X, type LucideIcon } from "lucide-react";
import {
  useId,
  useRef,
  useState,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/shared/ui/context-menu";
import { OverflowText } from "@/shared/ui/overflow-text";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  SceneChromeLayout,
  useSceneChromeAccessoryFullscreen,
} from "@/shared/ui/scene-chrome-portal";

export type SceneTab = {
  id: string;
  label: string;
  icon?: LucideIcon;
  indicator?: ReactNode;
  description?: string;
  autoFocus?: boolean;
  closeable?: boolean;
  draggable?: boolean;
  notFound?: boolean;
  separatorBefore?: boolean;
};

type SceneTabStripProps = {
  activeTabId: string;
  ariaLabel: string;
  tabs: readonly SceneTab[];
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onRename?: (tabId: string, label: string) => void;
  onCloseOthers?: (tabId: string) => void;
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
  onRename,
  onCloseOthers,
  onReorder,
  onSelect,
  paneActions,
  tabs,
  trailing,
}: SceneTabStripProps) {
  const { t } = useTranslation("common");
  const [renameTarget, setRenameTarget] = useState<{
    id: string;
    label: string;
  }>();
  const renameInputId = useId();
  const draggedTabId = useRef("");
  const tabButtons = useRef(new Map<string, HTMLButtonElement>());
  const accessoryFullscreen = useSceneChromeAccessoryFullscreen();

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

  const tabList = (
    <div
      key="tabs"
      className={cn(
        "flex shrink-0 items-center gap-1",
        tabs.length === 0 && "hidden",
        accessoryFullscreen && "hidden",
      )}
      hidden={accessoryFullscreen || tabs.length === 0}
      role="tablist"
      aria-label={ariaLabel}
    >
      {tabs.map((tab, visibleIndex) => {
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
              "shell-header-tab h-control-md w-scene-tab max-w-scene-tab min-w-scene-tab-min rounded-control relative flex shrink items-center",
              tab.separatorBefore && "ml-2",
            )}
            data-selected={active || undefined}
            data-invalid={tab.notFound || undefined}
            data-scene-tab-item
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
                tab.notFound ? t("sceneTabs.notFound") : tab.description
              }
              autoFocus={tab.autoFocus}
              tabIndex={
                active ||
                (!tabs.some((item) => item.id === activeTabId) &&
                  visibleIndex === 0)
                  ? 0
                  : -1
              }
              onClick={() => selectTab(tab.id)}
              onKeyDown={(event) => moveFocus(event, tab.id)}
              className={cn(
                "shell-header-tab-select focus-visible:ring-ring type-ui rounded-control flex h-full min-w-0 flex-1 items-center gap-1.5 py-0 text-left outline-none focus-visible:ring-2 focus-visible:ring-offset-2",
                "px-2",
              )}
            >
              {tab.notFound ? (
                <CircleAlert
                  className="size-icon-sm shrink-0"
                  strokeWidth={1.5}
                  aria-hidden="true"
                />
              ) : (
                (tab.indicator ??
                (Icon ? (
                  <Icon
                    className="size-icon-sm shrink-0"
                    strokeWidth={1.5}
                    aria-hidden="true"
                  />
                ) : null))
              )}
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
                className="shell-header-tab-close focus-visible:ring-ring size-control-md grid shrink-0 place-items-center outline-none focus-visible:ring-2"
              >
                <X className="size-icon-sm" strokeWidth={1.5} />
              </button>
            ) : null}
          </div>
        );
        const triggerContent =
          onRename && onCloseOthers ? (
            <ContextMenuTrigger asChild>{content}</ContextMenuTrigger>
          ) : (
            content
          );
        const tabContent = tab.notFound ? (
          <Tooltip key={tab.id}>
            <TooltipTrigger asChild>{triggerContent}</TooltipTrigger>
            <TooltipContent>{t("sceneTabs.notFound")}</TooltipContent>
          </Tooltip>
        ) : (
          triggerContent
        );
        return onRename && onCloseOthers ? (
          <ContextMenu key={tab.id}>
            {tabContent}
            <ContextMenuContent
              onCloseAutoFocus={(event) => {
                if (renameTarget) event.preventDefault();
              }}
            >
              <ContextMenuItem
                onSelect={() =>
                  setRenameTarget({ id: tab.id, label: tab.label })
                }
              >
                {t("sceneTabs.rename")}
              </ContextMenuItem>
              <ContextMenuItem
                disabled={
                  !tabs.some((item) => item.id !== tab.id && item.closeable)
                }
                onSelect={() => onCloseOthers(tab.id)}
              >
                {t("sceneTabs.closeOthers")}
              </ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ) : (
          tabContent
        );
      })}
    </div>
  );
  return (
    <SceneChromeLayout
      className={cn("pl-0 [--scene-chrome-left-inset:0px]", className)}
      paneActions={paneActions}
      leading={leading}
    >
      {!accessoryFullscreen ? (
        <div
          className="shell-header-tab-track scrollbar-hidden flex min-w-0 items-center overflow-x-auto"
          data-scene-tab-scroll
        >
          {tabList}
          {trailing ? (
            <div className="flex shrink-0 items-center" data-scene-tab-trailing>
              {trailing}
            </div>
          ) : null}
        </div>
      ) : null}
      <Dialog
        open={Boolean(renameTarget)}
        onOpenChange={(open) => {
          if (!open) setRenameTarget(undefined);
        }}
      >
        <DialogContent
          size="sm"
          title={t("sceneTabs.renameTitle")}
          description={t("sceneTabs.renameHint")}
        >
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const label = renameTarget?.label.trim();
              if (!renameTarget || !label) return;
              onRename?.(renameTarget.id, label);
              setRenameTarget(undefined);
            }}
          >
            <div className="px-6 pb-5">
              <label htmlFor={renameInputId} className="type-body mb-2 block">
                {t("sceneTabs.nameLabel")}
              </label>
              <Input
                id={renameInputId}
                autoFocus
                maxLength={120}
                placeholder={t("sceneTabs.namePlaceholder")}
                value={renameTarget?.label ?? ""}
                onChange={(event) => {
                  const label = event.target.value;
                  setRenameTarget((current) =>
                    current ? { ...current, label } : current,
                  );
                }}
              />
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setRenameTarget(undefined)}
              >
                {t("cancel")}
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={!renameTarget?.label.trim()}
              >
                {t("save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </SceneChromeLayout>
  );
}
