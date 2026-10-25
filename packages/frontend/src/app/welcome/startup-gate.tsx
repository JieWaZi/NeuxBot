import {
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Check, Copy, ExternalLink } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type {
  ListRuntimesResponse,
  UpdateRuntimeEnvironmentData,
} from "@/shared/api/generated/types.gen";
import {
  refreshRuntimeMutation,
  refreshRuntimesMutation,
  updateRuntimeEnvironmentMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { openExternalURL } from "@/shared/platform";
import { runtimeDisplayVersion } from "@/shared/lib/runtime-display-name";
import {
  permissionLevelOf,
  permissionLevelTranslationKeys,
  modelPermissionLevels,
  safeDefaultPermissionLevel,
  type PermissionLevel,
  runtimeDefaultPermissionLevels,
} from "@/shared/lib/runtime-permissions";
import { Button } from "@/shared/ui/button";
import { ExecutionModelControl } from "@/shared/ui/execution-model-control";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { cliInstallation, installationPlatform } from "./cli-installation";
import { WelcomeBrand } from "./welcome-brand";

type RuntimeOption = NonNullable<ListRuntimesResponse["resources"]>[number];

function runtimeConfiguration(
  runtime: RuntimeOption,
): UpdateRuntimeEnvironmentData["body"] {
  return {
    enabled: runtime.enabled ?? true,
    binary: runtime.binary ?? "",
    prefixArgs: runtime.prefixArgs ?? [],
    configHome: runtime.configHome ?? "",
    defaultAccountId: runtime.defaultAccountId ?? "",
    customModels: runtime.customModels ?? [],
    defaultModel: runtime.defaultModel ?? "",
    defaultReasoning: runtime.defaultReasoning ?? "",
    defaultPermissionLevel: runtime.defaultPermissionLevel ?? "full-access",
  };
}

function modelSelectionReady(runtime?: RuntimeOption) {
  if (!runtime?.installed || runtime.state !== "ready") return false;
  if (
    runtime.diagnostics?.some((item) => item.code === "ACP_OPTIONS_UNAVAILABLE")
  )
    return false;
  const model = runtime.models?.find(
    (item) => item.id && item.id === runtime.defaultModel,
  );
  const permission = permissionLevelOf(runtime.defaultPermissionLevel);
  return Boolean(
    model &&
    permission &&
    runtimeDefaultPermissionLevels(runtime).has(permission),
  );
}

/** 只在本次启动入口作准入判断，进入后后台探测不再打断工作区。 */
export function StartupGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation("common");
  const [entered, setEntered] = useState(false);
  const client = useQueryClient();
  const options = listRuntimesOptions();
  const snapshot = useQuery({ ...options, enabled: !entered });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    enabled: !entered,
  });
  const refresh = useMutation({
    ...refreshRuntimesMutation(),
    onSuccess: () => client.invalidateQueries({ queryKey: options.queryKey }),
  });
  // 准入是入口的一次性选择；进入后后台快照变化不重新触发首次引导。
  if (!entered && snapshot.data?.startupSetupRequired === false) {
    setEntered(true);
    return children;
  }
  if (entered) return children;
  const installed = (snapshot.data?.resources ?? []).filter(
    (runtime) => runtime.installed,
  );
  const error = snapshot.error ?? refresh.error;
  const failed =
    Boolean(error) ||
    (snapshot.data?.startupDetectionFailed && installed.length === 0);
  return (
    <WelcomeBrand intro={false}>
      {failed ? (
        <Card>
          <CardHeader>
            <CardTitle>{t("onboarding.checkFailed")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="type-body text-destructive break-words" role="alert">
              {error?.message ?? t("onboarding.checkFailedHelp")}
            </p>
          </CardContent>
          <CardFooter>
            <Button
              disabled={refresh.isPending || snapshot.isFetching}
              onClick={() => {
                if (snapshot.error) void snapshot.refetch();
                else refresh.mutate({});
              }}
            >
              {t("retry")}
            </Button>
          </CardFooter>
        </Card>
      ) : !snapshot.data ? undefined : (
        <CliSetup
          runtimes={snapshot.data?.resources ?? []}
          installed={installed}
          startupDetectionPending={Boolean(
            snapshot.data.startupDetectionPending,
          )}
          onEnter={() => setEntered(true)}
        />
      )}
    </WelcomeBrand>
  );
}

