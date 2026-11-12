import { ChevronDown, CircleAlert, RotateCw, Zap } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type {
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeModelOption as Model,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as Runtime,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as Reasoning,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as Account,
} from "@/shared/api/generated/types.gen";
import { usesNativeModelCatalog } from "@/shared/api/runtime-queries";
import {
  useExecutionSelection,
  type ExecutionSelectionState,
} from "@/shared/hooks/use-execution-selection";
import { accountSupportsRuntime } from "@/shared/lib/account-runtime-compatibility";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { RadioGroup, RadioGroupOption } from "@/shared/ui/radio-group";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { SearchField } from "@/shared/ui/search-field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

const inheritEffort = "__default";
const tokenCount = new Intl.NumberFormat("en", { notation: "compact" });
const effortLabels = {
  none: "none",
  minimal: "minimal",
  low: "low",
  medium: "medium",
  high: "high",
  xhigh: "xhigh",
  max: "max",
  ultra: "ultra",
} as const;

function cursorSettings(provider: string, model?: Model) {
  if (provider !== "cursor") return;
  const suffix = model?.id?.match(/\[([^\]]+)\]$/u);
  if (!suffix?.[1]) return;
  // Cursor uses the entire value as the wire ID. These parameters are display facts.
  const params = new URLSearchParams(suffix[1].replaceAll(",", "&"));
  const context = params.get("context");
  return {
    context:
      context && /^\d+(?:\.\d+)?[km]$/iu.test(context) ? context : undefined,
    effort: params.get("reasoning") || params.get("effort") || undefined,
    fast: params.get("fast") === "true",
  };
}

function modelGroups(models: Model[], provider: string, search: string) {
  const groups = new Map<
    string,
    { label: string; rows: { model: Model; free: boolean }[] }
  >();
  const query = search.trim().toLowerCase();
  for (const model of models) {
    if (!model.id) continue;
    const service =
      provider === "opencode" && model.id.includes("/")
        ? (model.id.split("/", 1)[0] ?? "")
        : "";
    const group = provider === "qoder" ? "" : model.group || service;
    const label =
      provider === "qoder"
        ? ""
        : model.groupName || (group === "opencode" ? "OpenCode" : group);
    if (
      !`${model.name ?? ""} ${model.id} ${label}`.toLowerCase().includes(query)
    )
      continue;
    const free =
      model.free === true ||
      (provider === "opencode" &&
        (model.id === "opencode/big-pickle" ||
          /\bfree$/iu.test(model.name?.trim() ?? "")));
    const entry = groups.get(group) ?? { label, rows: [] };
    entry.rows.push({ model, free });
    groups.set(group, entry);
  }
  return [...groups.entries()]
    .map(([id, group]) => ({
      id,
      ...group,
      rows: group.rows.sort((a, b) => Number(b.free) - Number(a.free)),
    }))
    .sort(
      (a, b) =>
        Number(Boolean(b.rows[0]?.free)) - Number(Boolean(a.rows[0]?.free)),
    );
}

type Props = {
  runtimes: Runtime[];
  accounts: Account[];
  runtime?: Runtime;
  historicalRuntime?: Runtime;
  accountId?: string;
  models: Model[];
  modelId: string;
  modelName: string;
  defaultModel?: string;
  reasoning: string;
  reasoningOptions?: Reasoning[];
  allowDefaultReasoning: boolean;
  fast?: boolean;
  fastSupported?: boolean;
  catalogState?: string;
  catalogIssue?: ExecutionSelectionState["catalogIssue"];
  pending?: boolean;
  refreshing?: boolean;
  sessionSwitchPending?: boolean;
  onRefresh?: () => unknown;
  onModel: (runtime: Runtime, accountId: string, model: Model) => void;
  onReasoning: (reasoning: string) => void;
  onFast: (fast: boolean) => void;
  contextWindow?: string;
  onContextWindow?: (contextWindow: string) => void;
};

