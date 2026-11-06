import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import {
  ArrowLeftRight,
  Columns3,
  FileDiff,
  Files,
  Globe,
  MessagesSquare,
  PanelsTopLeft,
  TerminalSquare,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

import type { WorkbenchTab } from "./workbench-model";
import type {
  WorkbenchDropZone,
  WorkbenchNode,
  WorkbenchPane,
  WorkbenchSplit,
} from "./workbench-pane";
import { paneGeometry, type PaneRect } from "./pane-geometry";

const dragMime = "application/x-ally-workbench-tab";

function tabIcon(kind: WorkbenchTab["kind"]) {
  switch (kind) {
    case "files":
    case "file":
      return Files;
    case "changes":
    case "git-diff":
      return FileDiff;
    case "browser":
      return Globe;
    case "terminal":
      return TerminalSquare;
    case "sidechat":
      return MessagesSquare;
    case "task-board":
      return Columns3;
    case "content":
      return PanelsTopLeft;
  }
}

function positioned(rect: PaneRect): CSSProperties {
  return {
    left: `${rect.left}%`,
    top: `${rect.top}%`,
    width: `${rect.width}%`,
    height: `${rect.height}%`,
  };
}

function zoneAt(event: DragEvent<HTMLElement>): WorkbenchDropZone {
  const rect = event.currentTarget.getBoundingClientRect();
  if (!rect.width || !rect.height) return "center";
  const x = (event.clientX - rect.left) / rect.width;
  const y = (event.clientY - rect.top) / rect.height;
  if (x < 0.25) return "left";
  if (x > 0.75) return "right";
  if (y < 0.25) return "up";
  if (y > 0.75) return "down";
  return "center";
}

type PaneTabStripProps = {
  active: boolean;
  scopeKey: string;
  pane: WorkbenchPane;
  tabs: WorkbenchTab[];
  trailing?: ReactNode;
  draggedTabId: React.RefObject<string>;
  onClose: (tab: WorkbenchTab) => void | Promise<void>;
  onDragStateChange: (dragging: boolean) => void;
  onFocus: (tab: WorkbenchTab) => void;
  onFocusPane: (paneId: string) => void;
  onMove: (tabId: string, paneId: string, beforeTabId: string) => void;
  onSplit: (tabId: string, paneId: string, zone: WorkbenchDropZone) => void;
};

export function WorkbenchPaneTabStrip({
  active,
  scopeKey,
  pane,
  tabs,
  trailing,
  draggedTabId,
  onClose,
  onDragStateChange,
  onFocus,
  onFocusPane,
  onMove,
  onSplit,
}: PaneTabStripProps) {
  const { t } = useTranslation("chat");
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const focusContext = useRef({ active, paneId: pane.id, scopeKey });
  const pendingCloseFocus = useRef<{
    tabId: string;
    cancelled: boolean;
  } | null>(null);
  useLayoutEffect(() => {
    focusContext.current = { active, paneId: pane.id, scopeKey };
  }, [active, pane.id, scopeKey]);
  const ordered = pane.tabIds.flatMap((id) => {
    const tab = tabs.find((item) => item.id === id);
    return tab ? [tab] : [];
  });
  const onKeyDown = (
    event: KeyboardEvent<HTMLButtonElement>,
    tab: WorkbenchTab,
  ) => {
    if (
      (event.key === "Delete" || event.key === "Backspace") &&
      tab.closeable !== false
    ) {
      event.preventDefault();
      const request = { tabId: tab.id, cancelled: false };
      pendingCloseFocus.current = request;
      void Promise.resolve(onClose(tab)).then(() => {
        requestAnimationFrame(() => {
          const current = focusContext.current;
          // A user focus change, hidden Scene, or replaced scope ends this request.
          if (
            pendingCloseFocus.current !== request ||
            request.cancelled ||
            !current.active ||
            current.paneId !== pane.id ||
            current.scopeKey !== scopeKey ||
            buttons.current.has(tab.id) ||
            document.activeElement !== document.body
          )
            return;
          const selected = Array.from(buttons.current.values()).find(
            (button) => button.getAttribute("aria-selected") === "true",
          );
          selected?.focus();
        });
      });
      return;
    }
    if (event.altKey && event.shiftKey) {
      const zone =
        event.key === "ArrowRight"
          ? "right"
          : event.key === "ArrowLeft"
            ? "left"
            : event.key === "ArrowUp"
              ? "up"
              : event.key === "ArrowDown"
                ? "down"
                : null;
      if (zone) {
        event.preventDefault();
        onSplit(tab.id, pane.id, zone);
        requestAnimationFrame(() =>
          document.getElementById(`workbench-tab-${tab.id}`)?.focus(),
        );
        return;
      }
    }
    const index = ordered.findIndex((item) => item.id === tab.id);
    const nextIndex =
      event.key === "ArrowRight"
        ? (index + 1) % ordered.length
        : event.key === "ArrowLeft"
          ? (index - 1 + ordered.length) % ordered.length
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? ordered.length - 1
              : undefined;
    if (nextIndex === undefined) return;
    event.preventDefault();
    const next = ordered[nextIndex];
    if (!next) return;
    onFocus(next);
    buttons.current.get(next.id)?.focus();
  };
  return (
    <div
      className="shell-header-tab-track"
      role="region"
      aria-label={t("workbench.tabs")}
      data-workbench-pane-scroll={pane.id}
    >
      <div
        className="workbench-pane-tabs"
        role="tablist"
        aria-label={t("workbench.tabs")}
        data-workbench-pane-tabs={pane.id}
      >
        {ordered.map((tab) => {
          const Icon = tabIcon(tab.kind);
          const selected = pane.activeTabId === tab.id;
          return (
            <div
              key={tab.id}
              className="shell-header-tab"
              data-selected={selected || undefined}
              draggable
              onDragStart={(event) => {
                draggedTabId.current = tab.id;
                onDragStateChange(true);
                event.dataTransfer.effectAllowed = "move";
                event.dataTransfer.setData(dragMime, tab.id);
                event.dataTransfer.setData("text/plain", tab.id);
              }}
              onDragEnd={() => {
                draggedTabId.current = "";
                onDragStateChange(false);
              }}
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault();
                event.stopPropagation();
                const source =
                  draggedTabId.current || event.dataTransfer.getData(dragMime);
                if (source && source !== tab.id)
                  onMove(source, pane.id, tab.id);
                draggedTabId.current = "";
                onDragStateChange(false);
              }}
            >
              <button
                id={`workbench-tab-${tab.id}`}
                ref={(node) => {
                  if (node) buttons.current.set(tab.id, node);
                  else buttons.current.delete(tab.id);
                }}
                type="button"
                role="tab"
                title={tab.title}
                aria-selected={selected}
                tabIndex={selected ? 0 : -1}
                aria-controls={`workbench-panel-${tab.id}`}
                aria-keyshortcuts="Delete Backspace Alt+Shift+ArrowLeft Alt+Shift+ArrowRight Alt+Shift+ArrowUp Alt+Shift+ArrowDown"
                className="shell-header-tab-select"
                onClick={() => onFocus(tab)}
                onFocus={() => onFocusPane(pane.id)}
                onBlur={() => {
                  const request = pendingCloseFocus.current;
                  if (request?.tabId === tab.id) request.cancelled = true;
                }}
                onAuxClick={(event) => {
                  if (event.button === 1 && tab.closeable !== false)
                    onClose(tab);
                }}
                onKeyDown={(event) => onKeyDown(event, tab)}
              >
                <Icon className="size-icon-sm shrink-0" strokeWidth={1.5} />
                <span className="min-w-0 flex-1 truncate">{tab.title}</span>
              </button>
              {tab.closeable !== false ? (
                <button
                  type="button"
                  aria-label={t("workbench.closeTab", { title: tab.title })}
                  title={t("workbench.closeTab", { title: tab.title })}
                  className="shell-header-tab-close"
                  draggable={false}
                  onClick={() => onClose(tab)}
                >
                  <X className="size-icon-sm" strokeWidth={1.5} />
                </button>
              ) : null}
            </div>
          );
        })}
      </div>
      {trailing ? (
        <div
          className="flex shrink-0 items-center"
          data-workbench-pane-trailing
        >
          {trailing}
        </div>
      ) : null}
    </div>
  );
}

