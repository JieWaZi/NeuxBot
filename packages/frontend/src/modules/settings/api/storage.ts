import {
  infiniteQueryOptions,
  queryOptions,
  type QueryClient,
  type QueryKey,
  type UseInfiniteQueryResult,
} from "@tanstack/react-query";
import {
  getStorageUsage,
  listStorageEntities,
  listStorageFiles,
  deleteStorageFile,
  getStoragePolicy,
  updateStoragePolicy,
  getStorageMaintenance,
  previewStorageCleanup,
  cleanupStorage,
  previewExpiredChats,
} from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { toApiError } from "@/shared/api/errors";
import { statusErrorCatalog } from "@/shared/api/status-error-catalog.generated";

export type StorageFileScope = { kind: "wakers" | "chats"; id: string };

export type CleanupCategory = "caches" | "logs" | "runtimes";
export const cleanupCategories: CleanupCategory[] = [
  "caches",
  "logs",
  "runtimes",
];
export const storageKeys = {
  usage: ["storage", "usage"] as const,
  policy: ["storage", "policy"] as const,
  maintenance: ["storage", "maintenance"] as const,
  cleanup: ["storage", "cleanup-preview"] as const,
  entities: ["storage", "entities"] as const,
  files: ["storage", "files"] as const,
};

export async function readUsage(force = false, signal?: AbortSignal) {
  return (
    await getStorageUsage({
      ...withLocalErrorSurface({ query: { force }, signal }),
      throwOnError: true,
    })
  ).data;
}
export function storageOptions() {
  return queryOptions({
    queryKey: storageKeys.usage,
    queryFn: ({ signal }) => readUsage(false, signal),
  });
}
export function storagePolicyOptions() {
  return queryOptions({
    queryKey: storageKeys.policy,
    queryFn: async ({ signal }) =>
      (
        await getStoragePolicy({
          ...withLocalErrorSurface({ signal }),
          throwOnError: true,
        })
      ).data,
  });
}
export function storageMaintenanceOptions() {
  return queryOptions({
    queryKey: storageKeys.maintenance,
    queryFn: async ({ signal }) =>
      (
        await getStorageMaintenance({
          ...withLocalErrorSurface({ signal }),
          throwOnError: true,
        })
      ).data,
  });
}

function snapshotExpired(error: unknown) {
  const { code } = toApiError(error);
  return (
    code === "StorageSnapshotExpired" ||
    code === String(statusErrorCatalog.StorageSnapshotExpired.code)
  );
}

/** 旧代游标在同一次加载中不可恢复，重新读取头部后再取当前代续页。 */
export async function loadMoreStorage(
  client: QueryClient,
  queryKey: QueryKey,
  list: Pick<
    UseInfiniteQueryResult<unknown, unknown>,
    "fetchNextPage" | "refetch"
  >,
) {
  const query = client.getQueryCache().find({ queryKey, exact: true });
  const result = await list.fetchNextPage({ cancelRefetch: false });
  if (
    !query?.isActive() ||
    !result.isFetchNextPageError ||
    !snapshotExpired(result.error)
  ) {
    return;
  }
  const refreshed = await list.refetch({ cancelRefetch: false });
  if (query.isActive() && refreshed.isSuccess)
    await list.fetchNextPage({ cancelRefetch: false });
}

function retryStoragePage(failures: number, error: unknown) {
  return failures < 2 && !snapshotExpired(error);
}

export function storageEntitiesOptions(
  kind: "wakers" | "chats",
  wakerId?: string,
) {
  return infiniteQueryOptions({
    queryKey: [...storageKeys.entities, kind, wakerId],
    retry: retryStoragePage,
    initialPageParam: "",
    queryFn: async ({ pageParam, signal }) =>
      (
        await listStorageEntities({
          ...withLocalErrorSurface({
            query: {
              kind,
              limit: 10,
              wakerId,
              cursor: pageParam || undefined,
            },
            signal,
          }),
          throwOnError: true,
        })
      ).data,
    getNextPageParam: (page) =>
      kind === "wakers" || wakerId ? page.nextCursor || undefined : undefined,
  });
}
export function storageFilesOptions(
  scope: StorageFileScope,
  search: string,
  sort: "latest" | "largest",
) {
  return infiniteQueryOptions({
    queryKey: [...storageKeys.files, scope, search, sort],
    retry: retryStoragePage,
    initialPageParam: "",
    queryFn: async ({ pageParam, signal }) =>
      (
        await listStorageFiles({
          ...withLocalErrorSurface({
            query: { ...scope, search, sort, cursor: pageParam || undefined },
            signal,
          }),
          throwOnError: true,
        })
      ).data,
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}
export const removeStorageFile = async (
  scope: StorageFileScope,
  fileId: string,
  version: string,
) =>
  (
    await deleteStorageFile({
      ...withLocalErrorSurface({ body: { ...scope, fileId, version } }),
      throwOnError: true,
    })
  ).data;

export const reviewResources = async (
  categories: CleanupCategory[],
  signal?: AbortSignal,
) =>
  (
    await previewStorageCleanup({
      ...withLocalErrorSurface({ body: { categories }, signal }),
      throwOnError: true,
    })
  ).data;
export const clearResources = async (previewId: string) =>
  (
    await cleanupStorage({
      ...withLocalErrorSurface({ body: { previewId } }),
      throwOnError: true,
    })
  ).data;
export const reviewChats = async (retentionDays: number) =>
  (
    await previewExpiredChats({
      ...withLocalErrorSurface({ body: { retentionDays } }),
      throwOnError: true,
    })
  ).data;
export const savePolicy = async (
  enabled: boolean,
  retentionDays: number,
  expectedRevision: number,
  previewId?: string,
) =>
  (
    await updateStoragePolicy({
      ...withLocalErrorSurface({
        body: { enabled, retentionDays, expectedRevision, previewId },
      }),
      throwOnError: true,
    })
  ).data;
