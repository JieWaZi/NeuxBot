import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { browsePlugins } from "@/shared/api/generated/sdk.gen";
import { browsePluginsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import type { BrowsePluginsResponse } from "@/shared/api/generated/types.gen";
import { pluginLocale } from "../model/plugin-view";

/** 有限目录分批读取公开摘要；完整成功后才发布分类和数量，不扫描管理详情。 */
export function usePluginBrowse({
  search = "",
  installedOnly = false,
  active = true,
}: {
  search?: string;
  installedOnly?: boolean;
  active?: boolean;
} = {}) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const options = withLocalErrorSurface({
    query: { query: search, installedOnly, locale, pageSize: 100 },
  });
  const queryKey = browsePluginsQueryKey(options);
  const query = useQuery({
    queryKey,
    enabled: active,
    queryFn: async ({ signal }) => {
      let first: BrowsePluginsResponse | undefined;
      const entries = new Map<
        string | undefined,
        NonNullable<BrowsePluginsResponse["entries"]>[number]
      >();
      for (let pageNum = 1; ; pageNum++) {
        signal.throwIfAborted();
        const { data } = await browsePlugins(
          withLocalErrorSurface({
            query: { ...options.query, pageNum },
            signal,
            throwOnError: true as const,
          }),
        );
        signal.throwIfAborted();
        first ??= data;
        for (const entry of data.entries ?? [])
          entries.set(entry.sourceKey, entry);
        if (!data.hasMore) break;
        if (!data.entries?.length) throw new Error(t("failed"));
      }
      return {
        entries: [...entries.values()],
        total: first?.totalSize ?? null,
        installedTotal: first?.installedTotalSize ?? 0,
        source: first?.catalogSource,
      };
    },
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [queryKey],
    active,
  });
  return {
    ...query,
    entries: query.data?.entries ?? [],
    total: query.data?.total ?? null,
    installedTotal: query.data?.installedTotal ?? 0,
    source: query.data?.source,
  };
}

/** 正文按滚动读取公开摘要；完整安装摘要仍由 usePluginBrowse 提供给侧栏。 */
export function usePluginBrowsePages({
  search = "",
  active = true,
}: {
  search?: string;
  active?: boolean;
} = {}) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const options = withLocalErrorSurface({
    query: { query: search, locale, pageSize: 24 },
  });
  const queryKey = [...browsePluginsQueryKey(options), "infinite"];
  const query = useInfiniteQuery({
    queryKey,
    enabled: active,
    initialPageParam: 1,
    queryFn: async ({ signal, pageParam }) => {
      const { data } = await browsePlugins(
        withLocalErrorSurface({
          query: { ...options.query, pageNum: pageParam },
          signal,
          throwOnError: true as const,
        }),
      );
      if (data.hasMore && !data.entries?.length) throw new Error(t("failed"));
      return data;
    },
    getNextPageParam: (last, pages) =>
      last.hasMore && last.entries?.length ? pages.length + 1 : undefined,
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [queryKey],
    active,
  });
  const entries = new Map<
    string | undefined,
    NonNullable<BrowsePluginsResponse["entries"]>[number]
  >();
  for (const page of query.data?.pages ?? [])
    for (const entry of page.entries ?? []) entries.set(entry.sourceKey, entry);
  return {
    ...query,
    entries: [...entries.values()],
    total: query.data?.pages[0]?.totalSize ?? null,
    installedTotal: query.data?.pages[0]?.installedTotalSize ?? 0,
    source: query.data?.pages[0]?.catalogSource,
  };
}
