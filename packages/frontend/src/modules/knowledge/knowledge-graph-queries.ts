import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { knowledgeDirectoryIncomplete } from "@/shared/api/knowledge/knowledge-adapter-types";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";
import type { KnowledgeGraph } from "./knowledge-graph-model";
import { wikiPageOptions, type WikiPage } from "./knowledge-wiki-queries";

/** 图谱按需读取链接关系，独立缓存不能复用不含出链的轻量目录。 */
export function localGraphPagesOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "wiki", "graph-pages"],
    queryFn: async ({ signal }) => {
      const pages: WikiPage[] = [];
      const adapter = knowledgeAdapter(ref);
      while (true) {
        const result = (
          await adapter.listPages(
            withLocalErrorSurface({
              path: {
                libraryId: ref.knowledgeBaseId,
                connectionId: ref.connectionId,
              },
              query: { offset: pages.length, limit: 100 },
              signal,
              throwOnError: true,
            }),
          )
        ).data;
        const rows = result.items ?? [];
        pages.push(...rows);
        if (pages.length >= (result.total ?? pages.length)) return pages;
        if (!rows.length) knowledgeDirectoryIncomplete();
        signal.throwIfAborted();
      }
    },
  });
}

export function remoteGraphOptions(ref: ResourceRef, center: string) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "graph", center],
    queryFn: async ({ signal }): Promise<KnowledgeGraph> => {
      const result = (
        await knowledgeAdapter(ref).getGraph(
          withLocalErrorSurface({
            path: {
              libraryId: ref.knowledgeBaseId,
              connectionId: ref.connectionId,
            },
            query: {
              mode: center ? "ego" : "overview",
              center,
              depth: 1,
              // 官方图谱及本机远端代理均以 200 个节点为单次上限。
              limit: 200,
            },
            signal,
            throwOnError: true,
          }),
        )
      ).data;
      return {
        nodes: (result.nodes ?? []).flatMap((node) =>
          node.slug
            ? [
                {
                  slug: node.slug,
                  title: node.title || node.slug,
                  page_type: node.page_type || "concept",
                  link_count: node.link_count ?? 0,
                },
              ]
            : [],
        ),
        edges: (result.edges ?? []).flatMap((edge) =>
          edge.source && edge.target
            ? [{ source: edge.source, target: edge.target }]
            : [],
        ),
        meta: {
          mode: result.meta?.mode || (center ? "ego" : "overview"),
          total: result.meta?.total ?? result.nodes?.length ?? 0,
          returned: result.meta?.returned ?? result.nodes?.length ?? 0,
          truncated: result.meta?.truncated ?? false,
        },
      };
    },
  });
}

export function graphPageOptions(
  ref: ResourceRef,
  slug: string,
  pageId?: string,
) {
  if (knowledgeAdapter(ref).revisionsIncludeContent && pageId)
    return wikiPageOptions(ref, pageId);
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "wiki", "page-by-slug", slug],
    queryFn: async ({ signal }) => {
      const options = withLocalErrorSurface({
        path: {
          libraryId: ref.knowledgeBaseId,
          connectionId: ref.connectionId,
        },
        query: { slug },
        signal,
        throwOnError: true as const,
      });
      return (await knowledgeAdapter(ref).getPageBySlug(options)).data;
    },
  });
}

/** 上游前沿展开使用最多六个并发读取，结果仍由 Query Cache 管理。 */
export function remoteGraphExpansionOptions(
  ref: ResourceRef,
  anchors: string[],
  client: QueryClient,
) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "graph", "expansion", anchors],
    queryFn: async ({ signal }) => {
      const results = new Map<string, KnowledgeGraph>();
      let cursor = 0;
      async function worker() {
        while (cursor < anchors.length) {
          signal.throwIfAborted();
          const slug = anchors[cursor++];
          if (slug)
            results.set(
              slug,
              await client.fetchQuery(remoteGraphOptions(ref, slug)),
            );
        }
      }
      const workers = await Promise.allSettled(
        Array.from({ length: Math.min(6, anchors.length) }, worker),
      );
      signal.throwIfAborted();
      const failure = workers.find((result) => result.status === "rejected");
      if (failure?.status === "rejected") throw failure.reason;
      return anchors.flatMap((slug) => {
        const graph = results.get(slug);
        return graph ? [graph] : [];
      });
    },
  });
}

export function remoteGraphSearchOptions(ref: ResourceRef, text: string) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "graph", "search", text],
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter(ref).listPages(
          withLocalErrorSurface({
            path: {
              libraryId: ref.knowledgeBaseId,
              connectionId: ref.connectionId,
            },
            query: { query: text, offset: 0, limit: 20 },
            signal,
            throwOnError: true,
          }),
        )
      ).data.items ?? [],
  });
}
