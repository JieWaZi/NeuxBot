import { useQuery } from "@tanstack/react-query";
import { MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import { listWakerChatsOptions } from "../api/chat-queries";
import { chatHref } from "@/shared/lib/chat-navigation";
import { ChatActivityDateRangePicker } from "../components/chat-activity-date-range-picker";
import { ChatBatchDeleteDialog } from "../components/chat-batch-delete-dialog";
import { ChatTaskDialog } from "../components/chat-task-dialog";
import {
  activityDateRange,
  chatPageSizeFromSearchParam,
  normalizeDateInput,
  pageFromSearchParam,
} from "../model/activity-date-range";
import { WakerDetailLayout } from "@/modules/waker";
import { Button } from "@/shared/ui/button";
import { ResourceSelectionActions } from "@/shared/ui/resource-selection";
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

const pageSizeOptions = [10, 30, 50] as const;

type TaskAction = {
  chatId: string;
  mode: "rename" | "delete";
  title: string;
};

export function ChatTaskListPage() {
  const { t } = useTranslation("chat");
  const { wakerId = "" } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [taskAction, setTaskAction] = useState<TaskAction>();
  const [batchDeleteOpen, setBatchDeleteOpen] = useState(false);
  const [selectedChatIDs, setSelectedChatIDs] = useState(
    () => new Set<string>(),
  );
  const page = pageFromSearchParam(searchParams.get("page"));
  const pageSize = chatPageSizeFromSearchParam(searchParams.get("pageSize"));
  const nameQuery = searchParams.get("query") ?? "";
  const from = normalizeDateInput(searchParams.get("from"));
  const to = normalizeDateInput(searchParams.get("to"));
  const range = activityDateRange(from, to);
  const query = useQuery({
    ...listWakerChatsOptions({
      path: { wakerId },
      query: {
        pageNum: page,
        pageSize,
        query: nameQuery.trim() || undefined,
        updatedAtEnd: range.updatedAtEnd,
        updatedAtStart: range.updatedAtStart,
      },
    }),
    enabled: Boolean(wakerId) && range.valid,
  });
  const chats = query.data?.resources ?? [];
  const visibleChatIDs = chats.flatMap((chat) => (chat.id ? [chat.id] : []));
  const selectedVisibleCount = visibleChatIDs.filter((id) =>
    selectedChatIDs.has(id),
  ).length;
  const allVisibleSelected =
    visibleChatIDs.length > 0 && selectedVisibleCount === visibleChatIDs.length;
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected;
  const totalItems = query.data?.totalSize ?? 0;
  const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));

  useEffect(() => {
    if (!query.isSuccess || page <= totalPages) return;
    const next = new URLSearchParams(searchParams);
    if (totalPages === 1) next.delete("page");
    else next.set("page", String(totalPages));
    setSearchParams(next, { replace: true });
  }, [page, query.isSuccess, searchParams, setSearchParams, totalPages]);

  const updateDateRange = (nextFrom: string, nextTo: string) => {
    const next = new URLSearchParams(searchParams);
    if (nextFrom) next.set("from", nextFrom);
    else next.delete("from");
    if (nextTo) next.set("to", nextTo);
    else next.delete("to");
    next.delete("page");
    setSelectedChatIDs(new Set());
    setSearchParams(next);
  };
  const updateNameQuery = (value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set("query", value);
    else next.delete("query");
    next.delete("page");
    setSelectedChatIDs(new Set());
    setSearchParams(next, { replace: true });
  };
  const changePage = (nextPage: number) => {
    const next = new URLSearchParams(searchParams);
    if (nextPage <= 1) next.delete("page");
    else next.set("page", String(nextPage));
    setSelectedChatIDs(new Set());
    setSearchParams(next);
  };
  const changePageSize = (nextPageSize: number) => {
    const next = new URLSearchParams(searchParams);
    if (nextPageSize === 10) next.delete("pageSize");
    else next.set("pageSize", String(nextPageSize));
    next.delete("page");
    setSelectedChatIDs(new Set());
    setSearchParams(next);
  };
  const setChatSelected = (chatID: string, selected: boolean) => {
    setSelectedChatIDs((current) => {
      const next = new Set(current);
      if (selected) next.add(chatID);
      else next.delete(chatID);
      return next;
    });
  };
  const setVisibleChatsSelected = (selected: boolean) => {
    setSelectedChatIDs((current) => {
      const next = new Set(current);
      for (const chatID of visibleChatIDs) {
        if (selected) next.add(chatID);
        else next.delete(chatID);
      }
      return next;
    });
  };
  const items: ChatListItem[] = chats.flatMap((chat) => {
    if (!chat.id) return [];
    const status = chat.waitingApproval
      ? {
          badge: true,
          label: t("taskPage.waitingApproval"),
          tone: "warning" as const,
        }
      : chat.activeExecutionId
        ? {
            label: t("taskPage.executing"),
            tone: "active" as const,
          }
        : { label: t("taskPage.idle"), tone: "idle" as const };
    return [
      {
        href: chatHref(chat.id, chat.wakerId ?? ""),
        id: chat.id,
        status,
        title: chat.title ?? "",
        updatedAt: chat.updatedAt,
        workspace:
          chat.projectName ||
          chat.workspaceDirectory ||
          t("taskPage.unknownWorkspace"),
      },
    ];
  });

  return (
    <WakerDetailLayout active="chats">
      <ChatListPage
        title={t("taskPage.title")}
        description={t("taskPage.description")}
        query={{
          label: t("taskPage.nameQuery"),
          onChange: updateNameQuery,
          placeholder: t("taskPage.nameQueryPlaceholder"),
          value: nameQuery,
        }}
        toolbarEnd={
          <ChatActivityDateRangePicker
            from={from}
            to={to}
            onChange={updateDateRange}
          />
        }
        selectionActions={
          <ResourceSelectionActions
            count={selectedChatIDs.size}
            onClear={() => setSelectedChatIDs(new Set())}
            onDelete={() => setBatchDeleteOpen(true)}
          />
        }
        invalidMessage={!range.valid ? t("taskPage.invalidRange") : undefined}
        table={{
          error: query.isError,
          filtered: Boolean(nameQuery.trim() || from || to),
          items,
          labels: {
            actions: t("taskPage.actions"),
            empty: t("taskPage.empty"),
            filteredEmpty: t("taskPage.filteredEmpty"),
            lastActive: t("taskPage.lastActive"),
            loadError: t("taskPage.loadError"),
            name: t("taskPage.name"),
            retry: t("taskPage.retry"),
            table: t("taskPage.tableLabel"),
            workspace: t("taskPage.workspace"),
          },
          loading: query.isPending,
          onOpen: (item) => navigate(item.href),
          onRetry: () => void query.refetch(),
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
                    navigation: t("taskPage.pagination.navigation"),
                    next: t("taskPage.pagination.next"),
                    page: (targetPage) =>
                      t("taskPage.pagination.page", { page: targetPage }),
                    pageSize: (size) =>
                      t("taskPage.pagination.pageSize", { size }),
                    pageSizeSelection: t(
                      "taskPage.pagination.pageSizeSelection",
                    ),
                    previous: t("taskPage.pagination.previous"),
                    total: (count) => t("taskPage.pagination.total", { count }),
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
                  aria-label={t("taskMenu")}
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() =>
                    setTaskAction({
                      chatId: item.id,
                      mode: "rename",
                      title: item.title,
                    })
                  }
                >
                  <Pencil />
                  {t("renameTask")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive"
                  onSelect={() =>
                    setTaskAction({
                      chatId: item.id,
                      mode: "delete",
                      title: item.title,
                    })
                  }
                >
                  <Trash2 />
                  {t("deleteTask")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ),
          selection: {
            allVisibleSelected,
            onSelectAll: setVisibleChatsSelected,
            onSelectItem: setChatSelected,
            selectAllLabel: t("taskPage.selectAll"),
            selectItemLabel: (item) =>
              t("taskPage.selectTask", { title: item.title }),
            selectedIds: selectedChatIDs,
            someVisibleSelected,
          },
        }}
      />

      {taskAction ? (
        <ChatTaskDialog
          key={`${taskAction.mode}:${taskAction.chatId}`}
          chatId={taskAction.chatId}
          mode={taskAction.mode}
          open
          title={taskAction.title}
          onOpenChange={(open) => {
            if (!open) setTaskAction(undefined);
          }}
          onChanged={() => query.refetch().then(() => undefined)}
          onDeleted={(deletedChatId) => {
            setSelectedChatIDs((current) => {
              const next = new Set(current);
              next.delete(deletedChatId);
              return next;
            });
          }}
        />
      ) : null}
      {batchDeleteOpen ? (
        <ChatBatchDeleteDialog
          chatIds={Array.from(selectedChatIDs)}
          open
          onOpenChange={setBatchDeleteOpen}
          onChanged={() => query.refetch().then(() => undefined)}
          onDeleted={(deletedIds) => {
            setSelectedChatIDs((current) => {
              const next = new Set(current);
              for (const chatId of deletedIds) {
                next.delete(chatId);
              }
              return next;
            });
          }}
        />
      ) : null}
    </WakerDetailLayout>
  );
}
