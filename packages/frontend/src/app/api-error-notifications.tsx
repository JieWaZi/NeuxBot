import { useEffect, useRef } from "react";
import {
  dismissErrorNotice,
  useErrorNotices,
} from "@/shared/api/error-notices";
import { toast } from "@/shared/ui/sonner";

/** 非聊天 API 错误在当前页面通知，超时或滑动移除后清理本地提示。 */
export function ApiErrorNotifications() {
  const notices = useErrorNotices();
  const displayed = useRef(new Set<string>());
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
