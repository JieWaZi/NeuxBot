import type { ResourceRef } from "@/shared/api/knowledge";
export type { ResourceRef } from "@/shared/api/knowledge";
export function resourceKey(ref: ResourceRef): string {
  return JSON.stringify([ref.backend, ref.connectionId, ref.knowledgeBaseId]);
}
/** 文档操作只由持久终态解除保护；未知状态继续按运行中处理。 */
export function isKnowledgeJobBusy(state: string) {
  return !["completed", "partial_failed", "failed", "cancelled"].includes(
    state,
  );
}
