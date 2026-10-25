import {
  useCallback,
  createContext,
  useContext,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

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
}: {
  navigationCollapsed?: boolean;
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
      />
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
