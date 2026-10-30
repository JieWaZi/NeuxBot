import {
  infiniteQueryOptions,
  replaceEqualDeep,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";

import {
  getChannelThreadQueryKey as generatedGetChannelThreadQueryKey,
  listChannelsQueryKey as generatedListChannelsQueryKey,
  listChannelTimelineQueryKey as generatedListChannelTimelineQueryKey,
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
  GithubComJieWaZiNeuxBotServerApisChannelsChannelThreadDetail as ChannelThreadDetail,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelEvent as ChannelEvent,
  ListChannelsResponse,
  ListChannelThreadsResponse,
  ListChannelTimelineResponse,
  ListWakersResponse,
} from "@/shared/api/generated/types.gen";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import type { ChatEvent } from "@/shared/api/events";
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
      ...generatedListChannelTimelineQueryKey({
        path: { channelId, threadId },
      }),
      "infinite",
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
  scope: "pending" | "recovery" = "recovery",
) {
  return infiniteQueryOptions({
    queryKey: teamQueryKeys.timelinePages(channelId, threadId),
    initialPageParam: "",
    queryFn: async ({ pageParam, signal, client }) => {
      const cached = client.getQueryData<TeamTimelineData>(
        teamQueryKeys.timelinePages(channelId, threadId),
      );
      const index = cached?.pageParams.indexOf(pageParam);
      const page =
        index !== undefined && index >= 0 ? cached?.pages[index] : undefined;
      if (
        pageParam &&
        page &&
        !page.resources?.some((item) => mutableTimelineItem(item, scope))
      )
        return page;
      return readTeamTimelineWindow(
        channelId,
        threadId,
        pageParam,
        signal,
        page,
        pageParam ? undefined : cached,
        scope,
      );
    },
    structuralSharing: reconcileTeamTimeline,
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

type TeamTimelineData = InfiniteData<ListChannelTimelineResponse, string>;

function isTeamTimelineData(value: unknown): value is TeamTimelineData {
  return (
    typeof value === "object" &&
    value !== null &&
    "pages" in value &&
    Array.isArray(value.pages) &&
    "pageParams" in value &&
    Array.isArray(value.pageParams)
  );
}

type TeamTimelineItem = NonNullable<
  ListChannelTimelineResponse["resources"]
>[number];

function mutableTimelineItem(
  item: TeamTimelineItem,
  scope: "pending" | "recovery",
) {
  return Boolean(
    item.message?.id?.startsWith("channel-task-") ||
    (scope === "recovery" &&
      item.message?.actor === "team_member" &&
      !item.directDelivered),
  );
}

function compareTimelineItems(left: TeamTimelineItem, right: TeamTimelineItem) {
  const time = (left.message?.createdAt ?? 0) - (right.message?.createdAt ?? 0);
  if (time) return time;
  if (left.message?.role !== right.message?.role)
    return left.message?.role === "user" ? -1 : 1;
  const leftId = left.message?.id ?? "",
    rightId = right.message?.id ?? "";
  if (leftId !== rightId) return leftId < rightId ? -1 : 1;
  const leftChat = left.chatId ?? "",
    rightChat = right.chatId ?? "";
  return leftChat === rightChat ? 0 : leftChat < rightChat ? -1 : 1;
}

function timelineItemKey(item: TeamTimelineItem) {
  return `${item.chatId ?? ""}:${item.message?.id ?? ""}`;
}

/** 刷新仍保留已加载的持久正文与原窗口下界；只为未覆盖的缺口或可变投影续读有界页。 */
async function readTeamTimelineWindow(
  channelId: string,
  threadId: string,
  cursor: string,
  signal: AbortSignal | undefined,
  cached: ListChannelTimelineResponse | undefined,
  history: TeamTimelineData | undefined,
  scope: "pending" | "recovery",
): Promise<ListChannelTimelineResponse> {
  const fresh = await fetchTeamTimelinePage(
    channelId,
    threadId,
    cursor,
    signal,
  );
  if (!cached) return fresh;
  const knownCanonical = new Set(
    (
      history?.pages.flatMap((page) => page.resources ?? []) ??
      cached.resources ??
      []
    )
      .filter((item) => item.coordinator)
      .map(timelineItemKey),
  );
  const mutable = (cached.resources ?? [])
    .filter((item) => mutableTimelineItem(item, scope))
    .sort(compareTimelineItems);
  const floor = mutable[0];
  const incoming = [...(fresh.resources ?? [])];
  let tail = fresh;
  const needsBridge = Boolean(
    history &&
    knownCanonical.size &&
    (fresh.leadChatLatestSequence ?? 0) > (cached.leadChatLatestSequence ?? 0),
  );
  const visited = new Set<string>();
  const covered = () => {
    const overlaps = incoming.some(
      (item) => item.coordinator && knownCanonical.has(timelineItemKey(item)),
    );
    const oldest = [...incoming].sort(compareTimelineItems)[0];
    return (
      (!needsBridge || overlaps) &&
      (!floor || (oldest && compareTimelineItems(oldest, floor) <= 0))
    );
  };
  while (tail.hasMore && tail.nextCursor && !covered()) {
    if (visited.has(tail.nextCursor)) throw new Error("团队时间线游标未推进");
    visited.add(tail.nextCursor);
    tail = await fetchTeamTimelinePage(
      channelId,
      threadId,
      tail.nextCursor,
      signal,
    );
    incoming.push(...(tail.resources ?? []));
  }
  const resources = new Map<string, TeamTimelineItem>();
  for (const item of cached.resources ?? [])
    if (item.coordinator) resources.set(timelineItemKey(item), item);
  for (const item of incoming) resources.set(timelineItemKey(item), item);
  return {
    ...fresh,
    resources: [...resources.values()].sort(compareTimelineItems),
    hasMore: cached.hasMore,
    nextCursor: cached.nextCursor,
  };
}

/** 领域快照更新临时输入；迟到 HTTP 不能回退已提交的主 Chat 消息与游标。 */
function reconcileTeamTimeline(current: unknown, incoming: unknown) {
  if (!isTeamTimelineData(current) || !isTeamTimelineData(incoming))
    return incoming;
  const currentHead = current.pages[0];
  const incomingHead = incoming.pages[0];
  if (!currentHead || !incomingHead) return replaceEqualDeep(current, incoming);
  const known = new Map(
    current.pages
      .flatMap((page) => page.resources ?? [])
      .filter((item) => item.coordinator && item.message?.id)
      .map((item) => [item.message?.id, item]),
  );
  const incomingPages = [
    ...incoming.pages,
    ...current.pages.slice(incoming.pages.length),
  ];
  const pages = incomingPages.map((page) => ({
    ...page,
    resources: page.resources?.map((item) => {
      if (!item.coordinator || !item.message?.id) return item;
      const previous = known.get(item.message.id);
      if (!previous?.message) return item;
      const projected = previous.directDelivered
        ? { ...item, directDelivered: true }
        : item;
      const previousTerminal = ["completed", "failed", "canceled"].includes(
        previous.message.status ?? "",
      );
      const incomingTerminal = ["completed", "failed", "canceled"].includes(
        item.message.status ?? "",
      );
      return (previousTerminal && !incomingTerminal) ||
        (previous.message.revision ?? -1) > (item.message.revision ?? -1)
        ? { ...projected, message: previous.message }
        : projected;
    }),
  }));
  const head = pages[0];
  if (head) {
    const ids = new Set(head.resources?.map(timelineItemKey));
    const retained = (currentHead.resources ?? []).filter(
      (item) => item.coordinator && !ids.has(timelineItemKey(item)),
    );
    if (retained.length)
      head.resources = [...retained, ...(head.resources ?? [])].sort(
        compareTimelineItems,
      );
  }
  // 有界续读可以与已经加载的相邻窗口重叠，同一原消息只保留最新窗口的一份。
  const seen = new Set<string>();
  for (const page of pages)
    page.resources = page.resources?.filter((item) => {
      const key = timelineItemKey(item);
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  if (
    (currentHead.leadChatLatestSequence ?? 0) <=
    (incomingHead.leadChatLatestSequence ?? 0)
  )
    return replaceEqualDeep(current, {
      ...incoming,
      pages,
      pageParams: [
        ...incoming.pageParams,
        ...current.pageParams.slice(incoming.pageParams.length),
      ],
    });
  const resources = (pages[0]?.resources ?? []).map((item) =>
    item.coordinator && item.message?.id
      ? {
          ...item,
          message: known.get(item.message.id)?.message ?? item.message,
        }
      : item,
  );
  const ids = new Set(resources.map((item) => item.message?.id));
  resources.push(
    ...(currentHead.resources ?? []).filter(
      (item) => item.coordinator && !ids.has(item.message?.id),
    ),
  );
  return replaceEqualDeep(current, {
    ...incoming,
    pageParams: [
      ...incoming.pageParams,
      ...current.pageParams.slice(incoming.pageParams.length),
    ],
    pages: [
      {
        ...incomingHead,
        resources,
        leadChatLatestSequence: currentHead.leadChatLatestSequence,
      },
      ...pages.slice(1),
    ],
  });
}

/** 团队主 Chat 仅提交原时间线 Query；非主 Chat 投影仍由团队领域快照拥有。 */
export function commitTeamTimelineEvents(
  queryClient: QueryClient,
  channelId: string,
  threadId: string,
  chatId: string,
  leadWakerId: string,
  events: readonly ChatEvent[],
): number | undefined {
  const key = teamQueryKeys.timelinePages(channelId, threadId);
  const current = queryClient.getQueryData<TeamTimelineData>(key);
  const head = current?.pages[0];
  if (!current || !head) return undefined;
  let sequence = head.leadChatLatestSequence ?? 0;
  const updates = new Map<
    string,
    NonNullable<ListChannelTimelineResponse["resources"]>[number]
  >();
  for (const event of events) {
    if (event.transient) continue;
    const next = event.sequence;
    if (!Number.isSafeInteger(next) || next === undefined || next <= 0)
      return undefined;
    if (next <= sequence) continue;
    if (next !== sequence + 1) return undefined;
    sequence = next;
    const message = event.message;
    if (!message) continue;
    if (!message.id) return undefined;
    if (
      message.role !== "assistant" &&
      !(
        message.role === "user" &&
        ["user", "team_member"].includes(message.actor ?? "")
      )
    )
      continue;
    updates.set(message.id, {
      chatId,
      coordinator: true,
      wakerId:
        message.role === "assistant" ? leadWakerId : message.sourceWakerId,
      message: {
        ...message,
        createdAt: message.createdAt ?? event.recordedAt,
        updatedAt: message.updatedAt ?? event.recordedAt,
      },
    });
  }
  if (sequence === (head.leadChatLatestSequence ?? 0)) return sequence;
  const submissions = new Set(
    [...updates.values()]
      .map((item) => item.message?.submissionId)
      .filter(Boolean),
  );
  const added = new Map(updates);
  const pages = current.pages.map((page) => ({
    ...page,
    resources: page.resources
      ?.filter(
        (item) =>
          !(
            item.message?.id?.startsWith("channel-task-") &&
            item.message.submissionId &&
            submissions.has(item.message.submissionId)
          ),
      )
      .map((item) => {
        const update =
          item.chatId === chatId && item.message?.id
            ? updates.get(item.message.id)
            : undefined;
        if (!update) return item;
        added.delete(item.message?.id ?? "");
        return { ...update, directDelivered: item.directDelivered };
      }),
  }));
  queryClient.setQueryData<TeamTimelineData>(key, {
    ...current,
    pages: pages.map((page, index) =>
      index === 0
        ? {
            ...page,
            leadChatLatestSequence: sequence,
            resources: [...(page.resources ?? []), ...added.values()],
          }
        : page,
    ),
  });
  const assistant = [...updates.values()]
    .reverse()
    .find((item) => item.message?.role === "assistant")?.message;
  if (assistant?.executionId) {
    queryClient.setQueryData<ChannelThreadDetail>(
      generatedGetChannelThreadQueryKey({ path: { channelId, threadId } }),
      (detail) => {
        if (!detail) return detail;
        const terminal = ["completed", "failed", "canceled"].includes(
          assistant.status ?? "",
        );
        if (terminal && detail.leadActiveExecutionId !== assistant.executionId)
          return detail;
        return {
          ...detail,
          leadActiveExecutionId: terminal ? undefined : assistant.executionId,
        };
      },
    );
  }
  return sequence;
}

/** HTTP 恢复与领域变化使用共享实际 Query owner，期间新变化在命令完成前补读。 */
export async function reconcileTeamTimelineQuery(
  queryClient: QueryClient,
  channelId: string,
  threadId: string,
  scope: "pending" | "recovery" = "recovery",
  signal?: AbortSignal,
): Promise<number | undefined> {
  const key = teamQueryKeys.timelinePages(channelId, threadId);
  await refreshQueryFromEvent(
    queryClient,
    key,
    () => {
      const current = queryClient.getQueryData<TeamTimelineData>(key);
      return queryClient.fetchInfiniteQuery({
        ...teamTimelineInfiniteOptions(channelId, threadId, scope),
        staleTime: 0,
        getNextPageParam: (
          _lastPage: ListChannelTimelineResponse,
          pages: ListChannelTimelineResponse[],
        ) => current?.pageParams[pages.length],
      });
    },
    scope === "recovery" ? 1 : 0,
    signal,
  );
  return (
    queryClient.getQueryData<TeamTimelineData>(key)?.pages[0]
      ?.leadChatLatestSequence ?? 0
  );
}

/** 原团队领域事实只修改已有报告的交付投影，不保存未来消息的第二份状态。 */
export function commitTeamTimelineProjectionEvent(
  queryClient: QueryClient,
  channelId: string,
  threadId: string,
  event: ChannelEvent,
) {
  if (event.type !== "team.member.direct_delivered") return;
  let payload: unknown = event.payload;
  if (typeof payload === "string") {
    try {
      payload = JSON.parse(payload);
    } catch {
      return;
    }
  }
  if (
    !payload ||
    typeof payload !== "object" ||
    !("notificationId" in payload) ||
    typeof payload.notificationId !== "string"
  )
    return;
  const id = payload.notificationId;
  queryClient.setQueryData<TeamTimelineData>(
    teamQueryKeys.timelinePages(channelId, threadId),
    (current) =>
      current
        ? {
            ...current,
            pages: current.pages.map((page) => ({
              ...page,
              resources: page.resources?.map((item) =>
                item.message?.id === id && item.message.actor === "team_member"
                  ? { ...item, directDelivered: true }
                  : item,
              ),
            })),
          }
        : current,
  );
}
