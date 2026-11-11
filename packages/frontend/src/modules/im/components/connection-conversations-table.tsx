import { useState, type ReactNode } from "react";
import { Badge } from "@/shared/ui/badge";
import { SettingsCard, SettingsGroup } from "@/modules/settings";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Skeleton } from "@/shared/ui/skeleton";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Pagination } from "@/shared/ui/pagination";
import { useIMConversations } from "../queries";
import type { IMBinding } from "../types";

export function ConnectionConversationsTable({
  connectionId,
  bindings,
  ready,
  sourceError,
  onRetrySource,
  trailing,
}: {
  connectionId: string;
  bindings: IMBinding[];
  ready: boolean;
  sourceError?: unknown;
  onRetrySource: () => void;
  trailing?: ReactNode;
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
    <SettingsGroup title={t("conversations")} trailing={trailing}>
      <SettingsCard>
        {sourceError || query.isError ? (
          <div className="px-4 py-3">
            <p role="alert" className="text-destructive type-body">
              {localApiErrorMessage(
                sourceError || query.error,
                t("common:detailState.failed"),
              )}
            </p>
            <Button
              size="settings"
              variant="settings-outline"
              onClick={() =>
                sourceError ? onRetrySource() : void query.refetch()
              }
            >
              {t("reload")}
            </Button>
          </div>
        ) : null}
        {rows.length ? (
          <ul aria-label={t("conversations")}>
            {rows
              .slice((page - 1) * pageSize, page * pageSize)
              .map(({ conversation, binding: item }) => (
                <li
                  key={conversation.id}
                  className="before:bg-border-subtle min-h-row-detail relative flex min-w-0 flex-wrap items-center justify-between gap-3 px-4 py-3 before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden"
                >
                  <div className="min-w-0 flex-1">
                    <div className="type-label flex flex-wrap items-center gap-2 font-medium">
                      <span className="[overflow-wrap:anywhere]">
                        {conversation.title || t("conversationTitle")}
                      </span>
                      {item.activeLinkId === conversation.id ? (
                        <Badge role="status" variant="success">
                          {t("currentConversation")}
                        </Badge>
                      ) : null}
                    </div>
                    <p className="type-resource-body text-content-secondary mt-1 [overflow-wrap:anywhere]">
                      {[
                        item.name || item.externalChatId,
                        conversation.createdAt
                          ? new Intl.DateTimeFormat(i18n.resolvedLanguage, {
                              dateStyle: "medium",
                              timeStyle: "short",
                            }).format(new Date(conversation.createdAt))
                          : "—",
                        t("conversationTurns", {
                          count: conversation.turnCount ?? 0,
                        }),
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </p>
                  </div>
                  {conversation.chatId ? (
                    <Button asChild size="settings" variant="ghost">
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
                </li>
              ))}
          </ul>
        ) : sourceError || query.isError ? null : query.isPending || !ready ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("loading")}
            className="grid gap-3 px-4 py-3"
          >
            <Skeleton className="h-5 w-full" />
            <Skeleton className="h-5 w-full" />
            <Skeleton className="h-5 w-2/3" />
          </div>
        ) : !rows.length ? (
          <p className="text-content-secondary type-body px-4 py-3">
            {t("noConversations")}
          </p>
        ) : null}
        {rows.length ? (
          <div className="px-4 py-3">
            <Pagination
              page={page}
              pageSize={pageSize}
              totalItems={rows.length}
              onPageChange={setRequestedPage}
              labels={{
                navigation: t("pagination"),
                previous: t("previous"),
                next: t("next"),
                page: (value) => t("pageNumber", { page: value }),
                pageSize: (value) => t("pageSize", { count: value }),
                pageSizeSelection: t("pageSizeSelection"),
                total: (value) => t("conversationCount", { count: value }),
              }}
            />
          </div>
        ) : null}
      </SettingsCard>
    </SettingsGroup>
  );
}
