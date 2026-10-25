import {
  useRef,
  useState,
  type Dispatch,
  type RefObject,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";
import type {
  BrowserControlAction,
  BrowserNavigationEvent,
} from "@neuxbot/platform-contracts/resources";

export type EmbeddedBrowserSurfaceProps = {
  active: boolean;
  enabled: boolean;
  url: string;
  workspaceRoot: string;
  chatId?: string;
  onURLChange: (url: string) => void;
  onNavigationState?: (event: BrowserNavigationEvent) => void;
};
export type EmbeddedBrowserSurfaceResult = {
  hostRef: RefObject<HTMLDivElement | null>;
  browserReady: boolean;
  error: string;
  setError: Dispatch<SetStateAction<string>>;
  runBrowserAction: (action: BrowserControlAction) => void;
};
export type EmbeddedBrowserSurfaceHook = (
  props: EmbeddedBrowserSurfaceProps,
) => EmbeddedBrowserSurfaceResult;

function useUnavailableBrowserSurface(): EmbeddedBrowserSurfaceResult {
  const { t } = useTranslation("chat");
  const hostRef = useRef<HTMLDivElement>(null);
  const [error, setError] = useState(() => t("browserDesktopOnly"));
  return {
    hostRef,
    browserReady: false,
    error,
    setError,
    runBrowserAction: () => {},
  };
}
let useSurface: EmbeddedBrowserSurfaceHook = useUnavailableBrowserSurface;
/** 仅在应用挂载前注入；挂载期间保持同一 Hook，遵守 React 调用顺序。 */
export function configureEmbeddedBrowserSurface(
  hook: EmbeddedBrowserSurfaceHook,
): void {
  useSurface = hook;
}
export function usePlatformEmbeddedBrowserSurface(
  props: EmbeddedBrowserSurfaceProps,
): EmbeddedBrowserSurfaceResult {
  return useSurface(props);
}

export type BrowserSurfaceProps = Omit<
  EmbeddedBrowserSurfaceProps,
  "enabled" | "workspaceRoot"
> & {
  enabled?: boolean;
  workspaceRoot?: string;
};
export type BrowserSurfaceHook = (
  props: BrowserSurfaceProps,
) => EmbeddedBrowserSurfaceResult;
let useNativeSurface: BrowserSurfaceHook = useUnavailableBrowserSurface;
/** 原生受管 HTML 与外部网页视图由宿主保持其身份和坐标。 */
export function configureBrowserSurface(hook: BrowserSurfaceHook): void {
  useNativeSurface = hook;
}
export function usePlatformBrowserSurface(
  props: BrowserSurfaceProps,
): EmbeddedBrowserSurfaceResult {
  return useNativeSurface(props);
}
