import { ArrowRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import type { NotificationItem } from "../model/notification";
import { useNotificationHref } from "./notification-navigation";

export function NotificationRow({
  notice,
  executorAvatar,
}: {
  notice: NotificationItem;
  /** 调用方复用原执行者头像，通知模块不依赖其他模块内部组件。 */
  executorAvatar: ReactNode;
}) {
  const { t, i18n } = useTranslation(["notification", "workBoard"]);
  const href = useNotificationHref();
  return (
    <div className="bg-surface-scene min-h-row-rich flex min-w-0 flex-wrap items-center gap-3 rounded-md px-3 py-2">
      {executorAvatar}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="type-label font-medium break-words">
            <Link className="hover:text-link" to={href(notice)}>
              {notice.title}
            </Link>
          </p>
          {notice.failed && notice.kind === "result" ? (
            <Badge variant="destructive">{t("workBoard:failed")}</Badge>
          ) : null}
          <Badge
            variant={
              notice.bucket === "action"
                ? "warning"
                : notice.kind === "automatic_failure"
                  ? "destructive"
                  : "secondary"
            }
          >
            {t(notice.kind)}
          </Badge>
        </div>
        <div className="text-content-secondary type-ui mt-1 flex flex-wrap gap-x-3 gap-y-1">
          <span>{notice.executor.name}</span>
          <span>{notice.description}</span>
          <time dateTime={new Date(notice.createdAt).toISOString()}>
            {new Intl.DateTimeFormat(i18n.language, {
              month: "numeric",
              day: "numeric",
              hour: "numeric",
              minute: "2-digit",
            }).format(notice.createdAt)}
          </time>
        </div>
      </div>
      <Button asChild size="sm" variant="subtle">
        <Link to={href(notice)}>
          {t(
            notice.bucket === "action" ? "workBoard:handle" : "workBoard:view",
          )}
          <ArrowRight />
        </Link>
      </Button>
    </div>
  );
}
