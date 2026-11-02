import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import type { GetLibraryExecutionDefaultsResponse } from "@/shared/api/generated/types.gen";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { KnowledgeModelConfigurationFields } from "./knowledge-model-configuration-fields";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { Field } from "./knowledge-controls";
import { organizableSourcesOptions } from "./knowledge-queries";
import type { Source } from "./knowledge-model";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import type { KnowledgeOrganizeDialogProps as Props } from "./knowledge-organize-dialog";
import {
  libraryExecutionDefaultsOptions,
  readExecutionDefaults,
} from "./knowledge-execution-defaults";
import {
  createKnowledgeOrganizeIntent,
  createKnowledgeOrganizeStart,
} from "./knowledge-organize-start";
import { useCopy } from "./use-extended-copy";
import { useDocumentCopy } from "./use-document-copy";
import { useWikiCopy } from "./use-wiki-copy";
import {
  readOrganizePreferences,
  saveOrganizePreferences,
} from "./knowledge-organize-preferences";

export function KnowledgeOrganizeDialogContent(props: Props) {
  const t = useCopy();
  const defaults = useQuery(libraryExecutionDefaultsOptions(props.library.ref));
  const retainedDraft = props.intent?.get().draft;
  const sources = useQuery({
    ...organizableSourcesOptions(props.library.ref),
    enabled: !props.maintenance && !props.rebuild && !retainedDraft,
  });
  const available = props.maintenance
    ? []
    : props.rebuild
      ? props.library.sources.filter((source) =>
          props.batch.includes(source.id),
        )
      : (retainedDraft?.sources ??
        (!sources.isFetching && !sources.error ? sources.data : undefined));
  if (!defaults.data || !available)
    return (
      <>
        {(defaults.isPending && !defaults.data) ||
        ((sources.isPending || sources.isFetching) && !available) ? (
          <div className="px-6 pb-5">
            <KnowledgeWikiContentLoading />
          </div>
        ) : null}
        <DialogFooter>
          <Button variant="secondary" onClick={props.onClose}>
            {t("close")}
          </Button>
        </DialogFooter>
      </>
    );
  return (
    <LoadedOrganizeDialog
      {...props}
      defaults={defaults.data}
      sources={available}
    />
  );
}
function LoadedOrganizeDialog({
  library,
  batch,
  operation,
  onClose,
  onAccepted,
  defaults,
  intent: retained,
  rebuild = false,
  maintenance = false,
  sources,
}: Props & {
  defaults: GetLibraryExecutionDefaultsResponse;
  sources: Source[];
}) {
  const t = useCopy();
  const doc = useDocumentCopy();
  const wiki = useWikiCopy();
  const [preferences] = useState(() => readOrganizePreferences(library.ref));
  const [localIntent] = useState(createKnowledgeOrganizeIntent);
  const intent = retained ?? localIntent;
  const [draft, setDraft] = useState(() => {
    const value = intent.get().draft ?? {
      sources,
      batch: rebuild ? [...batch] : sources.map((source) => source.id),
      requirements: maintenance ? "" : (preferences.requirements ?? ""),
    };
    intent.update({ draft: value });
    return value;
  });
  const changeDraft = (patch: Partial<typeof draft>) => {
    const next = { ...draft, ...patch };
    intent.update({ draft: next });
    setDraft(next);
    if (patch.requirements !== undefined && !maintenance)
      saveOrganizePreferences(library.ref, {
        requirements: patch.requirements,
      });
  };
  const [selection, setSelection] = useState<ExecutionSelection>(
    () =>
      intent.get().frozen ??
      preferences.selection ??
      readExecutionDefaults(defaults),
  );
  const execution = useExecutionSelection({
    value: { ...selection, reasoning: selection.reasoning ?? "" },
    reasoningDefault: "minimum",
  });
  const start = useMutation({
    mutationFn: () =>
      createKnowledgeOrganizeStart({
        library,
        batch: draft.batch,
        selection: execution.selection,
        intent,
        perform: operation.perform,
        message: [
          maintenance
            ? wiki("libraryMaintenanceDraft")
            : rebuild
              ? doc("rebuildMessage")
              : t("organizeInitialMessage"),
          ...(draft.requirements.trim()
            ? [
                maintenance
                  ? draft.requirements.trim()
                  : t("organizeRequirementsMessage", {
                      requirements: draft.requirements.trim(),
                    }),
              ]
            : []),
        ].join("\n\n"),
        rebuild,
        maintenance,
        notAcceptedMessage: t("organizeTaskNotAccepted"),
      }).execute(),
    onSuccess: onAccepted,
    retry: false,
  });
  const frozen = Boolean(intent.get().frozen);
  return (
    <>
      <div className="grid gap-4 px-6 pb-5">
        {rebuild && (
          <p className="type-body text-content-secondary">
            {doc("rebuildHelp")}
          </p>
        )}
        {maintenance && (
          <p className="type-body text-content-secondary">
            {wiki("libraryMaintenanceContext", { title: library.name })}
          </p>
        )}
        {!rebuild && !maintenance && (
          <Field label={t("organizeFiles")} hint={t("organizeFilesHelp")}>
            <SearchableMultiSelect
              size="md"
              options={draft.sources.map((source) => ({
                value: source.id,
                label: source.name,
              }))}
              value={draft.batch}
              onValueChange={(batch) => changeDraft({ batch })}
              triggerLabel={t("organizeSelectedFiles", {
                count: draft.batch.length,
              })}
              searchLabel={t("organizeSearchFiles")}
              emptyLabel={t("organizeNoFiles")}
              clearSearchLabel={t("organizeClearSearch")}
              clearSelectionLabel={t("organizeClearSelection")}
              disabled={frozen || start.isPending}
            />
          </Field>
        )}
        <KnowledgeModelConfigurationFields
          state={execution}
          onChange={(next) => {
            setSelection(next);
            saveOrganizePreferences(library.ref, { selection: next });
          }}
          disabled={frozen || start.isPending}
          permissionHidden
          helpHidden
        />
        <Field
          label={
            maintenance
              ? wiki("maintenanceRequirements")
              : t("organizeRequirements")
          }
          hint={
            maintenance
              ? wiki("maintenanceRequirementsHelp")
              : t("organizeRequirementsHelp")
          }
        >
          <Textarea
            aria-label={
              maintenance
                ? wiki("maintenanceRequirements")
                : t("organizeRequirements")
            }
            placeholder={
              maintenance
                ? wiki("maintenanceRequirementsPlaceholder")
                : t("organizeRequirementsPlaceholder")
            }
            value={draft.requirements}
            onChange={(event) =>
              changeDraft({ requirements: event.target.value })
            }
            disabled={frozen || start.isPending}
            maxLength={4000}
          />
        </Field>
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button
          disabled={
            start.isPending ||
            (!maintenance && !draft.batch.length) ||
            (!execution.valid && !frozen) ||
            library.kind !== "general" ||
            library.storage !== "local"
          }
          onClick={() => start.mutate()}
        >
          {t("start")}
        </Button>
      </DialogFooter>
    </>
  );
}
