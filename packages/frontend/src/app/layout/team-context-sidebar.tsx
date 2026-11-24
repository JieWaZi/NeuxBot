import {
  localizedDateTimeFormat,
  localizedRelativeTimeFormat,
} from "@/shared/i18n/format";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Folder, LoaderCircle } from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";

import {
  TeamMemberStrip,
  type TeamMemberStripItem,
} from "@/modules/chat/components/team-member-strip";
import { teamMemberActivityOf } from "@/modules/chat/model/team-member-activity";
import {
  deleteChannelThreadMutation,
  getChannelOptions,
  getChannelThreadQueryKey,
  getChannelThreadOptions,
  teamQueryKeys,
  teamThreadsInfiniteOptions,
} from "@/modules/team/api/channel-queries";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { TeamConfirmationDialog } from "@/modules/team/components/team-confirmation-dialog";
import { projectTeamMembers } from "@/modules/team/model/team-member-projection";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelThread as ChannelThread } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

import type { ChatAttention } from "@/modules/workspaceattention";
import { captureChatOpenIntent } from "@/shared/api/chat-reading";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  workAttentionHref,
  waitingConversations,
  WorkRequestChooser,
  type WaitingConversation,
} from "@/modules/work-board";
import { navigateRouteImmediately } from "./immediate-route-navigation";
import { ChatGroupHeader, ChatHistoryNavigationRow } from "@/modules/chat";
import {
  forgetTeamThread,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";

const relativeTimeFormat = localizedRelativeTimeFormat({
  numeric: "auto",
});

function formatRelativeTime(value?: number) {
  if (!value) return "";
  const delta = value - Date.now();
  const minute = 60_000;
  const hour = minute * 60;
  const day = hour * 24;
  if (Math.abs(delta) < minute) return relativeTimeFormat.format(0, "minute");
  if (Math.abs(delta) < hour) {
    return relativeTimeFormat.format(Math.round(delta / minute), "minute");
  }
  if (Math.abs(delta) < day) {
    return relativeTimeFormat.format(Math.round(delta / hour), "hour");
  }
  if (Math.abs(delta) < day * 14) {
    return relativeTimeFormat.format(Math.round(delta / day), "day");
  }
  return localizedDateTimeFormat({
    month: "numeric",
    day: "numeric",
  }).format(new Date(value));
}

type TeamThreadGroup = {
  key: string;
  label: string;
  threads: ChannelThread[];
};

function teamThreadGroupOf(
  thread: ChannelThread,
  labels: { temporaryFolder: string; unnamedProject: string },
) {
  const projectId = thread.projectId?.trim();
  const projectName = thread.projectName?.trim();
  if (projectId) {
    return {
      key: `project:${projectId}`,
      label: projectName || labels.unnamedProject,
    };
  }
  return {
    key: "folder:temporary",
    label: labels.temporaryFolder,
  };
}

function groupTeamThreads(
  threads: readonly ChannelThread[],
  labels: { temporaryFolder: string; unnamedProject: string },
) {
  const groups = new Map<string, TeamThreadGroup>();
  for (const thread of threads) {
    if (!thread.id) continue;
    const group = teamThreadGroupOf(thread, labels);
    const current = groups.get(group.key);
    if (current) current.threads.push(thread);
    else groups.set(group.key, { ...group, threads: [thread] });
  }
  for (const group of groups.values()) {
    group.threads.sort(
      (left, right) => (right.updatedAt ?? 0) - (left.updatedAt ?? 0),
    );
  }
  return [...groups.values()].sort((left, right) =>
    left.label.localeCompare(right.label, "zh-CN"),
  );
}

function teamLocationHref(channelId: string, threadId: string | undefined) {
  return threadId
    ? `/teams/${channelId}/threads/${threadId}`
    : `/teams/${channelId}`;
}

type TeamNavigationProps = {
  attention?: ChatAttention[];
  channelId: string;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  preferences: PartnerNavigationPreferences;
  threadId?: string;
};

export function TeamMemberMenu({
  channelId,
  onMemberOpened,
  threadId,
}: Pick<TeamNavigationProps, "channelId" | "threadId"> & {
  onMemberOpened?: () => void;
}) {
  const { t } = useTranslation("team");
  const [searchParams] = useSearchParams();
  const draftId = searchParams.get("draftId") ?? "default";
  const workspaceScopeKey = threadId
    ? `team:${channelId}:${threadId}`
    : `team:${channelId}:draft:${draftId}`;
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const channelQuery = useQuery(
    getChannelOptions({ path: { channelId: channelId || "pending" } }),
  );
  const activeThreadQuery = useQuery({
    ...getChannelThreadOptions({
      path: {
        channelId: channelId || "pending",
        threadId: threadId || "pending",
      },
    }),
    enabled: Boolean(channelId && threadId),
  });
  const activeThread = activeThreadQuery.data?.thread;
  const leadWakerId =
    activeThread?.leadWakerId ?? channelQuery.data?.channel?.leadWakerId ?? "";
  const teamMembers = useMemo(
    () =>
      projectTeamMembers({
        channelMembers: channelQuery.data?.members ?? [],
        fallbackName: t("member"),
        leadWakerId,
        roster: "navigation",
        threadDetail: threadId ? activeThreadQuery.data : undefined,
      }),
    [
      activeThreadQuery.data,
      channelQuery.data?.members,
      leadWakerId,
      t,
      threadId,
    ],
  );
  const memberStripItems = useMemo<TeamMemberStripItem[]>(
    () =>
      teamMembers.map((member) => ({
        avatar: member.avatar,
        chatId: member.chatId,
        id: member.id,
        name: member.name,
        selectable: !threadId || Boolean(member.chatId),
        phase: teamMemberActivityOf({
          activeExecutionId: member.activeExecutionId,
          leadCoordinating: Boolean(
            member.isLead &&
            (activeThreadQuery.data?.leadProcessing ||
              (activeThreadQuery.data?.leadActiveExecutionId &&
                member.taskStatus !== "in_progress")),
          ),
          taskStatus: member.taskStatus,
          waitingApproval: member.waitingApproval,
          waitingElicitation: member.waitingElicitation,
        }),
      })),
    [
      activeThreadQuery.data?.leadActiveExecutionId,
      activeThreadQuery.data?.leadProcessing,
      teamMembers,
      threadId,
    ],
  );
  const activeWorkspaceTab = workspace.snapshot.tabs.find(
    (tab) => tab.id === workspace.snapshot.activeTabId,
  );
  const activeMemberId =
    activeWorkspaceTab?.kind === "member"
      ? activeWorkspaceTab.wakerId
      : activeWorkspaceTab?.kind === "chat"
        ? leadWakerId
        : undefined;
  const openTeamMember = (member: TeamMemberStripItem) => {
    if (member.id === leadWakerId) {
      const mainTab = workspace.snapshot.tabs.find(
        (tab) => tab.kind === "chat",
      );
      if (mainTab) {
        workspace.dispatch({ type: "select-tab", tabId: mainTab.id });
      }
      onMemberOpened?.();
      return;
    }
    workspace.dispatch({
      type: "open-member",
      member: {
        chatId: member.chatId,
        label: member.name,
        wakerId: member.id,
      },
    });
    onMemberOpened?.();
  };

  return (
    <div className="flex min-w-0 flex-col">
      <TeamMemberStrip
        activeMemberId={activeMemberId}
        loading={Boolean(
          (!channelQuery.data && !channelQuery.isError) ||
          (threadId && !activeThreadQuery.data && !activeThreadQuery.isError),
        )}
        members={memberStripItems}
        onOpenMember={openTeamMember}
      />
    </div>
  );
}

export function TeamChatTree({
  channelId,
  attention,
  onPreferencesChange,
  preferences,
  threadId,
}: TeamNavigationProps) {
  const { t } = useTranslation("team");
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const draftId = searchParams.get("draftId") ?? "default";
  const workspaceScopeKey = threadId
    ? `team:${channelId}:${threadId}`
    : `team:${channelId}:draft:${draftId}`;
  const workspace = useSceneWorkspace(workspaceScopeKey);
  const [deleteTarget, setDeleteTarget] = useState<{
    id: string;
    title: string;
  }>();
  const [pendingRequests, setPendingRequests] =
    useState<WaitingConversation[]>();
  const [deleteError, setDeleteError] = useState("");
  const removeThread = useMutation(
    withLocalErrorMutation(deleteChannelThreadMutation()),
  );
  const historyOptions = teamThreadsInfiniteOptions(channelId || "pending");
  const threadsQuery = useInfiniteQuery({
    ...historyOptions,
    enabled: Boolean(channelId),
  });
  useQueryEvents({
    topic: "entities",
    resource: { kind: "global" },
    queryKeys: [historyOptions.queryKey],
    enabled: Boolean(channelId),
    eventTypes: ["entities.activity.changed"],
    eventFilter: ({ resource }) =>
      resource.kind === "global" ||
      (resource.kind === "channel" && resource.id === channelId),
  });
  const activeThreadQuery = useQuery({
    ...getChannelThreadOptions({
      path: {
        channelId: channelId || "pending",
        threadId: threadId || "pending",
      },
    }),
    enabled: Boolean(channelId && threadId),
  });
  const threads = useMemo(
    () =>
      threadsQuery.data?.pages.flatMap((page) => page.resources ?? []) ?? [],
    [threadsQuery.data?.pages],
  );
  const groups = useMemo(
    () =>
      groupTeamThreads(threads, {
        temporaryFolder: t("conversationGroups.temporaryDirectory", {
          ns: "common",
        }),
        unnamedProject: t("conversationGroups.unnamedProject", {
          ns: "common",
        }),
      }),
    [t, threads],
  );
  const preferenceKey = `team:${channelId}`;
  const collapsedGroups = new Set(
    preferences.collapsedGroups[preferenceKey] ?? [],
  );
  const activeThread = activeThreadQuery.data?.thread;
  const activeGroupKey = activeThread
    ? teamThreadGroupOf(activeThread, {
        temporaryFolder: t("conversationGroups.temporaryDirectory", {
          ns: "common",
        }),
        unnamedProject: t("conversationGroups.unnamedProject", {
          ns: "common",
        }),
      }).key
    : "";
  const autoExpanded = useRef("");

  useEffect(() => {
    if (!threadId || !activeGroupKey) return;
    const marker = `${preferenceKey}:${threadId}:${activeGroupKey}`;
    if (autoExpanded.current === marker) return;
    autoExpanded.current = marker;
    onPreferencesChange((current) => ({
      ...current,
      collapsedGroups: {
        ...current.collapsedGroups,
        [preferenceKey]: (current.collapsedGroups[preferenceKey] ?? []).filter(
          (key) => key !== activeGroupKey,
        ),
      },
    }));
  }, [activeGroupKey, onPreferencesChange, preferenceKey, threadId]);

  const toggleGroup = (groupKey: string) => {
    onPreferencesChange((current) => {
      const next = new Set(current.collapsedGroups[preferenceKey] ?? []);
      if (next.has(groupKey)) next.delete(groupKey);
      else next.add(groupKey);
      return {
        ...current,
        collapsedGroups: {
          ...current.collapsedGroups,
          [preferenceKey]: [...next],
        },
      };
    });
  };
  const removeThreadPreferences = (targetThreadId: string) => {
    onPreferencesChange((current) =>
      forgetTeamThread(current, channelId, targetThreadId),
    );
  };

  const threadRow = (thread: ChannelThread) => {
    const targetThreadId = thread.id ?? "";
    const title = thread.title || t("chatNavigation.unnamedChat");
    const chatAttention = attention?.find(
      (entry) => entry.work?.id === targetThreadId,
    );
    const choices = waitingConversations(chatAttention?.requests);
    const first = choices[0];
    const question = first?.request.chatId
      ? chatAttention?.requests?.find(
          (request) =>
            request.kind === "input" && request.chatId === first.request.chatId,
        )
      : undefined;
    const href =
      choices.length === 1 && first
        ? workAttentionHref(question ?? first.request)
        : teamLocationHref(channelId, targetThreadId);
    const status =
      thread.activityState === "running" || thread.activityState === "waiting"
        ? thread.activityState
        : "idle";
    return (
      <ChatHistoryNavigationRow
        key={targetThreadId}
        active={targetThreadId === threadId}
        title={title}
        status={status}
        runtimeId={thread.leadRuntimeId}
        modelId={thread.leadModel}
        requestKinds={chatAttention?.requestKinds}
        unreadCount={chatAttention?.unreadMessageCount}
        href={href}
        metadata={
          thread.updatedAt ? (
            <time className="text-content-secondary type-caption shrink-0 whitespace-nowrap tabular-nums">
              {formatRelativeTime(thread.updatedAt)}
            </time>
          ) : null
        }
        allowRename={false}
        onAction={() => {
          setDeleteError("");
          setDeleteTarget({ id: targetThreadId, title });
        }}
        onOpen={(event) => {
          if (
            choices.length > 1 &&
            event.button === 0 &&
            !event.ctrlKey &&
            !event.metaKey &&
            !event.shiftKey &&
            !event.altKey
          ) {
            event.preventDefault();
            setPendingRequests(choices);
            return;
          }
          navigateRouteImmediately(event, navigate, href, () =>
            !first && thread.leadChatId
              ? captureChatOpenIntent(queryClient, thread.leadChatId)
              : undefined,
          );
        }}
      />
    );
  };

  return (
    <>
      <div
        className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-transparent"
        data-chat-tree="team"
      >
        <div className="scrollbar-hidden min-h-0 flex-1 overflow-x-hidden overflow-y-auto overscroll-contain pr-1 pb-1">
          {threadsQuery.isPending ? (
            <div className="text-muted-foreground type-caption flex items-center gap-2 px-2 py-4">
              <LoaderCircle className="size-icon-sm animate-spin" />
              {t("chatNavigation.loading")}
            </div>
          ) : threadsQuery.isLoadingError ? (
            <div className="px-2 py-4">
              <p className="text-muted-foreground type-caption">
                {t("chatNavigation.loadError")}
              </p>
              <Button
                className="mt-2"
                size="sm"
                variant="ghost"
                onClick={() => void threadsQuery.refetch()}
              >
                {t("common:retry")}
              </Button>
            </div>
          ) : groups.length === 0 ? (
            <p className="text-muted-foreground type-caption px-2 py-4">
              {t("chatNavigation.empty")}
            </p>
          ) : (
            groups.map((group, index) => (
              <div className="mt-3 first:mt-0" key={group.key}>
                <ChatGroupHeader
                  action={
                    index === 0
                      ? {
                          current: location.pathname.endsWith("/chats"),
                          href: `/teams/${channelId}/chats`,
                          label: t("chatNavigation.table"),
                        }
                      : undefined
                  }
                  collapsed={collapsedGroups.has(group.key)}
                  icon={Folder}
                  label={group.label}
                  onToggle={() => toggleGroup(group.key)}
                />
                {!collapsedGroups.has(group.key) ? (
                  <div className="mt-1 grid gap-0.5 pl-2">
                    {group.threads.map((thread) => threadRow(thread))}
                  </div>
                ) : null}
              </div>
            ))
          )}
          <ResourceInfiniteScroll
            hasMore={threadsQuery.hasNextPage}
            pending={threadsQuery.isFetching}
            error={
              threadsQuery.isFetchNextPageError ? threadsQuery.error : undefined
            }
            onLoadMore={() => void threadsQuery.fetchNextPage()}
          />
        </div>
      </div>
      <WorkRequestChooser
        choices={pendingRequests}
        onClose={() => setPendingRequests(undefined)}
        onOpen={(href) => {
          navigate(href, { flushSync: true });
        }}
      />
      <TeamConfirmationDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(undefined);
        }}
        title={t("chatNavigation.deleteDialog.title")}
        description={t("chatNavigation.deleteDialog.description", {
          title: deleteTarget?.title ?? "",
        })}
        errorMessage={deleteError}
        confirmLabel={t("chatNavigation.deleteDialog.confirm")}
        isPending={removeThread.isPending}
        onConfirm={async () => {
          if (!deleteTarget) return;
          try {
            await removeThread.mutateAsync({
              path: { channelId, threadId: deleteTarget.id },
            });
            removeThreadPreferences(deleteTarget.id);
            workspace.dispatch({
              type: "clear",
              scopeKey: `team:${channelId}:${deleteTarget.id}`,
            });
            queryClient.removeQueries({
              queryKey: getChannelThreadQueryKey({
                path: { channelId, threadId: deleteTarget.id },
              }),
            });
            queryClient.removeQueries({
              queryKey: teamQueryKeys.timelinePages(channelId, deleteTarget.id),
            });
            await queryClient.invalidateQueries({
              queryKey: teamQueryKeys.threads(channelId),
            });
            if (threadId === deleteTarget.id) {
              navigate(`/teams/${channelId}?draftId=${crypto.randomUUID()}`, {
                replace: true,
              });
            }
            setDeleteTarget(undefined);
          } catch (error) {
            setDeleteError(
              localApiErrorMessage(
                error,
                t("chatNavigation.deleteDialog.failed"),
              ),
            );
          }
        }}
      />
    </>
  );
}
