import { minimumReasoning } from "@/shared/lib/runtime-reasoning";
import {
  nativeModelCatalogOptions,
  listRuntimesOptions,
  usesNativeModelCatalog,
} from "@/shared/api/runtime-queries";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";

import { listAiAccountsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
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
} from "@/shared/lib/runtime-permissions";

export type ExecutionSelection = {
  runtimeId?: string;
  accountId?: string;
  model?: string;
  reasoning?: string;
  fast?: boolean;
  contextWindow?: string;
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
            account.endpoint,
            account.selectedModels,
            account.modelDisplayNames,
            account.modelReasoningLevels,
          ]
        : undefined,
    version: runtime?.version,
    binary: runtime?.resolvedBinary,
  };
}

function resolveModel(
  value: ExecutionSelection,
  models: RuntimeModelOption[],
  preferredModels: ReadonlyArray<string | undefined>,
) {
  const fallback =
    preferredModels.find((candidate) => candidate) || models[0]?.id || "";
  const model = value.model ?? fallback;
  return { model, effectiveModel: model || fallback };
}

function resolveNativeSelection({
  value,
  runtime,
  account,
  runtimeId,
  accountId,
  catalog,
  reasoningDefault,
}: {
  value: ExecutionSelection;
  runtime?: RuntimeOption;
  account?: AIAccount;
  runtimeId: string;
  accountId: string;
  catalog?: ModelCatalog;
  reasoningDefault?: "minimum";
}) {
  const models = catalog?.models ?? [];
  const configuredAccount =
    accountId === (runtime?.defaultAccountId || "system");
  const preferredModels = [
    configuredAccount ? runtime?.defaultModel : undefined,
    account?.defaultModel,
    catalog?.defaultModel,
  ];
  const { model, effectiveModel } = resolveModel(
    value,
    models,
    preferredModels,
  );
  const modelOption = effectiveModel
    ? models.find((item) => item.id === effectiveModel)
    : undefined;
  const reasoningOptions = modelOption?.reasoningOptions ?? [];
  const preferredReasoning =
    configuredAccount && effectiveModel === runtime?.defaultModel
      ? (runtime?.defaultReasoning ??
        account?.defaultReasoning ??
        modelOption?.defaultReasoning)
      : effectiveModel === account?.defaultModel
        ? (account?.defaultReasoning ?? modelOption?.defaultReasoning)
        : modelOption?.defaultReasoning;
  const defaultReasoning = preferredReasoning || "";
  const requestedReasoning =
    reasoningDefault === "minimum"
      ? value.reasoning || minimumReasoning(reasoningOptions)
      : (value.reasoning ?? defaultReasoning);
  const reasoning = requestedReasoning;
  const supportedPermissions = modelPermissionLevels(models, effectiveModel);
  const permissionLevel =
    permissionLevelOf(value.permissionLevel) ??
    permissionLevelOf(runtime?.defaultPermissionLevel) ??
    "default";
  const fastSupported = Boolean(modelOption?.fastSupported);
  // 目录只提供候选与显示能力；执行时由原生握手确认明确选择。
  const configurationValid = Boolean(
    runtime?.id &&
    (accountId === "system" || account?.hasCredential) &&
    (!value.permissionLevel || permissionLevelOf(value.permissionLevel)),
  );
  return {
    selection: {
      runtimeId,
      accountId,
      model,
      reasoning,
      permissionLevel,
      fast: value.fast,
      contextWindow: value.contextWindow,
    },
    effectiveModel,
    defaultModel: preferredModels.find((id) => id),
    models,
    reasoningOptions,
    supportedPermissions,
    fastSupported,
    modelListed: Boolean(modelOption),
    configurationValid,
  };
}

/** 批量校验原生配置；同一账号和 CLI 的目录共享 Query。 */
export function useNativeExecutionSelections(
  values: readonly ExecutionSelection[],
  runtimes: readonly RuntimeOption[],
  accounts: readonly AIAccount[],
) {
  const enabledRuntimes = runtimes.filter(
    (runtime) => runtime.id && runtime.enabled,
  );
  const fallbackRuntime =
    enabledRuntimes.find((runtime) => runtime.isDefault) ?? enabledRuntimes[0];
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
        item.runtimeId === runtimeId &&
        accountSupportsRuntime(item, runtime?.baseRuntimeId || runtimeId),
    );
    const options = nativeModelCatalogOptions(
      nativeCatalogIdentity(runtime, runtimeId, accountId, account),
    );
    const canRead = Boolean(runtime?.id && (accountId === "system" || account));
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
    });
    return {
      selection: resolved.selection,
      valid: Boolean(resolved.configurationValid && target.runtime?.enabled),
      supportedPermissions: resolved.supportedPermissions,
      pending: query && query.fetchStatus !== "idle",
    };
  });
}

