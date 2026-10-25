import { knowledgeAdapter } from "./knowledge-adapter";
import type { GithubComJieWaZiNeuxBotServerApisKnowledgeImportSourceInput as ImportSource } from "@/shared/api/generated/types.gen";
import type { ResourceRef } from "./knowledge-adapter-types";

export const KNOWLEDGE_SOURCE_PAGE_SIZE = 10;
export type KnowledgeSourceFilters = {
  query?: string;
  state?: string;
  sort?: string;
};
/** 兼容现有分页调用；所有目标分流与能力判断由统一 Adapter 拥有。 */
export function knowledgeClient(ref: ResourceRef) {
  const path = { libraryId: ref.knowledgeBaseId };
  const adapter = knowledgeAdapter(ref);
  return {
    getLibrary: async (signal?: AbortSignal) =>
      (await adapter.getLibrary({ path, signal, throwOnError: true })).data,
    listSources: async (
      offset: number,
      signal?: AbortSignal,
      pageSize = KNOWLEDGE_SOURCE_PAGE_SIZE,
      filters: KnowledgeSourceFilters = {},
    ) =>
      (
        await adapter.listSources({
          path,
          query: { offset, limit: pageSize, ...filters },
          signal,
          throwOnError: true,
        })
      ).data,
    importSource: async (body: ImportSource, signal?: AbortSignal) => {
      return (
        await adapter.importSource({ path, body, signal, throwOnError: true })
      ).data;
    },
  };
}
