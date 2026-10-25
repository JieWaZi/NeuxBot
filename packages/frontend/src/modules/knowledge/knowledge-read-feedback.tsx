import { useEffect, useId, useRef, type ReactNode } from "react";
import { isCancelledError } from "@tanstack/react-query";
import {
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
  useErrorNotices,
  type ErrorNotice,
} from "@/shared/api/error-notices";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { reportKnowledgeReadError } from "./knowledge-error-notice";
import type { KnowledgeReadState } from "./knowledge-model";
import { useCopy } from "./use-copy";

/** 读取失败统一进入全局通知，内容区只负责加载与陈旧状态。 */
export function KnowledgeReadFeedback({
  state,
  hasData = false,
  loading,
}: {
  state?: KnowledgeReadState;
  hasData?: boolean;
  loading?: ReactNode;
}) {
  const t = useCopy();
  const active = useRouteSceneActive();
  const notices = useErrorNotices();
  const operation = `knowledge:read:${useId()}`;
  const failure = useRef<unknown>(undefined);
  const recovery = useRef<ErrorNotice[]>([]);
  const error = state?.error;
  const pending = state?.pending;
  const refreshing = state?.refreshing;
  useEffect(() => {
    if (error) {
      if (!active || failure.current === error) return;
      failure.current = error;
      const binding = reportKnowledgeReadError(
        error,
        t("knowledgeOperationFailed"),
        operation,
      );
      if (binding) {
        const captured = binding.operation
          ? captureErrorNoticeRecovery(binding.operation)
          : notices.filter((notice) => notice.id === binding.id);
        recovery.current = [
          ...recovery.current.filter(
            (old) =>
              !captured.some((notice) => notice.operation === old.operation),
          ),
          ...captured,
        ];
      }
    } else if (state && !pending && !refreshing) {
      failure.current = undefined;
      const captured = recovery.current;
      recovery.current = [];
      resolveErrorNoticeRecovery(captured);
    }
  }, [active, error, pending, state, notices, t, operation, refreshing]);
  if (!state) return null;
  if (
    isCancelledError(state.error) ||
    (state.error instanceof DOMException && state.error.name === "AbortError")
  )
    return null;
  if (state.error) return null;
  if (state.pending && !hasData && !state.error && loading) return loading;
  if (state.stale)
    return (
      <p role="status" className="text-content-muted type-caption px-3 py-2">
        {t("staleData")}
      </p>
    );
  return null;
}
