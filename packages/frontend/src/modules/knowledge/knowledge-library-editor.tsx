import { useState, useSyncExternalStore, type ReactNode } from "react";
import { Copy, Lock } from "lucide-react";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { KnowledgeModelConfigurationFields } from "./knowledge-model-configuration-fields";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { toast } from "@/shared/ui/sonner";
import { KnowledgeLibraryDialogShell } from "./knowledge-library-dialog-shell";
import { Field, SectionTitle } from "./knowledge-controls";
import { KnowledgeChunkingFields } from "./knowledge-chunking-fields";
import { GeneralLibraryConfigFields } from "./knowledge-library-settings-fields";
import { KnowledgeEmbeddingFields } from "./knowledge-embedding-fields";
import type {
  KnowledgeEditorCommand,
  KnowledgeEditorController,
} from "./knowledge-editor-controller";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { i18n } from "@/shared/i18n";
import { useCopy } from "./use-extended-copy";

import {
  KnowledgeLibraryNavigation,
  type KnowledgeLibrarySection as Section,
} from "./knowledge-library-navigation";

/** 沿固定 WeKnora 编辑器的单一草稿、分区与底栏结构，控件复用宿主组件。 */
export function KnowledgeLibraryEditor({
  opened,
  mode,
  controller,
  close,
  save,
  activity,
  embedded = false,
  sourceTotal,
}: {
  opened: boolean;
  mode: "create" | "edit";
  controller: KnowledgeEditorController;
  close: () => void;
  save: (
    command: KnowledgeEditorCommand,
    controller: KnowledgeEditorController,
  ) => Promise<void>;
  activity?: ReactNode;
  embedded?: boolean;
  sourceTotal?: number;
}) {
  const t = useCopy();
  const state = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
  );
  const [section, setSection] = useState<Section>("basic");
  const [chunkingEdited, setChunkingEdited] = useState(false);
  const [executionEdited, setExecutionEdited] = useState(false);
  const [validation, setValidation] = useState("");
  const draft = state.draft;
  const library = draft.library;
  const localConfiguration = knowledgeAdapter(library.ref).localConfiguration;
  const locked = state.pending || Boolean(state.command);
  const execution = useExecutionSelection({
    value: { ...draft.execution, reasoning: draft.execution.reasoning ?? "" },
    reasoningDefault: "minimum",
    enabled: opened && localConfiguration,
  });
  const embedding = draft.embedding;
  const embeddingPresent = Boolean(
    embedding.baseUrl.trim() ||
    embedding.model.trim() ||
    embedding.apiKey ||
    embedding.hasApiKey,
  );
  const embeddingReady =
    !localConfiguration ||
    !(library.indexing.vectorEnabled || embeddingPresent) ||
    Boolean(
      embedding.baseUrl.trim() &&
      embedding.model.trim() &&
      (embedding.apiKey || embedding.hasApiKey),
    );
  const indexingLocked =
    mode === "edit" &&
    ((sourceTotal ?? library.sourceTotal ?? library.sources.length) > 0 ||
      (library.pageTotal ?? 0) > 0);
  const update = (patch: Partial<typeof library>) =>
    controller.edit({ ...draft, library: { ...library, ...patch } });
  const requestClose = () => {
    if (!state.pending) close();
  };
  const submit = async () => {
    if (state.pending) return;
    if (!state.command) {
      if (!library.name.trim()) {
        setSection("basic");
        setValidation(t("nameRequired"));
        return;
      }
      if (
        localConfiguration &&
        (mode === "create" || executionEdited) &&
        !execution.valid
      ) {
        setSection("models");
        setValidation(t("executionRequired"));
        return;
      }
      if (!embeddingReady) {
        setSection("models");
        setValidation(t("embeddingIncomplete"));
        return;
      }
      if (embedding.baseUrl.trim()) {
        try {
          const url = new URL(embedding.baseUrl.trim());
          if (!["http:", "https:"].includes(url.protocol))
            throw new Error("invalid protocol");
        } catch {
          setSection("models");
          setValidation(t("embeddingBaseUrlInvalid"));
          return;
        }
      }
      const cfg = library.chunkingConfig;
      const ranges: [number, number, number][] = [
        [cfg.chunk_size, 100, 4000],
        [cfg.chunk_overlap, 0, 500],
      ];
      if (cfg.enable_parent_child)
        ranges.push(
          [cfg.parent_chunk_size, 512, 8192],
          [cfg.child_chunk_size, 64, 2048],
        );
      if (cfg.strategy !== "legacy") ranges.push([cfg.token_limit, 0, 8192]);
      if (
        ranges.some(
          ([value, min, max]) =>
            !Number.isInteger(value) || value < min || value > max,
        )
      ) {
        setSection("chunking");
        setValidation(t("chunkingInvalid"));
        return;
      }
    }
    setValidation("");
    const value = {
      ...draft,
      library: {
        ...library,
        name: library.name.trim(),
        embeddingModelId: embedding.model.trim() || library.embeddingModelId,
      },
      embedding: {
        ...embedding,
        baseUrl: embedding.baseUrl.trim(),
        model: embedding.model.trim(),
      },
      execution:
        mode === "create" || executionEdited
          ? execution.selection
          : draft.execution,
    };
    if (await controller.submit(value, save)) close();
  };
  const content = (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <KnowledgeLibraryNavigation
        mode={mode}
        section={section}
        disabled={state.pending}
        onSectionChange={setSection}
      />
      <form
        noValidate
        className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="scrollbar-hidden flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-6 pb-5">
          <fieldset
            disabled={locked}
            className={
              section === "basic" ? "grid min-w-0 gap-5 border-0 p-0" : "hidden"
            }
          >
            <SectionTitle title={t("basicSettings")} />
            {mode === "edit" && (
              <Field label={t("libraryIdentifier")}>
                <div className="flex min-w-0 items-center gap-2">
                  <output
                    aria-label={t("libraryIdentifier")}
                    className="bg-surface-subtle type-body text-content-secondary min-w-0 flex-1 rounded-md px-3 py-2 break-all"
                  >
                    <code>{library.id}</code>
                  </output>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t("copyLibraryIdentifier")}
                    onClick={() => {
                      void navigator.clipboard
                        .writeText(library.id)
                        .then(() => toast.success(t("copied")))
                        .catch(() => toast.error(t("copyFailed")));
                    }}
                  >
                    <Copy />
                  </Button>
                </div>
              </Field>
            )}
            <Field label={t("name")}>
              <Input
                autoFocus
                required
                aria-label={t("name")}
                placeholder={t("namePlaceholder")}
                maxLength={50}
                value={library.name}
                onChange={(event) => update({ name: event.target.value })}
              />
            </Field>
            <Field label={t("purpose")}>
              <Textarea
                aria-label={t("purpose")}
                placeholder={t("purposePlaceholder")}
                maxLength={200}
                value={library.description}
                onChange={(event) =>
                  update({ description: event.target.value })
                }
              />
            </Field>
            {indexingLocked && (
              <p className="type-caption text-content-secondary flex items-center gap-2">
                <Lock className="size-icon-sm shrink-0" />
                {t("indexingLocked")}
              </p>
            )}
            <GeneralLibraryConfigFields
              disabled={locked}
              indexingDisabled={indexingLocked}
              wiki={library.wiki}
              onWikiChange={(wiki) => update({ wiki })}
              indexing={library.indexing}
              onIndexingChange={(indexing) => {
                const wikiOnly = (value: typeof indexing) =>
                  value.wikiEnabled &&
                  !value.vectorEnabled &&
                  !value.keywordEnabled;
                const presetChanged =
                  mode === "create" &&
                  !chunkingEdited &&
                  wikiOnly(library.indexing) !== wikiOnly(indexing);
                update({
                  indexing,
                  ...(presetChanged
                    ? {
                        chunkingConfig: {
                          ...library.chunkingConfig,
                          chunk_size: wikiOnly(indexing) ? 2048 : 512,
                          chunk_overlap: wikiOnly(indexing) ? 0 : 80,
                          enable_parent_child: !wikiOnly(indexing),
                        },
                      }
                    : {}),
                });
              }}
            />
          </fieldset>
          <fieldset
            disabled={locked}
            className={
              section === "models"
                ? "grid min-w-0 gap-5 border-0 p-0"
                : "hidden"
            }
          >
            {!localConfiguration ? (
              <p role="status" className="type-body text-content-secondary">
                {i18n.t("knowledge:remoteUnsupported")}
              </p>
            ) : (
              <>
                <SectionTitle title={t("modelSettings")} />
                <p className="type-caption text-content-secondary">
                  {t("executionDefaultsHelp")}
                </p>
                <KnowledgeModelConfigurationFields
                  state={execution}
                  required
                  onChange={(value) => {
                    setExecutionEdited(true);
                    controller.edit({ ...draft, execution: value });
                  }}
                  disabled={locked}
                  permissionHidden
                />
                <SectionTitle title={t("embeddingSettings")} />
                <KnowledgeEmbeddingFields
                  value={embedding}
                  required={library.indexing.vectorEnabled || embeddingPresent}
                  onChange={(embedding) =>
                    controller.edit({ ...draft, embedding })
                  }
                  libraryId={mode === "edit" ? library.id : undefined}
                  resource={library.ref}
                  disabled={locked}
                  modelDisabled={
                    indexingLocked && Boolean(library.embeddingModelId)
                  }
                />
              </>
            )}
          </fieldset>
          <div
            className={section === "chunking" ? "grid min-w-0 gap-5" : "hidden"}
          >
            <KnowledgeChunkingFields
              value={library.chunkingConfig}
              disabled={locked}
              onChange={(chunkingConfig) => {
                setChunkingEdited(true);
                update({ chunkingConfig });
              }}
            />
          </div>
          {section === "activity" && activity}
          {validation && (
            <p role="alert" className="type-body text-destructive">
              {validation}
            </p>
          )}
        </div>
        <DialogFooter>
          {section === "activity" ? (
            <Button type="button" variant="secondary" onClick={requestClose}>
              {t("close")}
            </Button>
          ) : (
            <>
              <Button
                type="button"
                variant="ghost"
                disabled={state.pending}
                onClick={requestClose}
              >
                {t("cancel")}
              </Button>
              <Button
                type="submit"
                disabled={
                  state.pending ||
                  (!state.command &&
                    (!library.name.trim() ||
                      ((mode === "create" || executionEdited) &&
                        !execution.valid)))
                }
              >
                {t(mode === "create" ? "saveNew" : "saveAndClose")}
              </Button>
            </>
          )}
        </DialogFooter>
      </form>
    </div>
  );
  return embedded ? (
    content
  ) : (
    <KnowledgeLibraryDialogShell
      opened={opened}
      title={t(mode === "create" ? "create" : "librarySettings")}
      close={requestClose}
    >
      {content}
    </KnowledgeLibraryDialogShell>
  );
}
