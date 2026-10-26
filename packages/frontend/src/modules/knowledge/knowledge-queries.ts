import { queryOptions, infiniteQueryOptions } from "@tanstack/react-query";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  knowledgeClient,
  KNOWLEDGE_SOURCE_PAGE_SIZE,
} from "@/shared/api/knowledge";
import {
  knowledgeAdapter,
  type KnowledgeSourceFilters,
} from "@/shared/api/knowledge";
import type { ResourceRef } from "./knowledge-transfer";
import {
  emptyLibrary,
  type Library,
  type LibraryLocation,
  type Source,
  isOrganizableSource,
} from "./knowledge-model";
import {
  readEmbeddingModelId,
  readLibraryIndexingStrategy,
  readLibraryWikiSettings,
  readLibraryChunkingConfig,
} from "./knowledge-library-config";
import type {
  GithubComJieWaZiNeuxBotServerApisKnowledgeLibrary as LibraryDTO,
  GithubComJieWaZiNeuxBotServerApisKnowledgeSource as SourceDTO,
} from "@/shared/api/generated/types.gen";
export const knowledgeKeys = {
  all: ["knowledge"] as const,
  libraryLists: ["knowledge", "libraries"] as const,
  capabilities: (ref: ResourceRef) =>
    [...knowledgeKeys.resource(ref), "capabilities"] as const,
  jobs: (ref: ResourceRef) => [...knowledgeKeys.resource(ref), "jobs"] as const,
  job: (ref: ResourceRef, id: string) =>
    [...knowledgeKeys.resource(ref), "job", id] as const,
  activity: (ref: ResourceRef, action = "", outcome = "") =>
    [...knowledgeKeys.resource(ref), "activity", action, outcome] as const,
  libraries: (offset = 0, location = "all", search = "") =>
    ["knowledge", "libraries", location, offset, search] as const,
  resource: (ref: ResourceRef) =>
    ["knowledge", ref.backend, ref.connectionId, ref.knowledgeBaseId] as const,
};

/** 当前占用库的任务与最近任务共用同一有界查询，不从分页结果推测活跃状态。 */
export function jobsOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: knowledgeKeys.jobs(ref),
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter(ref).listJobs(
          withLocalErrorSurface({
            path: { libraryId: ref.knowledgeBaseId },
            query: { offset: 0, limit: 1 },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
  });
}

export function librariesOptions(
  offset: number,
  location: LibraryLocation = "all",
  search = "",
) {
  const query = { offset, limit: 50, location, search };
  return queryOptions({
    queryKey: knowledgeKeys.libraries(offset, location, search),
    // 返回列表时读取详情已更新的统计摘要，不等待全局缓存新鲜期。
    staleTime: 0,
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter({
          backend: "local",
          connectionId: "local",
          knowledgeBaseId: "",
        }).listAvailableLibraries(
          withLocalErrorSurface({
            query,
            signal,
            throwOnError: true,
          }),
        )
      ).data,
  });
}
export function libraryOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "detail"],
    queryFn: ({ signal }) => knowledgeClient(ref).getLibrary(signal),
    enabled: Boolean(ref.knowledgeBaseId),
  });
}
export function sourcesOptions(
  ref: ResourceRef,
  offset: number,
  enabled: boolean,
  pageSize = KNOWLEDGE_SOURCE_PAGE_SIZE,
  filters: KnowledgeSourceFilters = {},
) {
  return queryOptions({
    queryKey: [
      ...knowledgeKeys.resource(ref),
      "sources",
      offset,
      pageSize,
      filters,
    ],
    queryFn: ({ signal }) =>
      knowledgeClient(ref).listSources(offset, signal, pageSize, filters),
    enabled: enabled && Boolean(ref.knowledgeBaseId),
    placeholderData: undefined,
  });
}

