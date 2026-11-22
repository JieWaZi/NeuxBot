import type { DesktopStorageCategory } from "@neuxbot/platform-contracts/services";
import { platform } from "@/shared/platform/provider";
import {
  clearResources,
  reviewResources,
  type CleanupCategory,
} from "./storage";

export const desktopStorageCategories = [
  "desktop-cache",
  "desktop-logs",
] as const satisfies readonly DesktopStorageCategory[];
export type StorageCleanupCategory = CleanupCategory | DesktopStorageCategory;
export type CleanupItem = {
  category: StorageCleanupCategory;
  bytes: number;
  available: boolean;
  complete: boolean;
};
export type CleanupReceipt = { category: StorageCleanupCategory } & (
  | { state: "failed"; error?: unknown; runId?: string }
  | { state: "empty" }
  | { state: "accepted"; runId: string }
  | { state: "cleared"; removedBytes: number }
);

function isCoreCleanup(value: unknown): value is CleanupCategory {
  return value === "caches" || value === "logs" || value === "runtimes";
}
export function isDesktopCleanup(
  value: StorageCleanupCategory,
): value is DesktopStorageCategory {
  return desktopStorageCategories.some((category) => category === value);
}

function measuredBytes(bytes: unknown): bytes is number {
  return typeof bytes === "number" && Number.isFinite(bytes) && bytes >= 0;
}

export function cleanupItemEmpty(item: CleanupItem | undefined) {
  return item?.available === true && item.complete && item.bytes === 0;
}

export async function previewCoreCleanup(
  categories: CleanupCategory[],
  signal?: AbortSignal,
) {
  const preview = await reviewResources(categories, signal);
  const items: CleanupItem[] = (preview.items ?? []).flatMap((row) =>
    isCoreCleanup(row.category) && measuredBytes(row.bytes)
      ? [
          {
            category: row.category,
            available: row.available === true,
            complete: row.complete !== false,
            bytes: row.bytes,
          },
        ]
      : [],
  );
  return { items, id: preview.id };
}

export async function previewDesktopCleanup(
  category: DesktopStorageCategory,
): Promise<CleanupItem> {
  const preview = platform().host.preview_desktop_storage;
  if (!preview) throw new Error();
  const result = await preview({ category });
  if (!measuredBytes(result.bytes)) throw new Error();
  return { ...result, category, complete: true };
}

async function confirmCore(categories: CleanupCategory[]) {
  if (!categories.length) return [];
  const results: CleanupReceipt[] = [];
  const pending: CleanupCategory[] = [];
  try {
    const { items, id } = await previewCoreCleanup(categories);
    for (const category of categories) {
      const item = items.find((row) => row.category === category);
      if (item?.available && item.bytes > 0) pending.push(category);
      else if (cleanupItemEmpty(item))
        results.push({ category, state: "empty" });
      else results.push({ category, state: "failed" });
    }
    if (!pending.length) return results;
    if (!id) throw new Error();
    const { runId } = await clearResources(id);
    if (!runId) throw new Error();
    for (const category of pending)
      results.push({ category, state: "accepted", runId });
  } catch (error) {
    for (const category of categories) {
      if (!results.some((result) => result.category === category)) {
        results.push({ category, state: "failed", error });
      }
    }
  }
  return results;
}

async function confirmDesktop(
  category: DesktopStorageCategory,
): Promise<CleanupReceipt> {
  try {
    const item = await previewDesktopCleanup(category);
    if (cleanupItemEmpty(item)) return { category, state: "empty" };
    const clear = platform().host.clear_desktop_storage;
    if (!clear || !item.available) throw new Error();
    const { removedBytes } = await clear({ category });
    if (!measuredBytes(removedBytes)) throw new Error();
    return { category, state: "cleared", removedBytes };
  } catch (error) {
    return { category, state: "failed", error };
  }
}

/** 按真实清理所有者执行，局部读取或清理失败不阻断其它类别。 */
export async function confirmStorageCleanup(
  categories: StorageCleanupCategory[],
) {
  const results = await Promise.all([
    confirmCore(categories.filter(isCoreCleanup)),
    ...categories.filter(isDesktopCleanup).map(confirmDesktop),
  ]);
  return results.flat();
}
