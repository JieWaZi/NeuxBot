import { useState } from "react";
import { Badge } from "@/shared/ui/badge";
import { SettingsGroup } from "@/modules/settings";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Skeleton } from "@/shared/ui/skeleton";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { PaginatedTable } from "@/shared/ui/paginated-table";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import { useIMConversations } from "../queries";
import type { IMBinding } from "../types";

export function ConnectionConversationsTable({
  connectionId,
  bindings,
  ready,
  sourceError,
  onRetrySource,
}: {
  connectionId: string;
  bindings: IMBinding[];
  ready: boolean;
  sourceError?: unknown;
  onRetrySource: () => void;
}) {
  const { t, i18n } = useTranslation("im");
  const [requestedPage, setRequestedPage] = useState(1);
  const query = useIMConversations(connectionId, bindings, ready);
  const pageSize = 10;
  const rows = query.data || [];
  const page = Math.min(
    requestedPage,
    Math.max(1, Math.ceil(rows.length / pageSize)),
  );
  return (
    <section aria-label={t("conversations")}>
      <SettingsGroup
        title={t("conversations")}
        description={
          query.data && !query.isError && !sourceError
            ? t("conversationSummary", { count: rows.length })
            : undefined
        }
      >
        <PaginatedTable
          aria-label={t("conversations")}
          pagination={
            rows.length
              ? {
                  page,
                  pageSize,
                  totalItems: rows.length,
                  onPageChange: setRequestedPage,
                  labels: {
                    navigation: t("pagination"),
                    previous: t("previous"),
                    next: t("next"),
                    page: (value) => t("pageNumber", { page: value }),
                    pageSize: (value) => t("pageSize", { count: value }),
                    pageSizeSelection: t("pageSizeSelection"),
                    total: (value) => t("conversationCount", { count: value }),
                  },
                }
              : undefined
          }
        >
          <TableHeader>
            <TableRow>
              <TableHead>{t("conversationTitle")}</TableHead>
              <TableHead>{t("turnCount")}</TableHead>
              <TableHead>{t("createdAt")}</TableHead>
              <TableHead>{t("openWork")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sourceError || query.isError ? (
              <TableRow>
                <TableCell colSpan={4}>
                  <p role="alert" className="text-destructive type-body">
                    {localApiErrorMessage(
                      sourceError || query.error,
                      t("common:detailState.failed"),
                    )}
                  </p>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() =>
                      sourceError ? onRetrySource() : void query.refetch()
                    }
                  >
                    {t("reload")}
                  </Button>
                </TableCell>
              </TableRow>
            ) : query.isPending || !ready ? (
              <TableRow>
                <TableCell colSpan={4}>
                  <div
                    role="status"
                    aria-busy="true"
                    aria-label={t("loading")}
                    className="grid gap-3"
                  >
                    <Skeleton className="h-5 w-full" />
                    <Skeleton className="h-5 w-full" />
                    <Skeleton className="h-5 w-2/3" />
                  </div>
                </TableCell>
              </TableRow>
            ) : !rows.length ? (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className="text-content-secondary text-center"
                >
                  {t("noConversations")}
                </TableCell>
              </TableRow>
            ) : (
              rows
                .slice((page - 1) * pageSize, page * pageSize)
                .map(({ conversation, binding: item }) => (
                  <TableRow key={conversation.id}>
                    <TableCell>
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="[overflow-wrap:anywhere]">
                          {conversation.title || t("conversationTitle")}
                        </span>
                        {item.activeLinkId === conversation.id ? (
                          <Badge role="status" variant="success">
                            {t("currentConversation")}
                          </Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-content-secondary tabular-nums">
                      {conversation.turnCount ?? 0}
                    </TableCell>
                    <TableCell className="text-content-secondary whitespace-nowrap">
                      {conversation.createdAt
                        ? new Intl.DateTimeFormat(i18n.resolvedLanguage, {
                            dateStyle: "medium",
                            timeStyle: "short",
                          }).format(new Date(conversation.createdAt))
                        : "—"}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-2">
                        {conversation.chatId ? (
                          <Button asChild size="sm" variant="ghost">
                            <Link
                              to={
                                conversation.threadId && item.channelId
                                  ? `/teams/${item.channelId}/threads/${conversation.threadId}`
                                  : `/chats/${conversation.chatId}`
                              }
                            >
                              {t("openWork")}
                            </Link>
                          </Button>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
            )}
          </TableBody>
        </PaginatedTable>
      </SettingsGroup>
    </section>
  );
}
