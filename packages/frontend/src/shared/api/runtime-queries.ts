import { queryOptions } from "@tanstack/react-query";

import { listRuntimesOptions as generatedListRuntimesOptions } from "./generated/@tanstack/react-query.gen";
import { getRuntimeModelCatalog } from "./generated/sdk.gen";
import { withLocalErrorSurface } from "./local-error-surface";
import type { GlobalEventFrame } from "./global-events";

/** 安装与登录进度只刷新所属任务；程序和账号合同变化才刷新模型及智能体目录。 */
export function isRuntimeConfigurationEvent(
  frame: Pick<GlobalEventFrame, "type" | "payload">,
) {
  const payload = frame.payload;
  const type =
    payload &&
    typeof payload === "object" &&
    "type" in payload &&
    typeof payload.type === "string"
      ? payload.type
      : frame.type;
  return (
    type !== "runtime.installation.changed" &&
    type !== "runtime.authentication.changed"
  );
}

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
    runtimeId === "qoder-cn" ||
    runtimeId === "kimi" ||
    runtimeId === "gemini" ||
    runtimeId === "grok" ||
    runtimeId === "opencode" ||
    runtimeId === "pi"
  );
}

type ModelCatalogIdentity = {
  runtimeId: string;
  accountId: string;
  authGeneration?: number;
  modelConfiguration?: unknown[];
  version?: string;
  binary?: string;
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
    staleTime: Infinity,
  });
}
