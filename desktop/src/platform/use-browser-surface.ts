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
import { useNativeOverlayVisible as useBrowserOverlayVisible } from "@/shared/ui/native-overlay";
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
  const overlayVisible = useBrowserOverlayVisible();
  const visible = active && !overlayVisible;
  const hostRef = useRef<HTMLDivElement>(null);
  const viewIDRef = useRef<string | null>(null);
  const activeRef = useRef(visible);
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
    };
  }, [onNavigationState, onURLChange, t]);

  useEffect(() => {
    activeRef.current = visible;
    const viewID = viewIDRef.current;
    if (!viewID) return;
    void setBrowserViewVisible(viewID, visible).catch((cause: unknown) => {
      setError(browserErrorMessage(cause, t("browserVisibilityFailed")));
    });
  }, [visible, t]);

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
    setBrowserReady(false);
    setError("");
    const unsubscribeNavigation = subscribeBrowserNavigation((event) => {
      if (event.viewId !== viewIDRef.current) return;
      lastNativeURLRef.current = event.url;
      lifecycleRef.current.onURLChange(event.url);
      lifecycleRef.current.onNavigationState?.(event);
    });
    const createView = (
      bounds: NonNullable<ReturnType<typeof elementBounds>>,
    ) => {
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
          setError("");
          await setBrowserViewVisible(viewID, activeRef.current);
          if (disposed) return;
          const desiredURL = desiredURLRef.current;
          if (desiredURL && desiredURL !== lastNativeURLRef.current) {
            lastNativeURLRef.current = desiredURL;
            await navigateBrowserView(viewID, desiredURL);
          }
          if (disposed) return;
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
    const syncBounds = () => {
      if (animationFrame) return;
      animationFrame = window.requestAnimationFrame(() => {
        animationFrame = 0;
        const nextBounds = elementBounds(host);
        if (!nextBounds) return;
        const viewID = viewIDRef.current;
        if (!viewID) {
          createView(nextBounds);
          return;
        }
        void setBrowserViewBounds(viewID, nextBounds).catch(
          (cause: unknown) => {
            if (!disposed) {
              setError(
                browserErrorMessage(cause, lifecycleRef.current.resizeFailed),
              );
            }
          },
        );
      });
    };
    const resizeObserver = new ResizeObserver(syncBounds);
    resizeObserver.observe(host);
    window.addEventListener("resize", syncBounds);
    syncBounds();

    return () => {
      disposed = true;
      unsubscribeNavigation();
      resizeObserver.disconnect();
      window.removeEventListener("resize", syncBounds);
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
