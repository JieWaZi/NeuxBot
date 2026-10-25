import {
  ArrowLeft,
  ArrowRight,
  Columns3,
  ExternalLink,
  FileDiff,
  Files,
  MessagesSquare,
  RotateCw,
  TerminalSquare,
  X,
} from "lucide-react";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

import {
  isDesktopRuntime,
  normalizeBrowserURL,
  openExternalURL,
} from "@/shared/platform";
import { useEmbeddedBrowserSurface } from "./use-embedded-browser-surface";
import { browserErrorMessage } from "./use-browser-surface";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
type ChatBrowserViewProps = {
  active: boolean;
  chatId?: string;
  onClose?: () => void;
  onOpenTool?: (
    kind: "files" | "terminal" | "changes" | "sidechat" | "board",
  ) => void;
  onTitleChange?: (title: string) => void;
  onURLChange: (url: string) => void;
  url: string;
  workspaceRoot?: string;
  showBoardTool?: boolean;
};

function browserAddressValue(url: string) {
  if (url === "about:blank") return "";
  return url;
}

/**
 * 浏览器 Tab 的桌面适配层。网页在受限访客视图中渲染，并与悬浮对话参与同一页面合成。
 */
export function ChatBrowserView({
  active,
  chatId,
  onClose,
  onOpenTool,
  onTitleChange,
  onURLChange,
  url,
  workspaceRoot = "",
  showBoardTool = false,
}: ChatBrowserViewProps) {
  const { t } = useTranslation("chat");
  const isNewTab = !url || url === "about:blank";
  const [navigation, setNavigation] = useState({
    canGoBack: false,
    canGoForward: false,
  });
  const { hostRef, browserReady, error, setError, runBrowserAction } =
    useEmbeddedBrowserSurface({
      active,
      enabled: !isNewTab,
      url,
      workspaceRoot,
      chatId,
      onURLChange,
      onNavigationState: (event) => {
        setNavigation({
          canGoBack: event.canGoBack,
          canGoForward: event.canGoForward,
        });
        if (event.title) onTitleChange?.(event.title);
      },
    });
  const [addressState, setAddressState] = useState({
    address: browserAddressValue(url),
    sourceRoot: workspaceRoot,
    sourceURL: url,
  });
  if (
    addressState.sourceURL !== url ||
    addressState.sourceRoot !== workspaceRoot
  ) {
    setAddressState({
      address: browserAddressValue(url),
      sourceRoot: workspaceRoot,
      sourceURL: url,
    });
  }
  const address = addressState.address;

  const submitAddress = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalized = normalizeBrowserURL(address);
    if (!normalized) {
      setError(t("browserInvalidURL"));
      return;
    }
    setError("");
    onURLChange(normalized);
  };
  const externalURL = normalizeBrowserURL(address);

  return (
    <section
      aria-label={t("browser")}
      className={cn(
        "absolute inset-0 min-h-0 flex-col",
        isNewTab ? "bg-surface-chrome" : "bg-surface",
        active ? "flex" : "hidden",
      )}
      data-chat-browser-view
    >
      <div
        className={cn(
          "flex h-(--shell-titlebar-height) shrink-0 items-center gap-2 px-2",
          isNewTab ? "bg-surface-chrome" : "bg-surface-canvas",
        )}
      >
        <div className="border-border-subtle bg-surface-panel flex shrink-0 items-center gap-0.5 rounded-full border px-0.5">
          <Button
            aria-label={t("browserBack")}
            disabled={!browserReady || !navigation.canGoBack}
            size="icon-sm"
            title={t("browserBack")}
            type="button"
            variant="ghost"
            onClick={() => runBrowserAction("back")}
          >
            <ArrowLeft />
          </Button>
          <Button
            aria-label={t("browserForward")}
            disabled={!browserReady || !navigation.canGoForward}
            size="icon-sm"
            title={t("browserForward")}
            type="button"
            variant="ghost"
            onClick={() => runBrowserAction("forward")}
          >
            <ArrowRight />
          </Button>
          <Button
            aria-label={t("browserReload")}
            disabled={!browserReady}
            size="icon-sm"
            title={t("browserReload")}
            type="button"
            variant="ghost"
            onClick={() => runBrowserAction("reload")}
          >
            <RotateCw />
          </Button>
        </div>
        <form className="min-w-0 flex-1" onSubmit={submitAddress}>
          <Input
            aria-label={t("browserAddress")}
            appearance="quiet"
            className="px-4 text-center focus:text-left"
            onChange={(event) =>
              setAddressState({
                address: event.target.value,
                sourceRoot: workspaceRoot,
                sourceURL: url,
              })
            }
            placeholder={t("browserAddressPlaceholder")}
            size="sm"
            shape="pill"
            spellCheck={false}
            value={address}
          />
        </form>
        <Button
          aria-label={t("openInSystemBrowser")}
          disabled={!externalURL}
          size="icon-sm"
          title={t("openInSystemBrowser")}
          type="button"
          variant="ghost"
          onClick={() => {
            const normalized = externalURL;
            if (!normalized) {
              setError(t("browserInvalidURL"));
              return;
            }
            void openExternalURL(normalized).catch((cause: unknown) => {
              setError(
                browserErrorMessage(cause, t("browserExternalOpenFailed")),
              );
            });
          }}
        >
          <ExternalLink />
        </Button>
        {onClose ? (
          <Button
            aria-label={t("workspace.closeContent")}
            size="icon-sm"
            title={t("workspace.closeContent")}
            type="button"
            variant="ghost"
            onClick={onClose}
          >
            <X />
          </Button>
        ) : null}
      </div>
      {error ? (
        <p
          className="border-border bg-muted text-muted-foreground type-caption shrink-0 border-b px-3 py-2"
          role="alert"
        >
          {error}
        </p>
      ) : null}
      <div
        className={cn(
          "relative min-h-0 flex-1",
          isNewTab ? "bg-surface-chrome" : "bg-surface",
        )}
      >
        {isNewTab ? (
          <div className="mx-auto w-full max-w-3xl px-5 py-8">
            <h2 className="text-content-primary type-label mb-5 font-medium">
              {t("workbench.tools")}
            </h2>
            <div className="grid gap-4 sm:grid-cols-2">
              {(
                [
                  ["changes", FileDiff, t("workspace.changes")],
                  ["terminal", TerminalSquare, t("terminal")],
                  ["sidechat", MessagesSquare, t("workbench.sideChat")],
                  ["files", Files, t("workspace.files")],
                  ...(showBoardTool
                    ? ([
                        ["board", Columns3, t("team:taskBoard.boardTitle")],
                      ] as const)
                    : []),
                ] as const
              ).map(([kind, Icon, label]) => {
                const disabled =
                  !onOpenTool || (kind === "sidechat" && !chatId);
                return (
                  <button
                    key={kind}
                    type="button"
                    className={cn(
                      "bg-surface-panel text-foreground type-body min-h-row-rich rounded-lg p-5",
                      "flex h-auto w-full items-center gap-3 text-left transition-colors",
                      "hover:bg-muted active:bg-muted",
                      "focus-visible:ring-ring outline-none focus-visible:ring-2 focus-visible:ring-inset",
                      "disabled:pointer-events-none disabled:opacity-45",
                      "[&_svg]:size-icon-md",
                    )}
                    disabled={disabled}
                    onClick={() => onOpenTool?.(kind)}
                  >
                    <Icon strokeWidth={1.5} />
                    <span className="font-medium">{label}</span>
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}
        {isDesktopRuntime() && !browserReady && !error && !isNewTab ? (
          <div
            aria-busy="true"
            aria-label={t("browser")}
            className="bg-surface absolute inset-0 grid place-items-center"
            role="status"
          >
            <span className="bg-muted h-5 w-28 animate-pulse rounded-md" />
          </div>
        ) : null}
        <div
          className={cn(
            "h-full min-h-0",
            isNewTab && "pointer-events-none invisible absolute inset-0",
          )}
          ref={hostRef}
        />
      </div>
    </section>
  );
}
