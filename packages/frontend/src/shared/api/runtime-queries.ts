import { queryOptions } from "@tanstack/react-query";

import { listRuntimesOptions as generatedListRuntimesOptions } from "./generated/@tanstack/react-query.gen";

/** Runtime 快照由全局 runtimes 事件失效，继续复用生成的 Query Key。 */
export function listRuntimesOptions(
  options?: Parameters<typeof generatedListRuntimesOptions>[0],
) {
  return queryOptions({
    ...generatedListRuntimesOptions(options),
  });
}
