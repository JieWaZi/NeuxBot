import type { GithubComJieWaZiNeuxBotServerApisKnowledgeLibrary as LibraryDTO } from "@/shared/api/generated/types.gen";
import type {
  ExtractionGranularity,
  LibraryChunkingConfig,
  LibraryIndexingStrategy,
  LibraryWikiSettings,
} from "./knowledge-model";
import { defaultLibraryChunkingConfig } from "./knowledge-model";

const granularityValues: ExtractionGranularity[] = [
  "focused",
  "standard",
  "exhaustive",
];

function readGranularity(value: string | undefined): ExtractionGranularity {
  return granularityValues.find((item) => item === value) ?? "standard";
}

export function readLibraryWikiSettings(dto: LibraryDTO): LibraryWikiSettings {
  const cfg = dto.wikiConfig;
  return {
    extractionGranularity: readGranularity(cfg?.extraction_granularity),
    contentInstructions: cfg?.content_instructions ?? "",
    extractionInstructions: cfg?.extraction_instructions ?? "",
  };
}

export function readLibraryIndexingStrategy(
  dto: LibraryDTO,
): LibraryIndexingStrategy {
  const strategy = dto.indexingStrategy;
  return {
    wikiEnabled: strategy?.wikiEnabled ?? true,
    vectorEnabled: strategy?.vectorEnabled ?? false,
    keywordEnabled: strategy?.keywordEnabled ?? false,
  };
}

export function readEmbeddingModelId(dto: LibraryDTO): string {
  return dto.embeddingModelId?.trim() ?? "";
}

export function readLibraryChunkingConfig(
  dto: LibraryDTO,
): LibraryChunkingConfig {
  const base = defaultLibraryChunkingConfig();
  const cfg = dto.chunkingConfig;
  return {
    chunk_size: cfg?.chunk_size ?? base.chunk_size,
    chunk_overlap: cfg?.chunk_overlap ?? base.chunk_overlap,
    separators: cfg?.separators ?? base.separators,
    // Go 的 omitempty 会省略 false；已有配置中缺失该字段表示关闭。
    enable_parent_child: cfg
      ? (cfg.enable_parent_child ?? false)
      : base.enable_parent_child,
    parent_chunk_size: cfg?.parent_chunk_size ?? base.parent_chunk_size,
    child_chunk_size: cfg?.child_chunk_size ?? base.child_chunk_size,
    strategy:
      cfg?.strategy === "auto" ||
      cfg?.strategy === "heading" ||
      cfg?.strategy === "heuristic" ||
      cfg?.strategy === "legacy"
        ? cfg.strategy
        : "",
    token_limit: cfg?.token_limit ?? base.token_limit,
    languages: cfg?.languages ?? base.languages,
  };
}
