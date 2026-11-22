import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { BrowserSurfaceProps } from "@neuxbot/frontend/browser-surface";
import {
  closeBrowserView,
  controlBrowserView,
  createBrowserView,
  isDesktopRuntime,
  navigateBrowserView,
  setBrowserViewBounds,
  setBrowserViewVisible,
  subscribeBrowserNavigation,
  type BrowserBounds,
  type BrowserControlAction,
} from "@/shared/platform";
import {
  observeNativeOverlayRegions,
  useNativeOverlayVisible,
} from "@/shared/ui/native-overlay";
import { logger } from "@/shared/diagnostics/logger";

export function browserErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return fallback;
}

function elementBounds(element: HTMLElement): BrowserBounds | null {
  const bounds = element.getBoundingClientRect();
  if (bounds.width <= 0 || bounds.height <= 0) return null;
  return {
    x: bounds.left,
    y: bounds.top,
    width: bounds.width,
    height: bounds.height,
  };
}

function sameBounds(first: BrowserBounds, second: BrowserBounds) {
  return (
    first.x === second.x &&
    first.y === second.y &&
    first.width === second.width &&
    first.height === second.height
  );
}

/** 原生子视图只能占一个矩形，通知重叠时保留面积最大的未遮挡一侧。 */
function boundsOutsideRegion(bounds: BrowserBounds, region: DOMRect) {
  const right = bounds.x + bounds.width;
  const bottom = bounds.y + bounds.height;
  const leftEdge = Math.max(bounds.x, region.left);
  const rightEdge = Math.min(right, region.right);
  const topEdge = Math.max(bounds.y, region.top);
  const bottomEdge = Math.min(bottom, region.bottom);
  if (leftEdge >= rightEdge || topEdge >= bottomEdge) return bounds;

  const candidates = [
    { ...bounds, y: bottomEdge, height: bottom - bottomEdge },
    { ...bounds, width: leftEdge - bounds.x },
    { ...bounds, height: topEdge - bounds.y },
    { ...bounds, x: rightEdge, width: right - rightEdge },
  ];
  let largest: BrowserBounds | null = null;
  let largestArea = 0;
  for (const candidate of candidates) {
    const area = candidate.width * candidate.height;
    if (area <= largestArea) continue;
    largest = candidate;
    largestArea = area;
  }
  return largest;
}

