import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import { Skeleton } from "@/shared/ui/skeleton";

/** 工作流数据区域共用局部加载状态；后台刷新保留原内容。 */
export function WorkflowLoadingState({ className }: { className?: string }) {
  const { t } = useTranslation("workflow");
  return <ContentLoadingState label={t("loading")} className={className} />;
}

/** 聊天中的工作流请求在组件和详情就绪前保留卡片占位。 */
export function WorkflowRequestLoadingState() {
  const { t } = useTranslation("workflow");
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t("loading")}
      className="bg-message-card-background max-w-chat-question w-full space-y-3 rounded-2xl px-4 py-3"
    >
      <Skeleton className="h-4 w-1/3" />
      <Skeleton className="h-4 w-1/2" />
    </div>
  );
}

/** 首次失败与缓存刷新失败使用同一就近重试入口。 */
export function WorkflowQueryError({
  error,
  onRetry,
  retrying = false,
  className,
}: {
  error: unknown;
  onRetry: () => void;
  retrying?: boolean;
  className?: string;
}) {
  const { t } = useTranslation("workflow");
  return (
    <div
      data-workflow-query-feedback
      className={cn("grid min-w-0 gap-2", className)}
    >
      <p role="alert" className="text-destructive type-body wrap-anywhere">
        {localApiErrorMessage(error, t("error"))}
      </p>
      <Button
        size="sm"
        variant="secondary"
        className="justify-self-start"
        disabled={retrying}
        onClick={onRetry}
      >
        {t(retrying ? "loading" : "retry")}
      </Button>
    </div>
  );
}
