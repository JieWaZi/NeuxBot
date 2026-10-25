import type { QueryClient } from "@tanstack/react-query";

export {
  createMemoryMutation,
  deleteMemoryMutation,
  getMemorySettingsOptions,
  getMemoryStatusOptions,
  listMemoriesOptions,
  listTimelineOptions,
  reindexMemoryIndexMutation,
  testMemoryEmbeddingMutation,
  updateMemoryMutation,
  updateMemorySettingsMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

const memoryQueryIDs = new Set([
  "getMemorySettings",
  "getMemoryStatus",
  "listMemories",
  "listTimeline",
]);

export async function invalidateMemoryQueries(queryClient: QueryClient) {
  await queryClient.invalidateQueries({
    predicate: (query) => {
      const descriptor = query.queryKey[0] as { _id?: string } | undefined;
      return Boolean(descriptor?._id && memoryQueryIDs.has(descriptor._id));
    },
  });
}
