import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Skeleton } from "@/shared/ui/skeleton";

const historySkeletonDelayMs = 150;

/** 短暂读取保留加载语义，持续等待时再显示占位。 */
export function ChatHistoryLoadingState() {
  const { t } = useTranslation("chat");
  const [showSkeleton, setShowSkeleton] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(
      () => setShowSkeleton(true),
      historySkeletonDelayMs,
    );
    return () => window.clearTimeout(timer);
  }, []);

  return (
    <div role="status" aria-busy="true" aria-label={t("loadingMessages")}>
      {showSkeleton && (
        <div
          data-motion="presence"
          data-state="delayed-open"
          className="space-y-4 px-6"
        >
          <Skeleton className="h-control-md ml-auto w-1/3 rounded-xl" />
          <div className="flex items-start gap-3">
            <Skeleton className="size-avatar-sm shrink-0 rounded-full" />
            <div className="bg-message-card-background w-full max-w-md min-w-0 space-y-2 rounded-xl px-4 py-3">
              <Skeleton className="h-3 w-full" />
              <Skeleton className="h-3 w-3/4" />
              <Skeleton className="h-3 w-1/2" />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
