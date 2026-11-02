import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useEffect, useLayoutEffect } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import {
  chatListInfiniteOptions,
  chatListKey,
  getChatOptions,
} from "../api/chat-queries";
import { resolveChatRecentTarget } from "../model/resolve-chat-recent-target";
import { ChatPageContent } from "./chat-page";
import { listWakersOptions } from "@/modules/waker";
import type { ListChatsResponse } from "@/shared/api/generated/types.gen";
import {
  clearChatLastActiveIfMatches,
  chatHref,
  readChatLastActive,
  readChatLastWakerId,
  writeChatLastWakerId,
} from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import { useScenePresentationKey } from "@/shared/workspace/app-scene-workspace";

export function ChatEntryPage({
  presentationScopeKey,
}: { presentationScopeKey?: string } = {}) {
  const scopeKey = useScenePresentationKey(presentationScopeKey);
  const { chatId } = useParams();
  const [searchParams] = useSearchParams();
  const requestedWakerId = searchParams.get("wakerId")?.trim() ?? "";
  const resolveRecent = !chatId && !searchParams.get("draftId");
  useLayoutEffect(() => {
    if (!requestedWakerId) return;
    writeChatLastWakerId(requestedWakerId);
  }, [requestedWakerId]);
  const { t } = useTranslation("chat");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const wakersOptions = listWakersOptions({
    query: { pageNum: 1, pageSize: 100 },
  });
  const wakersQuery = useQuery({ ...wakersOptions, enabled: resolveRecent });
  const cachedWakers = queryClient.getQueryData(wakersOptions.queryKey);
  const wakers = wakersQuery.data?.resources ?? cachedWakers?.resources ?? [];
  const lastWakerId = readChatLastWakerId();
  const lastActive = readChatLastActive();
  const requestedWakerReady = Boolean(requestedWakerId);
  const wakerId =
    resolveChatRecentTarget({
      requestedWakerId,
      lastWakerId,
      wakers,
    }).wakerId || "";
  const listOptions = chatListInfiniteOptions(wakerId || "pending");
  const cachedList = queryClient.getQueryData<InfiniteData<ListChatsResponse>>(
    chatListKey(wakerId || "pending"),
  );
  const latestFromCache = cachedList?.pages[0]?.resources?.[0];

  const lastActiveQuery = useQuery({
    ...getChatOptions({
      path: { chatId: lastActive?.chatId ?? "pending" },
    }),
    enabled:
      resolveRecent &&
      Boolean(lastActive?.chatId) &&
      Boolean(wakerId) &&
      lastActive?.wakerId === wakerId &&
      (requestedWakerReady || wakersQuery.isSuccess),
  });
  useLayoutEffect(() => {
    if (!resolveRecent || !lastActiveQuery.isError || !lastActive?.chatId)
      return;
    clearChatLastActiveIfMatches(lastActive.chatId);
  }, [lastActive?.chatId, lastActiveQuery.isError, resolveRecent]);
  const staleLastActiveId =
    lastActiveQuery.isError && lastActive?.chatId
      ? lastActive.chatId
      : undefined;
  const latestFromCacheSafe =
    latestFromCache?.id && latestFromCache.id === staleLastActiveId
      ? undefined
      : latestFromCache;

  const chatsQuery = useInfiniteQuery({
    ...listOptions,
    enabled:
      resolveRecent &&
      Boolean(wakerId) &&
      (requestedWakerReady || wakersQuery.isSuccess),
  });
  const latestChat =
    chatsQuery.data?.pages[0]?.resources?.[0] ?? latestFromCacheSafe;
  const lastActiveChat = lastActiveQuery.data?.chat;
  const resolvedTarget = resolveChatRecentTarget({
    requestedWakerId,
    lastWakerId,
    lastActive: lastActiveChat?.id
      ? {
          id: lastActiveChat.id,
          wakerId: lastActiveChat.wakerId,
        }
      : undefined,
    wakers,
    latestChat,
  });
  const resolvedChatHref =
    resolveRecent && resolvedTarget.kind === "chat"
      ? chatHref(resolvedTarget.chatId, resolvedTarget.wakerId)
      : "";

  useEffect(() => {
    if (!resolvedChatHref) return;
    navigate(resolvedChatHref, { replace: true });
  }, [navigate, resolvedChatHref]);

  // 最近对话解析与接纳后的路由只改变参数，保留同一个聊天组件及输入区。
  if (chatId)
    return <ChatPageContent chatId={chatId} presentationScopeKey={scopeKey} />;
  if (!resolveRecent)
    return <ChatPageContent presentationScopeKey={scopeKey} />;

  if (resolvedTarget.kind === "chat") {
    return (
      <ChatPageContent
        chatId={resolvedTarget.chatId}
        presentationScopeKey={scopeKey}
      />
    );
  }

  if (wakersQuery.isError && !resolvedChatHref) {
    return (
      <ChatEntryState
        description={t("entryLoadFailed")}
        actionLabel={t("entryRetry")}
        onAction={() => void wakersQuery.refetch()}
      />
    );
  }

  if (
    chatsQuery.isError &&
    (requestedWakerReady || wakersQuery.isSuccess) &&
    Boolean(wakerId) &&
    !latestFromCacheSafe
  ) {
    return (
      <ChatEntryState
        description={t("entryLoadFailed")}
        actionLabel={t("entryRetry")}
        onAction={() => void chatsQuery.refetch()}
      />
    );
  }

  if (
    !wakersQuery.isSuccess ||
    (Boolean(wakerId) &&
      lastActive?.wakerId === wakerId &&
      lastActiveQuery.isPending) ||
    (Boolean(wakerId) && !latestChat && chatsQuery.isPending)
  ) {
    return (
      <ChatPageContent routeTransitioning presentationScopeKey={scopeKey} />
    );
  }

  return <ChatPageContent presentationScopeKey={scopeKey} />;
}

function ChatEntryState({
  actionLabel,
  description,
  onAction,
}: {
  actionLabel?: string;
  description: string;
  onAction?: () => void;
}) {
  return (
    <div className="text-muted-foreground type-body bg-surface-scene grid h-full place-items-center px-6 text-center">
      <div className="grid gap-3">
        <p>{description}</p>
        {actionLabel && onAction ? (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            onClick={onAction}
          >
            {actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
