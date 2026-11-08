import {
  Check,
  CircleAlert,
  CircleCheck,
  Copy,
  ExternalLink,
  Globe,
  KeyRound,
} from "lucide-react";
import { useState, useSyncExternalStore, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";

function subscribeVisibility(update: () => void) {
  document.addEventListener("visibilitychange", update);
  return () => document.removeEventListener("visibilitychange", update);
}

/** 只呈现原生作业的状态；不推断浏览器是否打开或授权是否完成。 */
export function LoginStatusPanel({
  state,
  title,
  description,
  children,
}: {
  state: "ready" | "pending" | "success" | "error";
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  const active = useRouteSceneActive();
  const visible = useSyncExternalStore(
    subscribeVisibility,
    () => !document.hidden,
    () => false,
  );
  const Icon =
    state === "success"
      ? CircleCheck
      : state === "error"
        ? CircleAlert
        : state === "ready"
          ? KeyRound
          : Globe;
  return (
    <section className="flex flex-col items-center gap-4 py-6 text-center">
      <div
        className={cn(
          "size-avatar-lg duration-standard relative grid place-items-center rounded-full transition-colors motion-reduce:transition-none",
          state === "success"
            ? "bg-status-success-surface text-status-success-content"
            : state === "error"
              ? "bg-status-danger-surface text-status-danger-content"
              : "bg-status-info-surface text-status-info-content",
        )}
      >
        <Icon className="size-icon-xl" strokeWidth={1.5} aria-hidden />
        {state === "pending" ? (
          <span className="bg-surface-raised size-icon-xl absolute -right-1 -bottom-1 grid place-items-center rounded-full">
            {active && visible ? <Spinner aria-hidden /> : null}
          </span>
        ) : null}
      </div>
      <div
        className="space-y-2"
        role="status"
        aria-live="polite"
        aria-busy={state === "pending" || undefined}
      >
        <h2 className="type-title text-content-primary font-semibold">
          {title}
        </h2>
        {description ? (
          <p className="type-body text-content-secondary">{description}</p>
        ) : null}
      </div>
      {children}
    </section>
  );
}

/** 只展示 Core 真正返回的授权链接和设备码，不构造第三方登录地址。 */
export function LoginAuthorizationDetails({
  userCode,
  verificationUrl,
  onOpen,
}: {
  userCode?: string;
  verificationUrl?: string;
  onOpen: () => void;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const [copiedCode, setCopiedCode] = useState<string>();
  const [copyFailed, setCopyFailed] = useState(false);
  const copied = Boolean(userCode && copiedCode === userCode);
  return (
    <TooltipProvider>
      <div className="flex w-full flex-col gap-3">
        {userCode ? (
          <div className="bg-surface-tertiary flex items-center gap-3 rounded-lg p-4">
            <output
              aria-label={t("loginStatus.code")}
              className="type-display min-w-0 flex-1 font-mono font-semibold tracking-wider break-all select-all"
            >
              {userCode}
            </output>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  variant="plain"
                  aria-label={t(
                    copied ? "common:copied" : "loginStatus.copyCode",
                  )}
                  onClick={async () => {
                    setCopyFailed(false);
                    try {
                      await navigator.clipboard.writeText(userCode);
                      setCopiedCode(userCode);
                    } catch {
                      setCopyFailed(true);
                    }
                  }}
                >
                  {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {t(copied ? "common:copied" : "loginStatus.copyCode")}
              </TooltipContent>
            </Tooltip>
          </div>
        ) : null}
        {verificationUrl ? (
          <Button
            type="button"
            size="settings"
            variant="settings-outline"
            onClick={onOpen}
          >
            <ExternalLink aria-hidden />
            {t("openAuthorization")}
          </Button>
        ) : null}
        {copyFailed ? (
          <p role="alert" className="text-destructive type-body">
            {t("loginStatus.copyFailed")}
          </p>
        ) : null}
      </div>
    </TooltipProvider>
  );
}
