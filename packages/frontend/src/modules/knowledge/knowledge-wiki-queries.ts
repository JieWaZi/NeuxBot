import { queryOptions } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type {
  GithubComJieWaZiNeuxBotServerApisKnowledgePage as WikiPage,
  GithubComJieWaZiNeuxBotServerApisKnowledgePageDirectoryEntry as WikiPageEntry,
  GithubComJieWaZiNeuxBotServerApisKnowledgeTheme as WikiTheme,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { i18n } from "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import type { GithubComJieWaZiNeuxBotServerApisKnowledgeRevision as WikiRevision } from "@/shared/api/generated/types.gen";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";
export type { WikiPage, WikiPageEntry, WikiTheme };
export type { WikiRevision };

/** 快照不可变；缺失历史与普通读取故障分别表达，不将缺失伪装为空正文。 */
export function wikiRevisionOptions(
  ref: ResourceRef,
  page: WikiPage,
  version: number,
  snapshot?: WikiRevision,
) {
  return queryOptions({
    queryKey: [
      ...knowledgeKeys.resource(ref),
      "wiki",
      "revision",
      page.id,
      page.slug ?? "",
      version,
    ],
    staleTime: Infinity,
    initialData: snapshot,
    queryFn: async ({ signal }): Promise<WikiRevision | null> => {
      try {
        return (
          await knowledgeAdapter(ref).getRevision(
            withLocalErrorSurface({
              path: {
                libraryId: ref.knowledgeBaseId,
                connectionId: ref.connectionId,
                pageId: page.id ?? "",
                version,
              },
              query: { slug: page.slug },
              signal,
              throwOnError: true,
            }),
          )
        ).data;
      } catch (cause) {
        if (cause instanceof ApiError && cause.status === 404) return null;
        throw cause;
      }
    },
  });
}
export function wikiPagesOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "wiki", "pages"],
    queryFn: async ({ signal }) => {
      const pages: WikiPageEntry[] = [];
      const seen = new Set<string>();
      const adapter = knowledgeAdapter(ref);
      // 目录与统计必须使用完整分页；中断或空页不冒充完整目录。
      while (true) {
        const options = withLocalErrorSurface({
          path: {
            libraryId: ref.knowledgeBaseId,
            connectionId: ref.connectionId,
          },
          query: { offset: pages.length, limit: 1000 },
          signal,
          throwOnError: true as const,
        });
        const result = (await adapter.listPageDirectory(options)).data;
        const rows = result.items ?? [];
        for (const row of rows) {
          if (!row.id || seen.has(row.id))
            throw new Error(String(i18n.t("knowledgeWiki:incomplete")));
          seen.add(row.id);
        }
        pages.push(...rows);
        if (pages.length >= (result.total ?? pages.length)) return pages;
        if (!rows.length)
          throw new Error(String(i18n.t("knowledgeWiki:incomplete")));
        signal.throwIfAborted();
      }
    },
  });
}
export function wikiThemesOptions(ref: ResourceRef) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "wiki", "themes"],
    queryFn: async ({ signal }) => {
      const options = withLocalErrorSurface({
        path: {
          libraryId: ref.knowledgeBaseId,
          connectionId: ref.connectionId,
        },
        signal,
        throwOnError: true as const,
      });
      return (await knowledgeAdapter(ref).listThemes(options)).data.items ?? [];
    },
  });
}
export function wikiPageOptions(
  ref: ResourceRef,
  pageId: string,
  slug?: string,
) {
  return queryOptions({
    queryKey: [
      ...knowledgeKeys.resource(ref),
      "wiki",
      "page",
      pageId,
      slug ?? "",
    ],
    queryFn: async ({ signal }) => {
      const options = withLocalErrorSurface({
        path: {
          libraryId: ref.knowledgeBaseId,
          connectionId: ref.connectionId,
          pageId,
        },
        signal,
        throwOnError: true as const,
      });
      return (
        await knowledgeAdapter(ref).getPage({ ...options, query: { slug } })
      ).data;
    },
    enabled: Boolean(pageId),
  });
}

/** 索引导言与轻量目录独立读取；单组游标不影响其他组。 */
export function wikiIndexOptions(
  ref: ResourceRef,
  pageTypes?: string,
  cursor?: string,
) {
  return queryOptions({
    queryKey: [
      ...knowledgeKeys.resource(ref),
      "wiki",
      "index",
      pageTypes ?? "",
      cursor ?? "",
    ],
    queryFn: ({ signal }) => readWikiIndex(ref, pageTypes, cursor, signal),
    enabled: knowledgeAdapter(ref).localConfiguration,
  });
}

export async function readWikiIndex(
  ref: ResourceRef,
  pageTypes: string | undefined,
  cursor: string | undefined,
  signal: AbortSignal,
) {
  return (
    await knowledgeAdapter(ref).getWikiIndex(
      withLocalErrorSurface({
        path: { libraryId: ref.knowledgeBaseId },
        query: { pageTypes, cursor, limit: 50 },
        signal,
        throwOnError: true as const,
      }),
    )
  ).data;
}
