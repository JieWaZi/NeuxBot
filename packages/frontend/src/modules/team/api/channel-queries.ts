import { infiniteQueryOptions } from "@tanstack/react-query";

import {
  listChannelsQueryKey as generatedListChannelsQueryKey,
  listChannelThreadsQueryKey as generatedListChannelThreadsQueryKey,
  listWakersQueryKey as generatedListWakersQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  listChannels,
  listChannelTimeline,
  listChannelThreads,
  listWakers,
} from "@/shared/api/generated/sdk.gen";
import type {
  ListChannelsResponse,
  ListChannelThreadsResponse,
  ListChannelTimelineResponse,
  ListWakersResponse,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";

export {
  createChannelMutation,
  createChannelTaskMutation,
  createChannelThreadMutation,
  deleteChannelMutation,
  deleteChannelThreadMutation,
  followUpChannelTaskMutation,
  getChannelOptions,
  getChannelQueryKey,
  getChannelThreadOptions,
  getChannelThreadQueryKey,
  listChannelEventsOptions,
  listChannelEventsQueryKey,
  listChannelTimelineOptions,
  listChannelsOptions,
  listChannelsQueryKey,
  listChannelThreadsOptions,
  listChannelThreadsQueryKey,
  reviewChannelTaskMutation,
  sendChannelMessageMutation,
  stopChannelTaskMutation,
  stopChannelWorkMutation,
  updateChannelMutation,
  updateChannelThreadMemberConfigsMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

const teamWakersPageSize = 30;
const teamChannelsPageSize = 30;
const teamThreadsPageSize = 50;

export const teamQueryKeys = {
  channels: () => generatedListChannelsQueryKey(),
  channelPages: () => [...generatedListChannelsQueryKey(), "infinite"] as const,
  threads: (channelId: string) =>
    generatedListChannelThreadsQueryKey({ path: { channelId } }),
  threadPages: (channelId: string) =>
    [
      ...generatedListChannelThreadsQueryKey({ path: { channelId } }),
      "infinite",
    ] as const,
  timelinePages: (channelId: string, threadId: string) =>
    [
      ...generatedListChannelThreadsQueryKey({ path: { channelId } }),
      "timeline",
      threadId,
    ] as const,
  wakers: () => generatedListWakersQueryKey(),
  wakerPages: (query: string) =>
    [
      ...generatedListWakersQueryKey({
        query: { query: query || undefined },
      }),
      "infinite",
    ] as const,
};

export async function fetchTeamTimelinePage(
  channelId: string,
  threadId: string,
  cursor = "",
  signal?: AbortSignal,
): Promise<ListChannelTimelineResponse> {
  const { data } = await listChannelTimeline({
    path: { channelId, threadId },
    query: { cursor: cursor || undefined, limit: 50 },
    signal,
    throwOnError: true,
  });
  return data;
}

export function teamChannelsInfiniteOptions() {
  return infiniteQueryOptions({
    queryKey: teamQueryKeys.channelPages(),
    initialPageParam: 1,
    queryFn: async ({ pageParam, signal }) => {
      const { data } = await listChannels({
        query: {
          pageNum: pageParam,
          pageSize: teamChannelsPageSize,
        },
        signal,
        throwOnError: true,
      });
      return data;
    },
    getNextPageParam: (lastPage: ListChannelsResponse) => {
      const pageNum = lastPage.pageNum ?? 1;
      const pageSize = lastPage.pageSize ?? teamChannelsPageSize;
      return pageNum * pageSize < (lastPage.totalSize ?? 0)
        ? pageNum + 1
        : undefined;
    },
  });
}

export function teamThreadsInfiniteOptions(channelId: string) {
  return infiniteQueryOptions({
    queryKey: teamQueryKeys.threadPages(channelId),
    initialPageParam: "",
    queryFn: async ({ pageParam, signal }) => {
      const { data } = await listChannelThreads({
        path: { channelId },
        query: {
          cursor: pageParam || undefined,
          pageSize: teamThreadsPageSize,
        },
        signal,
        throwOnError: true,
      });
      return data;
    },
    getNextPageParam: (lastPage: ListChannelThreadsResponse) => {
      return lastPage.hasMore && lastPage.nextCursor
        ? lastPage.nextCursor
        : undefined;
    },
  });
}

export function teamTimelineInfiniteOptions(
  channelId: string,
  threadId: string,
) {
  return infiniteQueryOptions({
    queryKey: teamQueryKeys.timelinePages(channelId, threadId),
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      fetchTeamTimelinePage(channelId, threadId, pageParam, signal),
    getNextPageParam: (lastPage) =>
      lastPage.hasMore && lastPage.nextCursor ? lastPage.nextCursor : undefined,
  });
}

export function teamWakersInfiniteOptions(query: string) {
  return infiniteQueryOptions({
    queryKey: teamQueryKeys.wakerPages(query),
    initialPageParam: 1,
    queryFn: async ({ pageParam, signal }) => {
      const { data } = await listWakers(
        withLocalErrorSurface({
          query: {
            pageNum: pageParam,
            pageSize: teamWakersPageSize,
            query: query || undefined,
          },
          signal,
          throwOnError: true,
        }),
      );
      if (!data) throw new Error("伙伴列表返回为空");
      return data;
    },
    getNextPageParam: (lastPage: ListWakersResponse) => {
      const pageNum = lastPage.pageNum ?? 1;
      const pageSize = lastPage.pageSize ?? teamWakersPageSize;
      return pageNum * pageSize < (lastPage.totalSize ?? 0)
        ? pageNum + 1
        : undefined;
    },
  });
}
