import { useTranslation } from "react-i18next";

import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "./button";
import { StatusNotice } from "./status-notice";
import { ContentLoadingState } from "./content-loading-state";
import {
  ResourceLoadingState,
  type ResourceLoadingLayout,
} from "./resource-loading-state";

/** 读取区域的互斥状态；请求失败不能被解释为空数据。 */
export function QueryState({
  pending,
  loadingLayout,
  error,
  emptyLabel,
  onRetry,
}: {
  pending?: boolean;
  loadingLayout?: ResourceLoadingLayout;
  error?: unknown;
  emptyLabel?: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation("common");
  if (pending)
    return loadingLayout ? (
      <ResourceLoadingState layout={loadingLayout} />
    ) : (
      <ContentLoadingState
        className="min-h-64"
        label={t("detailState.loading")}
      />
    );
  return (
    <div
      className="type-body grid min-h-64 min-w-0 place-items-center text-center"
      role={error ? "alert" : undefined}
    >
      <div className="min-w-0 space-y-4">
        <p className="break-words">
          {error
            ? localApiErrorMessage(error, t("detailState.failed"))
            : emptyLabel}
        </p>
        {error ? (
          <Button type="button" variant="secondary" onClick={onRetry}>
            {t("retry")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/** 后台读取失败时保留旧内容，并在所属区域提供恢复入口。 */
export function QueryRefreshError({
  error,
  pending,
  onRetry,
}: {
  error: unknown;
  pending: boolean;
  onRetry: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <StatusNotice role="alert" className="flex flex-wrap items-center gap-3">
      <p className="min-w-0 flex-1 break-words">
        {localApiErrorMessage(error, t("resourceSelection.refreshFailed"))}
      </p>
      <Button
        type="button"
        variant="secondary"
        disabled={pending}
        onClick={onRetry}
      >
        {t("retry")}
      </Button>
    </StatusNotice>
  );
}
