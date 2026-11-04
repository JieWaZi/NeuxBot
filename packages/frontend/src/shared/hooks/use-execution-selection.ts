import { minimumReasoning } from "@/shared/lib/runtime-reasoning";
import {
  nativeModelCatalogOptions,
  listRuntimesOptions,
  usesNativeModelCatalog,
} from "@/shared/api/runtime-queries";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";

import { listAiAccountsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  getAiAccountNativeModels,
  getAiAccountThinking,
} from "@/shared/api/generated/sdk.gen";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as AIAccount,
  GetRuntimeModelCatalogResponse as ModelCatalog,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as ReasoningOption,
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
  type PermissionLevel,
} from "@/shared/lib/runtime-permissions";

export type ExecutionSelection = {
  runtimeId?: string;
  accountId?: string;
  model?: string;
  reasoning?: string;
  fast?: boolean;
  permissionLevel?: string;
};

function nativeCatalogIdentity(
  runtime: RuntimeOption | undefined,
  runtimeId: string,
  accountId: string,
  account?: AIAccount,
) {
  return {
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
  };
}

function resolveModel(
  value: ExecutionSelection,
  models: RuntimeModelOption[],
  preferredModels: ReadonlyArray<string | undefined>,
  defaults: boolean,
) {
  const fallback =
    preferredModels.find(
      (candidate) => candidate && models.some((item) => item.id === candidate),
    ) ||
    models[0]?.id ||
    "";
  const requested = value.model ?? fallback;
  const model =
    defaults && !models.some((item) => item.id === requested)
      ? fallback
      : requested;
  return { model, effectiveModel: model || fallback };
}

function resolveNativeSelection({
  value,
  runtime,
  account,
  runtimeId,
  accountId,
  catalog,
  defaults,
  reasoningDefault,
}: {
  value: ExecutionSelection;
  runtime?: RuntimeOption;
  account?: AIAccount;
  runtimeId: string;
  accountId: string;
  catalog?: ModelCatalog;
  defaults: boolean;
  reasoningDefault?: "minimum";
}) {
  const models = catalog?.models ?? [];
  const configuredAccount =
    accountId === (runtime?.defaultAccountId || "system");
  const { model, effectiveModel } = resolveModel(
    value,
    models,
    [
      configuredAccount ? runtime?.defaultModel : undefined,
      account?.defaultModel,
      catalog?.defaultModel,
    ],
    defaults,
  );
  const modelOption = effectiveModel
    ? models.find((item) => item.id === effectiveModel)
    : undefined;
  const reasoningOptions = modelOption?.reasoningOptions ?? [];
  const preferredReasoning =
    configuredAccount && effectiveModel === runtime?.defaultModel
      ? runtime?.defaultReasoning || modelOption?.defaultReasoning
      : modelOption?.defaultReasoning;
  const defaultReasoning =
    reasoningOptions.find((item) => item.id === preferredReasoning)?.id || "";
  const requestedReasoning =
    reasoningDefault === "minimum"
      ? value.reasoning || minimumReasoning(reasoningOptions)
      : (value.reasoning ?? defaultReasoning);
  const reasoning =
    defaults &&
    requestedReasoning &&
    !reasoningOptions.some((item) => item.id === requestedReasoning)
      ? reasoningOptions.find(
          (item) => item.id === modelOption?.defaultReasoning,
        )?.id || ""
      : requestedReasoning;
  const supportedPermissions = modelPermissionLevels(models, effectiveModel);
  const permissionLevel =
    permissionLevelOf(value.permissionLevel) ??
    safeDefaultPermissionLevel(
      permissionLevelOf(runtime?.defaultPermissionLevel) ?? "default",
      supportedPermissions,
    );
  const fastSupported = Boolean(modelOption?.fastSupported);
  const modelConfigurationValid = Boolean(
    runtime &&
    (accountId === "system" || account?.hasCredential) &&
    catalog?.state === "fresh" &&
    modelOption?.configurationKnown &&
    (!reasoning || reasoningOptions.some((item) => item.id === reasoning)),
  );
  return {
    selection: {
      runtimeId,
      accountId,
      model,
      reasoning,
      permissionLevel,
      fast: fastSupported ? value.fast : undefined,
    },
    effectiveModel,
    models,
    reasoningOptions,
    supportedPermissions,
    fastSupported,
    configurationKnown: modelOption?.configurationKnown === true,
    modelConfigurationValid,
    configurationValid:
      modelConfigurationValid && supportedPermissions.has(permissionLevel),
  };
}

