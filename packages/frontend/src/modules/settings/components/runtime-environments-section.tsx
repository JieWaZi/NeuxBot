import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Power, RefreshCw } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  listRuntimesOptions,
  nativeModelCatalogQueryKey,
} from "@/shared/api/runtime-queries";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  listRuntimesQueryKey,
  refreshRuntimesMutation,
  restartRuntimeMutation,
  updateRuntimeEnvironmentMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption } from "@/shared/api/generated/types.gen";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import {
  runtimeDisplayName,
  runtimeDisplayVersion,
} from "@/shared/lib/runtime-display-name";
import { StatusPill } from "@/shared/ui/status-pill";
import { runtimeHeaderState } from "../lib/runtime-status";
import { Button } from "@/shared/ui/button";
import { FieldLabel } from "@/shared/ui/field-label";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/shared/ui/tooltip";
import { ExecutionPermissionPicker } from "@/shared/ui/execution-permission-picker";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { PageHeader } from "@/shared/ui/page-header";
import { SearchField } from "@/shared/ui/search-field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import { Switch } from "@/shared/ui/switch";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";
import { runtimeSettingsBody } from "../lib/runtime-settings";
import { AIAccountsSection } from "./ai-accounts-section";
import {
  RuntimeInstallationControls,
  runtimeInstallationOptions,
} from "./runtime-installation-controls";
import { runtimeAuthenticationOptions } from "./runtime-authentication-controls";
import { RuntimeProgramRemoval } from "./runtime-program-removal";
import {
  SettingsContentShell,
  SettingsGroup,
  SettingsRow,
} from "./settings-section-layout";

type RuntimePatch = Partial<ReturnType<typeof runtimeSettingsBody>> & {
  installationPath?: string;
};

function RuntimeSkeleton() {
  const { t } = useTranslation("aiAccounts");
  return (
    <div
      role="status"
      aria-label={t("agents.loading")}
      aria-busy="true"
      className="bg-resource-surface overflow-hidden rounded-lg"
    >
      {[0, 1, 2].map((index) => (
        <div
          aria-hidden="true"
          key={index}
          className="min-h-row-detail flex items-center gap-4 px-4 py-3"
        >
          <Skeleton className="size-8 rounded-lg" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3 w-48" />
          </div>
        </div>
      ))}
    </div>
  );
}

