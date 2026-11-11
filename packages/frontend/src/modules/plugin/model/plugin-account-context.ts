import type {
  CheckPluginAccountData,
  GetPluginAuthorizationResponse,
} from "@/shared/api/generated/types.gen";

/** 写操作只使用实际缓存读取返回的并发事实，未读取时不猜测初始代次。 */
export function pluginAccountContext(
  snapshot:
    | Pick<
        GetPluginAuthorizationResponse,
        "configurationRevision" | "accountGeneration"
      >
    | undefined,
): CheckPluginAccountData["body"] | undefined {
  if (
    snapshot?.configurationRevision === undefined ||
    snapshot.accountGeneration === undefined
  )
    return undefined;
  return {
    expectedConfigurationRevision: snapshot.configurationRevision,
    expectedAccountGeneration: snapshot.accountGeneration,
  };
}
