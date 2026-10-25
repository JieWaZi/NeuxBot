import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useNavigate,
  useOutletContext,
  useParams,
  useSearchParams,
} from "react-router-dom";

import {
  deleteChannelThreadMutation,
  getChannelOptions,
  getChannelThreadQueryKey,
  listChannelThreadsOptions,
  teamQueryKeys,
} from "../api/channel-queries";
import { TeamConfirmationDialog } from "../components/team-confirmation-dialog";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelThread as ChannelThread } from "@/shared/api/generated/types.gen";
import {
  localDateRangeBounds,
  normalizeDateInput,
} from "@/shared/lib/local-date";
import { Button } from "@/shared/ui/button";
import {
  ChatListPage,
  type ChatListItem,
} from "@/shared/ui/conversation-list-page";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

const pageSizeOptions = [10, 30, 50] as const;

function pageFromSearchParam(value: string | null) {
  if (!value || !/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page > 0 ? page : 1;
}

function pageSizeFromSearchParam(value: string | null) {
  if (value === "30") return 30;
  if (value === "50") return 50;
  return 10;
}

function workspaceName(thread: ChannelThread, fallback: string) {
  return (
    thread.projectName ||
    thread.additionalWorkingDirectories?.[0]
      ?.split(/[\\/]/)
      .filter(Boolean)
      .at(-1) ||
    fallback
  );
}

export function TeamThreadListPage() {
  const { t } = useTranslation("team");
  const { channelId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const { onTeamThreadDeleted } = useOutletContext<{
    onTeamThreadDeleted?: (channelId: string, threadId: string) => void;
  }>();
  const workspace = useSceneWorkspace(`team:${channelId}:thread-table`);
  const [deleteTarget, setDeleteTarget] = useState<{
    id: string;
    title: string;
  }>();
  const [deleteError, setDeleteError] = useState("");
  const page = pageFromSearchParam(searchParams.get("page"));
  const pageSize = pageSizeFromSearchParam(searchParams.get("pageSize"));
  const queryText = searchParams.get("query") ?? "";
  const from = normalizeDateInput(searchParams.get("from"));
  const to = normalizeDateInput(searchParams.get("to"));
  const range = localDateRangeBounds(from, to);
  const channelQuery = useQuery(
    getChannelOptions({ path: { channelId: channelId || "pending" } }),
  );
  const threadsQuery = useQuery({
    ...listChannelThreadsOptions({
      path: { channelId: channelId || "pending" },
      query: {
        pageNum: page,
        pageSize,
        query: queryText.trim() || undefined,
        updatedAtEnd: range.end,
        updatedAtStart: range.start,
      },
    }),
    enabled: Boolean(channelId) && range.valid,
  });
  const removeThread = useMutation(
    withLocalErrorMutation(deleteChannelThreadMutation()),
  );
  const threads = threadsQuery.data?.resources ?? [];
  const totalItems = threadsQuery.data?.totalSize ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));

  useEffect(() => {
    if (!threadsQuery.isSuccess || page <= totalPages) return;
    const next = new URLSearchParams(searchParams);
    if (totalPages === 1) next.delete("page");
    else next.set("page", String(totalPages));
    setSearchParams(next, { replace: true });
  }, [page, searchParams, setSearchParams, threadsQuery.isSuccess, totalPages]);

  const changePage = (nextPage: number) => {
    const next = new URLSearchParams(searchParams);
    if (nextPage <= 1) next.delete("page");
    else next.set("page", String(nextPage));
    setSearchParams(next);
  };
  const changePageSize = (nextPageSize: number) => {
    const next = new URLSearchParams(searchParams);
    if (nextPageSize === 10) next.delete("pageSize");
    else next.set("pageSize", String(nextPageSize));
    next.delete("page");
    setSearchParams(next);
  };
  const changeQuery = (value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set("query", value);
    else next.delete("query");
    next.delete("page");
    setSearchParams(next, { replace: true });
  };
  const changeDateRange = (nextFrom: string, nextTo: string) => {
    const next = new URLSearchParams(searchParams);
    if (nextFrom) next.set("from", nextFrom);
    else next.delete("from");
    if (nextTo) next.set("to", nextTo);
    else next.delete("to");
    next.delete("page");
    setSearchParams(next);
  };
  const items: ChatListItem[] = threads.flatMap((thread) => {
    if (!thread.id) return [];
    const status = thread.activityState ?? "idle";
    return [
      {
        href: `/teams/${channelId}/threads/${thread.id}`,
        id: thread.id,
        title: thread.title || t("chatNavigation.unnamedChat"),
        updatedAt: thread.updatedAt,
        workspace: workspaceName(thread, t("threadTable.unknownWorkspace")),
        status: {
          label: t(`threadTable.status.${status}`),
          tone:
            status === "waiting"
              ? "warning"
              : status === "running"
                ? "active"
                : "idle",
        },
      },
    ];
  });

  return (
    <main className="bg-surface-scene h-full min-w-0 overflow-hidden">
      <ChatListPage
        title={t("threadTable.title")}
        description={t("threadTable.description", {
          team: channelQuery.data?.channel?.name ?? t("title"),
        })}
        query={{
          label: t("threadTable.search"),
          onChange: changeQuery,
          placeholder: t("threadTable.searchPlaceholder"),
          value: queryText,
        }}
        toolbarEnd={
          <DateRangePicker
            from={from}
            labels={{
              clear: t("threadTable.clearRange"),
              empty: t("threadTable.allTime"),
              help: t("threadTable.activityRangeHelp"),
              title: t("threadTable.activityRange"),
              trigger: t("threadTable.selectActivityRange"),
            }}
            to={to}
            onChange={changeDateRange}
          />
        }
        invalidMessage={
          !range.valid ? t("threadTable.invalidRange") : undefined
        }
        table={{
          error: threadsQuery.isError,
          filtered: Boolean(queryText.trim() || from || to),
          items,
          labels: {
            actions: t("threadTable.actions"),
            empty: t("threadTable.empty"),
            filteredEmpty: t("threadTable.filteredEmpty"),
            lastActive: t("threadTable.lastActive"),
            loadError: t("threadTable.loadError"),
            name: t("threadTable.name"),
            retry: t("common:retry"),
            table: t("threadTable.tableLabel"),
            workspace: t("threadTable.workspace"),
          },
          loading: threadsQuery.isPending,
          onOpen: (item) => navigate(item.href),
          onRetry: () => void threadsQuery.refetch(),
          pageSize,
          pagination:
            totalItems > 0
              ? {
                  page,
                  pageSize,
                  pageSizeOptions,
                  totalItems,
                  onPageChange: changePage,
                  onPageSizeChange: changePageSize,
                  labels: {
                    navigation: t("threadTable.pagination.navigation"),
                    next: t("threadTable.pagination.next"),
                    page: (targetPage) =>
                      t("threadTable.pagination.page", { page: targetPage }),
                    pageSize: (size) =>
                      t("threadTable.pagination.pageSize", { size }),
                    pageSizeSelection: t(
                      "threadTable.pagination.pageSizeSelection",
                    ),
                    previous: t("threadTable.pagination.previous"),
                    total: (count) =>
                      t("threadTable.pagination.total", { count }),
                  },
                }
              : undefined,
          renderActions: (item) => (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  aria-label={t("threadTable.rowActions", {
                    title: item.title,
                  })}
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  onSelect={() => {
                    setDeleteError("");
                    setDeleteTarget({ id: item.id, title: item.title });
                  }}
                >
                  <Trash2 />
                  {t("chatNavigation.delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ),
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
            onTeamThreadDeleted?.(channelId, deleteTarget.id);
            setDeleteTarget(undefined);
            if (threads.length === 1 && page > 1) {
              changePage(page - 1);
            }
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
    </main>
  );
}
