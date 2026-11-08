import { useState } from "react";
import { RESOURCE_BATCH_SIZE } from "./use-resource-pages";

/** 已有完整快照的列表按批次展示，筛选变化时恢复首批。 */
export function useResourceWindow<T>(
  items: T[],
  scope: string,
  batchSize = RESOURCE_BATCH_SIZE,
) {
  const [window, setWindow] = useState({ scope, limit: batchSize });
  if (window.scope !== scope) setWindow({ scope, limit: batchSize });
  const limit = window.scope === scope ? window.limit : batchSize;
  return {
    visible: items.slice(0, limit),
    hasMore: limit < items.length,
    loadMore: () => setWindow({ scope, limit: limit + batchSize }),
  };
}
