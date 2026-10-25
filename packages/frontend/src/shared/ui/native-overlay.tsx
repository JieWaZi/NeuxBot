import { useLayoutEffect, useRef, useSyncExternalStore } from "react";

type Listener = () => void;

const activeOverlays = new Set<symbol>();
const listeners = new Set<Listener>();

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: Listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function hasActiveOverlay() {
  return activeOverlays.size > 0;
}

/** 注册需要显示在 Electron 原生子视图上方的 Renderer 浮层。 */
export function NativeOverlayPresence({ active = true }: { active?: boolean }) {
  const tokenRef = useRef(Symbol("native-overlay"));

  useLayoutEffect(() => {
    if (!active) return;
    const token = tokenRef.current;
    const wasEmpty = activeOverlays.size === 0;
    activeOverlays.add(token);
    if (wasEmpty) emit();
    return () => {
      const wasActive = activeOverlays.delete(token);
      if (wasActive && activeOverlays.size === 0) emit();
    };
  }, [active]);

  return null;
}

/** 原生浏览器视图据此为全部已注册 Renderer 浮层让出层级。 */
export function useNativeOverlayVisible() {
  return useSyncExternalStore(subscribe, hasActiveOverlay, () => false);
}
