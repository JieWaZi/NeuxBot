import { queryOptions } from "@tanstack/react-query";

import { listRuntimesOptions as generatedListRuntimesOptions } from "./generated/@tanstack/react-query.gen";
import { getRuntimeModelCatalog } from "./generated/sdk.gen";
import { withLocalErrorSurface } from "./local-error-surface";

/** Runtime 快照由全局 runtimes 事件失效，继续复用生成的 Query Key。 */
export function listRuntimesOptions(
  options?: Parameters<typeof generatedListRuntimesOptions>[0],
) {
  return queryOptions({
    ...generatedListRuntimesOptions(options),
  });
}

export const codexModelCatalogQueryKey = ["codex-model-catalog"] as const;

/** 同一账号的密钥轮换与显示名变化不会把目录切换成另一份查询。 */
export function codexModelCatalogOptions(identity: {
  runtimeId: string;
  accountId: string;
  authGeneration?: number;
  modelConfiguration?: unknown[];
  version?: string;
  binary?: string;
  prefixArgs?: string[];
  configHome?: string;
}) {
  const { runtimeId, accountId } = identity;
  return queryOptions({
    queryKey: [...codexModelCatalogQueryKey, identity],
    queryFn: async ({ signal }) => {
      const { data } = await getRuntimeModelCatalog(
        withLocalErrorSurface({
          path: { runtimeId },
          query: { accountId },
          signal,
          throwOnError: true,
        }),
      );
      if (
        data?.accountId !== accountId ||
        data?.runtimeId !== runtimeId ||
        (identity.authGeneration !== undefined &&
          data.state !== "signed_out" &&
          data.authGeneration !== identity.authGeneration)
      ) {
        throw new Error("Codex catalog identity changed");
      }
      return data;
    },
    staleTime: accountId === "system" ? 0 : 60_000,
  });
}
