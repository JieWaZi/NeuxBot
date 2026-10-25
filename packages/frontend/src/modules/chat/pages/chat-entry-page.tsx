import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useRef } from "react";
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

export function ChatEntryPage() {
  const { chatId } = useParams();
  const [searchParams] = useSearchParams();
  const requestedWakerId = searchParams.get("wakerId")?.trim() ?? "";
  useLayoutEffect(() => {
    if (!requestedWakerId) return;
    writeChatLastWakerId(requestedWakerId);
  }, [requestedWakerId]);
  if (chatId) return <ChatPageContent chatId={chatId} />;
  if (searchParams.get("draftId")) {
    return <ChatPageContent />;
  }
  return <ChatRecentEntry />;
}

function ChatRecentEntry() {
  const { t } = useTranslation("chat");
  const navigate = useNavigate();
  const lastNavigationTargetRef = useRef("");
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const requestedWakerId = searchParams.get("wakerId")?.trim() ?? "";
  const wakersOptions = listWakersOptions({
    query: { pageNum: 1, pageSize: 100 },
  });
  const wakersQuery = useQuery(wakersOptions);
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
      Boolean(lastActive?.chatId) &&
      Boolean(wakerId) &&
      lastActive?.wakerId === wakerId &&
      (requestedWakerReady || wakersQuery.isSuccess),
  });
  useLayoutEffect(() => {
    if (!lastActiveQuery.isError || !lastActive?.chatId) return;
    clearChatLastActiveIfMatches(lastActive.chatId);
  }, [lastActive?.chatId, lastActiveQuery.isError]);
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
    enabled: Boolean(wakerId) && (requestedWakerReady || wakersQuery.isSuccess),
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
    resolvedTarget.kind === "chat"
      ? chatHref(resolvedTarget.chatId, resolvedTarget.wakerId)
      : "";

  useEffect(() => {
    if (!resolvedChatHref) return;
    if (lastNavigationTargetRef.current === resolvedChatHref) return;
    lastNavigationTargetRef.current = resolvedChatHref;
    navigate(resolvedChatHref, { replace: true });
  }, [navigate, resolvedChatHref]);

  if (resolvedTarget.kind === "chat") {
    return <ChatPageContent chatId={resolvedTarget.chatId} />;
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
    return <ChatPageContent routeTransitioning />;
  }

  return <ChatPageContent />;
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
