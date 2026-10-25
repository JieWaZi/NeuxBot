import { ApiError } from "@/shared/api/errors";

/** 只有确定拒绝的客户端错误允许更换请求输入；网络和服务端失败可能已有持久结果。 */
export function isKnowledgeWriteOutcomeUnknown(cause: unknown) {
  if (!(cause instanceof ApiError)) return true;
  if (
    ["NETWORK", "KnowledgeRemoteOutcomeUnknown", "50400009501"].includes(
      cause.code,
    )
  )
    return true;
  return (
    !cause.status ||
    cause.status < 400 ||
    cause.status >= 500 ||
    cause.status === 408
  );
}