/** 聊天选择器用临时浏览状态预览目录，选中模型后一次提交执行配置。 */
export function ChatExecutionPicker(props: Props) {
  const { t, i18n } = useTranslation(["chat", "common", "aiAccounts"]);
  const creditMultiplier = new Intl.NumberFormat(i18n.resolvedLanguage, {
    maximumFractionDigits: 4,
  });
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState<{
    runtimeId: string;
    accountId: string;
  }>();
  const [search, setSearch] = useState("");
  const runtimes = props.runtimes.filter((runtime) => runtime.id);
  const runtime =
    runtimes.find((item) => item.id === preview?.runtimeId) ??
    props.runtime ??
    runtimes.find((item) => item.enabled);
  const provider = runtime?.baseRuntimeId || runtime?.id || "";
  const compatible = props.accounts.filter(
    (account) =>
      account.id &&
      account.runtimeId === runtime?.id &&
      accountSupportsRuntime(account, provider),
  );
  const accountId = preview?.accountId || props.accountId || "system";
  const account = compatible.find((item) => item.id === accountId);
  const accountMissing = accountId !== "system" && !account;
  const browsing = Boolean(
    runtime &&
    (runtime.id !== props.runtime?.id ||
      accountId !== (props.accountId || "system")),
  );
  const directory = useExecutionSelection({
    runtime,
    value: { runtimeId: runtime?.id, accountId },
    enabled: open && browsing,
  });
  const models = browsing ? directory.models : props.models;
  const defaultModel =
    (browsing ? directory.defaultModel : props.defaultModel) ||
    (accountId === (runtime?.defaultAccountId || "system")
      ? runtime?.defaultModel
      : account?.defaultModel);
  const modelId = browsing ? "" : props.modelId;
  const selected = models.find((model) => model.id === modelId);
  const reasoningOptions = browsing ? [] : (props.reasoningOptions ?? []);
  const native = usesNativeModelCatalog(provider);
  const catalogState = browsing ? directory.catalogState : props.catalogState;
  const catalogIssue = browsing ? directory.catalogIssue : props.catalogIssue;
  const catalogRefreshFailed = catalogState === "stale";
  const inlineCatalogIssue =
    catalogIssue === "catalogStale" ? undefined : catalogIssue;
  const pending = browsing
    ? native
      ? directory.catalogPending
      : directory.loading
    : props.pending;
  const refreshing = browsing ? directory.catalogRefreshing : props.refreshing;
  const refresh = native
    ? browsing
      ? directory.refreshCatalog
      : props.onRefresh
    : undefined;
  const settingsDisabled = Boolean(props.sessionSwitchPending);
  const modelsDisabled = settingsDisabled;
  const fixed = cursorSettings(provider, selected);
  const contextWindows = (selected?.contextWindowOptions ?? []).flatMap(
    ({ id, tokens }) => (id && tokens ? [{ id, tokens }] : []),
  );
  const hasEffort = reasoningOptions.filter((option) => option.id).length > 1;
  const showReasoning = !browsing && (hasEffort || Boolean(fixed?.effort));
  const showContextWindow =
    Boolean(props.onContextWindow) && contextWindows.length > 1;
  const nativeDefaultEffort = reasoningOptions.find(
    (option) => option.id === "default",
  )?.id;
  const fallbackEffort =
    nativeDefaultEffort ??
    (props.allowDefaultReasoning ? inheritEffort : reasoningOptions[0]?.id);
  const selectedEffort = reasoningOptions.find(
    (option) => option.id === props.reasoning,
  );
  const triggerRuntime = props.runtime ?? props.historicalRuntime;
  const triggerName =
    props.modelName ||
    runtimeDisplayName(triggerRuntime) ||
    t("chat:runtimeUnavailable");
  const triggerAccount = props.accounts.find(
    (item) =>
      item.id === props.accountId && item.runtimeId === props.runtime?.id,
  );
  const groups = modelGroups(models, provider, search);
  const chooseModel = t("chat:modelPicker.chooseModel");
  const runtimeName = runtimeDisplayName(runtime);
  const refreshLabel = t("aiAccounts:refreshModels");
  const refreshFailureLabel = catalogRefreshFailed
    ? t("common:executionSelection.catalogStale")
    : undefined;

  const effortName = (value: string, name?: string) => {
    if (value === "default") return t("chat:modelPicker.defaultReasoning");
    const label = Object.entries(effortLabels).find(
      ([id]) => id === value,
    )?.[1];
    return label ? t(`chat:modelPicker.efforts.${label}`) : name || value;
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        setPreview(undefined);
        setSearch("");
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          data-testid="composer-model-control"
          size="sm"
          variant="ghost"
          shape="capsule"
          className="max-w-65"
        >
          <RuntimeCliIcon
            runtimeId={triggerRuntime?.baseRuntimeId || triggerRuntime?.id}
            size={14}
          />
          <span className="truncate">{triggerName}</span>
          {triggerAccount ? (
            <span className="text-content-secondary truncate">
              {triggerAccount.name}
            </span>
          ) : null}
          {selectedEffort?.id && showReasoning ? (
            <span className="text-content-secondary truncate">
              {effortName(selectedEffort.id, selectedEffort.name)}
            </span>
          ) : null}
          {props.fast ? (
            <Zap aria-hidden className="text-accent fill-current" />
          ) : null}
          <ChevronDown
            aria-hidden
            className="text-content-secondary shrink-0"
          />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        width="wide"
        variant="selection"
        aria-label={chooseModel}
        onInteractOutside={(event) => {
          if (props.sessionSwitchPending) event.preventDefault();
        }}
        className="flex h-[min(28rem,var(--radix-popover-content-available-height))] min-h-0 overflow-hidden p-0"
      >
        <TooltipProvider>
          <div
            role="group"
            aria-label={t("chat:runtimeSection")}
            className="bg-muted/50 flex shrink-0 flex-col gap-1 overflow-y-auto p-2"
          >
            {runtimes.map((item) => {
              const name = runtimeDisplayName(item);
              return (
                <Tooltip key={item.id}>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      variant="choice"
                      size="icon"
                      disabled={!item.enabled}
                      aria-label={name}
                      aria-pressed={runtime?.id === item.id}
                      onClick={() => {
                        if (runtime?.id === item.id) return;
                        setPreview({
                          runtimeId: item.id ?? "",
                          accountId:
                            item.id === props.runtime?.id
                              ? props.accountId || "system"
                              : item.defaultAccountId || "system",
                        });
                        setSearch("");
                      }}
                    >
                      <RuntimeCliIcon
                        runtimeId={item.baseRuntimeId || item.id}
                      />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="left">{name}</TooltipContent>
                </Tooltip>
              );
            })}
          </div>
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex shrink-0 flex-col gap-2 p-3">
              <div className="flex items-center gap-1">
                <div className="min-w-0 flex-1">
                  <div className="type-body truncate font-semibold">
                    {runtimeName || t("chat:runtimeUnavailable")}
                  </div>
                  {runtime?.version ? (
                    <div className="text-content-secondary type-caption truncate">
                      {runtime.version}
                    </div>
                  ) : null}
                </div>
                {props.fastSupported && !browsing ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        disabled={settingsDisabled}
                        aria-label={t("common:executionSelection.fast")}
                        aria-pressed={props.fast === true}
                        onClick={() => props.onFast(!props.fast)}
                      >
                        <Zap
                          aria-hidden
                          className={
                            props.fast ? "text-accent fill-current" : undefined
                          }
                        />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {t("common:executionSelection.fast")}
                    </TooltipContent>
                  </Tooltip>
                ) : null}
                {refreshFailureLabel ? (
                  <span
                    className="text-warning shrink-0"
                    role="status"
                    title={refreshFailureLabel}
                  >
                    <CircleAlert
                      aria-hidden
                      className="size-icon-sm"
                      strokeWidth={1.5}
                    />
                    <span className="sr-only">{refreshFailureLabel}</span>
                  </span>
                ) : null}
                {refresh ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        disabled={pending || refreshing}
                        aria-busy={pending || refreshing || undefined}
                        aria-label={refreshLabel}
                        onClick={() => void refresh()}
                      >
                        <RotateCw
                          aria-hidden
                          className={refreshing ? "animate-spin" : undefined}
                        />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{refreshLabel}</TooltipContent>
                  </Tooltip>
                ) : null}
              </div>
              {compatible.length || accountMissing ? (
                <Select
                  value={accountId}
                  onValueChange={(value) => {
                    setPreview({
                      runtimeId: runtime?.id ?? "",
                      accountId: value,
                    });
                    setSearch("");
                  }}
                >
                  <SelectTrigger
                    size="sm"
                    aria-label={t("common:executionSelection.account")}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {accountMissing ? (
                      <SelectItem value={accountId} disabled>
                        {accountId} ·{" "}
                        {t("common:executionSelection.unavailable")}
                      </SelectItem>
                    ) : null}
                    <SelectItem value="system">
                      {t("aiAccounts:system")}
                    </SelectItem>
                    {compatible.map((item) => (
                      <SelectItem key={item.id} value={item.id ?? ""}>
                        {item.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
              <SearchField
                width="full"
                aria-label={t("aiAccounts:searchModels")}
                placeholder={t("aiAccounts:searchModels")}
                clearLabel={t("aiAccounts:clearSearch")}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
            <div
              className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-2"
              aria-busy={pending || undefined}
            >
              <RadioGroup
                aria-label={chooseModel}
                value={modelId}
                className="gap-0"
                onValueChange={(id) => {
                  const model = models.find((item) => item.id === id);
                  if (!runtime || !model) return;
                  props.onModel(runtime, accountId, model);
                }}
              >
                {groups.map(({ id, label, rows }) => (
                  <div key={id} role="group" aria-label={label || undefined}>
                    {label ? (
                      <div className="text-content-secondary type-ui px-2 pt-2 pb-1">
                        {label}
                      </div>
                    ) : null}
                    {rows.map(({ model, free }) => {
                      const encoded = cursorSettings(provider, model);
                      const size =
                        (model.id === modelId &&
                          contextWindows.find(
                            ({ id }) => id === props.contextWindow,
                          )?.tokens) ||
                        model.contextWindowTokens ||
                        account?.modelContextSizes?.[model.id ?? ""];
                      const context =
                        size && size > 0
                          ? tokenCount.format(size)
                          : encoded?.context;
                      const maximum = model.maxContextWindowTokens;
                      const contextLabel =
                        maximum && maximum !== size
                          ? [context, tokenCount.format(maximum)]
                              .filter(Boolean)
                              .join(" / ")
                          : context;
                      const effort = model.reasoningOptions?.length
                        ? model.defaultReasoning
                        : encoded?.effort;
                      const nativeEffortName = model.reasoningOptions?.find(
                        (option) => option.id === effort,
                      )?.name;
                      const detail = [
                        contextLabel,
                        effort &&
                          effort !== "default" &&
                          effortName(effort, nativeEffortName),
                        encoded?.fast && t("common:executionSelection.fast"),
                      ]
                        .filter(Boolean)
                        .join(" · ");
                      const name = model.name || model.id;
                      const creditLabel =
                        model.creditMultiplier != null
                          ? t("chat:modelPicker.creditMultiplier", {
                              value: creditMultiplier.format(
                                model.creditMultiplier,
                              ),
                            })
                          : undefined;
                      return (
                        <RadioGroupOption
                          key={model.id}
                          value={model.id ?? ""}
                          aria-label={name}
                          aria-description={creditLabel}
                          leadingIndicator
                          title={
                            maximum
                              ? t("chat:modelPicker.maximumContext", {
                                  size: tokenCount.format(maximum),
                                })
                              : undefined
                          }
                          disabled={modelsDisabled}
                        >
                          <span className="flex min-w-0 items-center gap-1.5">
                            <span className="min-w-0 flex-1 truncate">
                              {name}
                            </span>
                            {detail ? (
                              <span className="text-content-secondary type-caption max-w-1/2 shrink-0 truncate">
                                {detail}
                              </span>
                            ) : null}
                            {free ? (
                              <span className="bg-muted text-content-secondary type-caption shrink-0 rounded-full px-1.5">
                                {t("chat:modelPicker.free")}
                              </span>
                            ) : null}
                            {model.id === defaultModel ? (
                              <span className="text-content-secondary type-caption shrink-0">
                                {t("chat:modelPicker.defaultReasoning")}
                              </span>
                            ) : null}
                            {creditLabel ? (
                              <span className="text-content-secondary type-caption shrink-0 tabular-nums">
                                {creditLabel}
                              </span>
                            ) : null}
                          </span>
                        </RadioGroupOption>
                      );
                    })}
                  </div>
                ))}
              </RadioGroup>
              {pending ||
              inlineCatalogIssue ||
              !models.length ||
              !groups.length ? (
                <div
                  className="text-content-secondary type-ui px-2 py-2"
                  role="status"
                >
                  {pending
                    ? t("common:executionSelection.loading")
                    : inlineCatalogIssue
                      ? t(`common:executionSelection.${inlineCatalogIssue}`)
                      : models.length
                        ? t("aiAccounts:noMatchingModels")
                        : t("aiAccounts:noModelsSelected")}
                </div>
              ) : null}
            </div>
            {showReasoning || showContextWindow ? (
              <div className="border-border-default flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-t px-3 py-2">
                {showReasoning ? (
                  <div className="flex min-w-0 flex-auto items-center justify-between gap-2">
                    <span className="text-content-secondary type-ui shrink-0 whitespace-nowrap">
                      {t("chat:modelPicker.reasoning")}
                    </span>
                    {hasEffort ? (
                      <Select
                        value={selectedEffort?.id || fallbackEffort}
                        onValueChange={(value) =>
                          props.onReasoning(
                            value === inheritEffort ? "" : value,
                          )
                        }
                      >
                        <SelectTrigger
                          size="sm"
                          className="max-w-40 min-w-0"
                          disabled={settingsDisabled}
                          aria-label={t("chat:modelPicker.chooseReasoning")}
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {fallbackEffort === inheritEffort ? (
                            <SelectItem value={inheritEffort}>
                              {t("chat:modelPicker.defaultReasoning")}
                            </SelectItem>
                          ) : null}
                          {reasoningOptions.flatMap((option) =>
                            option.id
                              ? [
                                  <SelectItem key={option.id} value={option.id}>
                                    {effortName(option.id, option.name)}
                                  </SelectItem>,
                                ]
                              : [],
                          )}
                        </SelectContent>
                      </Select>
                    ) : (
                      <span className="text-content-secondary type-ui text-right">
                        {t("chat:modelPicker.fixedReasoning", {
                          effort: effortName(fixed?.effort ?? ""),
                          runtime: runtimeName,
                        })}
                      </span>
                    )}
                  </div>
                ) : null}
                {showContextWindow ? (
                  <div className="flex min-w-0 flex-auto items-center justify-between gap-2">
                    <span className="text-content-secondary type-ui shrink-0 whitespace-nowrap">
                      {t("chat:modelPicker.contextWindow")}
                    </span>
                    <Select
                      value={
                        props.contextWindow ||
                        selected?.defaultContextWindow ||
                        ""
                      }
                      onValueChange={props.onContextWindow}
                    >
                      <SelectTrigger
                        size="sm"
                        className="max-w-40 min-w-0"
                        disabled={settingsDisabled}
                        aria-label={t("chat:modelPicker.contextWindow")}
                      >
                        <SelectValue
                          placeholder={t("chat:modelPicker.defaultReasoning")}
                        />
                      </SelectTrigger>
                      <SelectContent>
                        {contextWindows.map((option) => (
                          <SelectItem key={option.id} value={option.id}>
                            {tokenCount.format(option.tokens)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        </TooltipProvider>
      </PopoverContent>
    </Popover>
  );
}
