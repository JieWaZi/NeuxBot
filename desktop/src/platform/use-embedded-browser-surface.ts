import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { EmbeddedBrowserSurfaceProps } from "@neuxbot/frontend/browser-surface";

import {
  isDesktopRuntime,
  prepareBrowserWebview,
  type BrowserControlAction,
} from "@/shared/platform";
function browserErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return fallback;
}

type EmbeddedWebview = HTMLElement & {
  getURL(): string;
  getTitle(): string;
  getWebContentsId(): number;
  canGoBack(): boolean;
  canGoForward(): boolean;
  goBack(): void;
  goForward(): void;
  reload(): void;
  loadURL(url: string): Promise<void>;
};

/** 网页访客视图参与 Renderer 合成，让同一窗口中的悬浮对话盖在网页上方。 */
export function useEmbeddedBrowserSurface({
  active,
  enabled,
  url,
  workspaceRoot,
  chatId,
  onURLChange,
  onNavigationState,
}: EmbeddedBrowserSurfaceProps) {
  const { t } = useTranslation("chat");
  const hostRef = useRef<HTMLDivElement>(null);
  const guestRef = useRef<EmbeddedWebview | null>(null);
  const readyRef = useRef(false);
  const desiredURLRef = useRef(url);
  const lastURLRef = useRef(url);
  const callbacksRef = useRef({ onURLChange, onNavigationState });
  const lifecycleRef = useRef({ active, url, t });
  const [browserReady, setBrowserReady] = useState(false);
  const [error, setError] = useState(() =>
    isDesktopRuntime() ? "" : t("browserDesktopOnly"),
  );
  // 无本地目录的网页使用临时访客分区，首次 Chat 接纳不改变其原生身份。
  const scopedChatId = workspaceRoot ? chatId : undefined;

  useEffect(() => {
    callbacksRef.current = { onURLChange, onNavigationState };
  }, [onNavigationState, onURLChange]);

  useEffect(() => {
    lifecycleRef.current = { active, url, t };
    desiredURLRef.current = url;
  }, [active, t, url]);

  useEffect(() => {
    if (!enabled || !isDesktopRuntime()) return;
    const host = hostRef.current;
    if (!host) return;
    let disposed = false;
    let guest: EmbeddedWebview | null = null;
    let removeGuestListeners = () => {};
    let initialReady = false;
    const initialURL = lifecycleRef.current.url;
    readyRef.current = false;
    lastURLRef.current = initialURL;
    setBrowserReady(false);
    setError("");

    void prepareBrowserWebview(
      initialURL,
      workspaceRoot || undefined,
      scopedChatId,
    )
      .then((partition) => {
        if (disposed) return;
        guest = document.createElement("webview") as EmbeddedWebview;
        guest.setAttribute("partition", partition);
        guest.setAttribute("src", initialURL);
        guest.style.display = "flex";
        guest.style.width = "100%";
        guest.style.height = "100%";
        guest.style.visibility = lifecycleRef.current.active
          ? "visible"
          : "hidden";
        const current = guest;
        const publish = () => {
          if (disposed) return;
          const nextURL = current.getURL();
          if (!nextURL) return;
          lastURLRef.current = nextURL;
          callbacksRef.current.onURLChange(nextURL);
          callbacksRef.current.onNavigationState?.({
            viewId: String(current.getWebContentsId()),
            url: nextURL,
            title: current.getTitle(),
            canGoBack: current.canGoBack(),
            canGoForward: current.canGoForward(),
          });
        };
        const ready = () => {
          if (disposed) return;
          const firstReady = !initialReady;
          initialReady = true;
          readyRef.current = true;
          setBrowserReady(true);
          const desired = desiredURLRef.current;
          if (
            firstReady &&
            desired &&
            desired !== initialURL &&
            desired !== current.getURL()
          ) {
            lastURLRef.current = desired;
            void current.loadURL(desired).catch((cause: unknown) => {
              if (!disposed)
                setError(
                  browserErrorMessage(
                    cause,
                    lifecycleRef.current.t("browserNavigationFailed"),
                  ),
                );
            });
            return;
          }
          publish();
        };
        const failed = (event: Event) => {
          if (disposed) return;
          const failure = event as Event & {
            errorCode?: number;
            errorDescription?: string;
            isMainFrame?: boolean;
          };
          if (failure.errorCode === -3 || failure.isMainFrame === false) return;
          setError(
            failure.errorDescription ||
              lifecycleRef.current.t("browserNavigationFailed"),
          );
        };
        current.addEventListener("dom-ready", ready);
        current.addEventListener("did-navigate", publish);
        current.addEventListener("did-navigate-in-page", publish);
        current.addEventListener("page-title-updated", publish);
        current.addEventListener("did-fail-load", failed);
        removeGuestListeners = () => {
          current.removeEventListener("dom-ready", ready);
          current.removeEventListener("did-navigate", publish);
          current.removeEventListener("did-navigate-in-page", publish);
          current.removeEventListener("page-title-updated", publish);
          current.removeEventListener("did-fail-load", failed);
        };
        guestRef.current = current;
        host.appendChild(current);
      })
      .catch((cause: unknown) => {
        if (!disposed)
          setError(
            browserErrorMessage(
              cause,
              lifecycleRef.current.t("browserCreateFailed"),
            ),
          );
      });

    return () => {
      disposed = true;
      readyRef.current = false;
      guestRef.current = null;
      removeGuestListeners();
      guest?.remove();
    };
  }, [scopedChatId, enabled, workspaceRoot]);

  useEffect(() => {
    if (guestRef.current)
      guestRef.current.style.visibility = active ? "visible" : "hidden";
  }, [active]);

  useEffect(() => {
    desiredURLRef.current = url;
    const guest = guestRef.current;
    if (!guest || !readyRef.current || !url || url === lastURLRef.current)
      return;
    let cancelled = false;
    lastURLRef.current = url;
    void guest.loadURL(url).catch((cause: unknown) => {
      if (!cancelled)
        setError(browserErrorMessage(cause, t("browserNavigationFailed")));
    });
    return () => {
      cancelled = true;
    };
  }, [t, url]);

  const runBrowserAction = (action: BrowserControlAction) => {
    const guest = guestRef.current;
    if (!guest || !readyRef.current) return;
    setError("");
    if (action === "reload") guest.reload();
    else if (action === "back" && guest.canGoBack()) guest.goBack();
    else if (action === "forward" && guest.canGoForward()) guest.goForward();
  };

  return { hostRef, browserReady, error, setError, runBrowserAction };
}
