import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useChatProtection } from "@/shared/api/chat-protection";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { expiredChatsOptions } from "../api/storage";
import {
  StorageLoading,
  StorageError,
  storageSize,
  storageDate,
} from "./storage-feedback";
import {
  SettingsCard,
  SettingsGroup,
  SettingsDangerZone,
} from "./settings-section-layout";

export function ExpiredChatList({
  days,
  active,
  disabled,
  onReview,
}: {
  days: number;
  active: boolean;
  disabled: boolean;
  onReview: (ids: string[]) => void;
}) {
  const { t } = useTranslation("storage");
  const list = useInfiniteQuery({
    ...expiredChatsOptions(days),
    enabled: active,
  });
  const protect = useChatProtection();
  const [selection, setSelection] = useState<string[]>([]);
  const rows = list.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const selected = selection.filter((id) =>
    rows.some((row) => row.chatId === id),
  );
  const first = list.data?.pages[0];
  const knownBytes =
    list.data?.pages.reduce((sum, page) => sum + (page.knownBytes ?? 0), 0) ??
    0;
  return (
    <>
      <SettingsGroup
        title={t("previewChats")}
        trailing={
          first ? (
            <span className="type-ui text-content-secondary">
              {t("candidates", {
                count: first.total ?? 0,
                size: first.complete
                  ? storageSize(knownBytes)
                  : t("partial", { size: storageSize(knownBytes) }),
              })}
            </span>
          ) : null
        }
      >
        {list.isPending ? <StorageLoading /> : null}
        {list.isError ? (
          <StorageError
            error={list.error}
            retry={() => void list.refetch()}
            stale={Boolean(first)}
          />
        ) : null}
        {protect.isError ? <StorageError error={protect.error} /> : null}
        {rows.length ? (
          <SettingsCard>
            <ul>
              {rows.map((row) => {
                const id = row.chatId ?? "";
                const title = row.title || t("chatFallback");
                return (
                  <li
                    key={id}
                    className="border-border-subtle mx-4 flex min-w-0 flex-wrap items-start gap-3 border-t py-3 first:border-t-0"
                  >
                    <Checkbox
                      aria-label={t("select", { title })}
                      checked={selected.includes(id)}
                      disabled={
                        disabled ||
                        (!selected.includes(id) && selected.length >= 256)
                      }
                      onCheckedChange={(checked) =>
                        setSelection(
                          checked
                            ? [...selected, id]
                            : selected.filter((value) => value !== id),
                        )
                      }
                    />
                    <details className="min-w-0 flex-1 basis-40">
                      <summary className="focus-visible:ring-ring cursor-pointer rounded-xs outline-none focus-visible:ring-2">
                        <span className="type-label font-medium break-words">
                          {title}
                        </span>
                        <span className="type-ui text-content-secondary block">
                          {[
                            row.wakerName,
                            row.lastActivityAt
                              ? t("activity", {
                                  time: storageDate(row.lastActivityAt),
                                })
                              : null,
                            row.sizeKnown
                              ? storageSize(row.bytes ?? 0)
                              : row.bytes
                                ? t("partial", { size: storageSize(row.bytes) })
                                : t("unknown"),
                          ]
                            .filter(Boolean)
                            .join(" · ")}
                        </span>
                      </summary>
                      <ul className="type-ui text-content-secondary grid gap-2 py-2">
                        {row.files?.map((file) => (
                          <li key={file.category + ":" + file.path}>
                            <p>
                              {t("file." + file.category, {
                                defaultValue: file.category,
                              })}{" "}
                              ·{" "}
                              {file.bytes == null
                                ? t("unknown")
                                : storageSize(file.bytes)}
                            </p>
                            <p className="break-all">{file.path}</p>
                          </li>
                        ))}
                      </ul>
                    </details>
                    <Button
                      size="settings"
                      variant="settings-outline"
                      disabled={disabled || protect.isPending}
                      onClick={() =>
                        protect.mutate({
                          chatId: id,
                          wakerId: row.wakerId ?? "",
                          protected: true,
                        })
                      }
                    >
                      {t("protect")}
                    </Button>
                  </li>
                );
              })}
            </ul>
          </SettingsCard>
        ) : first ? (
          <p className="type-body text-content-secondary">{t("noneExpired")}</p>
        ) : null}
        {list.hasNextPage ? (
          <div>
            <Button
              size="settings"
              variant="settings-outline"
              disabled={list.isFetchingNextPage}
              onClick={() => void list.fetchNextPage()}
            >
              {t("more")}
            </Button>
          </div>
        ) : null}
      </SettingsGroup>
      {rows.length ? (
        <SettingsDangerZone
          title={t("danger")}
          actionTitle={t("deleteSelected")}
          description={
            selected.length
              ? t("selection", { count: selected.length })
              : t("noSelection")
          }
          action={
            <Button
              size="settings"
              variant="danger-quiet"
              disabled={disabled || !selected.length}
              onClick={() => onReview(selected)}
            >
              {t("deleteSelected")}
            </Button>
          }
        />
      ) : null}
    </>
  );
}
