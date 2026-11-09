import {
  ArrowLeft,
  ChevronDown,
  ChevronRight,
  RotateCw,
  Zap,
} from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type {
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeModelOption as RuntimeModelOption,
  GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeReasoningOption as RuntimeReasoningOption,
} from "@/shared/api/generated/types.gen";
import { readLayoutMotionTokens } from "@/shared/lib/ui-motion-tokens";
import { consumeOverlayWheelScroll } from "@/shared/lib/overlay-scroll-wheel";
import { Button } from "@/shared/ui/button";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { RadioGroup, RadioGroupOption } from "@/shared/ui/radio-group";
import { SearchField } from "@/shared/ui/search-field";
import { Slider } from "@/shared/ui/slider";
import { Spinner } from "@/shared/ui/spinner";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { isMaximumReasoning } from "@/shared/lib/runtime-reasoning";

/** 浮层切换后聚焦模型搜索、当前选择或强度控件，保留键盘连续操作。 */
function focusPicker(content: HTMLDivElement | null, showModels: boolean) {
  const target = showModels
    ? content?.querySelector<HTMLElement>("[data-model-search]") ||
      content?.querySelector<HTMLElement>(
        '[role="radio"][data-state="checked"]:not([disabled])',
      ) ||
      content?.querySelector<HTMLElement>('[role="radio"]:not([disabled])')
    : content?.querySelector<HTMLElement>(
        '[role="slider"]:not([data-disabled]):not([aria-disabled="true"])',
      ) ||
      content?.querySelector<HTMLElement>(
        "[data-model-list-trigger]:not([disabled])",
      );
  (
    target ||
    content?.querySelector<HTMLElement>(
      "[data-model-refresh]:not([disabled])",
    ) ||
    content
  )?.focus();
}

