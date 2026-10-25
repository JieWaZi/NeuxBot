import { useId } from "react";
import { Textarea } from "@/shared/ui/textarea";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Checkbox } from "@/shared/ui/checkbox";
import { Card } from "@/shared/ui/card";
import { toast } from "@/shared/ui/sonner";
import { BookOpen, Search } from "lucide-react";
import type {
  ExtractionGranularity,
  LibraryIndexingStrategy,
  LibraryWikiSettings,
} from "./knowledge-model";
import { useCopy } from "./use-extended-copy";
import { Field, SectionTitle } from "./knowledge-controls";

export function GeneralLibraryConfigFields({
  disabled,
  wiki,
  onWikiChange,
  indexing,
  onIndexingChange,
  indexingDisabled = false,
}: {
  disabled?: boolean;
  wiki: LibraryWikiSettings;
  onWikiChange: (value: LibraryWikiSettings) => void;
  indexing: LibraryIndexingStrategy;
  onIndexingChange: (value: LibraryIndexingStrategy) => void;
  indexingDisabled?: boolean;
}) {
  const t = useCopy();
  const ragId = useId();
  const wikiId = useId();
  const ragEnabled = indexing.vectorEnabled || indexing.keywordEnabled;
  const changeStrategy = (next: LibraryIndexingStrategy) => {
    if (!next.wikiEnabled && !next.vectorEnabled && !next.keywordEnabled) {
      toast.error(t("atLeastOneIndex"));
      return;
    }
    onIndexingChange(next);
  };
  return (
    <>
      <section className="@container/indexing grid gap-3">
        <SectionTitle title={t("indexingStrategy")} />
        <div className="grid grid-cols-1 gap-3 @min-[24rem]/indexing:grid-cols-2">
          <Card
            asChild
            variant="resource"
            data-checked={ragEnabled}
            data-disabled={Boolean(disabled || indexingDisabled)}
            className="data-[checked=true]:bg-accent-surface data-[disabled=true]:hover:bg-card data-[disabled=true]:data-[checked=true]:bg-accent-surface-subtle data-[disabled=true]:data-[checked=true]:hover:bg-accent-surface-subtle data-[disabled=true]:cursor-not-allowed"
          >
            <label htmlFor={ragId} className="grid cursor-pointer gap-2 p-4">
              <span className="flex items-center gap-2">
                <Search className="size-icon-md shrink-0" />
                <span className="type-body flex-1 font-medium">
                  {t("ragIndexing")}
                </span>
                <Checkbox
                  id={ragId}
                  aria-label={t("ragIndexing")}
                  checked={ragEnabled}
                  disabled={disabled || indexingDisabled}
                  onCheckedChange={(checked) =>
                    changeStrategy({
                      ...indexing,
                      vectorEnabled: Boolean(checked),
                      keywordEnabled: Boolean(checked),
                    })
                  }
                />
              </span>
              <span className="type-caption text-content-secondary">
                {t("ragIndexingHelp")}
              </span>
              {indexing.keywordEnabled && !indexing.vectorEnabled && (
                <span className="type-caption text-content-secondary">
                  {t("keywordOnlyActive")}
                </span>
              )}
            </label>
          </Card>
          <Card
            asChild
            variant="resource"
            data-checked={indexing.wikiEnabled}
            data-disabled={Boolean(disabled || indexingDisabled)}
            className="data-[checked=true]:bg-accent-surface data-[disabled=true]:hover:bg-card data-[disabled=true]:data-[checked=true]:bg-accent-surface-subtle data-[disabled=true]:data-[checked=true]:hover:bg-accent-surface-subtle data-[disabled=true]:cursor-not-allowed"
          >
            <label htmlFor={wikiId} className="grid cursor-pointer gap-2 p-4">
              <span className="flex items-center gap-2">
                <BookOpen className="size-icon-md shrink-0" />
                <span className="type-body flex-1 font-medium">
                  {t("wikiIndexing")}
                </span>
                <Checkbox
                  id={wikiId}
                  aria-label={t("wikiIndexing")}
                  checked={indexing.wikiEnabled}
                  disabled={disabled || indexingDisabled}
                  onCheckedChange={(checked) =>
                    changeStrategy({
                      ...indexing,
                      wikiEnabled: Boolean(checked),
                    })
                  }
                />
              </span>
              <span className="type-caption text-content-secondary">
                {t("wikiIndexingHelp")}
              </span>
            </label>
          </Card>
        </div>
        {indexing.vectorEnabled && (
          <p className="type-caption text-content-secondary">
            {t("ragEmbeddingRequired")}
          </p>
        )}
      </section>
      {indexing.wikiEnabled && (
        <section className="grid gap-4">
          <SectionTitle title={t("wikiTuning")} />
          <Field
            label={t("extractionGranularity")}
            hint={t("extractionGranularityHelp")}
          >
            <SegmentedControl
              ariaLabel={t("extractionGranularity")}
              disabled={disabled}
              value={wiki.extractionGranularity}
              onValueChange={(extractionGranularity: ExtractionGranularity) =>
                onWikiChange({ ...wiki, extractionGranularity })
              }
              options={[
                { value: "focused", label: t("granularityFocused") },
                { value: "standard", label: t("granularityStandard") },
                { value: "exhaustive", label: t("granularityExhaustive") },
              ]}
            />
          </Field>
          <Field
            label={t("contentInstructions")}
            hint={t("contentInstructionsHelp")}
          >
            <Textarea
              aria-label={t("contentInstructions")}
              placeholder={t("contentInstructionsPlaceholder")}
              disabled={disabled}
              maxLength={4000}
              value={wiki.contentInstructions}
              onChange={(event) =>
                onWikiChange({
                  ...wiki,
                  contentInstructions: event.target.value,
                })
              }
            />
          </Field>
          <Field
            label={t("extractionInstructions")}
            hint={t("extractionInstructionsHelp")}
          >
            <Textarea
              aria-label={t("extractionInstructions")}
              placeholder={t("extractionInstructionsPlaceholder")}
              disabled={disabled}
              maxLength={4000}
              value={wiki.extractionInstructions}
              onChange={(event) =>
                onWikiChange({
                  ...wiki,
                  extractionInstructions: event.target.value,
                })
              }
            />
          </Field>
        </section>
      )}
    </>
  );
}
