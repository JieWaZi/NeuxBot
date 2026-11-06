import {
  useCallback,
  createContext,
  useContext,
  useMemo,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import { cn } from "@/shared/lib/cn";
import { useSceneChromeActions } from "./scene-chrome-actions";

type SceneChromePortalContextValue = {
  setTarget: (target: HTMLDivElement | null) => void;
  target: HTMLDivElement | null;
  paneActionsTarget: HTMLDivElement | null;
  setPaneActionsTarget: (target: HTMLDivElement | null) => void;
  accessoryTarget: HTMLDivElement | null;
  setAccessoryTarget: (target: HTMLDivElement | null) => void;
  accessoryWidth: number | null;
  accessoryFullscreen: boolean;
  accessorySide: "left" | "right";
  setAccessoryWidth: (
    owner: string,
    width: number | null,
    fullscreen?: boolean,
    side?: "left" | "right",
  ) => void;
};

const SceneChromePortalContext =
  createContext<SceneChromePortalContextValue | null>(null);

export function SceneChromePortalProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [target, setTarget] = useState<HTMLDivElement | null>(null);
  const [paneActionsTarget, setPaneActionsTarget] =
    useState<HTMLDivElement | null>(null);
  const [accessoryTarget, setAccessoryTarget] = useState<HTMLDivElement | null>(
    null,
  );
  const [accessory, setAccessory] = useState<{
    owner: string;
    width: number;
    fullscreen: boolean;
    side: "left" | "right";
  } | null>(null);
  const setAccessoryWidth = useCallback(
    (
      owner: string,
      width: number | null,
      fullscreen = false,
      side: "left" | "right" = "right",
    ) =>
      setAccessory((current) =>
        width === null
          ? current?.owner === owner
            ? null
            : current
          : { owner, width, fullscreen, side },
      ),
    [],
  );
  const value = useMemo(
    () => ({
      setTarget,
      target,
      paneActionsTarget,
      setPaneActionsTarget,
      accessoryTarget,
      setAccessoryTarget,
      accessoryWidth: accessory?.width ?? null,
      accessoryFullscreen: accessory?.fullscreen ?? false,
      accessorySide: accessory?.side ?? "right",
      setAccessoryWidth,
    }),
    [target, paneActionsTarget, accessoryTarget, accessory, setAccessoryWidth],
  );

  return (
    <SceneChromePortalContext.Provider value={value}>
      {children}
    </SceneChromePortalContext.Provider>
  );
}

export function SceneChromeHost({
  navigationCollapsed = false,
  children,
}: {
  navigationCollapsed?: boolean;
  children?: ReactNode;
}) {
  const context = useContext(SceneChromePortalContext);

  return (
    <div
      className="relative h-(--shell-titlebar-height) min-w-0"
      data-navigation-collapsed={navigationCollapsed || undefined}
      data-scene-chrome-host
      style={
        {
          "--scene-chrome-left-inset": navigationCollapsed
            ? "var(--shell-titlebar-navigation-width)"
            : "8px",
        } as CSSProperties
      }
    >
      <div
        ref={context?.setTarget}
        className={
          navigationCollapsed
            ? "h-full min-w-0 [&_[data-scene-tab-strip]]:pl-(--shell-titlebar-navigation-width) [&_[data-scene-tab-strip][data-accessory-left]]:pl-0"
            : "h-full min-w-0"
        }
      >
        {children}
      </div>
    </div>
  );
}

export function SceneChromePortal({ children }: { children: ReactNode }) {
  const target = useContext(SceneChromePortalContext)?.target;
  return target ? createPortal(children, target) : children;
}

/** 当前 Scene 的内容将操作挂到主 pane 顶栏右端，随分屏排列移动。 */
export function SceneChromePaneActionsPortal({
  children,
}: {
  children: ReactNode;
}) {
  const target = useContext(SceneChromePortalContext)?.paneActionsTarget;
  return target ? createPortal(children, target) : null;
}

export function useSceneChromePaneActionsTargetSetter() {
  return useContext(SceneChromePortalContext)?.setPaneActionsTarget;
}

export function useSceneChromePortalTarget() {
  return useContext(SceneChromePortalContext)?.target ?? null;
}

export function useSceneChromeAccessoryTarget() {
  return useContext(SceneChromePortalContext)?.accessoryTarget ?? null;
}

export function useSceneChromeAccessoryTargetSetter() {
  return useContext(SceneChromePortalContext)?.setAccessoryTarget;
}

export function useSceneChromeAccessoryWidth() {
  return useContext(SceneChromePortalContext)?.accessoryWidth ?? null;
}

export function useSceneChromeAccessoryFullscreen() {
  return useContext(SceneChromePortalContext)?.accessoryFullscreen ?? false;
}

export function useSceneChromeAccessorySide() {
  return useContext(SceneChromePortalContext)?.accessorySide ?? "right";
}

export function useSceneChromeAccessoryWidthSetter() {
  return useContext(SceneChromePortalContext)?.setAccessoryWidth;
}

/** 主顶栏与独立 Scene 复用同一组 pane portal，宿主实例保持唯一。 */
export function SceneChromeLayout({
  children,
  className,
  leading,
  paneActions,
  actions,
}: {
  children: ReactNode;
  className?: string;
  leading?: ReactNode;
  paneActions?: ReactNode;
  actions?: ReactNode;
}) {
  const inheritedActions = useSceneChromeActions();
  const sceneActions = actions ?? inheritedActions;
  const setAccessoryTarget = useSceneChromeAccessoryTargetSetter();
  const setPaneActionsTarget = useSceneChromePaneActionsTargetSetter();
  const accessoryWidth = useSceneChromeAccessoryWidth();
  const accessoryFullscreen = useSceneChromeAccessoryFullscreen();
  const accessorySide = useSceneChromeAccessorySide();
  const accessoryAtLeft =
    accessorySide === "left" && Boolean(accessoryWidth) && !accessoryFullscreen;
  const actionRef = useRef<HTMLDivElement>(null);
  const [actionWidth, setActionWidth] = useState(0);
  useLayoutEffect(() => {
    const actionElement = actionRef.current;
    if (!actionElement) return;
    const measure = () =>
      setActionWidth(actionElement.getBoundingClientRect().width);
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(measure);
    observer?.observe(actionElement);
    return () => observer?.disconnect();
  }, [sceneActions, accessoryAtLeft]);
  const accessory = (
    <div
      key="accessory"
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
            "bg-shell-separator pointer-events-none absolute top-1.5 bottom-1.5 z-10 w-px",
            accessoryAtLeft ? "right-0" : "left-0",
          )}
          data-scene-chrome-divider
        />
      ) : null}
      {sceneActions && !accessoryAtLeft ? (
        <div
          ref={actionRef}
          data-desktop-no-drag-region
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
      data-accessory-left={accessoryAtLeft || undefined}
      className={cn(
        "bg-surface-chrome flex h-(--shell-titlebar-height) min-w-0 shrink-0 items-center pr-0",
        accessoryAtLeft ? "pl-0" : "pl-2",
        className,
      )}
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
      {children}
      {!accessoryFullscreen ? <div className="min-w-0 flex-1" /> : null}
      <div
        data-desktop-no-drag-region
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
          data-desktop-no-drag-region
          ref={actionRef}
          className="flex h-full shrink-0 items-center gap-1 pr-2"
        >
          {sceneActions}
        </div>
      ) : null}
    </div>
  );
}