/** 聊天与执行配置共用的模型与推理入口，同一浮层在强度面板和模型列表之间切换。 */
export function ExecutionModelControl({
  selectedModelName,
  variant = "composer",
  id,
  label,
  selectedModel,
  models,
  selectedReasoning,
  reasoningOptions,
  thinkingPending = false,
  allowDefaultReasoning = false,
  allowDefaultModel = false,
  disabled = false,
  defaultReasoning = "",
  onModelChange,
  onReasoningChange,
  fast,
  onFastChange,
  onRefresh,
  refreshing = false,
  catalogNotice,
}: {
  selectedModelName: string;
  variant?: "composer" | "field";
  id?: string;
  label?: string;
  selectedModel: string;
  models: RuntimeModelOption[];
  selectedReasoning: string;
  reasoningOptions?: RuntimeReasoningOption[];
  thinkingPending?: boolean;
  allowDefaultReasoning?: boolean;
  allowDefaultModel?: boolean;
  disabled?: boolean;
  defaultReasoning?: string;
  onModelChange: (model: string) => void;
  onReasoningChange: (reasoning: string) => void;
  fast?: boolean;
  onFastChange?: (fast: boolean) => void;
  onRefresh?: () => unknown;
  refreshing?: boolean;
  catalogNotice?: string;
}) {
  const { t } = useTranslation(["chat", "common"]);
  const reducedMotion = useReducedMotion();
  const transition = useMemo(
    () => readLayoutMotionTokens("--duration-content-swap"),
    [],
  );
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<"reasoning" | "models">("reasoning");
  const [modelQuery, setModelQuery] = useState("");
  const contentRef = useRef<HTMLDivElement>(null);
  const availableOptions = (reasoningOptions ?? []).flatMap((option) =>
    option.id ? [{ id: option.id, name: option.name || option.id }] : [],
  );
  const choices = [
    ...(allowDefaultReasoning
      ? [{ id: "", name: t("chat:modelPicker.defaultReasoning") }]
      : []),
    ...availableOptions,
  ];
  const modelChoices = [
    ...(allowDefaultModel
      ? [{ id: "", name: t("common:executionSelection.inheritModel") }]
      : []),
    ...models,
  ];
  // 门槛使用实际模型总数，不受筛选结果或“默认模型”入口影响。
  const showModelSearch = models.length > 10;
  const normalizedQuery = showModelSearch
    ? modelQuery.trim().toLocaleLowerCase()
    : "";
  const visibleModels = normalizedQuery
    ? modelChoices.filter((model) =>
        `${model.name ?? ""} ${model.id ?? ""}`
          .toLocaleLowerCase()
          .includes(normalizedQuery),
      )
    : modelChoices;
  const returnToReasoning = () => {
    setModelQuery("");
    setView("reasoning");
  };
  const hasReasoning = availableOptions.length > 1 || thinkingPending;
  const hasSettings = hasReasoning || Boolean(onFastChange);
  const fallbackReasoning = allowDefaultReasoning
    ? ""
    : availableOptions.find((option) => option.id === defaultReasoning)?.id ||
      availableOptions[0]?.id ||
      "";
  const effectiveReasoning =
    selectedReasoning || (allowDefaultReasoning ? "" : fallbackReasoning);
  const selectedIndex = choices.findIndex(
    (option) => option.id === effectiveReasoning,
  );
  const reasoningName = thinkingPending
    ? t(
        refreshing
          ? "common:executionSelection.loading"
          : "aiAccounts:thinkingLoading",
      )
    : choices[selectedIndex]?.name || selectedReasoning;
  const showModels = view === "models" || !hasSettings;
  const maxed =
    !thinkingPending &&
    isMaximumReasoning(reasoningOptions, effectiveReasoning, defaultReasoning);
  useEffect(() => {
    if (open) focusPicker(contentRef.current, showModels);
  }, [open, showModels, showModelSearch]);

  const triggerName = selectedModelName || t("chat:modelPicker.chooseModel");
  return selectedModelName || onRefresh ? (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        if (nextOpen)
          setView(variant === "field" || !hasSettings ? "models" : "reasoning");
        setModelQuery("");
        setOpen(nextOpen);
      }}
    >
      <PopoverTrigger asChild>
        {variant === "field" ? (
          <FieldControlTrigger
            id={id}
            aria-label={label}
            disabled={disabled && !onRefresh}
            aria-busy={thinkingPending || undefined}
          >
            <span>
              {triggerName}
              {hasReasoning && reasoningName ? ` · ${reasoningName}` : ""}
              {fast === true ? " · Fast" : ""}
            </span>
          </FieldControlTrigger>
        ) : (
          <Button
            type="button"
            disabled={disabled && !onRefresh}
            data-testid="composer-model-control"
            size="sm"
            shape="capsule"
            variant="ghost"
            className="max-w-[220px]"
            data-effort-max={maxed || undefined}
            aria-busy={thinkingPending || undefined}
            title={[
              triggerName,
              hasReasoning && reasoningName,
              fast === true && "Fast",
            ]
              .filter(Boolean)
              .join(" · ")}
          >
            <span className="truncate">{triggerName}</span>
            {thinkingPending ? (
              <Spinner
                className="text-content-secondary size-icon-xs"
                aria-hidden="true"
                role="presentation"
              />
            ) : null}
            {hasReasoning && reasoningName ? (
              <span
                className={
                  maxed
                    ? "text-control-highlight max-w-20 shrink-0 truncate"
                    : "text-content-secondary max-w-20 shrink-0 truncate"
                }
              >
                {reasoningName}
              </span>
            ) : null}
            {fast === true ? (
              <span className="text-control-highlight">Fast</span>
            ) : null}
            <ChevronDown
              aria-hidden
              className="text-content-secondary shrink-0"
            />
          </Button>
        )}
      </PopoverTrigger>
      <PopoverContent
        ref={contentRef}
        side={variant === "field" ? "bottom" : "top"}
        align="start"
        width={variant === "field" ? "trigger" : "default"}
        variant="selection"
        className="flex max-h-[min(320px,var(--radix-popover-content-available-height))] flex-col overflow-hidden"
        aria-label={t(
          showModels
            ? "chat:modelPicker.chooseModel"
            : "chat:modelPicker.chooseReasoning",
        )}
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          focusPicker(contentRef.current, showModels);
        }}
      >
        {onRefresh ? (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  data-model-refresh
                  className="absolute top-3 right-3 z-10"
                  aria-label={t("aiAccounts:refreshModels")}
                  aria-busy={refreshing || undefined}
                  disabled={refreshing}
                  onClick={() => void onRefresh()}
                >
                  {refreshing ? (
                    <Spinner aria-hidden />
                  ) : (
                    <RotateCw aria-hidden />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t("aiAccounts:refreshModels")}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : null}
        <motion.div
          initial={false}
          animate={{
            height: showModels ? "auto" : 0,
            opacity: showModels ? 1 : 0,
          }}
          transition={reducedMotion ? { duration: 0 } : transition}
          className="flex min-h-0 flex-col overflow-hidden"
          aria-hidden={!showModels}
          inert={!showModels}
        >
          <div
            className={
              onRefresh
                ? "min-h-control-md pr-control-md mb-2 flex shrink-0 items-center gap-2"
                : "mb-2 flex shrink-0 items-center gap-2"
            }
          >
            {hasSettings ? (
              <Button
                type="button"
                size="icon"
                variant="ghost"
                aria-label={t("chat:modelPicker.backToReasoning")}
                onClick={returnToReasoning}
              >
                <ArrowLeft aria-hidden />
              </Button>
            ) : null}
            <span className="text-content-secondary type-ui">
              {t("chat:modelPicker.chooseModel")}
            </span>
          </div>
          {showModelSearch ? (
            <SearchField
              data-model-search
              width="full"
              containerClassName="mb-2 shrink-0"
              aria-label={t("aiAccounts:searchModels")}
              placeholder={t("aiAccounts:searchModels")}
              clearLabel={t("aiAccounts:clearSearch")}
              value={modelQuery}
              onChange={(event) => setModelQuery(event.target.value)}
              onKeyDown={(event) => {
                if (
                  event.key === "ArrowDown" &&
                  !event.nativeEvent.isComposing
                ) {
                  const option = contentRef.current?.querySelector<HTMLElement>(
                    '[role="radio"]:not([disabled])',
                  );
                  if (option) {
                    event.preventDefault();
                    option.focus();
                  }
                }
              }}
            />
          ) : null}
          <RadioGroup
            aria-label={t("chat:modelPicker.chooseModel")}
            value={selectedModel}
            className="min-h-0 gap-0 overflow-y-auto overscroll-contain"
            onWheel={consumeOverlayWheelScroll}
            onValueChange={(model) => {
              onModelChange(model);
              returnToReasoning();
              if (variant === "field") setOpen(false);
            }}
          >
            {visibleModels.map((model) => (
              <RadioGroupOption
                key={model.id}
                value={model.id ?? ""}
                disabled={disabled}
                onClick={returnToReasoning}
              >
                <span className="block truncate" title={model.name || model.id}>
                  {model.name || model.id}
                </span>
              </RadioGroupOption>
            ))}
            {!visibleModels.length ? (
              <span className="text-content-secondary type-ui px-2 py-3">
                {t(
                  modelChoices.length
                    ? "aiAccounts:noMatchingModels"
                    : "aiAccounts:noModelsSelected",
                )}
              </span>
            ) : null}
          </RadioGroup>
        </motion.div>
        <motion.div
          initial={false}
          animate={{
            height: showModels ? 0 : "auto",
            opacity: showModels ? 0 : 1,
          }}
          transition={reducedMotion ? { duration: 0 } : transition}
          className="overflow-hidden"
          aria-hidden={showModels}
          inert={showModels}
        >
          <div
            className={
              onFastChange || onRefresh
                ? "min-h-control-md px-control-md relative text-center"
                : "min-w-0 text-center"
            }
          >
            {onFastChange ? (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      className="absolute top-0 left-0"
                      disabled={disabled}
                      aria-label={t("common:executionSelection.fast")}
                      aria-pressed={fast === true}
                      onClick={() => onFastChange(fast !== true)}
                    >
                      <Zap
                        aria-hidden
                        className={
                          fast ? "text-accent fill-current" : undefined
                        }
                      />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    {t("common:executionSelection.fast")}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            ) : null}
            <div
              hidden={!hasReasoning}
              role={thinkingPending ? "status" : undefined}
              className="text-control-highlight type-label flex items-center justify-center gap-2 font-medium"
            >
              {thinkingPending ? (
                <Spinner
                  className="text-control-highlight size-icon-xs"
                  aria-hidden="true"
                  role="presentation"
                />
              ) : null}
              <span className="truncate">
                {reasoningName || t("chat:modelPicker.chooseReasoning")}
              </span>
            </div>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={disabled}
              data-model-list-trigger
              aria-label={t("chat:modelPicker.chooseModel")}
              title={selectedModelName}
              className="max-w-full"
              onClick={() => {
                setModelQuery("");
                setView("models");
              }}
            >
              <span className="text-content-secondary truncate">
                {selectedModelName}
              </span>
              <ChevronRight
                aria-hidden
                className="text-content-secondary shrink-0"
              />
            </Button>
          </div>
          {hasReasoning && choices.length > 1 ? (
            <Slider
              className="mt-2"
              variant="stepped"
              highlighted={maxed}
              marks={choices.map((option) => option.id)}
              min={0}
              max={Math.max(choices.length - 1, 1)}
              step={1}
              value={[Math.max(selectedIndex, 0)]}
              thumbLabel={t("chat:modelPicker.chooseReasoning")}
              thumbValueText={reasoningName}
              disabled={
                disabled ||
                thinkingPending ||
                choices.length < 2 ||
                selectedIndex < 0
              }
              onValueChange={([index]) => {
                const option = index !== undefined ? choices[index] : undefined;
                if (option) onReasoningChange(option.id);
              }}
            />
          ) : null}
        </motion.div>
        {catalogNotice ? (
          <span role="status" className="text-content-secondary type-ui mt-2">
            {catalogNotice}
          </span>
        ) : null}
      </PopoverContent>
    </Popover>
  ) : (
    <Button size="sm" shape="capsule" variant="ghost" disabled>
      {t("aiAccounts:noModelsSelected")}
    </Button>
  );
}