function CliSetup({
  runtimes,
  installed,
  startupDetectionPending,
  onEnter,
}: {
  runtimes: RuntimeOption[];
  installed: RuntimeOption[];
  startupDetectionPending: boolean;
  onEnter: () => void;
}) {
  const { t } = useTranslation("common");
  const [selected, setSelected] = useState<string>();
  const [selectedVersions, setSelectedVersions] = useState<
    Record<string, string>
  >({});
  const [drafts, setDrafts] = useState<
    Record<
      string,
      { model: string; reasoning: string; permission: PermissionLevel }
    >
  >({});
  const [operationError, setOperationError] = useState<string>();
  const [copied, setCopied] = useState(false);
  const client = useQueryClient();
  const choices = [...runtimes].sort(
    (a, b) =>
      Number(b.baseRuntimeId === "opencode") -
      Number(a.baseRuntimeId === "opencode"),
  );
  const preferred =
    installed.find((item) => item.isDefault) ??
    installed.find((item) => item.baseRuntimeId === "opencode") ??
    installed[0];
  const runtime =
    choices.find((item) => item.id === selected) ??
    choices.find((item) => item.id === preferred?.id) ??
    choices[0];
  // 按 CLI 从 Mutation Cache 读取忙碌状态，切换后返回也不会丢失仍在进行的探测。
  const mutationKey = ["onboarding-runtime", runtime?.id];
  const mutating = useIsMutating({ mutationKey }) > 0;
  const refreshingSelected =
    useIsMutating({ mutationKey: [...mutationKey, "refresh"] }) > 0;
  const refreshing =
    (startupDetectionPending && !runtime?.detectedAt) || refreshingSelected;
  const installations = (runtime?.installations ?? []).filter(
    (item): item is typeof item & { path: string } => Boolean(item.path),
  );
  const hasMultipleVersions = installations.length > 1;
  const currentBinary = runtime?.resolvedBinary || runtime?.binary || "";
  const selectedBinary = hasMultipleVersions
    ? (selectedVersions[runtime?.id ?? ""] ?? "")
    : currentBinary;
  const versionReady =
    !hasMultipleVersions ||
    (selectedBinary === currentBinary &&
      installations.some(
        (item) => item.path === selectedBinary && item.available,
      ));
  // 草稿只保存用户的明确选择，以实际安装路径隔离不同版本。
  const draftKey = `${runtime?.id ?? ""}:${currentBinary}`;
  const draft = drafts[draftKey];
  const configured = runtime
    ? {
        ...runtime,
        defaultModel: draft?.model ?? runtime.defaultModel,
        defaultReasoning: draft?.reasoning ?? runtime.defaultReasoning,
        defaultPermissionLevel:
          draft?.permission ?? runtime.defaultPermissionLevel,
      }
    : undefined;
  const model = runtime?.models?.find(
    (item) => item.id === configured?.defaultModel,
  );
  const permissions = modelPermissionLevels(
    runtime?.models,
    configured?.defaultModel,
  );
  const changeDraft = (patch: Partial<NonNullable<typeof draft>>) => {
    setOperationError(undefined);
    setDrafts((current) => ({
      ...current,
      [draftKey]: {
        model: configured?.defaultModel ?? "",
        reasoning: configured?.defaultReasoning ?? "",
        permission:
          permissionLevelOf(configured?.defaultPermissionLevel) ?? "default",
        ...patch,
      },
    }));
  };
  const platform = installationPlatform();
  const guide = cliInstallation(
    runtime?.baseRuntimeId ?? runtime?.id ?? "",
    platform,
  );
  const save = useMutation({
    ...updateRuntimeEnvironmentMutation(),
    mutationKey: [...mutationKey, "save"],
    onSuccess: async (_data, variables) => {
      await client.invalidateQueries({
        queryKey: listRuntimesOptions().queryKey,
      });
      const snapshot = client.getQueryData<ListRuntimesResponse>(
        listRuntimesOptions().queryKey,
      );
      const confirmed = snapshot?.resources?.find(
        (item) => item.id === variables.path.runtimeId,
      );
      if (
        snapshot?.startupSetupRequired === false &&
        modelSelectionReady(confirmed)
      )
        onEnter();
      else setOperationError(t("onboarding.modelsUnavailable"));
    },
  });
  const switchVersion = useMutation({
    ...updateRuntimeEnvironmentMutation(),
    mutationKey: [...mutationKey, "version"],
    onSuccess: () =>
      client.invalidateQueries({ queryKey: listRuntimesOptions().queryKey }),
    onError: (_error, variables) =>
      setSelectedVersions((current) => ({
        ...current,
        [variables.path.runtimeId]: "",
      })),
  });
  const refresh = useMutation({
    ...refreshRuntimeMutation(),
    mutationKey: [...mutationKey, "refresh"],
    onSuccess: () =>
      client.invalidateQueries({ queryKey: listRuntimesOptions().queryKey }),
  });
  const busy = refreshing || mutating;
  const chooseVersion = (binary: string) => {
    const runtimeID = runtime?.id;
    if (!runtimeID || busy) return;
    switchVersion.reset();
    setOperationError(undefined);
    setSelectedVersions((current) => ({ ...current, [runtimeID]: binary }));
    if (binary === currentBinary) return;
    switchVersion.mutate({
      path: { runtimeId: runtimeID },
      body: { ...runtimeConfiguration(runtime), binary, isDefault: false },
    });
  };
  const finish = () => {
    if (
      !configured?.installed ||
      !configured.id ||
      !selectedBinary ||
      !versionReady ||
      !modelSelectionReady(configured) ||
      busy
    )
      return;
    setOperationError(undefined);
    save.mutate({
      path: { runtimeId: configured.id },
      body: {
        ...runtimeConfiguration(configured),
        enabled: true,
        isDefault: true,
        binary: selectedBinary,
      },
    });
  };
  const copy = async () => {
    if (!guide?.command) return;
    try {
      await navigator.clipboard.writeText(guide.command);
      setCopied(true);
      setOperationError(undefined);
    } catch {
      setOperationError(t("onboarding.copyFailed"));
    }
  };
  const openGuide = async () => {
    if (!guide) return;
    try {
      await openExternalURL(guide.url);
      setOperationError(undefined);
    } catch {
      setOperationError(t("onboarding.openFailed"));
    }
  };
  return (
    <Card variant="floating" className="welcome-setup-card">
      <CardHeader>
        <CardTitle>{t("onboarding.setupTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="welcome-setup-content grid gap-5">
        <p className="type-body text-muted-foreground">
          {t("onboarding.catalogHelp", { count: installed.length })}
        </p>
        <div
          className="welcome-runtime-list"
          role="group"
          aria-label={t("onboarding.cliLabel")}
        >
          {choices.map((item) => (
            <Button
              key={item.id}
              size="option"
              variant="choice"
              aria-pressed={item.id === runtime?.id}
              aria-label={item.name}
              aria-describedby={`onboarding-runtime-state-${item.id}`}
              onClick={() => {
                setSelected(item.id);
                setCopied(false);
                switchVersion.reset();
                save.reset();
                refresh.reset();
                setOperationError(undefined);
              }}
            >
              <RuntimeCliIcon runtimeId={item.baseRuntimeId ?? item.id} />
              <span className="grid min-w-0 gap-0.5">
                <span className="type-body truncate">{item.name}</span>
                <span
                  id={`onboarding-runtime-state-${item.id}`}
                  className={
                    item.installed
                      ? "type-ui text-success"
                      : "type-ui text-muted-foreground"
                  }
                >
                  {t(
                    item.installed
                      ? "onboarding.detected"
                      : startupDetectionPending && !item.detectedAt
                        ? "onboarding.checking"
                        : "onboarding.notInstalled",
                  )}
                  {item.baseRuntimeId === "opencode"
                    ? ` · ${t("onboarding.recommended")}`
                    : ""}
                </span>
              </span>
            </Button>
          ))}
        </div>
        <section className="grid min-w-0 gap-4" aria-label={runtime?.name}>
          {runtime?.installed ? (
            <>
              {hasMultipleVersions ? (
                <div className="grid min-w-0 gap-2">
                  <label
                    htmlFor="onboarding-version"
                    className="type-label font-medium"
                  >
                    {t("onboarding.versionLabel")}
                  </label>
                  <Select
                    value={selectedBinary}
                    onValueChange={chooseVersion}
                    disabled={busy}
                  >
                    <SelectTrigger id="onboarding-version">
                      <SelectValue
                        placeholder={t("onboarding.chooseVersion")}
                      />
                    </SelectTrigger>
                    <SelectContent>
                      {installations.map((item) => (
                        <SelectItem
                          key={item.path}
                          value={item.path}
                          disabled={!item.available}
                        >
                          {runtimeDisplayVersion(item.version) ||
                            t("runtimeEnvironment.status.versionUnknown")}
                          {` · ${item.path}`}
                          {!item.available
                            ? ` · ${t("onboarding.versionUnavailable")}`
                            : ""}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="type-body text-muted-foreground">
                    {t("onboarding.versionHelp")}
                  </p>
                </div>
              ) : (
                <p className="type-body text-muted-foreground break-all">
                  {runtimeDisplayVersion(runtime.version) ||
                    t("runtimeEnvironment.status.versionUnknown")}{" "}
                  · {currentBinary}
                </p>
              )}
              <div className="grid min-w-0 gap-2">
                <label
                  htmlFor="onboarding-model"
                  className="type-label font-medium"
                >
                  {t("executionSelection.model")}
                </label>
                <ExecutionModelControl
                  variant="field"
                  id="onboarding-model"
                  label={t("executionSelection.model")}
                  selectedModelName={
                    !versionReady
                      ? t("onboarding.chooseVersion")
                      : model?.name ||
                        configured?.defaultModel ||
                        t("onboarding.chooseModel")
                  }
                  selectedModel={configured?.defaultModel ?? ""}
                  models={runtime.models ?? []}
                  selectedReasoning={configured?.defaultReasoning ?? ""}
                  reasoningOptions={
                    model?.reasoningOptions ?? runtime.reasoningOptions
                  }
                  defaultReasoning={runtime.defaultReasoning}
                  disabled={busy || !versionReady || !runtime.models?.length}
                  onModelChange={(id) => {
                    const nextModel = runtime.models?.find(
                      (item) => item.id === id,
                    );
                    const reasoning =
                      nextModel?.reasoningOptions ??
                      runtime.reasoningOptions ??
                      [];
                    changeDraft({
                      model: id,
                      reasoning: reasoning.some(
                        (item) => item.id === runtime.defaultReasoning,
                      )
                        ? (runtime.defaultReasoning ?? "")
                        : (reasoning[0]?.id ?? ""),
                      permission: safeDefaultPermissionLevel(
                        permissionLevelOf(configured?.defaultPermissionLevel) ??
                          "default",
                        modelPermissionLevels(runtime.models, id),
                      ),
                    });
                  }}
                  onReasoningChange={(reasoning) => changeDraft({ reasoning })}
                />
                <p
                  role="status"
                  aria-busy={busy}
                  className="type-body text-muted-foreground"
                >
                  {busy
                    ? t("onboarding.checking")
                    : !versionReady
                      ? t("onboarding.chooseVersionBeforeModel")
                      : !modelSelectionReady(configured)
                        ? t("onboarding.modelsUnavailable")
                        : t("onboarding.modelsFound", {
                            count: runtime.models?.length ?? 0,
                          })}
                </p>
              </div>
              {permissions.size > 0 && (
                <div className="grid min-w-0 gap-2">
                  <label
                    htmlFor="onboarding-permission"
                    className="type-label font-medium"
                  >
                    {t("executionSelection.permission")}
                  </label>
                  <Select
                    value={configured?.defaultPermissionLevel}
                    disabled={busy || !versionReady}
                    onValueChange={(value) => {
                      const permission = permissionLevelOf(value);
                      if (permission) changeDraft({ permission });
                    }}
                  >
                    <SelectTrigger id="onboarding-permission">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {[...permissions].map((permission) => (
                        <SelectItem key={permission} value={permission}>
                          {t(permissionLevelTranslationKeys[permission].label)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="type-body text-muted-foreground">
                    {t("onboarding.permissionHelp")}
                  </p>
                </div>
              )}
            </>
          ) : (
            <>
              <p className="type-body text-muted-foreground">
                {t(
                  runtime?.baseRuntimeId === "opencode"
                    ? "onboarding.opencodeHelp"
                    : guide?.node
                      ? "onboarding.nodeHelp"
                      : "onboarding.terminalHelp",
                )}
              </p>
              {guide && (
                <p className="type-body text-muted-foreground">
                  {guide.command
                    ? t(`onboarding.installTerminal.${platform}`)
                    : t("onboarding.downloadHelp")}
                  {guide.command && guide.beta
                    ? ` ${t("onboarding.nativeInstallerBeta")}`
                    : ""}
                </p>
              )}
              {guide?.command && (
                <div className="bg-muted flex min-w-0 items-start gap-2 rounded-md p-3">
                  <pre className="type-body min-w-0 flex-1 font-mono break-all whitespace-pre-wrap">
                    <code>{guide.command}</code>
                  </pre>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={t(
                      copied ? "onboarding.copied" : "onboarding.copy",
                    )}
                    title={t(copied ? "onboarding.copied" : "onboarding.copy")}
                    onClick={() => void copy()}
                  >
                    {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
                  </Button>
                </div>
              )}
            </>
          )}
          {guide && (
            <Button
              asChild
              size="sm"
              variant="ghost"
              className="justify-self-start"
            >
              <a
                href={guide.url}
                onClick={(event) => {
                  event.preventDefault();
                  void openGuide();
                }}
              >
                <ExternalLink aria-hidden />
                <span className="underline underline-offset-2">
                  {t(
                    guide.command || runtime?.baseRuntimeId === "opencode"
                      ? "onboarding.officialGuide"
                      : "onboarding.officialDownload",
                  )}
                </span>
              </a>
            </Button>
          )}
        </section>
        {(operationError ||
          switchVersion.error ||
          save.error ||
          refresh.error) && (
          <p className="type-body text-destructive break-words" role="alert">
            {operationError ??
              switchVersion.error?.message ??
              save.error?.message ??
              refresh.error?.message}
          </p>
        )}
      </CardContent>
      <CardFooter className="shrink-0 flex-wrap justify-end">
        <Button
          variant="secondary"
          disabled={busy || !runtime?.id}
          onClick={() => {
            if (!runtime?.id) return;
            setOperationError(undefined);
            refresh.mutate({ path: { runtimeId: runtime.id } });
          }}
        >
          {t(refreshing ? "onboarding.checking" : "onboarding.recheck")}
        </Button>
        {runtime?.installed && (
          <Button
            disabled={busy || !versionReady || !modelSelectionReady(configured)}
            onClick={finish}
          >
            {t(save.isPending ? "onboarding.saving" : "onboarding.enter")}
          </Button>
        )}
      </CardFooter>
    </Card>
  );
}
