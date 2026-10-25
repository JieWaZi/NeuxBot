import type {
  CreateLibraryData,
  UpdateLibraryData,
} from "@/shared/api/generated/types.gen";
import type {
  Library,
  LibraryChunkingConfig,
  LibraryWikiSettings,
  LibraryIndexingStrategy,
} from "./knowledge-model";

export function buildUpdateLibraryBody(
  requestId: string,
  expectedVersion: number,
  library: Library,
): UpdateLibraryData["body"] {
  const config = buildCreateLibraryBody(requestId, library);
  return {
    requestId,
    expectedVersion,
    name: config.name,
    purpose: config.purpose,
    archived: false,
    policyVersion: library.policyVersion,
    chunkingConfig: config.chunkingConfig,
    wikiConfig: config.wikiConfig,
    indexingStrategy: config.indexingStrategy,
    embeddingModelId: library.embeddingModelId,
  };
}

type CreateLibraryBody = CreateLibraryData["body"] & {
  chunkingConfig: LibraryChunkingConfig;
};

export function buildCreateLibraryBody(
  requestId: string,
  library: {
    name: string;
    description: string;
    kind: string;
    language: string;
    wiki: LibraryWikiSettings;
    indexing: LibraryIndexingStrategy;
    embeddingModelId?: string;
    chunkingConfig: LibraryChunkingConfig;
  },
): CreateLibraryBody {
  const body: CreateLibraryBody = {
    requestId,
    name: library.name,
    wikiLanguage: library.language,
    purpose: library.description,
    chunkingConfig: library.chunkingConfig,
  };
  if (library.kind === "general") {
    body.wikiConfig = {
      extraction_granularity: library.wiki.extractionGranularity,
      content_instructions: library.wiki.contentInstructions,
      extraction_instructions: library.wiki.extractionInstructions,
    };
    body.indexingStrategy = {
      wikiEnabled: library.indexing.wikiEnabled,
      vectorEnabled: library.indexing.vectorEnabled,
      keywordEnabled: library.indexing.keywordEnabled,
      graphEnabled: false,
    };
    if (library.indexing.vectorEnabled && library.embeddingModelId) {
      body.embeddingModelId = library.embeddingModelId;
    }
  }
  return body;
}
