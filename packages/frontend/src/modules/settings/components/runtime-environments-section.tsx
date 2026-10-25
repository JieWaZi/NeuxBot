import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  Check,
  ChevronRight,
  CircleAlert,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@/shared/api/errors";
import {
  listAiAccountsQueryKey,
  listRuntimesQueryKey,
  refreshRuntimeMutation,
  refreshRuntimesMutation,
  updateRuntimeEnvironmentMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type {
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as AIAccount,
} from "@/shared/api/generated/types.gen";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { ExecutionConfigurationFields } from "@/shared/ui/execution-configuration-fields";
import { FieldLabel } from "@/shared/ui/field-label";
import {
  runtimeDisplayName,
  runtimeDisplayVersion,
} from "@/shared/lib/runtime-display-name";
import {
  SettingsContentShell,
  SettingsGroup,
  SettingsRow,
} from "./settings-section-layout";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import { ExecutionPermissionPicker } from "@/shared/ui/execution-permission-picker";
import {
  formatCommandLineArguments,
  parseCommandLineArguments,
} from "@/shared/lib/command-line-arguments";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { Switch } from "@/shared/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import {
  type PermissionLevel,
  permissionLevelTranslationKeys,
  permissionLevelOf,
  runtimeDefaultPermissionLevels,
} from "@/shared/lib/runtime-permissions";

type RuntimeDraft = {
  defaultAccountId: string;
  binary: string;
  configHome: string;
  customModels: string[];
  defaultModel: string;
  defaultReasoning: string;
  defaultPermissionLevel: PermissionLevel;
  prefixArgs: string;
};

type RuntimeIssueKind =
  | "cliLogin"
  | "accountMissing"
  | "accountSubscriptionLogin"
  | "accountKeyMissing"
  | "accountUnavailable"
  | "accountAuth"
  | "protocol"
  | "connection"
  | "modelCatalog";

function runtimeIssueKind(
  runtime: RuntimeOption,
  linkedAccount: AIAccount | undefined,
  accountsLoaded: boolean,
): RuntimeIssueKind | undefined {
  if (!runtime.installed) return undefined;
  const linked = Boolean(runtime.defaultAccountId);
  if (linked && accountsLoaded && !linkedAccount) return "accountMissing";
  if (linkedAccount?.hasCredential === false) {
    return linkedAccount.authMode === "subscription"
      ? "accountSubscriptionLogin"
      : "accountKeyMissing";
  }
  if (
    linked &&
    runtime.diagnostics?.some((item) => item.code === "AI_ACCOUNT_UNAVAILABLE")
  ) {
    return "accountUnavailable";
  }
  if (runtime.auth === "missing") return linked ? "accountAuth" : "cliLogin";
  if (runtime.state === "incompatible") return "protocol";
  if (runtime.state !== "ready") return "connection";
  if (
    runtime.diagnostics?.some((item) => item.code === "ACP_OPTIONS_UNAVAILABLE")
  ) {
    return "modelCatalog";
  }
  return undefined;
}

function runtimeDraft(runtime: RuntimeOption): RuntimeDraft {
  return {
    defaultAccountId: runtime.defaultAccountId ?? "",
    binary: runtime.binary ?? "",
    configHome: runtime.configHome ?? "",
    customModels: [...(runtime.customModels ?? [])],
    defaultModel: runtime.defaultModel ?? "",
    defaultReasoning: runtime.defaultReasoning ?? "",
    defaultPermissionLevel:
      permissionLevelOf(runtime.defaultPermissionLevel) ?? "auto",
    prefixArgs: formatCommandLineArguments(runtime.prefixArgs ?? []),
  };
}

function modelsForDraft(runtime: RuntimeOption, draft: RuntimeDraft) {
  const customModelIDs = new Set(draft.customModels);
  const models = (runtime.models ?? []).filter(
    (model) => !model.custom || customModelIDs.has(model.id ?? ""),
  );
  const seen = new Set(models.map((model) => model.id));
  for (const modelID of draft.customModels) {
    if (seen.has(modelID)) continue;
    seen.add(modelID);
    models.push({
      id: modelID,
      name: modelID,
      custom: true,
      reasoningOptions: runtime.reasoningOptions ?? [],
    });
  }
  return models;
}

