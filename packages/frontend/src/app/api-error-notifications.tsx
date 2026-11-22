import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  dismissErrorNotice,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { subscribeGlobalEvents } from "@/shared/api/global-events";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import { toast } from "@/shared/ui/sonner";

/** 非聊天 API 错误以自动通知展示，对应操作恢复时提前撤下。 */
export function ApiErrorNotifications() {
  const { t } = useTranslation("notification");
  const notices = useErrorNotices();
  const displayed = useRef(new Map<string, string>());
  const lastCatalogFailure = useRef("");
  useEffect(
    () =>
      subscribeGlobalEvents(
        "plugins",
        { kind: "global" },
        {
          onEvent: (frame) => {
            if (frame.type === "plugins.catalog.sync.succeeded") {
              dismissErrorNotice("plugins:catalog-sync");
              return;
            }
            if (frame.type !== "plugins.catalog.sync.failed") return;
            const identity = `${frame.epoch}:${frame.eventId}`;
            if (lastCatalogFailure.current === identity) return;
            const payload = frame.payload;
            if (
              !payload ||
              typeof payload !== "object" ||
              !("errorCode" in payload) ||
              typeof payload.errorCode !== "string"
            )
              return;
            lastCatalogFailure.current = identity;
            reportErrorNotice({
              id: "plugins:catalog-sync",
              scope: "global",
              lifetime: "session",
              message: "",
              messageSource: "localized",
              errorCode:
                applicationErrorKey(payload.errorCode) ??
                "PluginDistributionUnavailable",
            });
          },
        },
      ),
    [],
  );
  useEffect(() => {
    const current = new Map<string, string>();
    for (const notice of notices) {
      if (notice.scope !== "global") continue;
      current.set(notice.id, notice.version);
      if (displayed.current.get(notice.id) === notice.version) continue;
      const clear = () => dismissErrorNotice(notice.id, notice.version);
      toast.error(t("inApp.requestFailed"), {
        id: notice.id,
        description: notice.message,
        onDismiss: clear,
        onAutoClose: clear,
      });
    }
    for (const id of displayed.current.keys())
      if (!current.has(id)) toast.dismiss(id);
    displayed.current = current;
  }, [notices, t]);
  return null;
}
