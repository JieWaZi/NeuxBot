import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Folder, LoaderCircle, MoreHorizontal, Trash2 } from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
  type UIEvent,
} from "react";
import { useTranslation } from "react-i18next";
import {
  Link,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";

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
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

import { navigateRouteImmediately } from "./immediate-route-navigation";
import { ChatGroupHeader } from "@/modules/chat";
import {
  forgetTeamThread,
  type PartnerNavigationPreferences,
} from "./partner-navigation-preferences";

const relativeTimeFormat = new Intl.RelativeTimeFormat("zh-CN", {
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
  return new Intl.DateTimeFormat("zh-CN", {
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
  const directory = thread.additionalWorkingDirectories?.[0]?.trim();
  return {
    key: `folder:${directory || projectName || labels.temporaryFolder}`,
    label:
      projectName ||
      directory?.split(/[\\/]/).filter(Boolean).at(-1) ||
      labels.temporaryFolder,
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
  channelId: string;
  onPreferencesChange: Dispatch<SetStateAction<PartnerNavigationPreferences>>;
  onThreadOpened?: () => void;
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
  onPreferencesChange,
  onThreadOpened,
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
  const [menuOpenThreadId, setMenuOpenThreadId] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<{
    id: string;
    title: string;
  }>();
  const [deleteError, setDeleteError] = useState("");
  const removeThread = useMutation(
    withLocalErrorMutation(deleteChannelThreadMutation()),
  );
  const threadsQuery = useInfiniteQuery({
    ...teamThreadsInfiniteOptions(channelId || "pending"),
    enabled: Boolean(channelId),
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
        temporaryFolder: t("chatNavigation.temporaryFolder"),
        unnamedProject: t("chatNavigation.unnamedProject"),
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
        temporaryFolder: t("chatNavigation.temporaryFolder"),
        unnamedProject: t("chatNavigation.unnamedProject"),
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
  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget;
    if (
      threadsQuery.hasNextPage &&
      !threadsQuery.isFetchingNextPage &&
      element.scrollHeight - element.scrollTop - element.clientHeight < 120
    ) {
      void threadsQuery.fetchNextPage();
    }
  };

  const threadRow = (thread: ChannelThread) => {
    const targetThreadId = thread.id ?? "";
    const title = thread.title || t("chatNavigation.unnamedChat");
    const href = teamLocationHref(channelId, targetThreadId);
    return (
      <div
        key={targetThreadId}
        className={cn(
          "group/chat hover:bg-muted min-h-row-compact relative grid grid-cols-[minmax(0,1fr)_32px] items-center rounded-sm",
          targetThreadId === threadId && "bg-muted text-foreground",
        )}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenuOpenThreadId(targetThreadId);
        }}
      >
        <Link
          to={href}
          onClick={(event) => {
            onThreadOpened?.();
            navigateRouteImmediately(event, navigate, href);
          }}
          className="focus-visible:ring-ring type-body grid min-w-0 grid-cols-[14px_14px_minmax(0,1fr)_auto] items-center gap-1 rounded-sm px-1 py-1 outline-none focus-visible:ring-2 focus-visible:ring-inset"
          title={title}
        >
          <span aria-hidden="true" />
          <span
            aria-hidden="true"
            className="bg-border block size-1.5 place-self-center rounded-full"
          />
          <span className="truncate">{title}</span>
          <time className="text-muted-foreground type-caption group-focus-within/chat:hidden group-hover/chat:hidden">
            {formatRelativeTime(thread.updatedAt)}
          </time>
        </Link>
        <DropdownMenu
          open={menuOpenThreadId === targetThreadId}
          onOpenChange={(open) =>
            setMenuOpenThreadId(open ? targetThreadId : "")
          }
        >
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              className="opacity-0 group-focus-within/chat:opacity-100 group-hover/chat:opacity-100 hover:bg-transparent data-[state=open]:bg-transparent data-[state=open]:opacity-100"
              aria-label={t("common:partnerNavigation.chatMenu", { title })}
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              className="text-destructive data-[highlighted]:text-destructive"
              onSelect={() => {
                setDeleteError("");
                setDeleteTarget({ id: targetThreadId, title });
              }}
            >
              <Trash2 />
              {t("chatNavigation.delete")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    );
  };

  return (
    <>
      <div
        className="min-h-0 min-w-0 overflow-hidden bg-transparent pb-1"
        data-chat-tree="team"
      >
        <div
          className="scrollbar-hidden max-h-72 min-h-0 overflow-x-hidden overflow-y-auto pr-1"
          onScroll={handleScroll}
        >
          {threadsQuery.isPending ? (
            <div className="text-muted-foreground type-caption flex items-center gap-2 px-2 py-4">
              <LoaderCircle className="size-icon-sm animate-spin" />
              {t("chatNavigation.loading")}
            </div>
          ) : threadsQuery.isError ? (
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
              <div className="mt-0.5" key={group.key}>
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
                  onActionOpened={onThreadOpened}
                  onToggle={() => toggleGroup(group.key)}
                />
                {!collapsedGroups.has(group.key) ? (
                  <div className="grid gap-0.5 pl-3">
                    {group.threads.map((thread) => threadRow(thread))}
                  </div>
                ) : null}
              </div>
            ))
          )}
          {threadsQuery.isFetchingNextPage ? (
            <div
              aria-busy="true"
              className="text-muted-foreground type-caption flex items-center justify-center gap-2 py-3"
              role="status"
            >
              <LoaderCircle className="size-icon-sm animate-spin" />
              {t("chatNavigation.loadingMore")}
            </div>
          ) : null}
        </div>
      </div>
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