function RuntimeDetail({
  runtime,
  onBack,
}: {
  runtime: RuntimeOption;
  onBack: () => void;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const queryClient = useQueryClient();
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: listRuntimesQueryKey() });
  const update = useMutation({
    ...withLocalErrorMutation(updateRuntimeEnvironmentMutation()),
    onSuccess: async (_data, variables) => {
      await invalidate();
      if (variables.body.installationPath) {
        await Promise.all([
          queryClient.invalidateQueries({
            queryKey: runtimeInstallationOptions(runtime.id ?? "").queryKey,
          }),
          queryClient.invalidateQueries({
            queryKey: runtimeAuthenticationOptions(runtime.id ?? "").queryKey,
          }),
        ]);
      }
      if (
        variables.body.installationPath ||
        variables.body.defaultAccountId !== runtime.defaultAccountId
      ) {
        void queryClient.invalidateQueries({
          queryKey: nativeModelCatalogQueryKey({ runtimeId: runtime.id }),
        });
      }
    },
  });
  const restart = useMutation({
    ...withLocalErrorMutation(restartRuntimeMutation()),
    onSuccess: invalidate,
  });
  const installation = useQuery(runtimeInstallationOptions(runtime.id ?? ""));
  const programInstalled = installation.data?.installed ?? runtime.installed;
  const authentication = useQuery({
    ...runtimeAuthenticationOptions(runtime.id ?? ""),
    enabled: Boolean(programInstalled),
  });
  const installing = installation.data?.state === "installing";
  const authenticatingSystem = Boolean(
    authentication.data?.jobId &&
    authentication.data.scope === "system" &&
    authentication.data.cancelable &&
    (authentication.data.state === "authenticating" ||
      authentication.data.state === "checking"),
  );
  const pending =
    update.isPending || installing || restart.isPending || authenticatingSystem;
  const modelPending =
    update.isPending ||
    installing ||
    restart.isPending ||
    (authenticatingSystem && !runtime.defaultAccountId);
  const selection = useExecutionSelection({
    runtime,
    enabled: Boolean(programInstalled),
    value: {
      runtimeId: runtime.id,
      accountId: runtime.defaultAccountId || "system",
      model: runtime.defaultModel,
      reasoning: runtime.defaultReasoning,
      fast: runtime.defaultFast,
      permissionLevel: runtime.defaultPermissionLevel,
    },
  });
  const save = async (patch: RuntimePatch) => {
    await update.mutateAsync({
      path: { runtimeId: runtime.id ?? "" },
      body: { ...runtimeSettingsBody(runtime), ...patch },
    });
  };
  const name = runtimeDisplayName(runtime);
  const status = runtimeHeaderState(
    runtime,
    installation.data,
    authentication.data,
  );
  const modelValue = selection.selection.model || "__native_default";
  const reasoningValue = selection.selection.reasoning || "__native_default";
  const fastValue =
    selection.selection.fast === undefined
      ? "__native_default"
      : selection.selection.fast
        ? "on"
        : "off";
  const modelDisabled =
    modelPending || selection.loading || selection.models.length === 0;
  const installations = (runtime.installations ?? []).filter(
    (item) => item.available && item.path,
  );
  const multipleInstallations = installations.length > 1;
  const version = runtimeDisplayVersion(
    installation.data?.version || runtime.version,
  );
  const versionDescription = !programInstalled
    ? t("agents.status.notInstalled")
    : !version
      ? t("agents.versionUnknown")
      : installation.data?.updateAvailable && installation.data.latestVersion
        ? t("agents.versionAvailable", {
            version,
            latest: installation.data.latestVersion,
          })
        : installation.data?.source === "managed"
          ? t("agents.versionManaged", { version })
          : installation.data?.source === "system"
            ? t("agents.versionSystem", { version })
            : version;
  return (
    <>
      <PageHeader
        appearance="compact"
        layout="detail"
        title={name}
        status={
          <StatusPill tone={status.tone}>
            {t(`agents.status.${status.label}`)}
          </StatusPill>
        }
        backAction={{ label: t("agents.back"), onClick: onBack }}
        description={runtime.isDefault ? t("agents.defaultHint") : undefined}
        visual={
          <div className="bg-resource-surface flex size-12 shrink-0 items-center justify-center rounded-lg">
            <RuntimeCliIcon
              runtimeId={runtime.baseRuntimeId || runtime.id}
              size={24}
            />
          </div>
        }
        actions={
          <div className="flex items-center gap-2">
            <Switch
              hitArea="comfortable"
              aria-label={t("agents.enabled")}
              checked={runtime.enabled ?? false}
              disabled={
                pending ||
                (!runtime.enabled &&
                  (!programInstalled || runtime.state !== "ready"))
              }
              onClick={() =>
                void save({ enabled: !runtime.enabled }).catch(() => undefined)
              }
            />
            <ExecutionPermissionPicker
              hitArea="comfortable"
              ariaLabel={t("agents.permission")}
              value={
                permissionLevelOf(runtime.defaultPermissionLevel) ?? "auto"
              }
              supported={selection.supportedPermissions}
              disabled={
                pending || !programInstalled || runtime.state !== "ready"
              }
              onChange={(defaultPermissionLevel) =>
                void save({ defaultPermissionLevel }).catch(() => undefined)
              }
            />
          </div>
        }
      />
      {!programInstalled ? (
        <SettingsGroup title={t("login")}>
          <SettingsRow title={t("agents.status.notInstalled")} />
        </SettingsGroup>
      ) : null}
      {programInstalled ? (
        <AIAccountsSection
          runtime={runtime}
          disabled={pending}
          onSelectAccount={(defaultAccountId) =>
            save({
              defaultAccountId,
              defaultModel: "",
              defaultReasoning: "",
              defaultFast: undefined,
            })
          }
        />
      ) : null}
      <SettingsGroup title={t("agents.runtime")}>
        <div className="bg-resource-surface overflow-hidden rounded-lg">
          <SettingsRow
            grouped
            title={t("agents.version")}
            description={
              multipleInstallations ? undefined : (
                <span title={installation.data?.installationPath}>
                  {versionDescription}
                </span>
              )
            }
          >
            {multipleInstallations ? (
              <Select
                value={runtime.resolvedBinary}
                disabled={pending}
                onValueChange={(installationPath) =>
                  void save({ installationPath }).catch(() => undefined)
                }
              >
                <SelectTrigger
                  aria-label={t("agents.version")}
                  className="w-64"
                >
                  <SelectValue>{versionDescription}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {installations.map((item) => (
                    <SelectItem key={item.path} value={item.path ?? ""}>
                      {runtimeDisplayVersion(item.version) ||
                        t("agents.versionUnknown")}{" "}
                      · {item.path}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
            <RuntimeInstallationControls
              runtimeId={runtime.id ?? ""}
              baseRuntimeId={runtime.baseRuntimeId}
              installed={programInstalled ?? false}
              version={runtime.version}
              disabled={restart.isPending}
            />
          </SettingsRow>
          {update.error && update.variables?.body.installationPath ? (
            <p role="alert" className="type-body text-destructive px-4 py-3">
              {update.error.message}
            </p>
          ) : null}
          {programInstalled ? (
            <SettingsRow
              grouped
              title={t("agents.restart")}
              content={
                restart.error ? (
                  <p role="alert" className="text-destructive type-body mt-2">
                    {restart.error.message}
                  </p>
                ) : undefined
              }
            >
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon"
                    variant="plain"
                    disabled={pending}
                    aria-label={t("agents.restartNamed", { name })}
                    aria-busy={restart.isPending}
                    onClick={() =>
                      restart.mutate({ path: { runtimeId: runtime.id ?? "" } })
                    }
                  >
                    <Power
                      aria-hidden="true"
                      className={
                        restart.isPending
                          ? "motion-safe:animate-spin"
                          : undefined
                      }
                    />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {t(
                    restart.isPending ? "agents.restarting" : "agents.restart",
                  )}
                </TooltipContent>
              </Tooltip>
            </SettingsRow>
          ) : null}
        </div>
      </SettingsGroup>
      {programInstalled ? (
        <SettingsGroup
          title={t("agents.models")}
          trailing={
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="icon"
                  variant="plain"
                  disabled={selection.catalogRefreshing || modelPending}
                  aria-label={t("agents.refreshModels")}
                  onClick={() => void selection.refreshCatalog()}
                >
                  <RefreshCw
                    className={
                      selection.catalogRefreshing ? "animate-spin" : undefined
                    }
                  />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("agents.refreshModels")}</TooltipContent>
            </Tooltip>
          }
        >
          <fieldset
            aria-label={t("agents.models")}
            className="border-field-border bg-resource-surface @container/runtime-models min-w-0 rounded-lg border p-4"
          >
            {selection.loading ? (
              <div
                role="status"
                aria-busy="true"
                aria-label={t("loadingNativeModels")}
              >
                <Skeleton className="h-control-md w-full" />
              </div>
            ) : (
              <div className="grid min-w-0 grid-cols-1 gap-4 @min-[20rem]/runtime-models:grid-cols-2">
                <div className="grid min-w-0 gap-2">
                  <FieldLabel htmlFor="runtime-default-model">
                    {t("agents.defaultModel")}
                  </FieldLabel>
                  <Select
                    value={modelValue}
                    disabled={modelDisabled}
                    onValueChange={(model) =>
                      void save({
                        defaultModel: model === "__native_default" ? "" : model,
                        defaultReasoning: "",
                        defaultFast: undefined,
                      }).catch(() => undefined)
                    }
                  >
                    <SelectTrigger
                      id="runtime-default-model"

                      aria-label={t("agents.defaultModel")}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__native_default">
                        {t("nativeDefault")}
                      </SelectItem>
                      {selection.models
                        .filter((model) => model.id)
                        .map((model) => (
                          <SelectItem key={model.id} value={model.id ?? ""}>
                            {model.name || model.id}
                          </SelectItem>
                        ))}
                    </SelectContent>
                  </Select>
                </div>
                {selection.reasoningOptions.length ? (
                  <div className="grid min-w-0 gap-2">
                    <FieldLabel htmlFor="runtime-default-reasoning">
                      {t("agents.reasoning")}
                    </FieldLabel>
                    <Select
                      value={reasoningValue}
                      disabled={modelDisabled}
                      onValueChange={(defaultReasoning) =>
                        void save({
                          defaultReasoning:
                            defaultReasoning === "__native_default"
                              ? ""
                              : defaultReasoning,
                        }).catch(() => undefined)
                      }
                    >
                      <SelectTrigger
                        id="runtime-default-reasoning"

                        aria-label={t("agents.reasoning")}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__native_default">
                          {t("nativeDefault")}
                        </SelectItem>
                        {selection.reasoningOptions
                          .filter((option) => option.id)
                          .map((option) => (
                            <SelectItem key={option.id} value={option.id ?? ""}>
                              {option.name || option.id}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
                {selection.fastSupported ? (
                  <div className="grid min-w-0 gap-2">
                    <FieldLabel htmlFor="runtime-default-fast">
                      {t("agents.fast")}
                    </FieldLabel>
                    <Select
                      value={fastValue}
                      disabled={modelDisabled}
                      onValueChange={(value) =>
                        void save({
                          defaultFast:
                            value === "__native_default"
                              ? undefined
                              : value === "on",
                        }).catch(() => undefined)
                      }
                    >
                      <SelectTrigger
                        id="runtime-default-fast"

                        aria-label={t("agents.fast")}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__native_default">
                          {t("nativeDefault")}
                        </SelectItem>
                        <SelectItem value="off">
                          {t("common:executionSelection.fastOff")}
                        </SelectItem>
                        <SelectItem value="on">
                          {t("common:executionSelection.fastOn")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
              </div>
            )}
          </fieldset>
          {selection.catalogIssue ? (
            <p role="status" className="type-body text-content-secondary">
              {runtime.auth === "missing"
                ? t("agents.modelsRequireLogin")
                : (runtime.baseRuntimeId || runtime.id) === "pi" &&
                    selection.catalogIssue === "catalogEmpty"
                  ? t("aiAccounts:systemModelsUnconfigured")
                  : t(`common:executionSelection.${selection.catalogIssue}`)}
            </p>
          ) : selection.error ? (
            <p role="alert" className="type-body text-destructive">
              {t("agents.catalogFailed")}
            </p>
          ) : null}
        </SettingsGroup>
      ) : null}
      {installation.data?.canUninstall || programInstalled ? (
        <RuntimeProgramRemoval
          runtimeId={runtime.id ?? ""}
          name={name}
          installation={installation.data}
          disabled={pending}
        />
      ) : null}
      {update.error ? (
        <p role="alert" className="type-body text-destructive">
          {update.error.message}
        </p>
      ) : null}
    </>
  );
}

export function RuntimeEnvironmentsSection({
  runtimeId,
  onOpenRuntime,
  onBack,
  showSceneChrome = true,
}: {
  runtimeId?: string;
  onOpenRuntime: (runtimeId: string) => void;
  onBack: () => void;
  showSceneChrome?: boolean;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const runtimesQuery = useQuery(
    listRuntimesOptions(withLocalErrorSurface({})),
  );
  const update = useMutation({
    ...withLocalErrorMutation(updateRuntimeEnvironmentMutation()),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: listRuntimesQueryKey() }),
  });
  const refresh = useMutation({
    ...withLocalErrorMutation(refreshRuntimesMutation()),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: listRuntimesQueryKey() }),
  });
  const runtimes = runtimesQuery.data?.resources ?? [];
  const selected = runtimes.find((runtime) => runtime.id === runtimeId);
  const currentDefault = runtimes.find((runtime) => runtime.isDefault);
  const status = (runtime: RuntimeOption) =>
    t(`agents.status.${runtimeHeaderState(runtime).label}`);
  const filtered = runtimes.filter((runtime) =>
    `${runtimeDisplayName(runtime)} ${status(runtime)}`
      .toLowerCase()
      .includes(search.trim().toLowerCase()),
  );
  const row = (runtime: RuntimeOption) => (
    <button
      key={runtime.id}
      type="button"
      aria-label={t("agents.open", { name: runtimeDisplayName(runtime) })}
      onClick={() => onOpenRuntime(runtime.id ?? "")}
      className="before:bg-border-subtle min-h-row-detail hover:bg-action-quiet-hover focus-visible:ring-ring relative flex w-full items-center gap-4 px-4 py-3 text-left outline-none before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden focus-visible:ring-2 focus-visible:ring-inset"
    >
      <RuntimeCliIcon
        runtimeId={runtime.baseRuntimeId || runtime.id}
        size={24}
      />
      <span className="min-w-0 flex-1">
        <span className="type-label text-content-primary block font-medium">
          {runtimeDisplayName(runtime)}
        </span>
        <span className="type-body text-content-secondary block">
          {status(runtime)}
        </span>
      </span>
      {runtime.installed && runtime.auth === "missing" ? (
        <StatusPill tone="attention">{t("needsLogin")}</StatusPill>
      ) : null}
      <ChevronRight
        aria-hidden="true"
        className="size-icon-md text-content-tertiary shrink-0"
      />
    </button>
  );
  return (
    <>
      {showSceneChrome ? (
        <SceneBreadcrumbs
          ariaLabel={t("agents.title")}
          items={
            runtimeId
              ? [
                  { label: t("agents.title"), href: "/settings/agents" },
                  {
                    label: selected
                      ? runtimeDisplayName(selected)
                      : t("agents.detail"),
                  },
                ]
              : [{ label: t("agents.title") }]
          }
        />
      ) : null}
      <SettingsContentShell showHeader={false}>
        {runtimesQuery.isError ? (
          <div role="alert" className="flex flex-wrap items-center gap-2">
            <p className="type-body text-destructive">
              {runtimesQuery.error.message}
            </p>
            <Button
              size="settings"
              variant="settings-outline"
              onClick={() => void runtimesQuery.refetch()}
            >
              {t("common:retry")}
            </Button>
          </div>
        ) : null}
        {runtimeId ? (
          selected ? (
            <RuntimeDetail key={runtimeId} runtime={selected} onBack={onBack} />
          ) : runtimesQuery.isPending ? (
            <RuntimeSkeleton />
          ) : runtimesQuery.isError ? null : (
            <div className="space-y-3">
              <p className="type-body text-content-secondary">
                {t("agents.notFound")}
              </p>
              <Button
                size="settings"
                variant="settings-outline"
                onClick={onBack}
              >
                {t("agents.back")}
              </Button>
            </div>
          )
        ) : (
          <>
            <div className="bg-resource-surface overflow-hidden rounded-lg">
              <SettingsRow
                grouped
                title={t("agents.defaultAgent")}
                description={t("agents.defaultHint")}
              >
                <Select
                  value={currentDefault?.id ?? ""}
                  disabled={update.isPending}
                  onValueChange={(id) => {
                    const runtime = runtimes.find((item) => item.id === id);
                    if (runtime)
                      update.mutate({
                        path: { runtimeId: id },
                        body: {
                          ...runtimeSettingsBody(runtime),
                          isDefault: true,
                        },
                      });
                  }}
                >
                  <SelectTrigger
                    size="settings"
                    appearance="settings"
                    aria-label={t("agents.defaultAgent")}
                  >
                    <SelectValue placeholder={t("agents.chooseAgent")} />
                  </SelectTrigger>
                  <SelectContent>
                    {runtimes
                      .filter(
                        (runtime) =>
                          runtime.isDefault ||
                          (runtime.enabled && runtime.state === "ready"),
                      )
                      .map((runtime) => (
                        <SelectItem key={runtime.id} value={runtime.id ?? ""}>
                          {runtimeDisplayName(runtime)}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
              </SettingsRow>
            </div>
            <SettingsGroup
              title={t("agents.installed")}
              trailing={
                <div className="flex min-w-0 flex-1 items-center justify-end gap-2">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        size="icon"
                        variant="plain"
                        disabled={refresh.isPending}
                        aria-label={t("agents.check")}
                        onClick={() => refresh.mutate({})}
                      >
                        <RefreshCw
                          className={
                            refresh.isPending ? "animate-spin" : undefined
                          }
                        />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("agents.check")}</TooltipContent>
                  </Tooltip>
                  <SearchField
                    width="toolbar"
                    aria-label={t("agents.search")}
                    placeholder={t("agents.search")}
                    clearLabel={t("clearSearch")}
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                  />
                </div>
              }
            >
              {runtimesQuery.isPending ? (
                <RuntimeSkeleton />
              ) : (
                <div className="bg-resource-surface overflow-hidden rounded-lg">
                  {filtered.filter((runtime) => runtime.installed).map(row)}
                  {!filtered.some((runtime) => runtime.installed) ? (
                    <p className="type-body text-content-secondary px-4 py-5">
                      {t(search ? "agents.noMatches" : "agents.noneInstalled")}
                    </p>
                  ) : null}
                </div>
              )}
            </SettingsGroup>
            {filtered.some((runtime) => !runtime.installed) ? (
              <SettingsGroup title={t("agents.other")}>
                <div className="bg-resource-surface overflow-hidden rounded-lg">
                  {filtered.filter((runtime) => !runtime.installed).map(row)}
                </div>
              </SettingsGroup>
            ) : null}
            {update.error || refresh.error ? (
              <p role="alert" className="type-body text-destructive">
                {(update.error ?? refresh.error)?.message}
              </p>
            ) : null}
          </>
        )}
      </SettingsContentShell>
    </>
  );
}
