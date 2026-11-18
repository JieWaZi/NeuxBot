import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import {
  getStorageUsage,
  listStorageEntities,
  getStoragePolicy,
  updateStoragePolicy,
  getStorageMaintenance,
  previewStorageCleanup,
  cleanupStorage,
  listExpiredChats,
  previewExpiredChats,
  cleanupExpiredChats,
} from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { expiredChatsKey } from "@/shared/api/chat-protection";

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
export function storageEntitiesOptions(kind: "wakers" | "chats") {
  return infiniteQueryOptions({
    queryKey: [...storageKeys.entities, kind],
    initialPageParam: "",
    queryFn: async ({ pageParam, signal }) =>
      (
        await listStorageEntities({
          ...withLocalErrorSurface({
            query: {
              kind,
              limit: kind === "chats" ? 5 : 10,
              cursor: pageParam || undefined,
            },
            signal,
          }),
          throwOnError: true,
        })
      ).data,
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}
export function expiredChatsOptions(retentionDays: number) {
  return infiniteQueryOptions({
    queryKey: [...expiredChatsKey, retentionDays],
    initialPageParam: "",
    queryFn: async ({ pageParam, signal }) =>
      (
        await listExpiredChats({
          ...withLocalErrorSurface({
            query: { retentionDays, limit: 20, cursor: pageParam || undefined },
            signal,
          }),
          throwOnError: true,
        })
      ).data,
    getNextPageParam: (page) => page.nextCursor || undefined,
  });
}
export const reviewResources = async (categories: CleanupCategory[]) =>
  (
    await previewStorageCleanup({
      ...withLocalErrorSurface({ body: { categories } }),
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
export const reviewChats = async (retentionDays: number, chatIds?: string[]) =>
  (
    await previewExpiredChats({
      ...withLocalErrorSurface({ body: { retentionDays, chatIds } }),
      throwOnError: true,
    })
  ).data;
export const deleteExpired = async (previewId: string) =>
  (
    await cleanupExpiredChats({
      ...withLocalErrorSurface({ body: { previewId } }),
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