/** 文件 HTML 预览的受限原生视图生命周期。 */
export function useBrowserSurface({
  active,
  enabled = true,
  url,
  workspaceRoot = "",
  chatId,
  onURLChange,
  onNavigationState,
}: BrowserSurfaceProps) {
  const { t } = useTranslation("chat");
  const overlayVisible = useNativeOverlayVisible();
  const visible = active && !overlayVisible;
  const hostRef = useRef<HTMLDivElement>(null);
  const viewIDRef = useRef<string | null>(null);
  const activeRef = useRef(visible);
  const applyLayoutRef = useRef<() => void>(undefined);
  const workspaceRootRef = useRef(workspaceRoot);
  const chatIdRef = useRef(chatId);
  const localPreview = url.startsWith("file:");
  const viewScope = localPreview
    ? `file:${workspaceRoot}\0${chatId ?? ""}`
    : "web";
  const nativeViewScopeRef = useRef(viewScope);
  const lastNativeURLRef = useRef(url || "about:blank");
  const desiredURLRef = useRef(url);
  const lifecycleRef = useRef({
    createFailed: t("browserCreateFailed"),
    onURLChange,
    onNavigationState,
    resizeFailed: t("browserResizeFailed"),
    visibilityFailed: t("browserVisibilityFailed"),
  });
  const [browserReady, setBrowserReady] = useState(false);
  const [error, setError] = useState(() =>
    isDesktopRuntime() ? "" : t("browserDesktopOnly"),
  );
  useEffect(() => {
    workspaceRootRef.current = workspaceRoot;
    chatIdRef.current = chatId;
  }, [chatId, workspaceRoot]);
  useEffect(() => {
    lifecycleRef.current = {
      createFailed: t("browserCreateFailed"),
      onURLChange,
      onNavigationState,
      resizeFailed: t("browserResizeFailed"),
      visibilityFailed: t("browserVisibilityFailed"),
    };
  }, [onNavigationState, onURLChange, t]);

  useEffect(() => {
    activeRef.current = visible;
    applyLayoutRef.current?.();
  }, [visible]);

  useEffect(() => {
    desiredURLRef.current = url;
    if (!url) return;
    if (nativeViewScopeRef.current !== viewScope) return;
    if (url === lastNativeURLRef.current) return;
    const viewID = viewIDRef.current;
    if (!viewID) return;
    lastNativeURLRef.current = url;
    void navigateBrowserView(viewID, url).catch((cause: unknown) => {
      setError(browserErrorMessage(cause, t("browserNavigationFailed")));
    });
  }, [t, url, viewScope]);

  useEffect(() => {
    if (!enabled || !isDesktopRuntime()) return;
    const creationRoot = workspaceRootRef.current;
    const creationChatId = chatIdRef.current;
    nativeViewScopeRef.current = viewScope;
    const initialURL = desiredURLRef.current || "about:blank";
    lastNativeURLRef.current = initialURL;
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let animationFrame = 0;
    let creating = false;
    let appliedBounds: BrowserBounds | null = null;
    let appliedVisibility: boolean | undefined;
    let regions: ReadonlySet<HTMLElement> = new Set();
    setBrowserReady(false);
    setError("");
    const unsubscribeNavigation = subscribeBrowserNavigation((event) => {
      if (event.viewId !== viewIDRef.current) return;
      lastNativeURLRef.current = event.url;
      lifecycleRef.current.onURLChange(event.url);
      lifecycleRef.current.onNavigationState?.(event);
    });
    const readBounds = () => {
      let bounds = elementBounds(host);
      for (const region of regions) {
        if (!bounds) break;
        bounds = boundsOutsideRegion(bounds, region.getBoundingClientRect());
      }
      return bounds;
    };
    const updateBounds = (viewID: string, bounds: BrowserBounds | null) => {
      if (!bounds || (appliedBounds && sameBounds(appliedBounds, bounds)))
        return;
      appliedBounds = bounds;
      return setBrowserViewBounds(viewID, bounds).catch((cause: unknown) => {
        if (appliedBounds === bounds) appliedBounds = null;
        throw cause;
      });
    };
    const updateVisibility = (viewID: string, bounds: BrowserBounds | null) => {
      const nextVisible = activeRef.current && bounds !== null;
      if (nextVisible === appliedVisibility) return;
      appliedVisibility = nextVisible;
      return setBrowserViewVisible(viewID, nextVisible).catch(
        (cause: unknown) => {
          if (appliedVisibility === nextVisible) appliedVisibility = undefined;
          throw cause;
        },
      );
    };
    const createView = (bounds: BrowserBounds) => {
      if (creating || viewIDRef.current) return;
      creating = true;
      let createdViewID: string | null = null;
      void createBrowserView(
        initialURL,
        bounds,
        creationRoot || undefined,
        creationChatId,
      )
        .then(async (viewID) => {
          if (disposed) {
            await closeBrowserView(viewID).catch((cause: unknown) => {
              logger.error("关闭已卸载的内置浏览器失败", {
                error: browserErrorMessage(cause, "未知错误"),
              });
            });
            return;
          }
          createdViewID = viewID;
          viewIDRef.current = viewID;
          appliedBounds = bounds;
          appliedVisibility = undefined;
          setError("");
          const currentBounds = readBounds();
          await Promise.all([
            updateBounds(viewID, currentBounds),
            updateVisibility(viewID, currentBounds),
          ]);
          if (disposed) return;
          const desiredURL = desiredURLRef.current;
          if (desiredURL && desiredURL !== lastNativeURLRef.current) {
            lastNativeURLRef.current = desiredURL;
            await navigateBrowserView(viewID, desiredURL);
          }
          if (disposed) return;
          creating = false;
          setBrowserReady(true);
          syncBounds();
        })
        .catch((cause: unknown) => {
          creating = false;
          if (createdViewID && viewIDRef.current === createdViewID) {
            viewIDRef.current = null;
            void closeBrowserView(createdViewID).catch(
              (closeCause: unknown) => {
                logger.error("回收初始化失败的内置浏览器失败", {
                  error: browserErrorMessage(closeCause, "未知错误"),
                });
              },
            );
          }
          if (!disposed) {
            setBrowserReady(false);
            setError(
              browserErrorMessage(cause, lifecycleRef.current.createFailed),
            );
          }
        });
    };
    const applyLayout = () => {
      if (creating) return;
      const nextBounds = readBounds();
      const viewID = viewIDRef.current;
      if (!viewID) {
        if (nextBounds) createView(nextBounds);
        return;
      }
      void updateBounds(viewID, nextBounds)?.catch((cause: unknown) => {
        if (!disposed && viewIDRef.current === viewID)
          setError(
            browserErrorMessage(cause, lifecycleRef.current.resizeFailed),
          );
      });
      void updateVisibility(viewID, nextBounds)?.catch((cause: unknown) => {
        if (!disposed && viewIDRef.current === viewID)
          setError(
            browserErrorMessage(cause, lifecycleRef.current.visibilityFailed),
          );
      });
    };
    const syncBounds = () => {
      if (animationFrame) return;
      animationFrame = window.requestAnimationFrame(() => {
        animationFrame = 0;
        applyLayout();
      });
    };
    applyLayoutRef.current = applyLayout;
    const resizeObserver = new ResizeObserver(syncBounds);
    resizeObserver.observe(host);
    const observedRegions = new Set<HTMLElement>();
    const unsubscribeRegions = observeNativeOverlayRegions((nextRegions) => {
      for (const region of observedRegions) {
        if (nextRegions.has(region)) continue;
        resizeObserver.unobserve(region);
        observedRegions.delete(region);
      }
      for (const region of nextRegions) {
        if (observedRegions.has(region)) continue;
        resizeObserver.observe(region, { box: "border-box" });
        observedRegions.add(region);
      }
      regions = nextRegions;
      syncBounds();
    });
    const onWindowResize = () => {
      // 主进程按当前缩放换算坐标，CSS 矩形未变时仍需重新同步。
      appliedBounds = null;
      syncBounds();
    };
    window.addEventListener("resize", onWindowResize);
    window.addEventListener("scroll", syncBounds, true);

    return () => {
      disposed = true;
      applyLayoutRef.current = undefined;
      unsubscribeNavigation();
      unsubscribeRegions();
      resizeObserver.disconnect();
      window.removeEventListener("resize", onWindowResize);
      window.removeEventListener("scroll", syncBounds, true);
      if (animationFrame) window.cancelAnimationFrame(animationFrame);
      const viewID = viewIDRef.current;
      viewIDRef.current = null;
      if (viewID) {
        void closeBrowserView(viewID).catch((cause: unknown) => {
          logger.error("关闭内置浏览器失败", {
            error: browserErrorMessage(cause, "未知错误"),
          });
        });
      }
    };
  }, [enabled, viewScope]);

  const runBrowserAction = (action: BrowserControlAction) => {
    const viewID = viewIDRef.current;
    if (!viewID) return;
    setError("");
    void controlBrowserView(viewID, action).catch((cause: unknown) => {
      setError(browserErrorMessage(cause, t("browserControlFailed")));
    });
  };

  return { hostRef, browserReady, error, setError, runBrowserAction };
}
