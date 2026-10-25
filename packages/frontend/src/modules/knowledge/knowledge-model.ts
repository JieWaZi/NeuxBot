import type { ResourceRef } from "./knowledge-transfer";

export type LibraryLocation = "all" | "local" | "remote";
export type SourceState =
  | "pending"
  | "queued"
  | "processing"
  | "processed"
  | "failed"
  | "archived"
  | "unknown"
  | "unchanged"
  | "cancelled";
export function isOrganizableSourceState(state: SourceState) {
  return state === "pending" || state === "failed" || state === "cancelled";
}
export function isOrganizableSource(
  source: Pick<Source, "state" | "organizeBlocked">,
) {
  return !source.organizeBlocked && isOrganizableSourceState(source.state);
}
export type Source = {
  organizeBlocked?: boolean;
  id: string;
  sourceId?: string;
  name: string;
  size: string;
  state: SourceState;
  diagnostic: string;
  date: string;
  createdAt?: number;
};
export type ExtractionGranularity = "focused" | "standard" | "exhaustive";
export type LibraryWikiSettings = {
  extractionGranularity: ExtractionGranularity;
  contentInstructions: string;
  extractionInstructions: string;
};
export type LibraryIndexingStrategy = {
  wikiEnabled: boolean;
  vectorEnabled: boolean;
  keywordEnabled: boolean;
};
/** 字段与 WeKnora ChunkingConfig 保持同名；新建默认来自上游编辑器。 */
export type LibraryChunkingConfig = {
  chunk_size: number;
  chunk_overlap: number;
  separators: string[];
  enable_parent_child: boolean;
  parent_chunk_size: number;
  child_chunk_size: number;
  strategy: "" | "auto" | "heading" | "heuristic" | "legacy";
  token_limit: number;
  languages: string[];
};
export function defaultLibraryChunkingConfig(): LibraryChunkingConfig {
  return {
    chunk_size: 512,
    chunk_overlap: 80,
    separators: ["\n\n", "\n", "。", "！", "？", ";", "；"],
    enable_parent_child: true,
    parent_chunk_size: 4096,
    child_chunk_size: 384,
    strategy: "auto",
    token_limit: 0,
    languages: [],
  };
}
export type Library = {
  offline?: boolean;
  mountId?: string;
  mountRevision?: number;
  name: string;
  description: string;
  wiki: LibraryWikiSettings;
  indexing: LibraryIndexingStrategy;
  chunkingConfig: LibraryChunkingConfig;
  ref: ResourceRef;
  sourceTotal?: number;
  pageTotal?: number;
  pageTotalObservedAt?: number;
  id: string;
  kind: "general" | "software" | "official";
  storage: "local" | "remote";
  language: "zh-CN" | "en-US";
  sources: Source[];
  revision: number;
  structureVersion?: number;
  policyVersion: number;
  writable: boolean;
  embeddingModelId: string;
};
export function defaultLibraryWikiSettings(): LibraryWikiSettings {
  return {
    extractionGranularity: "standard",
    contentInstructions: "",
    extractionInstructions: "",
  };
}
export function defaultLibraryIndexingStrategy(): LibraryIndexingStrategy {
  return { wikiEnabled: true, vectorEnabled: false, keywordEnabled: false };
}
export type Route = {
  sourceQuery: string;
  sourceState: string;
  sourceSort: string;
  view: "library" | "raw" | "wiki" | "graph";
  libraryId: string;
};
export function emptyLibrary(
  id: string,
  name: string,
  kind: Library["kind"],
): Library {
  return {
    id,
    name,
    kind,
    ref: { backend: "local", connectionId: "local", knowledgeBaseId: id },
    description: "",
    storage: "local",
    language: "zh-CN",
    sources: [],
    revision: 1,
    policyVersion: 1,
    writable: false,
    wiki: defaultLibraryWikiSettings(),
    indexing: defaultLibraryIndexingStrategy(),
    embeddingModelId: "",
    chunkingConfig: defaultLibraryChunkingConfig(),
  };
}
/** 库详情默认进入知识目录，原件与图谱保留稳定视图参数。 */
export function readRoute(query: URLSearchParams): Route {
  const libraryId = query.get("library") ?? "";
  return {
    view:
      query.get("view") === "graph"
        ? "graph"
        : query.get("view") === "wiki"
          ? "wiki"
          : query.get("view") === "raw"
            ? "raw"
            : libraryId && query.get("view") !== "library"
              ? "wiki"
              : "library",
    sourceQuery: query.get("sourceQuery") ?? "",
    sourceState: query.get("sourceState") ?? "all",
    sourceSort: query.get("sourceSort") ?? "newest",
    libraryId,
  };
}
export type KnowledgeReadState = {
  pending: boolean;
  error: unknown;
  retry: () => void;
  refreshing?: boolean;
  stale?: boolean;
};