function reasoningOptionsForModel(
  runtime: RuntimeOption,
  modelID: string,
  models = runtime.models ?? [],
) {
  const model = models.find((option) => option.id === modelID);
  return model?.reasoningOptions?.length
    ? model.reasoningOptions
    : (runtime.reasoningOptions ?? []);
}

function normalizedRuntimeDraft(
  runtime: RuntimeOption,
  draft: RuntimeDraft,
): RuntimeDraft {
  const models = modelsForDraft(runtime, draft);
  const defaultModel = models.some((model) => model.id === draft.defaultModel)
    ? draft.defaultModel
    : (models[0]?.id ?? "");
  const reasoningOptions = reasoningOptionsForModel(
    runtime,
    defaultModel,
    models,
  );
  const defaultReasoning = reasoningOptions.some(
    (option) => option.id === draft.defaultReasoning,
  )
    ? draft.defaultReasoning
    : (reasoningOptions[0]?.id ?? "");
  return { ...draft, defaultModel, defaultReasoning };
}

function runtimeBody(
  runtime: RuntimeOption,
  draft: RuntimeDraft,
  patch: Partial<{ enabled: boolean; isDefault: boolean }> = {},
) {
  return {
    enabled: patch.enabled ?? runtime.enabled ?? false,
    isDefault: patch.isDefault ?? runtime.isDefault ?? false,
    defaultAccountId: draft.defaultAccountId,
    binary: draft.binary.trim(),
    prefixArgs: parseCommandLineArguments(draft.prefixArgs),
    configHome: draft.configHome.trim(),
    customModels: draft.customModels,
    defaultModel: draft.defaultModel,
    defaultReasoning: draft.defaultReasoning,
    defaultPermissionLevel: draft.defaultPermissionLevel,
  };
}

function errorMessage(error: unknown) {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return "操作失败，请稍后重试。";
}

