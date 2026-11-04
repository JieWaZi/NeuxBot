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

export function usesNativeModelCatalog(runtimeId?: string) {
  return (
    runtimeId === "codex" ||
    runtimeId === "claude" ||
    runtimeId === "cursor" ||
    runtimeId === "workbuddy" ||
    runtimeId === "qoder" ||
    runtimeId === "qoder-cn"
  );
}

type ModelCatalogIdentity = {
  runtimeId: string;
  accountId: string;
  authGeneration?: number;
  modelConfiguration?: unknown[];
  version?: string;
  binary?: string;
  prefixArgs?: string[];
  configHome?: string;
};

export const nativeModelCatalogQueryKey = (
  identity?: Partial<ModelCatalogIdentity>,
) =>
  identity
    ? (["native-model-catalog", identity] as const)
    : (["native-model-catalog"] as const);

/** 目录按账号认证世代和 CLI 隔离，显示名与同主体令牌刷新不改变目录身份。 */
export function nativeModelCatalogOptions(
  identity: ModelCatalogIdentity,
  refresh = false,
) {
  const { runtimeId, accountId } = identity;
  return queryOptions({
    queryKey: nativeModelCatalogQueryKey(identity),
    queryFn: async ({ signal }) => {
      const { data } = await getRuntimeModelCatalog(
        withLocalErrorSurface({
          path: { runtimeId },
          query: refresh ? { accountId, refresh } : { accountId },
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
        throw new Error("Native model catalog identity changed");
      }
      return data;
    },
    staleTime: accountId === "system" ? 0 : 60_000,
  });
}
