import { useState } from "react";
import { RESOURCE_BATCH_SIZE } from "./use-resource-pages";

/** 已有完整快照的列表同样按九项展示，筛选变化时恢复首批。 */
export function useResourceWindow<T>(items: T[], scope: string) {
  const [window, setWindow] = useState({ scope, limit: RESOURCE_BATCH_SIZE });
  if (window.scope !== scope) setWindow({ scope, limit: RESOURCE_BATCH_SIZE });
  const limit = window.scope === scope ? window.limit : RESOURCE_BATCH_SIZE;
  return {
    visible: items.slice(0, limit),
    hasMore: limit < items.length,
    loadMore: () => setWindow({ scope, limit: limit + RESOURCE_BATCH_SIZE }),
  };
}