/** 账号目录、原生订阅模型和模型推理能力统一在此查询；页面只提交选择值。 */
export function useExecutionSelection({
  value,
  runtime: fixedRuntime,
  enabled = true,
  reasoningDefault,
}: {
  value: ExecutionSelection;
  runtime?: RuntimeOption;
  enabled?: boolean;
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
    (item) => item.id && item.enabled,
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
      item.runtimeId === runtimeId &&
      accountSupportsRuntime(item, runtime?.baseRuntimeId || runtimeId),
  );
  const accountId = value.accountId || runtime?.defaultAccountId || "system";
  const account = accounts.find((item) => item.id === accountId);
  const usesNativeCatalog = usesNativeModelCatalog(
    runtime?.baseRuntimeId || runtimeId,
  );
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
    runtime?.id &&
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
        reasoningDefault,
      })
    : undefined;
  const models: RuntimeModelOption[] =
    nativeSelection?.models ?? runtime?.models ?? [];
  const preferredModel = runtime?.defaultModel;
  const { model, effectiveModel } = nativeSelection
    ? {
        model: nativeSelection.selection.model,
        effectiveModel: nativeSelection.effectiveModel,
      }
    : resolveModel(value, models, [preferredModel]);
  const runtimeModel = runtime?.models?.find(
    (item) => item.id === effectiveModel,
  );
  const reasoningOptions: ReasoningOption[] =
    nativeSelection?.reasoningOptions ??
    (runtimeModel?.reasoningOptions?.length
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
  const reasoning = requestedReasoning;
  const supportedPermissions =
    nativeSelection?.supportedPermissions ??
    (runtimeModel
      ? modelPermissionLevels(runtime?.models, effectiveModel)
      : runtimeDefaultPermissionLevels(runtime ?? {}));
  const permissionLevel =
    permissionLevelOf(value.permissionLevel) ??
    permissionLevelOf(runtime?.defaultPermissionLevel) ??
    "default";
  const selection = nativeSelection?.selection ?? {
    runtimeId,
    accountId,
    model,
    reasoning,
    permissionLevel,
    fast: value.fast,
    contextWindow: value.contextWindow,
  };
  const catalogRefreshing =
    usesNativeCatalog && catalogQuery.fetchStatus !== "idle";
  const catalogPending = catalogRefreshing && catalogState !== "fresh";
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
              : !nativeSelection?.modelListed
                ? "catalogModelUnavailable"
                : undefined;
  const runtimeError = fixedRuntime ? null : runtimesQuery.error;
  const accountError = accountId === "system" ? null : accountsQuery.error;
  const error = runtimeError || accountError || catalogQuery.error;
  const loading =
    (!fixedRuntime && runtimesQuery.isPending) ||
    (accountId !== "system" && accountsQuery.isPending);
  const configurationValid =
    !loading &&
    !runtimeError &&
    !accountError &&
    (nativeSelection
      ? nativeSelection.configurationValid
      : Boolean(
          runtime?.id &&
          (accountId === "system" || account?.hasCredential) &&
          (!value.permissionLevel || permissionLevelOf(value.permissionLevel)),
        ));
  return {
    selection,
    usesNativeCatalog,
    catalogState,
    catalogIssue,
    refreshCatalog,
    catalogPending,
    catalogRefreshing,
    fastSupported: nativeSelection?.fastSupported ?? false,
    effectiveModel,
    defaultModel: usesNativeCatalog
      ? nativeSelection?.defaultModel
      : preferredModel,
    runtime,
    runtimes,
    accounts,
    accountsQuery,
    runtimesQuery,
    models,
    reasoningOptions,
    supportedPermissions,
    loading,
    error,
    configurationValid,
    valid: Boolean(configurationValid && runtime?.enabled),
  };
}

export type ExecutionSelectionState = ReturnType<typeof useExecutionSelection>;