type DividerProps = {
  rect: PaneRect;
  split: WorkbenchSplit;
  onResize: (splitId: string, delta: number) => void;
  onSwap: (splitId: string) => void;
  onDraggingChange: (dragging: boolean) => void;
};

function PaneDivider({
  rect,
  split,
  onResize,
  onSwap,
  onDraggingChange,
}: DividerProps) {
  const { t } = useTranslation("chat");
  const last = useRef(0);
  const size = useRef(1);
  const pending = useRef(0);
  const frame = useRef(0);
  const releaseFrame = useRef(0);
  const resizing = useRef(false);
  const flush = useCallback(() => {
    if (frame.current) cancelAnimationFrame(frame.current);
    frame.current = 0;
    const delta = pending.current;
    pending.current = 0;
    if (delta) onResize(split.id, delta / size.current);
  }, [onResize, split.id]);
  useEffect(
    () => () => {
      if (frame.current) cancelAnimationFrame(frame.current);
      if (releaseFrame.current) cancelAnimationFrame(releaseFrame.current);
    },
    [],
  );
  useEffect(() => {
    const end = () => {
      if (!resizing.current) return;
      resizing.current = false;
      flush();
      onDraggingChange(false);
    };
    const release = () => {
      if (releaseFrame.current) cancelAnimationFrame(releaseFrame.current);
      releaseFrame.current = requestAnimationFrame(() => {
        releaseFrame.current = 0;
        end();
      });
    };
    const escape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") end();
    };
    window.addEventListener("pointerup", release, true);
    window.addEventListener("mouseup", release, true);
    window.addEventListener("blur", end);
    window.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("pointerup", release, true);
      window.removeEventListener("mouseup", release, true);
      window.removeEventListener("blur", end);
      window.removeEventListener("keydown", escape);
    };
  }, [flush, onDraggingChange]);
  const vertical = split.direction === "row";
  const dividerPosition = vertical
    ? rect.left + rect.width * split.ratio
    : rect.top + rect.height * split.ratio;
  const style: CSSProperties = vertical
    ? {
        left: `${dividerPosition}%`,
        top: `${rect.top}%`,
        height: `${rect.height}%`,
      }
    : {
        top: `${dividerPosition}%`,
        left: `${rect.left}%`,
        width: `${rect.width}%`,
      };
  const resize = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!event.currentTarget.hasPointerCapture(event.pointerId)) return;
    const coordinate = vertical ? event.clientX : event.clientY;
    pending.current += coordinate - last.current;
    last.current = coordinate;
    if (!frame.current) frame.current = requestAnimationFrame(flush);
  };
  return (
    <>
      <div
        role="separator"
        tabIndex={0}
        aria-label={t("workbench.resizeSplit")}
        aria-orientation={vertical ? "vertical" : "horizontal"}
        aria-valuemin={8}
        aria-valuemax={92}
        aria-valuenow={Math.round(split.ratio * 100)}
        className={cn(
          "peer/divider absolute z-30 touch-none bg-transparent",
          vertical
            ? "after:bg-shell-separator w-2 -translate-x-1/2 cursor-col-resize after:absolute after:inset-y-0 after:left-1/2 after:w-px"
            : "after:bg-shell-separator h-2 -translate-y-1/2 cursor-row-resize after:absolute after:inset-x-0 after:top-1/2 after:h-px",
        )}
        style={style}
        onPointerDown={(event) => {
          event.preventDefault();
          event.currentTarget.focus();
          event.currentTarget.setPointerCapture(event.pointerId);
          last.current = vertical ? event.clientX : event.clientY;
          const canvas =
            event.currentTarget.parentElement?.getBoundingClientRect();
          size.current =
            (Math.max(1, (vertical ? canvas?.width : canvas?.height) ?? 1) *
              (vertical ? rect.width : rect.height)) /
            100;
          resizing.current = true;
          onDraggingChange(true);
        }}
        onPointerMove={resize}
        onPointerUp={(event) => {
          if (!event.currentTarget.hasPointerCapture(event.pointerId)) return;
          resize(event);
          flush();
          resizing.current = false;
          event.currentTarget.releasePointerCapture(event.pointerId);
          onDraggingChange(false);
        }}
        onPointerCancel={() => {
          flush();
          resizing.current = false;
          onDraggingChange(false);
        }}
        onLostPointerCapture={() => {
          flush();
          resizing.current = false;
          onDraggingChange(false);
        }}
        onKeyDown={(event) => {
          const delta =
            event.key === "ArrowRight" || event.key === "ArrowDown"
              ? 0.05
              : event.key === "ArrowLeft" || event.key === "ArrowUp"
                ? -0.05
                : 0;
          if (!delta) return;
          event.preventDefault();
          onResize(split.id, delta);
        }}
      />
      {vertical ? (
        <span
          className="bg-surface-scene pointer-events-none absolute z-40 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-0 transition-opacity peer-hover/divider:pointer-events-auto peer-hover/divider:opacity-100 peer-focus/divider:pointer-events-auto peer-focus/divider:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100 hover:pointer-events-auto hover:opacity-100"
          style={{
            left: `${dividerPosition}%`,
            top: `${rect.top + rect.height / 2}%`,
          }}
        >
          <Button
            type="button"
            size="icon-sm"
            variant="secondary"
            shape="circle"
            aria-label={t("workbench.swapPanes")}
            title={t("workbench.swapPanes")}
            onClick={() => onSwap(split.id)}
          >
            <ArrowLeftRight />
          </Button>
        </span>
      ) : null}
    </>
  );
}

