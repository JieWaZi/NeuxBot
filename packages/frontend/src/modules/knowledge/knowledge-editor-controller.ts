import {
  emptyEmbeddingDraft,
  type KnowledgeEmbeddingDraft,
} from "./knowledge-embedding-draft";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { emptyLibrary, type Library } from "./knowledge-model";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";

export type KnowledgeEditorDraft = {
  library: Library;
  execution: ExecutionSelection;
  embedding: KnowledgeEmbeddingDraft;
};
export type KnowledgeEditorCommand = {
  draft: KnowledgeEditorDraft;
  coreRequestId: string;
  executionRequestId: string;
  embeddingRequestId: string;
  libraryVersion: number;
  executionVersion: number;
  embeddingVersion: number;
};
type Snapshot = {
  draft: KnowledgeEditorDraft;
  saved: KnowledgeEditorDraft;
  executionVersion: number;
  embeddingVersion: number;
  acknowledgedEmbeddingRequestId?: string;
  command?: KnowledgeEditorCommand;
  pending: boolean;
  error?: unknown;
};
export type KnowledgeEditorController = {
  id: string;
  getSnapshot: () => Snapshot;
  subscribe: (listener: () => void) => () => void;
  edit: (draft: KnowledgeEditorDraft) => void;
  acknowledgeLibrary: (library: Library) => void;
  acknowledgeExecution: (
    execution: ExecutionSelection,
    version: number,
  ) => void;
  acknowledgeEmbedding: (
    profile: KnowledgeEmbeddingDraft,
    version: number,
    libraryVersion: number,
    requestId: string,
  ) => void;
  submit: (
    draft: KnowledgeEditorDraft,
    save: (
      command: KnowledgeEditorCommand,
      controller: KnowledgeEditorController,
    ) => Promise<void>,
  ) => Promise<boolean>;
};

function copyDraft(draft: KnowledgeEditorDraft): KnowledgeEditorDraft {
  return structuredClone(draft);
}

/** 页面持有未决编辑命令，关闭弹窗不丢身份；配置权威仍由服务器及其版本提供。 */
export function createKnowledgeEditorController(
  library?: Library,
  execution: ExecutionSelection = {},
  version = 0,
  embedding = emptyEmbeddingDraft(),
  embeddingVersion = 0,
): KnowledgeEditorController {
  const createdLibrary = emptyLibrary("", "", "general");
  createdLibrary.chunkingConfig = {
    ...createdLibrary.chunkingConfig,
    chunk_size: 2048,
    chunk_overlap: 0,
    enable_parent_child: false,
  };
  const initial = {
    library: library ?? createdLibrary,
    execution,
    embedding,
  };
  const creating = !initial.library.id;
  let state: Snapshot = {
    draft: copyDraft(initial),
    saved: copyDraft(initial),
    executionVersion: version,
    embeddingVersion,
    pending: false,
  };
  const listeners = new Set<() => void>();
  const update = (patch: Partial<Snapshot>) => {
    state = { ...state, ...patch };
    listeners.forEach((listener) => listener());
  };
  const controller: KnowledgeEditorController = {
    id: crypto.randomUUID(),
    getSnapshot: () => state,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    edit: (draft: KnowledgeEditorDraft) => {
      if (!state.pending && !state.command) update({ draft });
    },
    acknowledgeLibrary: (library: Library) =>
      update({ saved: { ...state.saved, library } }),
    acknowledgeExecution: (
      execution: ExecutionSelection,
      executionVersion: number,
    ) => update({ saved: { ...state.saved, execution }, executionVersion }),
    acknowledgeEmbedding: (
      embedding,
      embeddingVersion,
      libraryVersion,
      requestId,
    ) => {
      const confirmedEmbedding = { ...embedding, apiKey: "" };
      // 本阶段已经拿到权威回执，后续阶段失败只需补齐未确认写入，不再保留明文密钥。
      const confirmedDraft = { ...state.draft, embedding: confirmedEmbedding };
      update({
        draft: confirmedDraft,
        command: state.command
          ? {
              ...state.command,
              draft: { ...state.command.draft, embedding: confirmedEmbedding },
            }
          : undefined,
        saved: {
          ...state.saved,
          embedding: confirmedEmbedding,
          library: {
            ...state.saved.library,
            embeddingModelId: embedding.model,
            revision: libraryVersion,
          },
        },
        embeddingVersion,
        acknowledgedEmbeddingRequestId: requestId,
      });
    },
    submit: async (
      draft: KnowledgeEditorDraft,
      save: (
        command: KnowledgeEditorCommand,
        controller: KnowledgeEditorController,
      ) => Promise<void>,
    ) => {
      if (state.pending) return false;
      const command = state.command ?? {
        draft: copyDraft(draft),
        coreRequestId: crypto.randomUUID(),
        executionRequestId: crypto.randomUUID(),
        embeddingRequestId: crypto.randomUUID(),
        libraryVersion: state.saved.library.revision,
        executionVersion: state.executionVersion,
        embeddingVersion: state.embeddingVersion,
      };
      update({
        command,
        draft: copyDraft(command.draft),
        pending: true,
        error: undefined,
      });
      try {
        await save(command, controller);
        update({
          draft: copyDraft(state.saved),
          command: undefined,
          pending: false,
        });
        return true;
      } catch (error) {
        update({
          pending: false,
          error,
          command:
            isKnowledgeWriteOutcomeUnknown(error) ||
            (creating && Boolean(state.saved.library.id))
              ? (state.command ?? command)
              : undefined,
        });
        return false;
      }
    },
  };
  return controller;
}
