import { useInfiniteQuery, type QueryKey } from "@tanstack/react-query";

export const RESOURCE_BATCH_SIZE = 9;

/** 分页仅作为传输边界；卡片列表保留已加载内容并按滚动追加。 */
export function useResourcePages<T, TMetadata = unknown>({
  queryKey,
  fetchPage,
  enabled,
}: {
  queryKey: QueryKey;
  fetchPage: (
    page: number,
    signal: AbortSignal,
  ) => Promise<{ items: T[]; total: number; metadata?: TMetadata }>;
  enabled: boolean;
}) {
  const query = useInfiniteQuery({
    queryKey: [...queryKey, "infinite"],
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) => fetchPage(pageParam, signal),
    getNextPageParam: (last, pages) =>
      last.items.length > 0 &&
      pages.reduce((count, page) => count + page.items.length, 0) < last.total
        ? pages.length + 1
        : undefined,
    enabled,
  });
  return {
    ...query,
    items: query.data?.pages.flatMap((page) => page.items) ?? [],
    total: query.data?.pages[0]?.total ?? 0,
    metadata: query.data?.pages[0]?.metadata,
  };
}