type CanvasProps = {
  layout: WorkbenchNode;
  tabs: WorkbenchTab[];
  draggedTabId: React.RefObject<string>;
  draggingTab: boolean;
  onDragStateChange: (dragging: boolean) => void;
  onFocusPane: (paneId: string) => void;
  onMove: (
    tabId: string,
    paneId: string,
    zone: WorkbenchDropZone,
    beforeTabId?: string,
  ) => void;
  onResize: (splitId: string, delta: number) => void;
  onSwap: (splitId: string) => void;
  renderPaneTabs: (pane: WorkbenchPane) => ReactNode;
  renderTab: (tab: WorkbenchTab, active: boolean) => ReactNode;
};

export function WorkbenchPaneCanvas({
  layout,
  tabs,
  draggedTabId,
  draggingTab,
  onDragStateChange,
  onFocusPane,
  onMove,
  onResize,
  onSwap,
  renderPaneTabs,
  renderTab,
}: CanvasProps) {
  const geometry = paneGeometry(layout);
  const [target, setTarget] = useState<{
    paneId: string;
    zone: WorkbenchDropZone;
  } | null>(null);
  const [draggingDivider, setDraggingDivider] = useState(false);
  useEffect(() => {
    let releaseFrame = 0;
    const clear = () => {
      if (releaseFrame) cancelAnimationFrame(releaseFrame);
      releaseFrame = 0;
      draggedTabId.current = "";
      onDragStateChange(false);
      setTarget(null);
    };
    const release = () => {
      if (releaseFrame) cancelAnimationFrame(releaseFrame);
      releaseFrame = requestAnimationFrame(clear);
    };
    const escape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") clear();
    };
    window.addEventListener("dragend", clear);
    window.addEventListener("drop", clear);
    window.addEventListener("blur", clear);
    window.addEventListener("pointerup", release, true);
    window.addEventListener("mouseup", release, true);
    window.addEventListener("keydown", escape);
    return () => {
      if (releaseFrame) cancelAnimationFrame(releaseFrame);
      window.removeEventListener("dragend", clear);
      window.removeEventListener("drop", clear);
      window.removeEventListener("blur", clear);
      window.removeEventListener("pointerup", release, true);
      window.removeEventListener("mouseup", release, true);
      window.removeEventListener("keydown", escape);
    };
  }, [draggedTabId, onDragStateChange]);
  const paneByTab = new Map<string, { pane: WorkbenchPane; rect: PaneRect }>();
  for (const entry of geometry.panes)
    for (const tabId of entry.pane.tabIds) paneByTab.set(tabId, entry);
  const targetRect = target
    ? geometry.panes.find(({ pane }) => pane.id === target.paneId)?.rect
    : undefined;
  return (
    <div
      data-testid="workbench-content"
      className="relative min-h-0 flex-1 overflow-hidden"
    >
      {tabs.map((tab) => {
        const entry = paneByTab.get(tab.id);
        if (!entry) return null;
        const { pane, rect } = entry;
        const active = pane.activeTabId === tab.id;
        const style =
          rect.top === 0
            ? positioned(rect)
            : {
                ...positioned(rect),
                top: `calc(${rect.top}% + var(--shell-titlebar-height))`,
                height: `calc(${rect.height}% - var(--shell-titlebar-height))`,
              };
        return (
          <div
            id={`workbench-panel-${tab.id}`}
            key={`${tab.id}:${tab.occurrenceId}`}
            role="tabpanel"
            aria-hidden={!active}
            style={style}
            className={cn(
              "workbench-pane-content absolute min-h-0 min-w-0 overflow-hidden",
              !active && "pointer-events-none invisible",
            )}
            onPointerDown={() => onFocusPane(pane.id)}
            onDragOver={(event) => {
              if (!draggedTabId.current) return;
              event.preventDefault();
              event.dataTransfer.dropEffect = "move";
              setTarget({ paneId: pane.id, zone: zoneAt(event) });
            }}
            onDragLeave={(event) => {
              if (
                !event.currentTarget.contains(
                  event.relatedTarget as Node | null,
                )
              )
                setTarget(null);
            }}
            onDrop={(event) => {
              const source =
                draggedTabId.current || event.dataTransfer.getData(dragMime);
              if (!source) return;
              event.preventDefault();
              onMove(
                source,
                pane.id,
                target?.paneId === pane.id ? target.zone : zoneAt(event),
              );
              draggedTabId.current = "";
              onDragStateChange(false);
              setTarget(null);
            }}
          >
            {renderTab(
              tab,
              active &&
                !(tab.kind === "browser" && (draggingTab || draggingDivider)),
            )}
          </div>
        );
      })}
      {geometry.panes
        .filter(({ rect }) => rect.top > 0)
        .map(({ pane, rect }) => (
          <div
            key={pane.id}
            data-workbench-pane-header={pane.id}
            data-workbench-local-header
            className="workbench-pane-header bg-surface-chrome absolute z-10 flex h-(--shell-titlebar-height) min-w-0 items-center overflow-hidden"
            style={{
              left: `${rect.left}%`,
              top: `${rect.top}%`,
              width: `${rect.width}%`,
            }}
            onPointerDown={() => onFocusPane(pane.id)}
          >
            {renderPaneTabs(pane)}
          </div>
        ))}
      {geometry.splits.map(({ split, rect }) => (
        <PaneDivider
          key={split.id}
          rect={rect}
          split={split}
          onResize={onResize}
          onSwap={onSwap}
          onDraggingChange={setDraggingDivider}
        />
      ))}
      {draggingDivider ? (
        <div
          className="absolute inset-0 z-20 cursor-col-resize"
          aria-hidden="true"
        />
      ) : null}
      {target && targetRect ? (
        <div
          aria-hidden="true"
          style={positioned(targetRect)}
          className="pointer-events-none absolute z-20"
        >
          <div
            className={cn(
              "border-accent-default bg-selection-surface absolute border-2",
              target.zone === "left" && "inset-y-0 left-0 w-1/4",
              target.zone === "right" && "inset-y-0 right-0 w-1/4",
              target.zone === "up" && "inset-x-0 top-0 h-1/4",
              target.zone === "down" && "inset-x-0 bottom-0 h-1/4",
              target.zone === "center" &&
                "top-1/4 right-1/4 bottom-1/4 left-1/4 border-dashed bg-transparent",
            )}
            data-workbench-drop-zone={target.zone}
          />
        </div>
      ) : null}
    </div>
  );
}
