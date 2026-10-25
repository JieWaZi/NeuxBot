import { isCancelledError } from "@tanstack/react-query";
import { ApiError } from "@/shared/api/errors";
import {
  captureErrorNoticeRecovery,
  reportErrorNotice,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";

/** 将知识库本地预检失败送入统一通知，HTTP 已报告错误与取消不重复展示。 */
export function reportKnowledgeError(
  error: unknown,
  operation: string,
  fallback: string,
) {
  if (
    (error instanceof DOMException && error.name === "AbortError") ||
    (error instanceof ApiError && error.noticeId)
  ) {
    return;
  }
  const code = error instanceof ApiError ? error.code : "UNKNOWN";
  reportErrorNotice({
    id: `knowledge:${operation}:${code}`,
    scope: "global",
    operation: `knowledge:${operation}`,
    message: error instanceof ApiError ? error.message : fallback,
  });
}

/** 同一次查询失败可能同时被目录与正文消费，按错误对象只登记一次全局提示。 */
const readErrors = new WeakMap<object, { id: string; operation?: string }>();

export function reportKnowledgeReadError(
  error: unknown,
  fallback: string,
  operation: string,
) {
  if (
    isCancelledError(error) ||
    ((error instanceof Error || error instanceof DOMException) &&
      error.name === "AbortError")
  )
    return;
  if (error instanceof ApiError && error.noticeId)
    return { id: error.noticeId };
  if (error instanceof ApiError && error.requestOperation)
    operation = error.requestOperation;
  if (typeof error === "object" && error !== null) {
    const previous = readErrors.get(error);
    if (
      previous?.operation &&
      captureErrorNoticeRecovery(previous.operation).some(
        (notice) => notice.id === previous.id,
      )
    )
      return previous;
  }
  // 复用 API 通知的网络身份，使断连提示只保留到当前进程结束。
  const code = error instanceof ApiError ? error.code : "UNKNOWN";
  const prefix = operation.startsWith("api:") ? operation : `api:${operation}`;
  const binding = { id: `${prefix}:${code}`, operation };
  resolveErrorNoticeRecovery(captureErrorNoticeRecovery(operation));
  if (typeof error === "object" && error !== null)
    readErrors.set(error, binding);
  reportErrorNotice({
    ...binding,
    scope: "global",
    message: error instanceof ApiError ? error.message : fallback,
  });
  return binding;
}
