import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, ExternalLink, RefreshCw } from "lucide-react";
import { openExternalURL } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { QRCodeSVG } from "@/shared/ui/qr-code";

/** 仅对本次明确发起的浏览器授权自动打开 HTTPS 页面，失败时保留面板的手动入口。 */
export function openNewAuthorizationPage(
  session: {
    state?: string;
    actions?: { type?: string; url?: string; value?: string }[];
  },
  preferredAction?: string,
) {
  if (
    session.state !== "waiting" ||
    preferredAction === "show-qr" ||
    session.actions?.some((action) => action.type === "show-qr" && action.value)
  )
    return;
  const url = session.actions?.find(
    (action) =>
      action.type === "open-url" && /^https:\/\//.test(action.url ?? ""),
  )?.url;
  if (url) void openExternalURL(url).catch(() => undefined);
}

/** 交互只依据桥接动作；普通浏览器链接不推导成可跨设备扫码的内容。 */
export function PluginAuthorizationFooter({
  children,
}: {
  children: ReactNode;
}) {
  return (
    <footer className="flex flex-wrap items-center justify-end gap-2">
      {children}
    </footer>
  );
}

export function PluginAuthorizationActions({
  actions,
  onActionError,
  onRefresh,
  disabled = false,
  qrState,
  preparing = false,
}: {
  actions: { type?: string; url?: string; value?: string }[];
  onActionError: (error?: "failed" | "copyCodeFailed") => void;
  onRefresh: () => void;
  disabled?: boolean;
  qrState?: "loading" | "expired";
  preparing?: boolean;
}) {
  const { t } = useTranslation("plugin");
  const [opening, setOpening] = useState(false);
  if (preparing && !qrState) {
    return <AuthorizationProgress label={t("authorizationPreparing")} />;
  }

  const qr = actions.find(
    (action) => action.type === "show-qr" && action.value,
  )?.value;
  const codes = actions.flatMap(({ type, value }) =>
    type === "copy-code" && value ? [value] : [],
  );
  const links = actions.flatMap(({ type, url }) =>
    type === "open-url" && url && /^https?:\/\//.test(url) ? [url] : [],
  );
  return (
    <div className="space-y-3">
      {(qr || qrState) && (
        <div className="flex flex-col items-center gap-2">
          <div className="bg-surface-subtle grid size-48 place-items-center rounded-md">
            {qrState === "loading" && (
              <AuthorizationProgress label={t("qrLoading")} />
            )}
            {!qrState && qr && <AuthorizationQRCode url={qr} />}
          </div>
          {qrState !== "loading" && (
            <div className="flex items-center justify-center gap-2">
              <span className="type-caption text-content-secondary">
                {t(qrState === "expired" ? "qrExpired" : "scanTitle")}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t("refreshQR")}
                title={t("refreshQR")}
                disabled={disabled}
                onClick={onRefresh}
              >
                <RefreshCw aria-hidden="true" />
              </Button>
            </div>
          )}
        </div>
      )}
      {codes.length > 0 ? (
        <div className="space-y-2 text-center">
          <p className="type-body text-content-secondary">
            {t("deviceCodeHelp")}
          </p>
          {codes.map((code, index) => (
            <AuthorizationCode key={`${code}:${index}`} code={code} />
          ))}
        </div>
      ) : (
        !qr &&
        links.length > 0 && (
          <p className="type-body text-content-secondary text-center">
            {t("browserAuthorizationHelp")}
          </p>
        )
      )}
      {!qrState && <AuthorizationProgress label={t("authorizationWaiting")} />}
      {(codes.length > 0 || links.length > 0) && (
        <PluginAuthorizationFooter>
          {codes.map((code, index) => (
            <AuthorizationCopyCode
              key={`${code}:${index}`}
              code={code}
              onActionError={onActionError}
            />
          ))}
          {links.map((url, index) => (
            <Button
              key={`${url}:${index}`}
              type="button"
              size="catalog"
              disabled={disabled || opening}
              onClick={async () => {
                setOpening(true);
                try {
                  await openExternalURL(url);
                  onActionError();
                } catch {
                  onActionError("failed");
                } finally {
                  setOpening(false);
                }
              }}
            >
              <ExternalLink aria-hidden="true" />
              {t("openAuth")}
            </Button>
          ))}
        </PluginAuthorizationFooter>
      )}
    </div>
  );
}

function AuthorizationProgress({ label }: { label: string }) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label}
      className="flex h-4 items-center justify-center"
    >
      <span aria-hidden="true" className="flex items-center gap-1">
        <span className="bg-accent size-1.5 rounded-full motion-safe:animate-pulse" />
        <span className="bg-accent size-1.5 rounded-full [animation-delay:150ms] motion-safe:animate-pulse" />
        <span className="bg-accent size-1.5 rounded-full [animation-delay:300ms] motion-safe:animate-pulse" />
      </span>
    </div>
  );
}

function AuthorizationCode({ code }: { code: string }) {
  return (
    <code>
      <span className="sr-only">{code}</span>
      <span
        aria-hidden="true"
        className="type-title flex flex-wrap items-center justify-center gap-1 font-mono font-semibold"
      >
        {Array.from(code, (character, index) => (
          <span
            key={index}
            className={
              character === "-"
                ? "text-content-secondary px-0.5"
                : "bg-surface-subtle h-control-catalog-lg min-w-control-catalog grid place-items-center rounded-lg px-1"
            }
          >
            {character}
          </span>
        ))}
      </span>
    </code>
  );
}

/** 复制完整的一次性验证码，包括显示时独立排版的分隔符。 */
function AuthorizationCopyCode({
  code,
  onActionError,
}: {
  code: string;
  onActionError: (error?: "copyCodeFailed") => void;
}) {
  const { t } = useTranslation("plugin");
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      onActionError();
    } catch {
      setCopied(false);
      onActionError("copyCodeFailed");
    }
  }

  return (
    <Button
      type="button"
      size="catalog"
      variant="secondary"
      onClick={() => void copy()}
    >
      {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      {t(copied ? "codeCopied" : "copyCode")}
    </Button>
  );
}

/** 原始载荷生成二维码，受宿主校验的内联 PNG 原图直接展示；不加载远端图片。 */
export function AuthorizationQRCode({ url }: { url: string }) {
  const { t } = useTranslation("plugin");
  const isPNG = /^data:image\/png;base64,[A-Za-z0-9+/]+={0,2}$/.test(url);
  if (/^data:/i.test(url) && !isPNG) return null;
  return (
    <div className="flex justify-center">
      {isPNG ? (
        <img src={url} width={192} height={192} alt={t("scanTitle")} />
      ) : (
        <QRCodeSVG
          value={url}
          size={192}
          marginSize={4}
          level="M"
          title={t("scanTitle")}
        />
      )}
    </div>
  );
}
