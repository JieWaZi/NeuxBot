import {
  type InfiniteData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import { subscribeChannel } from "@/shared/api/events";
import { ApiError } from "@/shared/api/errors";
import type {
  GithubComJieWaZiNeuxBotServerApisChannelsChannelThreadDetail as ChannelThreadDetail,
  GithubComJieWaZiNeuxBotServerServicesChannelTimelineMessage as ChannelTimelineMessage,
  ListChannelTimelineResponse,
} from "@/shared/api/generated/types.gen";
import {
  createChannelThreadMutation,
  fetchTeamTimelinePage,
  getChannelOptions,
  getChannelThreadOptions,
  getChannelThreadQueryKey,
  sendChannelMessageMutation,
  stopChannelWorkMutation,
  teamQueryKeys,
  teamTimelineInfiniteOptions,
  updateChannelThreadMemberConfigsMutation,
} from "../api/channel-queries";
import { TeamTaskBoard } from "../components/team-task-board";
import { projectTeamMembers } from "../model/team-member-projection";
import {
  teamEventNoticeOf,
  teamLiveProjectionReducer,
} from "../model/team-live-projection";
import type { TeamChatContext } from "@/modules/team";
import {
  reportErrorNotice,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import {
  createTeamThreadRefreshScheduler,
  teamLeadActivityOf,
  teamWorkStateOf,
} from "./team-thread-refresh";

export type TeamThreadChatContext = TeamChatContext;

type TeamThreadPageProps = {
  onThreadMissing?: (channelId: string, threadId: string) => void;
  renderChat: (props: {
    chatId?: string;
    routeTransitioning: boolean;
    teamContext: TeamThreadChatContext;
  }) => ReactNode;
};

function projectTeamTimeline(resources: ChannelTimelineMessage[]) {
  const projected: TeamThreadChatContext["timeline"] = [];
  const indexByMessageKey = new Map<string, number>();
  for (const item of resources) {
    if (!item.message) continue;
    const timelineItem = {
      chatId: item.chatId,
      coordinator: item.coordinator ?? false,
      directDelivered: item.directDelivered,
      message: item.message,
      wakerId: item.wakerId,
    };
    const messageId = item.message.id;
    if (messageId) {
      const messageKey = `${item.chatId ?? ""}:${messageId}`;
      const existingIndex = indexByMessageKey.get(messageKey);
      if (existingIndex !== undefined) {
        projected[existingIndex] = timelineItem;
        continue;
      }
      indexByMessageKey.set(messageKey, projected.length);
    }
    projected.push(timelineItem);
  }
  return projected;
}

export function TeamThreadPage({
  onThreadMissing,
  renderChat,
}: TeamThreadPageProps) {
  const [searchParams] = useSearchParams();
  const requestedTask = searchParams.get("task") ?? undefined;
  const { t } = useTranslation("team");
  const { channelId = "", threadId } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const updateMemberConfigs = useMutation(
    updateChannelThreadMemberConfigsMutation(),
  );
  const createThread = useMutation(createChannelThreadMutation());
  const sendMessage = useMutation(sendChannelMessageMutation());
  const stopWork = useMutation(stopChannelWorkMutation());
  const [liveProjection, dispatchLiveProjection] = useReducer(
    teamLiveProjectionReducer,
    undefined,
  );
  const handledMissingThreadRef = useRef("");
  const notifiedEventIdsRef = useRef(new Set<string>());
  const channelQuery = useQuery(
    getChannelOptions({ path: { channelId: channelId || "pending" } }),
  );
  const threadQuery = useQuery({
    ...getChannelThreadOptions({
      path: {
        channelId: channelId || "pending",
        threadId: threadId || "pending",
      },
    }),
    enabled: Boolean(channelId && threadId),
    retry: (failureCount, error) =>
      !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });
  const timelineQuery = useInfiniteQuery({
    ...teamTimelineInfiniteOptions(
      channelId || "pending",
      threadId || "pending",
    ),
    enabled: Boolean(channelId && threadId),
  });
  useEffect(() => {
    const snapshot = threadQuery.data;
    if (!snapshot || snapshot.thread?.id !== threadId) return;
    dispatchLiveProjection({ type: "snapshot", snapshot });
  }, [threadId, threadQuery.data]);
  const projectedThreadDetail =
    liveProjection && liveProjection.view.thread?.id === threadId
      ? liveProjection.view
      : threadQuery.data;
  const eventCursorRef = useRef(projectedThreadDetail?.eventCursor ?? "");
  useLayoutEffect(() => {
    eventCursorRef.current = projectedThreadDetail?.eventCursor ?? "";
  }, [projectedThreadDetail?.eventCursor]);
  useEffect(() => {
    const error = threadQuery.error;
    const missingKey = `${channelId}:${threadId ?? ""}`;
    if (
      !threadId ||
      !(error instanceof ApiError) ||
      error.status !== 404 ||
      handledMissingThreadRef.current === missingKey
    ) {
      return;
    }
    handledMissingThreadRef.current = missingKey;
    onThreadMissing?.(channelId, threadId);
    navigate(`/teams/${channelId}?draftId=${crypto.randomUUID()}`, {
      replace: true,
    });
  }, [channelId, navigate, onThreadMissing, threadId, threadQuery.error]);
  const members = useMemo(
    () => channelQuery.data?.members ?? [],
    [channelQuery.data?.members],
  );
  const leadWakerId =
    projectedThreadDetail?.thread?.leadWakerId ??
    channelQuery.data?.channel?.leadWakerId ??
    "";
  const mainChatId = projectedThreadDetail?.thread?.leadChatId;
  const hasEarlierTimelineMessages = Boolean(timelineQuery.hasNextPage);
  const isLoadingEarlierTimelineMessages = timelineQuery.isFetchingNextPage;

  const loadEarlierTimelineMessages = useCallback(async () => {
    if (!threadId || !hasEarlierTimelineMessages) {
      return;
    }
    await timelineQuery.fetchNextPage();
  }, [hasEarlierTimelineMessages, threadId, timelineQuery]);

  const refreshLatestTimeline = useCallback(async () => {
    if (!threadId) return;
    try {
      const latestPage = await fetchTeamTimelinePage(channelId, threadId);
      queryClient.setQueryData<
        InfiniteData<ListChannelTimelineResponse, string>
      >(teamQueryKeys.timelinePages(channelId, threadId), (current) =>
        current
          ? {
              ...current,
              pages: [latestPage, ...current.pages.slice(1)],
              pageParams: ["", ...current.pageParams.slice(1)],
            }
          : { pages: [latestPage], pageParams: [""] },
      );
    } catch {
      // 实时刷新失败时保留当前可读快照；SSE 重连或用户重试会再次拉取。
    }
  }, [channelId, queryClient, threadId]);

  const refreshThreadDetail = useCallback(() => {
    if (threadId) {
      void queryClient.invalidateQueries({
        queryKey: getChannelThreadQueryKey({
          path: { channelId, threadId },
        }),
      });
      void refreshLatestTimeline();
    }
  }, [channelId, queryClient, refreshLatestTimeline, threadId]);

  const refreshThreadNavigation = useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: teamQueryKeys.threads(channelId),
    });
    void queryClient.invalidateQueries({
      queryKey: teamQueryKeys.channels(),
    });
    void queryClient.invalidateQueries({
      queryKey: teamQueryKeys.channelPages(),
    });
  }, [channelId, queryClient]);

  const refreshThread = useCallback(() => {
    refreshThreadDetail();
    refreshThreadNavigation();
  }, [refreshThreadDetail, refreshThreadNavigation]);

  const reconcileThreadSnapshot = useCallback(async () => {
    if (!threadId) return "";
    dispatchLiveProjection({ type: "reconnect" });
    const snapshot = await queryClient.fetchQuery({
      ...getChannelThreadOptions({ path: { channelId, threadId } }),
      staleTime: 0,
    });
    dispatchLiveProjection({ type: "snapshot", snapshot });
    await refreshLatestTimeline();
    refreshThreadNavigation();
    return snapshot.eventCursor ?? "";
  }, [
    channelId,
    queryClient,
    refreshLatestTimeline,
    refreshThreadNavigation,
    threadId,
  ]);

  const eventRefreshScheduler = useMemo(
    () =>
      createTeamThreadRefreshScheduler({
        refreshDetail: refreshThreadDetail,
        refreshNavigation: refreshThreadNavigation,
      }),
    [refreshThreadDetail, refreshThreadNavigation],
  );

  useEffect(() => () => eventRefreshScheduler.clear(), [eventRefreshScheduler]);

  useEffect(() => {
    if (!threadId || projectedThreadDetail?.thread?.id !== threadId) return;
    const streamOperation = `team-stream:${threadId}`;
    let recovery = captureErrorNoticeRecovery(streamOperation);
    const unsubscribe = subscribeChannel(channelId, threadId, {
      after: eventCursorRef.current,
      onEvent: (event) => {
        resolveErrorNoticeRecovery(recovery);
        dispatchLiveProjection({ type: "event", event });
        eventRefreshScheduler.push(event.type);
        const notice = teamEventNoticeOf(event);
        const eventId = event.id ?? "";
        if (!notice || !eventId || notifiedEventIdsRef.current.has(eventId)) {
          return;
        }
        notifiedEventIdsRef.current.add(eventId);
        if (notifiedEventIdsRef.current.size > 512) {
          const oldest = notifiedEventIdsRef.current.values().next().value;
          if (oldest) notifiedEventIdsRef.current.delete(oldest);
        }
        reportErrorNotice({
          id: `team-event:${eventId}`,
          scope: `team:${threadId}`,
          message: t(`eventNotice.${notice}`),
        });
      },
      reconcileCursor: reconcileThreadSnapshot,
      onReconnect: () => resolveErrorNoticeRecovery(recovery),
      onUnavailable: () => {
        reportErrorNotice({
          id: `team-stream:${threadId}`,
          scope: `team:${threadId}`,
          operation: streamOperation,
          message: t("chat:runtimeFailure.networkError"),
        });
        recovery = captureErrorNoticeRecovery(streamOperation);
        refreshThread();
      },
    });
    return unsubscribe;
  }, [
    channelId,
    eventRefreshScheduler,
    projectedThreadDetail?.thread?.id,
    reconcileThreadSnapshot,
    refreshThread,
    t,
    threadId,
  ]);

  const channel = channelQuery.data?.channel;
  const thread = projectedThreadDetail?.thread;
  const channelDetailLoading = Boolean(
    channelId && !channel && !channelQuery.isError,
  );
  // 与伙伴 draft 一致：channel 在后台补齐，不阻塞消息区；仅 thread 快照Hydration 时使用 skeleton。
  const threadDetailLoading = Boolean(
    threadId && !thread && !threadQuery.isError,
  );

  const loadError =
    channelQuery.isError || (channelQuery.isFetched && !channel)
      ? {
          message: t("chatLoadError"),
          error: channelQuery.error,
          onRetry: () => void channelQuery.refetch(),
        }
      : threadId && (threadQuery.isError || (threadQuery.isSuccess && !thread))
        ? {
            message: t("chatLoadError"),
            error: threadQuery.error,
            onRetry: () => {
              void channelQuery.refetch();
              void threadQuery.refetch();
              void timelineQuery.refetch();
            },
          }
        : timelineQuery.isError
          ? {
              message: t("chatLoadError"),
              error: timelineQuery.error,
              onRetry: () => void timelineQuery.refetch(),
            }
          : undefined;

  const resolvedChannel = channel ?? { id: channelId, name: t("title") };
  const tasks = projectedThreadDetail?.tasks ?? [];
  const leadActivity = teamLeadActivityOf(projectedThreadDetail);
  const isStopping = Boolean(thread?.cancelingAt) || stopWork.isPending;
  const workState = isStopping
    ? "running"
    : teamWorkStateOf(tasks, leadActivity);
  const teamMembers = useMemo(
    () =>
      projectTeamMembers({
        channelMembers: members,
        fallbackName: t("member"),
        leadWakerId,
        threadDetail: threadId ? projectedThreadDetail : undefined,
      }),
    [leadWakerId, members, projectedThreadDetail, t, threadId],
  );
  const timeline = projectTeamTimeline(
    [...(timelineQuery.data?.pages ?? [])]
      .reverse()
      .flatMap((page) => page.resources ?? []),
  );
  const stopCurrentWork = async () => {
    if (!threadId || isStopping) return;
    try {
      await stopWork.mutateAsync({ path: { channelId, threadId } });
    } finally {
      refreshThread();
    }
  };
  const terminalTaskCount = tasks.filter(
    (task) =>
      task.status === "completed" ||
      task.status === "failed" ||
      task.status === "cancelled",
  ).length;
  const reviewTaskCount = tasks.filter(
    (task) => task.status === "in_review",
  ).length;
  const activeTask =
    tasks.find((task) => task.status === "in_progress") ??
    tasks.find((task) => task.status === "pending");
  const activeMember = teamMembers.find(
    (member) => member.id === activeTask?.assigneeWakerId,
  );
  const workPhase: TeamThreadChatContext["workProgress"]["phase"] = isStopping
    ? "stopping"
    : workState === "idle"
      ? "idle"
      : workState === "review"
        ? "review"
        : activeTask
          ? "executing"
          : leadActivity.summarizing
            ? "summarizing"
            : "idle";
  const teamContext: TeamThreadChatContext = {
    channelId,
    channelDetailLoading,
    description: resolvedChannel.description,
    leadActiveExecutionId: leadActivity.activeExecutionId,
    leadProcessing: leadActivity.summarizing,
    leadWakerId,
    members: teamMembers,
    name: resolvedChannel.name ?? t("title"),
    timeline,
    hasEarlierTimelineMessages,
    isLoadingEarlierTimelineMessages,
    onLoadEarlierTimelineMessages: loadEarlierTimelineMessages,
    onCreateThread: async (request) => {
      const result = await createThread.mutateAsync({
        path: { channelId },
        body: request,
      });
      if (result.thread?.id) {
        // 首发响应已经给出稳定身份和成员快照，导航无需再次等待详情 GET。
        queryClient.setQueryData<ChannelThreadDetail>(
          getChannelThreadQueryKey({
            path: { channelId, threadId: result.thread.id },
          }),
          (previous) => ({
            ...previous,
            thread: result.thread,
            members: result.members,
            tasks: result.tasks,
          }),
        );
      }
      return result;
    },
    onSendMessage: (request) => {
      if (!threadId) return Promise.reject(new Error("missing team thread"));
      if (isStopping) return Promise.reject(new Error("team work is stopping"));
      return sendMessage.mutateAsync({
        path: { channelId, threadId },
        body: request,
      });
    },
    onStopWork: stopCurrentWork,
    onThreadChanged: refreshThread,
    onThreadCreated: (createdThreadId) => {
      refreshThreadNavigation();
      navigate(`/teams/${channelId}/threads/${createdThreadId}`, {
        replace: true,
      });
    },
    onUpdateMemberConfigs: async (nextMembers) => {
      if (!threadId) return;
      const queryKey = getChannelThreadQueryKey({
        path: { channelId, threadId },
      });
      const previous = queryClient.getQueryData<ChannelThreadDetail>(queryKey);
      const updates = new Map(
        nextMembers.map((member) => [member.wakerId, member] as const),
      );
      queryClient.setQueryData<ChannelThreadDetail>(queryKey, (current) =>
        current
          ? {
              ...current,
              members: current.members?.map((member) => {
                const update = updates.get(member.wakerId ?? "");
                return update ? { ...member, ...update } : member;
              }),
            }
          : current,
      );
      try {
        await updateMemberConfigs.mutateAsync({
          path: { channelId, threadId },
          body: { members: nextMembers },
        });
        await queryClient.invalidateQueries({ queryKey });
      } catch (error) {
        queryClient.setQueryData(queryKey, previous);
        throw error;
      }
    },
    renderTaskBoard: () => (
      <TeamTaskBoard
        selectedTaskId={requestedTask}
        channelId={channelId}
        actionsDisabled={isStopping}
        members={teamMembers}
        onChanged={refreshThread}
        tasks={tasks}
        threadId={threadId}
        loading={threadDetailLoading}
        errorMessage={loadError?.message}
        onRetry={loadError?.onRetry}
      />
    ),
    loadError,
    isStopping,
    threadId,
    workspaceDirectory: projectedThreadDetail?.thread?.workspaceDirectory,
    additionalWorkingDirectories:
      projectedThreadDetail?.thread?.additionalWorkingDirectories,
    workProgress: {
      activeMemberName: activeMember?.name,
      activeTaskSubject: activeTask?.subject,
      phase: workPhase,
      review: reviewTaskCount,
      terminal: terminalTaskCount,
      total: tasks.length,
    },
    workState,
    threadDetailLoading,
    timelineLoading: Boolean(threadId && timelineQuery.isPending),
  };

  return (
    <>
      {renderChat({
        chatId: mainChatId,
        routeTransitioning: false,
        teamContext,
      })}
    </>
  );
}