/** 原件目录累计读取真实分页，筛选变化使用独立缓存并从头开始。 */
export function sourceDirectoryOptions(
  ref: ResourceRef,
  filters: KnowledgeSourceFilters,
) {
  return infiniteQueryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "sources", "directory", filters],
    initialPageParam: 0,
    queryFn: async ({ pageParam, signal }) => {
      const result = await knowledgeClient(ref).listSources(
        pageParam,
        signal,
        KNOWLEDGE_SOURCE_PAGE_SIZE,
        filters,
      );
      if (!result.items?.length && (result.total ?? 0) > pageParam)
        throw new Error("Incomplete knowledge source list");
      return result;
    },
    getNextPageParam: (last, pages) => {
      const offset = pages.reduce(
        (total, page) => total + (page.items?.length ?? 0),
        0,
      );
      return last.items?.length && offset < (last.total ?? offset)
        ? offset
        : undefined;
    },
    select: (data) => ({
      ...data,
      items: [
        ...new Map(
          data.pages
            .flatMap((page) => page.items ?? [])
            .map((source) => [source.id, source]),
        ).values(),
      ],
      total: data.pages.at(-1)?.total ?? 0,
    }),
  });
}

/** 打开整理表单时完整读取来源，默认范围不受列表翻页与筛选影响。 */
export function organizableSourcesOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "sources", "organizable"],
    staleTime: 0,
    queryFn: async ({ signal }) => {
      const sources: Source[] = [];
      let offset = 0;
      while (true) {
        const page = await knowledgeClient(ref).listSources(offset, signal);
        const items = page.items ?? [];
        sources.push(...items.map((item) => toSource(item, ref.backend)));
        offset += items.length;
        if (offset >= (page.total ?? offset)) break;
        if (!items.length) throw new Error("Incomplete knowledge source list");
      }
      return sources.filter((source) => isOrganizableSource(source));
    },
  });
}
export function toLibrary(v: LibraryDTO): Library {
  return {
    offline: v.offline,
    mountId:
      "mountId" in v && typeof v.mountId === "string" ? v.mountId : undefined,
    mountRevision:
      "mountRevision" in v && typeof v.mountRevision === "number"
        ? v.mountRevision
        : undefined,
    ...emptyLibrary(
      v.id ?? "",
      v.name ?? "",
      v.kind === "software"
        ? "software"
        : v.kind === "general"
          ? "general"
          : "official",
    ),
    ref: {
      backend: v.ref?.backend ?? "local",
      connectionId: v.ref?.connectionId ?? "local",
      knowledgeBaseId: v.ref?.knowledgeBaseId ?? v.id ?? "",
    },
    storage: v.ref?.backend === "local" ? "local" : "remote",
    description: v.purpose ?? "",
    pageTotal: v.pageTotal,
    pageTotalObservedAt: v.pageTotalObservedAt,
    language: v.wikiLanguage === "en-US" ? "en-US" : "zh-CN",
    revision: v.version ?? 0,
    structureVersion: v.structureVersion ?? 0,
    policyVersion: v.policyVersion ?? 1,
    writable: v.ref?.backend === "local" && !v.archived,
    wiki: readLibraryWikiSettings(v),
    indexing: readLibraryIndexingStrategy(v),
    embeddingModelId: readEmbeddingModelId(v),
    chunkingConfig: readLibraryChunkingConfig(v),
  };
}
export function toSource(v: SourceDTO, backend = "local"): Source {
  return {
    sourceId: v.sourceId,
    organizeBlocked: v.organizeBlocked,
    id: v.id ?? "",
    name: v.filename || v.id || "",
    size:
      backend === "weknora"
        ? "—"
        : v.size === 0
          ? "0 B"
          : `${Math.ceil((v.size ?? 0) / 1024)} KB`,
    state:
      backend === "weknora"
        ? "unknown"
        : v.archived
          ? "archived"
          : v.state === "queued"
            ? "queued"
            : v.state === "processing"
              ? "processing"
              : v.state === "processed"
                ? "processed"
                : v.state === "no-change"
                  ? "unchanged"
                  : v.state === "cancelled"
                    ? "cancelled"
                    : v.state === "failed"
                      ? "failed"
                      : v.state === "pending"
                        ? "pending"
                        : "unknown",
    createdAt: v.createdAt,
    date: v.createdAt ? new Date(v.createdAt).toLocaleDateString() : "—",
    diagnostic: v.diagnostic ?? "",
  };
}
