import { minimumReasoning } from "@/shared/lib/runtime-reasoning";
import {
  nativeModelCatalogOptions,
  listRuntimesOptions,
  usesNativeModelCatalog,
} from "@/shared/api/runtime-queries";
import { useQuery } from "@tanstack/react-query";

import { listAiAccountsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  getAiAccountNativeModels,
  getAiAccountThinking,
} from "@/shared/api/generated/sdk.gen";
import type {
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeModelOption as RuntimeModelOption,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { accountSupportsRuntime } from "@/shared/lib/account-runtime-compatibility";
import {
  modelPermissionLevels,
  permissionLevelOf,
  runtimeDefaultPermissionLevels,
  safeDefaultPermissionLevel,
} from "@/shared/lib/runtime-permissions";

export type ExecutionSelection = {
  runtimeId?: string;
  accountId?: string;
  model?: string;
  reasoning?: string;
  fast?: boolean;
  permissionLevel?: string;
};

/** 账号目录、原生订阅模型和模型推理能力统一在此查询；页面只提交选择值。 */
export function useExecutionSelection({
  value,
  runtime: fixedRuntime,
  enabled = true,
  defaults = false,
  reasoningDefault,
}: {
  value: ExecutionSelection;
  runtime?: RuntimeOption;
  enabled?: boolean;
  defaults?: boolean;
  reasoningDefault?: "minimum";
}) {
  const runtimesQuery = useQuery({
    ...listRuntimesOptions(withLocalErrorSurface({})),
    enabled: enabled && !fixedRuntime,
  });
  const accountsQuery = useQuery({
    ...listAiAccountsOptions(withLocalErrorSurface({})),
    enabled,
  });
  const runtimes = (runtimesQuery.data?.resources ?? []).filter(
    (item) => item.id && item.enabled && item.state === "ready",
  );
  const runtimeId =
    fixedRuntime?.id ||
    value.runtimeId ||
    runtimes.find((item) => item.isDefault)?.id ||
    runtimes[0]?.id ||
    "";
  const runtime =
    fixedRuntime ??
    runtimesQuery.data?.resources?.find((item) => item.id === runtimeId);
  const accounts = (accountsQuery.data?.resources ?? []).filter(
    (item) =>
      item.id &&
      accountSupportsRuntime(item, runtime?.baseRuntimeId || runtimeId),
  );
  const accountId = value.accountId || runtime?.defaultAccountId || "system";
  const account = accounts.find((item) => item.id === accountId);
  const subscription = account?.authMode === "subscription";
  const usesNativeCatalog = usesNativeModelCatalog(
    runtime?.baseRuntimeId || runtimeId,
  );
  const runtimeInstalled = runtime?.installed ?? runtime?.state === "ready";
  const catalogQuery = useQuery({
    ...nativeModelCatalogOptions({
      runtimeId,
      accountId,
      authGeneration: account?.authGeneration,
      modelConfiguration:
        account?.authMode === "api-key"
          ? [
              account.endpoints,
              account.selectedModels,
              account.modelDisplayNames,
              account.modelReasoningLevels,
            ]
          : undefined,
      version: runtime?.version,
      binary: runtime?.resolvedBinary || runtime?.binary,
      prefixArgs: runtime?.prefixArgs,
      configHome: accountId === "system" ? runtime?.configHome : undefined,
    }),
    enabled: Boolean(
      enabled &&
      usesNativeCatalog &&
      runtimeInstalled &&
      (accountId === "system" || account),
    ),
  });
  const catalogReady = catalogQuery.data?.state === "fresh";
  const catalogState = usesNativeCatalog
    ? catalogQuery.isError
      ? catalogQuery.data
        ? "stale"
        : "unavailable"
      : catalogQuery.data?.state
    : undefined;
  const nativeModelsQuery = useQuery({
    queryKey: [
      "account-native-models",
      account?.id,
      account?.revision,
      runtimeId,
      runtime?.version,
    ],
    queryFn: async ({ signal }) => {
      const { data } = await getAiAccountNativeModels(
        withLocalErrorSurface({
          path: { accountId: account?.id ?? "" },
          query: { runtimeId },
          signal,
          throwOnError: true,
        }),
      );
      return data;
    },
    enabled: Boolean(
      enabled && !usesNativeCatalog && subscription && runtime?.enabled,
    ),
    staleTime: 5 * 60 * 1000,
  });
  const models: RuntimeModelOption[] = usesNativeCatalog
    ? (catalogQuery.data?.models ?? [])
    : account
      ? (subscription
          ? (nativeModelsQuery.data?.models ?? [])
          : (account.selectedModels ?? [])
        ).map((id) => ({ id, name: account.modelDisplayNames?.[id] || id }))
      : (runtime?.models ?? []);
  const configuredAccount =
    accountId === (runtime?.defaultAccountId || "system");
  const preferredModel = usesNativeCatalog
    ? (configuredAccount && runtime?.defaultModel) ||
      account?.defaultModel ||
      catalogQuery.data?.defaultModel
    : account
      ? account.defaultModel || nativeModelsQuery.data?.defaultModel
      : runtime?.defaultModel;
  const defaultModel =
    models.find((item) => item.id === preferredModel)?.id ||
    models[0]?.id ||
    "";
  const requestedModel = value.model ?? defaultModel;
  const model =
    defaults && !models.some((item) => item.id === requestedModel)
      ? defaultModel
      : requestedModel;
  const effectiveModel = model || defaultModel;
  const thinkingQuery = useQuery({
    queryKey: [
      "account-thinking",
      account?.id,
      account?.revision,
      account?.endpoints?.[0]?.protocol,
      account?.endpoints?.[0]?.baseUrl,
      runtimeId,
      runtime?.version,
      effectiveModel,
    ],
    queryFn: async ({ signal }) => {
      const { data } = await getAiAccountThinking(
        withLocalErrorSurface({
          path: { accountId: account?.id ?? "" },
          query: { runtimeId, model: effectiveModel },
          signal,
          throwOnError: true,
        }),
      );
      return data;
    },
    enabled: Boolean(
      enabled &&
      !usesNativeCatalog &&
      account &&
      runtime?.enabled &&
      effectiveModel,
    ),
    staleTime: 5 * 60 * 1000,
  });
  const modelCapabilities = usesNativeCatalog
    ? catalogQuery.data?.models
    : runtime?.models;
  const runtimeModel = modelCapabilities?.find(
    (item) => item.id === effectiveModel,
  );
  const reasoningOptions = usesNativeCatalog
    ? (runtimeModel?.reasoningOptions ?? [])
    : account
      ? thinkingQuery.data?.available
        ? (thinkingQuery.data.options ?? [])
        : []
      : runtimeModel?.reasoningOptions?.length
        ? runtimeModel.reasoningOptions
        : (runtime?.reasoningOptions ?? []);
  const preferredReasoning = usesNativeCatalog
    ? configuredAccount && effectiveModel === runtime?.defaultModel
      ? runtime?.defaultReasoning || runtimeModel?.defaultReasoning
      : runtimeModel?.defaultReasoning
    : account
      ? account.defaultReasoning
      : runtime?.defaultReasoning;
  const requestedReasoning =
    reasoningDefault === "minimum"
      ? value.reasoning || minimumReasoning(reasoningOptions)
      : (value.reasoning ??
        (reasoningOptions.find((item) => item.id === preferredReasoning)?.id ||
          ""));
  const reasoning =
    defaults &&
    requestedReasoning &&
    !reasoningOptions.some((item) => item.id === requestedReasoning)
      ? usesNativeCatalog
        ? reasoningOptions.find(
            (item) => item.id === runtimeModel?.defaultReasoning,
          )?.id || ""
        : account
          ? ""
          : reasoningOptions[0]?.id || ""
      : requestedReasoning;
  const supportedPermissions =
    usesNativeCatalog || runtimeModel
      ? modelPermissionLevels(modelCapabilities, effectiveModel)
      : runtimeDefaultPermissionLevels(runtime ?? {});
  const permissionLevel =
    permissionLevelOf(value.permissionLevel) ??
    safeDefaultPermissionLevel(
      permissionLevelOf(runtime?.defaultPermissionLevel) ?? "default",
      supportedPermissions,
    );
  const fastSupported = Boolean(
    usesNativeCatalog && runtimeModel?.fastSupported,
  );
  const fast = fastSupported ? value.fast : undefined;
  const selection = {
    runtimeId,
    accountId,
    model,
    reasoning,
    permissionLevel,
    fast,
  };
  const error =
    runtimesQuery.error ||
    accountsQuery.error ||
    nativeModelsQuery.error ||
    thinkingQuery.error ||
    catalogQuery.error;
  const loading =
    (!fixedRuntime && runtimesQuery.isPending) ||
    accountsQuery.isPending ||
    Boolean(
      usesNativeCatalog &&
      runtimeInstalled &&
      (accountId === "system" || account) &&
      catalogQuery.isPending,
    ) ||
    (!usesNativeCatalog && subscription && nativeModelsQuery.isPending) ||
    Boolean(
      !usesNativeCatalog &&
      account &&
      effectiveModel &&
      thinkingQuery.isPending,
    );
  const valid = Boolean(
    runtime?.enabled &&
    runtime.state === "ready" &&
    (accountId === "system" || account?.hasCredential) &&
    (!model || models.some((item) => item.id === model)) &&
    (!usesNativeCatalog ||
      (catalogReady && runtimeModel?.configurationKnown)) &&
    (!reasoning || reasoningOptions.some((item) => item.id === reasoning)) &&
    supportedPermissions.has(permissionLevel) &&
    !loading &&
    !error,
  );
  return {
    selection,
    usesNativeCatalog,
    catalogState,
    refreshCatalog: catalogQuery.refetch,
    catalogPending: usesNativeCatalog && catalogQuery.isFetching,
    fastSupported,
    effectiveModel,
    runtime,
    runtimes,
    accounts,
    accountsQuery,
    runtimesQuery,
    models,
    reasoningOptions,
    supportedPermissions,
    nativeModelsQuery,
    thinkingQuery,
    loading,
    error,
    valid,
  };
}

export type ExecutionSelectionState = ReturnType<typeof useExecutionSelection>;
