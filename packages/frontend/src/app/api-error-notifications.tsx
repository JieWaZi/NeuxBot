import { useEffect, useRef } from "react";
import {
  dismissErrorNotice,
  reportErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { subscribeGlobalEvents } from "@/shared/api/global-events";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import { toast } from "@/shared/ui/sonner";

/** 非聊天 API 错误在当前页面通知，超时或滑动移除后清理本地提示。 */
export function ApiErrorNotifications() {
  const notices = useErrorNotices();
  const displayed = useRef(new Set<string>());
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
    const current = new Set(
      notices
        .filter((notice) => notice.scope === "global")
        .map((notice) => notice.id),
    );
    for (const id of displayed.current) if (!current.has(id)) toast.dismiss(id);
    for (const notice of notices) {
      if (notice.scope !== "global") continue;
      toast.error(notice.message, {
        id: notice.id,
        onDismiss: () => dismissErrorNotice(notice.id),
        onAutoClose: () => dismissErrorNotice(notice.id),
      });
    }
    displayed.current = current;
  }, [notices]);
  return null;
}
