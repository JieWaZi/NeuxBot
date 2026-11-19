import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { lazy, Suspense, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import {
  loadMoreStorage,
  storageEntitiesOptions,
  storageKeys,
} from "../api/storage";
import {
  SettingsCard,
  SettingsGroup,
  SettingsNavigationRow,
} from "./settings-section-layout";
import { StorageError, StorageLoading, storageSize } from "./storage-feedback";
import { useStorageDeleteFocus } from "./storage-delete-focus";

const ChatTaskDialog = lazy(() =>
  import("@/modules/chat").then((module) => ({
    default: module.ChatTaskDialog,
  })),
);

export function StorageChatList({
  active,
  disabled,
  wakerId,
  fallbackFocus,
}: {
  active: boolean;
  disabled: boolean;
  wakerId?: string;
  fallbackFocus?: () => HTMLElement | null;
}) {
  const { t } = useTranslation(["storage", "chat"]);
  const navigate = useNavigate();
  const client = useQueryClient();
  const [selected, setSelected] = useState<{ id: string; title: string }>();
  const chatList = useRef<HTMLUListElement>(null);
  const heading = useRef<HTMLSpanElement>(null);
  const deleteFocus = useStorageDeleteFocus(
    () => fallbackFocus?.() ?? heading.current,
  );
  const options = storageEntitiesOptions("chats", wakerId);
  const list = useInfiniteQuery({
    ...options,
    enabled: active,
  });
  const pages = list.data?.pages ?? [];
  const results = wakerId
    ? pages.flatMap((page) => page.items ?? [])
    : (pages[0]?.items ?? []).slice(0, 10);
  const rows = results.filter((row): row is typeof row & { id: string } =>
    Boolean(row.id),
  );

  const content = (
    <>
      {list.isPending ? <StorageLoading /> : null}
      {list.isError ? (
        <StorageError
          error={list.error}
          retry={() => void list.refetch()}
          stale={rows.length > 0}
        />
      ) : null}
      {rows.length ? (
        <TooltipProvider>
          <SettingsCard>
            <ul ref={chatList}>
              {rows.map((row) => {
                const id = row.id;
                const title = row.title || t("chatFallback");
                const attachments = t("attachmentCount", {
                  count: row.fileCount ?? 0,
                });
                const description =
                  !wakerId && row.wakerName
                    ? [row.wakerName, attachments].join(" · ")
                    : attachments;
                return (
                  <li
                    key={id}
                    className="before:bg-border-subtle hover:bg-action-quiet-hover relative flex items-center before:pointer-events-none before:absolute before:inset-x-4 before:top-0 before:z-10 before:h-px first:before:hidden"
                  >
                    <div className="min-w-0 flex-1">
                      <SettingsNavigationRow
                        className="hover:bg-transparent"
                        showChevron={false}
                        title={<span className="block truncate">{title}</span>}
                        description={
                          <span className="block truncate">{description}</span>
                        }
                        onClick={() =>
                          navigate(chatHref(id, row.wakerId ?? wakerId ?? ""))
                        }
                        trailing={
                          <span
                            className="type-body shrink-0 tabular-nums"
                            title={t("chatSizeScope")}
                          >
                            {storageSize(row.bytes ?? 0)}
                          </span>
                        }
                      />
                    </div>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          type="button"
                          size="icon"
                          variant="danger-quiet"
                          className="mr-4 shrink-0"
                          disabled={disabled}
                          aria-label={t("deleteChat", { title })}
                          data-storage-delete-action={id}
                          onClick={() => {
                            deleteFocus.capture(chatList.current, id);
                            setSelected({ id, title });
                          }}
                        >
                          <Trash2 aria-hidden="true" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>{t("chat:deleteTask")}</TooltipContent>
                    </Tooltip>
                  </li>
                );
              })}
            </ul>
          </SettingsCard>
        </TooltipProvider>
      ) : list.isSuccess ? (
        <p className="type-body text-content-secondary">{t("noChats")}</p>
      ) : null}
      {wakerId && list.hasNextPage ? (
        <div>
          <Button
            type="button"
            size="settings"
            variant="settings-outline"
            disabled={disabled || list.isFetching}
            onClick={() => void loadMoreStorage(client, options.queryKey, list)}
          >
            {t("more")}
          </Button>
        </div>
      ) : null}
      {selected ? (
        <Suspense fallback={null}>
          <ChatTaskDialog
            key={selected.id}
            chatId={selected.id}
            mode="delete"
            open
            title={selected.title}
            returnFocusTo={deleteFocus.target}
            disabled={disabled}
            onOpenChange={(open) => {
              if (!open) setSelected(undefined);
            }}
            onDeleted={() => {
              deleteFocus.markRemoved();
              for (const queryKey of [
                storageKeys.usage,
                storageKeys.entities,
                storageKeys.files,
              ]) {
                void client.invalidateQueries({ queryKey });
              }
            }}
          />
        </Suspense>
      ) : null}
    </>
  );

  return wakerId ? (
    <div className="grid min-w-0 gap-2">{content}</div>
  ) : (
    <SettingsGroup
      title={
        <span ref={heading} tabIndex={-1} className="outline-none">
          {t("chats")}
        </span>
      }
    >
      {content}
    </SettingsGroup>
  );
}