/** 批量校验原生配置；同一账号和 CLI 的目录共享 Query。 */
export function useNativeExecutionSelections(
  values: readonly ExecutionSelection[],
  runtimes: readonly RuntimeOption[],
  accounts: readonly AIAccount[],
  { defaults = true }: { defaults?: boolean } = {},
) {
  const readyRuntimes = runtimes.filter(
    (runtime) => runtime.id && runtime.enabled && runtime.state === "ready",
  );
  const fallbackRuntime =
    readyRuntimes.find((runtime) => runtime.isDefault) ?? readyRuntimes[0];
  const targets = values.map((value) => {
    const runtime = value.runtimeId
      ? runtimes.find((item) => item.id === value.runtimeId)
      : fallbackRuntime;
    const runtimeId = value.runtimeId || runtime?.id || "";
    if (!usesNativeModelCatalog(runtime?.baseRuntimeId || runtimeId)) return;
    const accountId = value.accountId || runtime?.defaultAccountId || "system";
    const account = accounts.find(
      (item) =>
        item.id === accountId &&
        accountSupportsRuntime(item, runtime?.baseRuntimeId || runtimeId),
    );
    const options = nativeModelCatalogOptions(
      nativeCatalogIdentity(runtime, runtimeId, accountId, account),
    );
    const canRead = Boolean(
      (runtime?.installed ?? runtime?.state === "ready") &&
      (accountId === "system" || account),
    );
    return {
      value,
      runtime,
      runtimeId,
      accountId,
      account,
      options: { ...options, enabled: canRead },
      key: JSON.stringify(options.queryKey),
    };
  });
  const queries = new Map(
    targets.flatMap((target) =>
      target ? [[target.key, target.options] as const] : [],
    ),
  );
  const results = useQueries({ queries: [...queries.values()] });
  const byKey = new Map(
    [...queries.keys()].map((key, index) => [key, results[index]]),
  );
  return targets.map((target) => {
    if (!target) return;
    const query = byKey.get(target.key);
    const resolved = resolveNativeSelection({
      ...target,
      catalog: query?.data,
      defaults,
    });
    const settled = query?.fetchStatus === "idle" && !query.isError;
    const validFast =
      defaults || target.value.fast === undefined || resolved.fastSupported;
    return {
      selection: resolved.selection,
      valid: Boolean(
        resolved.configurationValid &&
        settled &&
        target.runtime?.enabled &&
        target.runtime.state === "ready",
      ),
      supportedPermissions:
        resolved.modelConfigurationValid && settled && validFast
          ? resolved.supportedPermissions
          : new Set<PermissionLevel>(),
      pending: query && query.fetchStatus !== "idle",
    };
  });
}

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
  const queryClient = useQueryClient();
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
  const catalogIdentity = nativeCatalogIdentity(
    runtime,
    runtimeId,
    accountId,
    account,
  );
  const catalogOptions = nativeModelCatalogOptions(catalogIdentity);
  const catalogEnabled = Boolean(
    enabled &&
    usesNativeCatalog &&
    runtimeInstalled &&
    (accountId === "system" || account),
  );
  const catalogQuery = useQuery({ ...catalogOptions, enabled: catalogEnabled });
  const refreshCatalog = async () => {
    if (!catalogEnabled) return;
    await queryClient.cancelQueries({
      queryKey: catalogOptions.queryKey,
      exact: true,
    });
    await queryClient
      .fetchQuery({
        ...nativeModelCatalogOptions(catalogIdentity, true),
        staleTime: 0,
      })
      .catch(() => undefined);
  };
  const catalogState = usesNativeCatalog
    ? catalogQuery.isError
      ? catalogQuery.data
        ? "stale"
        : "unavailable"
      : catalogQuery.data?.state
    : undefined;
  const nativeSelection = usesNativeCatalog
    ? resolveNativeSelection({
        value,
        runtime,
        account,
        runtimeId,
        accountId,
        catalog: catalogQuery.data,
        defaults,
        reasoningDefault,
      })
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
  const models: RuntimeModelOption[] =
    nativeSelection?.models ??
    (account
      ? (subscription
          ? (nativeModelsQuery.data?.models ?? [])
          : (account.selectedModels ?? [])
        ).map((id) => ({ id, name: account.modelDisplayNames?.[id] || id }))
      : (runtime?.models ?? []));
  const preferredModel = account
    ? account.defaultModel || nativeModelsQuery.data?.defaultModel
    : runtime?.defaultModel;
  const { model, effectiveModel } = nativeSelection
    ? {
        model: nativeSelection.selection.model,
        effectiveModel: nativeSelection.effectiveModel,
      }
    : resolveModel(value, models, [preferredModel], defaults);
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
  const runtimeModel = runtime?.models?.find(
    (item) => item.id === effectiveModel,
  );
  const reasoningOptions: ReasoningOption[] =
    nativeSelection?.reasoningOptions ??
    (account
      ? thinkingQuery.data?.available
        ? (thinkingQuery.data.options ?? [])
        : []
      : runtimeModel?.reasoningOptions?.length
        ? runtimeModel.reasoningOptions
        : (runtime?.reasoningOptions ?? []));
  const preferredReasoning = account
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
      ? account
        ? ""
        : reasoningOptions[0]?.id || ""
      : requestedReasoning;
  const supportedPermissions =
    nativeSelection?.supportedPermissions ??
    (runtimeModel
      ? modelPermissionLevels(runtime?.models, effectiveModel)
      : runtimeDefaultPermissionLevels(runtime ?? {}));
  const permissionLevel =
    permissionLevelOf(value.permissionLevel) ??
    safeDefaultPermissionLevel(
      permissionLevelOf(runtime?.defaultPermissionLevel) ?? "default",
      supportedPermissions,
    );
  const selection = nativeSelection?.selection ?? {
    runtimeId,
    accountId,
    model,
    reasoning,
    permissionLevel,
    fast: undefined,
  };
  const catalogPending =
    usesNativeCatalog && catalogQuery.fetchStatus !== "idle";
  const catalogIssue =
    !usesNativeCatalog || !catalogState
      ? undefined
      : catalogState === "stale"
        ? "catalogStale"
        : catalogState === "signed_out"
          ? "catalogSignedOut"
          : catalogState !== "fresh"
            ? "catalogUnavailable"
            : !models.length
              ? "catalogEmpty"
              : !nativeSelection?.configurationKnown
                ? "catalogConfigurationUnknown"
                : undefined;
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
  const configurationValid =
    !loading &&
    !error &&
    (nativeSelection
      ? nativeSelection.configurationValid && !catalogPending
      : Boolean(
          (accountId === "system" || account?.hasCredential) &&
          (!model || models.some((item) => item.id === model)) &&
          (!reasoning ||
            reasoningOptions.some((item) => item.id === reasoning)) &&
          supportedPermissions.has(permissionLevel),
        ));
  return {
    selection,
    usesNativeCatalog,
    catalogState,
    catalogIssue,
    refreshCatalog,
    catalogPending,
    fastSupported: nativeSelection?.fastSupported ?? false,
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
    configurationValid,
    valid: Boolean(
      configurationValid && runtime?.enabled && runtime.state === "ready",
    ),
  };
}

export type ExecutionSelectionState = ReturnType<typeof useExecutionSelection>;
