import { useId, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Input } from "@/shared/ui/input";
import { Slider } from "@/shared/ui/slider";
import { Switch } from "@/shared/ui/switch";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import type { LibraryChunkingConfig } from "./knowledge-model";
import { Choice, SectionTitle } from "./knowledge-controls";
import { useCopy } from "./use-extended-copy";

/** 迁移固定 WeKnora KBChunkingSettings 的字段顺序、范围、步长与条件展开。 */
export function KnowledgeChunkingFields({
  value,
  onChange,
  disabled,
}: {
  value: LibraryChunkingConfig;
  onChange: (value: LibraryChunkingConfig) => void;
  disabled?: boolean;
}) {
  const t = useCopy();
  const parentId = useId();
  const row = (
    label: string,
    hint: string,
    control: ReactNode,
    warning?: ReactNode,
  ) => (
    <div className="grid min-w-0 items-start gap-3 py-4 @min-[32rem]/chunking:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] @min-[32rem]/chunking:gap-6">
      <div className="min-w-0">
        <p className="type-body font-medium">{label}</p>
        <p className="type-caption text-content-secondary mt-1">{hint}</p>
        {warning}
      </div>
      <div className="min-w-0">{control}</div>
    </div>
  );
  const slider = (
    key:
      "chunk_size" | "chunk_overlap" | "parent_chunk_size" | "child_chunk_size",
    label: string,
    hint: string,
    min: number,
    max: number,
    step: number,
    marks: number[],
    warning?: ReactNode,
  ) =>
    row(
      label,
      hint,
      <div className="flex min-w-0 items-start gap-4">
        <div className="min-w-0 flex-1">
          <Slider
            thumbLabel={label}
            thumbValueText={t("chunkCharacters", { count: value[key] })}
            min={min}
            max={max}
            step={step}
            value={[value[key]]}
            disabled={disabled}
            onValueChange={([next]) => {
              if (next !== undefined) onChange({ ...value, [key]: next });
            }}
          />
          <div
            aria-hidden
            className="type-caption text-content-secondary relative mx-2 h-4"
          >
            {marks.map((mark) => (
              <span
                key={mark}
                className="absolute -translate-x-1/2 tabular-nums"
                style={{ left: `${((mark - min) / (max - min)) * 100}%` }}
              >
                {mark}
              </span>
            ))}
          </div>
        </div>
        <output className="type-body min-w-20 shrink-0 pt-2 text-right font-medium tabular-nums">
          {t("chunkCharacters", { count: value[key] })}
        </output>
      </div>,
      warning,
    );
  const standard = [
    { value: "\n\n", label: t("separatorDoubleNewline") },
    { value: "\n", label: t("separatorNewline") },
    ...["。", "！", "？", "；", ";"].map((symbol) => ({
      value: symbol,
      label: symbol,
    })),
    { value: " ", label: t("separatorSpace") },
  ];
  const options = [
    ...standard,
    ...value.separators
      .filter((item) => !standard.some((option) => option.value === item))
      .map((item) => ({ value: item, label: item })),
  ];
  const labels = {
    searchLabel: t("searchOptions"),
    emptyLabel: t("noResults"),
    clearSearchLabel: t("clearSearch"),
    clearSelectionLabel: t("clearSelection"),
  };
  return (
    <section className="@container/chunking min-w-0">
      <SectionTitle title={t("chunkingSettings")} />
      {row(
        t("chunkingStrategy"),
        t("chunkingStrategyHelp"),
        <div className="grid gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Choice
              disabled={disabled}
              label={t("chunkingStrategy")}
              value={value.strategy}
              onChange={(strategy) => {
                if (
                  strategy === "auto" ||
                  strategy === "heading" ||
                  strategy === "heuristic" ||
                  strategy === "legacy"
                )
                  onChange({ ...value, strategy });
              }}
              options={[
                { value: "auto", label: t("chunkingAuto") },
                { value: "heading", label: t("chunkingHeading") },
                { value: "heuristic", label: t("chunkingHeuristic") },
                { value: "legacy", label: t("chunkingLegacy") },
              ]}
            />
          </div>
        </div>,
      )}
      {slider(
        "chunk_size",
        t("chunkSize"),
        t("chunkSizeHelp"),
        100,
        4000,
        50,
        [100, 1000, 2000, 4000],
      )}
      {slider(
        "chunk_overlap",
        t("chunkOverlap"),
        t("chunkOverlapHelp"),
        0,
        500,
        20,
        [0, 250, 500],
        value.chunk_overlap > 0 &&
          value.chunk_overlap >= value.chunk_size / 2 ? (
          <p role="status" className="type-caption text-warning mt-1">
            {t("chunkOverlapWarning")}
          </p>
        ) : undefined,
      )}
      {row(
        t("chunkSeparators"),
        t("chunkSeparatorsHelp"),
        <fieldset disabled={disabled} className="min-w-0 border-0 p-0">
          <SearchableMultiSelect
            size="md"
            triggerLabel={t("chunkSeparatorsSelected", {
              count: value.separators.length,
            })}
            options={options}
            value={value.separators}
            onValueChange={(separators) => onChange({ ...value, separators })}
            createLabel={t("addSeparator")}
            {...labels}
          />
        </fieldset>,
      )}
      {row(
        t("parentChildChunking"),
        t("parentChildChunkingHelp"),
        <div className="flex justify-end">
          <Switch
            id={parentId}
            aria-label={t("parentChildChunking")}
            hitArea="comfortable"
            checked={value.enable_parent_child}
            disabled={disabled}
            onClick={() =>
              onChange({
                ...value,
                enable_parent_child: !value.enable_parent_child,
              })
            }
          />
        </div>,
      )}
      {value.enable_parent_child && (
        <>
          {slider(
            "parent_chunk_size",
            t("parentChunkSize"),
            t("parentChunkSizeHelp"),
            512,
            8192,
            64,
            [512, 2048, 4096, 8192],
          )}
          {slider(
            "child_chunk_size",
            t("childChunkSize"),
            t("childChunkSizeHelp"),
            64,
            2048,
            32,
            [64, 384, 1024, 2048],
          )}
        </>
      )}
      <Collapsible>
        <CollapsibleTrigger className="group type-body text-content-secondary hover:text-content-primary focus-visible:ring-ring min-h-control-md mt-3 flex w-full items-center gap-2 text-left outline-none focus-visible:ring-2">
          <ChevronRight
            aria-hidden="true"
            className="size-icon-sm transition-transform group-data-[state=open]:rotate-90 motion-reduce:transition-none"
          />
          {t("advancedChunking")}
        </CollapsibleTrigger>
        <CollapsibleContent>
          {row(
            t("chunkTokenLimit"),
            t("chunkTokenLimitHelp"),
            <Input
              type="number"
              aria-label={t("chunkTokenLimit")}
              placeholder="0"
              min={0}
              max={8192}
              step={64}
              disabled={disabled || value.strategy === "legacy"}
              value={value.token_limit}
              onChange={(event) =>
                onChange({ ...value, token_limit: Number(event.target.value) })
              }
            />,
          )}
          {row(
            t("chunkLanguages"),
            t("chunkLanguagesHelp"),
            <fieldset
              disabled={disabled || value.strategy === "legacy"}
              className="min-w-0 border-0 p-0"
            >
              <SearchableMultiSelect
                size="md"
                triggerLabel={
                  value.languages.length
                    ? t("chunkLanguagesSelected", {
                        count: value.languages.length,
                      })
                    : t("chunkLanguagesAuto")
                }
                options={[
                  { value: "de", label: t("languageGerman") },
                  { value: "en", label: t("languageEnglish") },
                  { value: "zh", label: t("languageChinese") },
                ]}
                value={value.languages}
                onValueChange={(languages) => onChange({ ...value, languages })}
                {...labels}
              />
            </fieldset>,
          )}
        </CollapsibleContent>
      </Collapsible>
    </section>
  );
}
