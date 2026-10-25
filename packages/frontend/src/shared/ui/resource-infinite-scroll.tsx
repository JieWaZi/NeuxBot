import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./button";

/** 由所属滚动容器触发下一批；错误时暂停自动重试并提供键盘入口。 */
export function ResourceInfiniteScroll({
  hasMore,
  pending = false,
  error,
  onLoadMore,
}: {
  hasMore: boolean;
  pending?: boolean;
  error?: unknown;
  onLoadMore: () => void;
}) {
  const { t } = useTranslation("common");
  const target = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = target.current;
    if (
      !node ||
      !hasMore ||
      pending ||
      error ||
      typeof IntersectionObserver === "undefined"
    )
      return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) onLoadMore();
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [hasMore, pending, error, onLoadMore]);
  if (!hasMore) return null;
  return (
    <div ref={target} className="flex justify-center py-3" aria-busy={pending}>
      <Button variant="ghost" size="sm" disabled={pending} onClick={onLoadMore}>
        {t(
          pending
            ? "resourceSelection.loadingMore"
            : error
              ? "retry"
              : "resourceSelection.loadMore",
        )}
      </Button>
      {error ? (
        <p role="alert" className="type-body text-destructive">
          {t("resourceSelection.loadMoreFailed")}
        </p>
      ) : null}
    </div>
  );
}