function RuntimeEnvironmentRow({
  onOpenAccounts,
  permissionEditingEnabled,
  runtime,
}: {
  onOpenAccounts?: () => void;
  permissionEditingEnabled: boolean;
  runtime: RuntimeOption;
}) {
  const { t } = useTranslation("common");
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [issueOpen, setIssueOpen] = useState(false);
  const [draft, setDraft] = useState(() => runtimeDraft(runtime));
  const [draftError, setDraftError] = useState("");
  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: listRuntimesQueryKey() }),
      queryClient.invalidateQueries({ queryKey: listAiAccountsQueryKey() }),
    ]);
  };
  const update = useMutation({
    ...withLocalErrorMutation(updateRuntimeEnvironmentMutation()),
    onSuccess: invalidate,
  });
  const refresh = useMutation({
    ...withLocalErrorMutation(refreshRuntimeMutation()),
    onSuccess: invalidate,
  });
  const pending = update.isPending || refresh.isPending;
  const optionsUnavailable = runtime.diagnostics?.some(
    (item) => item.code === "ACP_OPTIONS_UNAVAILABLE",
  );
  const usingPreviousOptions = runtime.diagnostics?.some(
    (item) => item.code === "ACP_OPTIONS_FALLBACK",
  );
  const ready = runtime.state === "ready";
  const runtimeID = runtime.id ?? "";
  const selection = useExecutionSelection({
    runtime: { ...runtime, models: modelsForDraft(runtime, draft) },
    value: {
      runtimeId: runtime.id,
      accountId: draft.defaultAccountId || "system",
      model: draft.defaultModel || undefined,
      reasoning: draft.defaultReasoning,
      permissionLevel: draft.defaultPermissionLevel,
    },
    enabled: true,
    defaults: true,
  });
  const accountsQuery = selection.accountsQuery;
  const linkedAccount = (accountsQuery.data?.resources ?? []).find(
    (account) => account.id === runtime.defaultAccountId,
  );
  const issueKind = runtimeIssueKind(
    runtime,
    linkedAccount,
    accountsQuery.isSuccess,
  );
  const accountName =
    linkedAccount?.name || t("runtimeEnvironment.issues.accountFallback");
  const issueMessages = {
    cliLogin: {
      title: t("runtimeEnvironment.issues.cliLogin.title"),
      guidance: t("runtimeEnvironment.issues.cliLogin.guidance", {
        runtime: runtimeDisplayName(runtime),
      }),
      accountRelated: false,
    },
    accountMissing: {
      title: t("runtimeEnvironment.issues.accountMissing.title"),
      guidance: t("runtimeEnvironment.issues.accountMissing.guidance"),
      accountRelated: true,
    },
    accountSubscriptionLogin: {
      title: t("runtimeEnvironment.issues.accountSubscriptionLogin.title"),
      guidance: t(
        "runtimeEnvironment.issues.accountSubscriptionLogin.guidance",
        { account: accountName },
      ),
      accountRelated: true,
    },
    accountKeyMissing: {
      title: t("runtimeEnvironment.issues.accountKeyMissing.title"),
      guidance: t("runtimeEnvironment.issues.accountKeyMissing.guidance", {
        account: accountName,
      }),
      accountRelated: true,
    },
    accountUnavailable: {
      title: t("runtimeEnvironment.issues.accountUnavailable.title"),
      guidance: t("runtimeEnvironment.issues.accountUnavailable.guidance", {
        account: accountName,
      }),
      accountRelated: true,
    },
    accountAuth: {
      title: t("runtimeEnvironment.issues.accountAuth.title"),
      guidance: t("runtimeEnvironment.issues.accountAuth.guidance", {
        account: accountName,
      }),
      accountRelated: true,
    },
    protocol: {
      title: t("runtimeEnvironment.issues.protocol.title"),
      guidance: t("runtimeEnvironment.issues.protocol.guidance"),
      accountRelated: false,
    },
    connection: {
      title: t("runtimeEnvironment.issues.connection.title"),
      guidance: t("runtimeEnvironment.issues.connection.guidance"),
      accountRelated: false,
    },
    modelCatalog: {
      title: t("runtimeEnvironment.issues.modelCatalog.title"),
      guidance: t("runtimeEnvironment.issues.modelCatalog.guidance"),
      accountRelated: Boolean(runtime.defaultAccountId),
    },
  };
  const issue = issueKind ? issueMessages[issueKind] : undefined;
  const normalizedDraft = {
    ...draft,
    defaultModel: selection.selection.model,
    defaultReasoning: selection.selection.reasoning,
  };
  const installations = (runtime.installations ?? []).filter(
    (installation): installation is typeof installation & { path: string } =>
      Boolean(installation.path),
  );
  const selectedPath = installations.find(
    (installation) =>
      installation.path === draft.binary ||
      (draft.binary === runtime.binary &&
        installation.path === runtime.resolvedBinary),
  )?.path;
  const selectedInstallation = installations.find(
    (installation) => installation.path === selectedPath,
  );
  const supportedPermissions = runtimeDefaultPermissionLevels(runtime);
  const selectedPermissionLevel =
    permissionLevelOf(runtime.defaultPermissionLevel) ?? "auto";
  const selectedPermissionLabel = t(
    permissionLevelTranslationKeys[selectedPermissionLevel].label,
  );
  const permissionDefaultDowngraded = runtime.diagnostics?.some(
    (item) => item.code === "PERMISSION_DEFAULT_DOWNGRADED",
  );
  const fullAccessUnavailable =
    supportedPermissions.size > 0 && !supportedPermissions.has("full-access");
  const pendingDetection = runtime.detectedAt === 0 && runtime.state === "";
  const unavailableStatus = pendingDetection
    ? t("runtimeEnvironment.status.detecting")
    : !runtime.installed
      ? t("runtimeEnvironment.status.notInstalled")
      : runtime.auth === "missing"
        ? t("runtimeEnvironment.status.loginRequired")
        : runtime.state === "incompatible"
          ? t("runtimeEnvironment.status.incompatible")
          : t("runtimeEnvironment.status.notReady");
  const version = runtimeDisplayVersion(runtime.version);
  const detectionStatus =
    (runtime.installed && version ? version : undefined) ??
    issue?.title ??
    (!runtime.installed
      ? unavailableStatus
      : runtime.auth === "missing"
        ? t("runtimeEnvironment.status.loginRequired")
        : ready
          ? version || t("runtimeEnvironment.status.versionUnknown")
          : unavailableStatus);
  const mutate = (
    nextDraft: RuntimeDraft,
    patch?: Partial<{ enabled: boolean; isDefault: boolean }>,
    onError?: () => void,
  ) => {
    try {
      update.mutate(
        {
          path: { runtimeId: runtimeID },
          body: runtimeBody(runtime, nextDraft, patch),
        },
        { onError },
      );
      setDraftError("");
    } catch {
      setDraftError("参数格式无效，请检查引号和转义。");
    }
  };

  return (
    <Collapsible
      open={expanded}
      onOpenChange={(open) => {
        if (open) {
          setDraft(runtimeDraft(runtime));
          setDraftError("");
        }
        setExpanded(open);
      }}
      className="bg-surface-tertiary overflow-hidden rounded-lg"
    >
      <div className="min-h-row-rich flex flex-wrap items-center gap-x-2">
        <CollapsibleTrigger
          aria-label={
            expanded
              ? `收起${runtimeDisplayName(runtime)}智能体设置`
              : `展开${runtimeDisplayName(runtime)}智能体设置`
          }
          className="group text-content-primary hover:bg-action-quiet-hover focus-visible:ring-ring flex min-w-0 flex-1 basis-48 items-center gap-2 self-stretch px-4 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
        >
          <ChevronRight
            aria-hidden="true"
            className="size-icon-sm text-content-muted shrink-0 group-data-[state=open]:rotate-90"
          />
          <RuntimeCliIcon runtimeId={runtimeID} />
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="type-body font-medium break-words">
              {runtimeDisplayName(runtime)}
            </span>
            {detectionStatus ? (
              <span
                className="type-ui text-content-secondary min-w-0 truncate"
                title={detectionStatus}
              >
                {detectionStatus}
              </span>
            ) : null}
          </span>
        </CollapsibleTrigger>
        <div className="ml-auto flex max-w-full flex-wrap items-center justify-end gap-2 px-3 py-2">
          {issue ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  className="text-status-warning-content hover:text-status-warning-content"
                  aria-label={`${runtimeDisplayName(runtime)}：${issue.title}，查看处理建议`}
                  onClick={() => setIssueOpen(true)}
                >
                  <CircleAlert />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{issue.title}</TooltipContent>
            </Tooltip>
          ) : null}
          <div className="flex items-center gap-2">
            <Switch
              checked={ready && (runtime.enabled ?? false)}
              disabled={pending || !ready}
              aria-label={`${runtimeDisplayName(runtime)}启用状态`}
              onClick={() =>
                mutate(normalizedRuntimeDraft(runtime, runtimeDraft(runtime)), {
                  enabled: !runtime.enabled,
                })
              }
            />
            <span className="text-content-secondary type-ui whitespace-nowrap">
              {t(
                !ready
                  ? "runtimeEnvironment.status.unavailable"
                  : runtime.enabled
                    ? "runtimeEnvironment.status.enabled"
                    : "runtimeEnvironment.status.disabled",
              )}
            </span>
          </div>
          <ExecutionPermissionPicker
            value={selectedPermissionLevel}
            supported={supportedPermissions}
            disabled={
              pending ||
              !ready ||
              !permissionEditingEnabled ||
              supportedPermissions.size === 0
            }
            ariaLabel={t("permission.runtimePickerAria", {
              runtime: runtimeDisplayName(runtime),
              name: selectedPermissionLabel,
            })}
            onChange={(value) => {
              setDraft((current) => ({
                ...current,
                defaultPermissionLevel: value,
              }));
              mutate(
                normalizedRuntimeDraft(runtime, {
                  ...runtimeDraft(runtime),
                  defaultPermissionLevel: value,
                }),
              );
            }}
          />
          <Button
            size="sm"
            variant="ghost"
            disabled={
              pending || !runtime.enabled || !ready || runtime.isDefault
            }
            onClick={() =>
              mutate(normalizedRuntimeDraft(runtime, runtimeDraft(runtime)), {
                isDefault: true,
              })
            }
          >
            {runtime.isDefault ? <Check /> : null}
            {runtime.isDefault ? "默认" : "设为默认"}
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            disabled={pending}
            aria-label={`重新探测${runtimeDisplayName(runtime)}`}
            onClick={() => refresh.mutate({ path: { runtimeId: runtimeID } })}
          >
            <RefreshCw className={pending ? "animate-spin" : undefined} />
          </Button>
        </div>
      </div>
      {ready && (permissionDefaultDowngraded || fullAccessUnavailable) ? (
        <p className="text-content-secondary type-caption px-4 pb-3">
          {t("permission.defaultDowngraded")}
        </p>
      ) : null}

      <CollapsibleContent>
        <div className="border-border-subtle border-t p-4">
          <ExecutionConfigurationFields
            state={selection}
            runtimeHidden
            permissionHidden
            defaults
            disabled={pending}
            leadingField={
              installations.length > 1 ? (
                <div className="relative grid min-w-0 gap-2">
                  <FieldLabel htmlFor={`${runtimeID}-version`}>版本</FieldLabel>
                  <Select
                    value={selectedPath ?? ""}
                    disabled={pending}
                    onValueChange={(path) => {
                      const nextDraft = normalizedRuntimeDraft(runtime, {
                        ...runtimeDraft(runtime),
                        binary: path,
                      });
                      setDraft(nextDraft);
                      mutate(nextDraft, undefined, () =>
                        setDraft(runtimeDraft(runtime)),
                      );
                    }}
                  >
                    <SelectTrigger
                      id={`${runtimeID}-version`}
                      aria-label={`${runtimeDisplayName(runtime)}版本`}
                    >
                      <SelectValue placeholder="选择版本" />
                    </SelectTrigger>
                    <SelectContent>
                      {installations.map((installation) => (
                        <SelectItem
                          key={installation.path}
                          value={installation.path}
                          disabled={!installation.available}
                        >
                          {runtimeDisplayVersion(installation.version) ||
                            "未知版本"}
                          {!installation.available ? " · 路径已失效" : ""}
                          {` · ${installation.path}`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {selectedInstallation && !selectedInstallation.available ? (
                    <p className="type-caption text-status-warning-content">
                      当前版本的安装路径已失效，请选择其他版本。
                    </p>
                  ) : null}
                </div>
              ) : undefined
            }
            onChange={(next) =>
              setDraft((current) => ({
                ...current,
                defaultAccountId:
                  next.accountId === "system" ? "" : next.accountId,
                defaultModel: next.model,
                defaultReasoning: next.reasoning,
                defaultPermissionLevel: next.permissionLevel,
              }))
            }
          />

          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <div className="text-muted-foreground type-caption flex items-center gap-2">
              {pending ? (
                <Loader2 className="size-icon-sm animate-spin" />
              ) : null}
              {pending
                ? "正在保存并重新探测…"
                : runtime.auth === "missing"
                  ? t("runtimeEnvironment.status.loginRequired")
                  : usingPreviousOptions
                    ? "模型目录刷新失败，已保留上次结果"
                    : optionsUnavailable
                      ? "模型目录刷新失败，当前使用 Default 兜底"
                      : ready
                        ? `已发现${version ? ` · ${version}` : ""}`
                        : unavailableStatus}
            </div>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="ghost"
                disabled={pending}
                onClick={() => {
                  setDraft(runtimeDraft(runtime));
                  setDraftError("");
                }}
              >
                取消
              </Button>
              <Button
                size="sm"
                disabled={pending || !draft.binary.trim()}
                onClick={() => mutate(normalizedDraft)}
              >
                保存
              </Button>
            </div>
          </div>
          {draftError || update.isError || refresh.isError ? (
            <p className="text-destructive type-caption mt-3 flex items-center gap-2">
              <CircleAlert className="size-icon-sm" />
              {draftError || errorMessage(update.error ?? refresh.error)}
            </p>
          ) : null}
        </div>
      </CollapsibleContent>
      {issue ? (
        <Dialog open={issueOpen} onOpenChange={setIssueOpen}>
          <DialogContent
            size="sm"
            title={issue.title}
            description={issue.guidance}
          >
            {issue.accountRelated && onOpenAccounts ? (
              <DialogFooter>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => {
                    setIssueOpen(false);
                    onOpenAccounts();
                  }}
                >
                  {t("runtimeEnvironment.issues.openAccounts")}
                </Button>
              </DialogFooter>
            ) : null}
          </DialogContent>
        </Dialog>
      ) : null}
    </Collapsible>
  );
}

function RuntimeGroupCount({ count }: { count: number | "loading" }) {
  return <Badge variant="secondary">{count === "loading" ? "—" : count}</Badge>;
}

function RuntimeGroupSkeleton({
  rows,
  title,
}: {
  rows: number;
  title: string;
}) {
  return (
    <SettingsGroup
      title={title}
      trailing={<RuntimeGroupCount count="loading" />}
    >
      <div
        aria-busy="true"
        aria-label="正在读取智能体"
        className="flex flex-col gap-3"
        role="status"
      >
        {Array.from({ length: rows }, (_, index) => (
          <div
            key={index}
            className="bg-surface-tertiary min-h-row-rich flex flex-wrap items-center gap-3 rounded-lg px-4 py-3"
          >
            <Skeleton className="size-icon-sm shrink-0 rounded-sm" />
            <div className="flex min-w-0 flex-1 basis-40 flex-wrap items-center gap-2">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-3 w-20" />
            </div>
            <Skeleton className="h-row-navigation w-48 shrink-0 rounded-md" />
          </div>
        ))}
      </div>
    </SettingsGroup>
  );
}

function RuntimeGroup({
  empty,
  onOpenAccounts,
  permissionEditingEnabled,
  runtimes,
  title,
}: {
  empty: string;
  onOpenAccounts?: () => void;
  permissionEditingEnabled: boolean;
  runtimes: RuntimeOption[];
  title: string;
}) {
  return (
    <SettingsGroup
      title={title}
      trailing={<RuntimeGroupCount count={runtimes.length} />}
    >
      {runtimes.length ? (
        <div className="flex flex-col gap-3">
          {runtimes.map((runtime) => (
            <RuntimeEnvironmentRow
              key={runtime.id}
              onOpenAccounts={onOpenAccounts}
              permissionEditingEnabled={permissionEditingEnabled}
              runtime={runtime}
            />
          ))}
        </div>
      ) : (
        <p className="text-content-secondary type-body bg-surface-tertiary rounded-lg px-4 py-6 text-center">
          {empty}
        </p>
      )}
    </SettingsGroup>
  );
}

export function RuntimeEnvironmentsSection({
  onOpenAccounts,
}: {
  onOpenAccounts?: () => void;
}) {
  const { t } = useTranslation("common");
  const queryClient = useQueryClient();
  const runtimesQuery = useQuery(
    listRuntimesOptions(withLocalErrorSurface({})),
  );
  const [manualEditing, setManualEditing] = useState(false);
  const [yoloConfirmOpen, setYoloConfirmOpen] = useState(false);
  const [yoloSaving, setYoloSaving] = useState(false);
  const [yoloError, setYoloError] = useState("");
  const refresh = useMutation({
    ...withLocalErrorMutation(refreshRuntimesMutation()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: listRuntimesQueryKey() });
    },
  });
  const runtimes = runtimesQuery.data?.resources ?? [];
  const discovered = runtimes.filter((runtime) => runtime.installed);
  const undiscovered = runtimes.filter((runtime) => !runtime.installed);
  const yoloCapable =
    discovered.some((runtime) => runtime.state === "ready") &&
    discovered
      .filter((runtime) => runtime.state === "ready")
      .every((runtime) =>
        runtimeDefaultPermissionLevels(runtime).has("full-access"),
      );
  const allDiscoveredUseFullAccess =
    yoloCapable &&
    discovered
      .filter((runtime) => runtime.state === "ready")
      .every((runtime) => runtime.defaultPermissionLevel === "full-access");
  const permissionMode =
    allDiscoveredUseFullAccess && !manualEditing ? "yolo" : "manual";
  const yoloUpdate = useMutation(
    withLocalErrorMutation(updateRuntimeEnvironmentMutation()),
  );
  const enableYolo = async () => {
    setYoloSaving(true);
    setYoloError("");
    try {
      await Promise.all(
        discovered.flatMap((runtime) =>
          runtime.id &&
          runtime.state === "ready" &&
          runtimeDefaultPermissionLevels(runtime).has("full-access")
            ? [
                yoloUpdate.mutateAsync({
                  path: { runtimeId: runtime.id },
                  body: runtimeBody(runtime, {
                    ...runtimeDraft(runtime),
                    defaultPermissionLevel: "full-access",
                  }),
                }),
              ]
            : [],
        ),
      );
      await queryClient.invalidateQueries({
        queryKey: listRuntimesQueryKey(),
      });
      setManualEditing(false);
      setYoloConfirmOpen(false);
    } catch (error) {
      setYoloError(errorMessage(error));
      await queryClient.invalidateQueries({
        queryKey: listRuntimesQueryKey(),
      });
    } finally {
      setYoloSaving(false);
    }
  };

  return (
    <SettingsContentShell
      title="智能体"
      description="管理本机智能体、默认模型和启动设置。"
      actions={
        <Button
          size="sm"
          variant="secondary"
          disabled={refresh.isPending}
          onClick={() => refresh.mutate({})}
        >
          <RefreshCw
            className={refresh.isPending ? "animate-spin" : undefined}
          />
          刷新
        </Button>
      }
    >
      <SettingsRow
        title={t("permission.sectionTitle")}
        description={
          discovered.length > 0 && !yoloCapable
            ? t("permission.yoloUnavailable")
            : undefined
        }
      >
        <SegmentedControl
          size="sm"
          ariaLabel={t("permission.modeAria")}
          value={permissionMode}
          options={[
            { value: "manual", label: t("permission.manual") },
            {
              value: "yolo",
              label: t("permission.yolo"),
              disabled: !yoloCapable,
            },
          ]}
          onValueChange={(mode) => {
            if (mode === "manual") {
              setManualEditing(true);
              return;
            }
            if (permissionMode === "manual") {
              setYoloError("");
              setYoloConfirmOpen(true);
            }
          }}
        />
      </SettingsRow>

      {runtimesQuery.isLoading ? (
        <>
          <RuntimeGroupSkeleton title="已发现" rows={3} />
          <RuntimeGroupSkeleton title="未发现" rows={1} />
        </>
      ) : runtimesQuery.isError ? (
        <p className="text-destructive type-body flex items-center gap-2">
          <CircleAlert className="size-icon-md" />
          {errorMessage(runtimesQuery.error)}
        </p>
      ) : (
        <>
          <RuntimeGroup
            title="已发现"
            runtimes={discovered}
            empty="尚未发现可用智能体。"
            onOpenAccounts={onOpenAccounts}
            permissionEditingEnabled={permissionMode === "manual"}
          />
          <RuntimeGroup
            title="未发现"
            runtimes={undiscovered}
            empty="没有其他受支持的智能体。"
            permissionEditingEnabled={false}
          />
        </>
      )}

      {refresh.isError ? (
        <p className="text-destructive type-caption flex items-center gap-2">
          <CircleAlert className="size-icon-sm" />
          {errorMessage(refresh.error)}
        </p>
      ) : null}

      <Dialog
        open={yoloConfirmOpen}
        onOpenChange={(open) => {
          if (!yoloSaving) setYoloConfirmOpen(open);
        }}
      >
        <DialogContent
          size="sm"
          title={t("permission.yoloConfirmTitle")}
          description={t("permission.yoloConfirmDescription")}
        >
          {yoloError ? (
            <p className="text-destructive type-body px-6 py-3">{yoloError}</p>
          ) : null}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="secondary" disabled={yoloSaving}>
                {t("common:cancel")}
              </Button>
            </DialogClose>
            <Button
              type="button"
              disabled={yoloSaving}
              onClick={() => void enableYolo()}
            >
              {yoloSaving ? <Loader2 className="animate-spin" /> : null}
              {t("permission.yoloConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SettingsContentShell>
  );
}
