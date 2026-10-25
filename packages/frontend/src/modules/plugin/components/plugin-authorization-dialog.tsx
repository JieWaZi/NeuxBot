import { useState } from "react";
import { useTranslation } from "react-i18next";
import { QRCodeSVG } from "@/shared/ui/qr-code";
import { Button } from "@/shared/ui/button";
import { Check, Copy, RefreshCw } from "lucide-react";
import { Spinner } from "@/shared/ui/spinner";
import { openExternalURL } from "@/shared/platform";

/** 交互只依据桥接动作；普通浏览器链接不推导成可跨设备扫码的内容。 */
export function PluginAuthorizationActions({
  actions,
  onOpenError,
  onRefresh,
  disabled = false,
  qrState,
  preparing = false,
}: {
  actions: { type?: string; url?: string; value?: string }[];
  onOpenError: () => void;
  onRefresh: () => void;
  disabled?: boolean;
  qrState?: "loading" | "expired";
  preparing?: boolean;
}) {
  const { t } = useTranslation("plugin");
  const qr = actions.find(
    (action) => action.type === "show-qr" && action.value,
  );
  if (preparing && !qrState)
    return (
      <div
        role="status"
        aria-busy="true"
        aria-label={t("authorizationPreparing")}
        className="flex flex-col items-center gap-2"
      >
        <div className="bg-surface-subtle grid size-36 place-items-center rounded-md">
          <Spinner aria-hidden="true" />
        </div>
        <span className="type-caption text-content-secondary">
          {t("authorizationPreparing")}
        </span>
      </div>
    );
  return (
    <div className="space-y-3">
      {(qr || qrState) && (
        <div className="flex flex-col items-center gap-2">
          <div className="bg-surface-subtle grid size-48 place-items-center rounded-md">
            {qrState ? (
              qrState === "loading" && <Spinner aria-label={t("qrLoading")} />
            ) : (
              <AuthorizationQRCode url={qr?.value ?? ""} />
            )}
          </div>
          <div className="flex items-center justify-center gap-2">
            <span className="type-caption text-content-secondary">
              {t(
                qrState === "loading"
                  ? "qrLoading"
                  : qrState === "expired"
                    ? "qrExpired"
                    : "scanTitle",
              )}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t("refreshQR")}
              title={t("refreshQR")}
              disabled={disabled || qrState === "loading"}
              onClick={onRefresh}
            >
              <RefreshCw aria-hidden="true" />
            </Button>
          </div>
        </div>
      )}
      {actions
        .filter((action) => action.type === "copy-code" && action.value)
        .map((action, index) => (
          <AuthorizationCode
            key={`${action.value}:${index}`}
            code={action.value ?? ""}
          />
        ))}
      {actions.map((action, index) => {
        const key = `${action.type}:${index}:${action.value ?? action.url ?? ""}`;
        if (
          action.type === "open-url" &&
          action.url &&
          /^https?:\/\//.test(action.url)
        )
          return (
            <Button
              key={key}
              type="button"
              variant="success"
              className="w-full"
              disabled={disabled}
              onClick={() =>
                void openExternalURL(action.url ?? "").catch(onOpenError)
              }
            >
              {t("openAuth")}
            </Button>
          );
        return null;
      })}
    </div>
  );
}

/** 验证码是官方要求用户输入的短时信息，不能拼进未经官方提供的 URL。 */
export function AuthorizationCode({ code }: { code: string }) {
  const { t } = useTranslation("plugin");
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  return (
    <div className="space-y-2 text-center">
      <div className="bg-surface-subtle mx-auto flex min-h-28 w-fit max-w-full flex-col items-center justify-center gap-2 rounded-md p-4">
        <code className="type-display max-w-full min-w-0 font-semibold tracking-wider break-all">
          {code}
        </code>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={copied ? t("codeCopied") : t("copyCode")}
          title={copied ? t("codeCopied") : t("copyCode")}
          onClick={() => {
            setFailed(false);
            void Promise.resolve()
              .then(() => navigator.clipboard.writeText(code))
              .then(
                () => setCopied(true),
                () => setFailed(true),
              );
          }}
        >
          {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
        </Button>
      </div>
      <p className="type-caption text-muted-foreground">
        {t("deviceCodeHelp")}
      </p>
      {failed && (
        <p role="alert" className="type-body text-destructive">
          {t("copyCodeFailed")}
        </p>
      )}
    </div>
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
