import type { QueryClient } from "@tanstack/react-query";
import {
  listMemoriesQueryKey,
  listTimelineQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListMemoriesResponse } from "@/shared/api/generated/types.gen";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";

export {
  createMemoryMutation,
  deleteMemoryMutation,
  getMemorySettingsOptions,
  getMemoryStatusOptions,
  listMemoriesOptions,
  listTimelineOptions,
  reindexMemoryIndexMutation,
  setMemoryCoreMutation,
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

export function usePartnerMemoryEvents(wakerId: string) {
  const active = useRouteSceneActive();
  const path = { wakerId };
  useQueryEvents({
    topic: "resources",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [listMemoriesQueryKey({ path }), listTimelineQueryKey({ path })],
    eventTypes: ["partner.memory.changed"],
    enabled: Boolean(wakerId),
    active,
  });
}

export async function invalidatePartnerMemory(
  queryClient: QueryClient,
  wakerId: string,
) {
  const path = { wakerId };
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: listMemoriesQueryKey({ path }) }),
    queryClient.invalidateQueries({ queryKey: listTimelineQueryKey({ path }) }),
  ]);
}

export async function removeMemoryFromGroup(
  queryClient: QueryClient,
  wakerId: string,
  memoryId: string,
  core: boolean,
) {
  const selection = {
    queryKey: listMemoriesQueryKey({ path: { wakerId }, query: { core } }),
  };
  await queryClient.cancelQueries({
    queryKey: listMemoriesQueryKey({ path: { wakerId } }),
  });
  queryClient.setQueriesData<ListMemoriesResponse>(selection, (previous) => {
    const resources = previous?.resources?.filter(({ id }) => id !== memoryId);
    if (
      !previous ||
      !resources ||
      resources.length === previous.resources?.length
    )
      return previous;
    return {
      ...previous,
      resources,
      totalSize: Math.max(0, (previous.totalSize ?? 0) - 1),
    };
  });
}
