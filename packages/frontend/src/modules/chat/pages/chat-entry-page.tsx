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
import { ApiError } from "@/shared/api/errors";
import {
  useRouteSceneActive,
  useScenePresentationKey,
} from "@/shared/workspace/app-scene-workspace";

export function ChatEntryPage({
  presentationScopeKey,
  routeCurrent = true,
}: { presentationScopeKey?: string; routeCurrent?: boolean } = {}) {
  const scopeKey = useScenePresentationKey(presentationScopeKey);
  const sceneActive = useRouteSceneActive();
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
  const preferredChatId =
    resolveRecent && wakerId && lastActive?.wakerId === wakerId
      ? lastActive.chatId
      : undefined;
  const lastActiveQuery = useQuery({
    ...getChatOptions({
      path: { chatId: preferredChatId ?? "pending" },
    }),
    enabled:
      Boolean(preferredChatId) &&
      (requestedWakerReady || wakersQuery.isSuccess),
  });
  const lastActiveChat = lastActiveQuery.data?.chat;
  const verifiedChat =
    preferredChatId &&
    lastActiveChat?.id === preferredChatId &&
    (!lastActiveChat?.wakerId || lastActiveChat.wakerId === wakerId)
      ? lastActiveChat
      : undefined;
  const preferredUnavailable = Boolean(
    preferredChatId &&
    !verifiedChat &&
    (lastActiveQuery.isSuccess ||
      (lastActiveQuery.error instanceof ApiError &&
        lastActiveQuery.error.status === 404 &&
        lastActiveQuery.fetchStatus === "idle")),
  );
  useLayoutEffect(() => {
    if (preferredUnavailable && preferredChatId)
      clearChatLastActiveIfMatches(preferredChatId);
  }, [preferredChatId, preferredUnavailable]);

  const chatsQuery = useInfiniteQuery({
    ...listOptions,
    enabled:
      resolveRecent &&
      Boolean(wakerId) &&
      (requestedWakerReady || wakersQuery.isSuccess),
  });
  const waitingForPreferred = Boolean(
    preferredChatId && !verifiedChat && !preferredUnavailable,
  );
  const rejectedChatId = preferredUnavailable ? preferredChatId : undefined;
  const latestChat = waitingForPreferred
    ? undefined
    : (chatsQuery.data ?? cachedList)?.pages
        .flatMap((page) => page.resources ?? [])
        .find((chat) => chat.id && chat.id !== rejectedChatId);
  const resolvedTarget = resolveChatRecentTarget({
    requestedWakerId,
    lastWakerId,
    lastActive: verifiedChat,
    wakers,
    latestChat,
  });
  const resolvedChatHref =
    resolveRecent && resolvedTarget.kind === "chat"
      ? chatHref(resolvedTarget.chatId, resolvedTarget.wakerId)
      : "";

  useEffect(() => {
    if (!routeCurrent || !sceneActive || !resolvedChatHref) return;
    navigate(resolvedChatHref, { replace: true });
  }, [navigate, resolvedChatHref, routeCurrent, sceneActive]);

  // 最近对话解析与接纳后的路由只改变参数，保留同一个聊天组件及输入区。
  if (chatId)
    return <ChatPageContent chatId={chatId} presentationScopeKey={scopeKey} />;
  if (!resolveRecent)
    return (
      <ChatPageContent
        resolvedWakerId={wakerId || undefined}
        presentationScopeKey={scopeKey}
      />
    );

  if (resolvedTarget.kind === "chat") {
    return (
      <ChatPageContent
        chatId={resolvedTarget.chatId}
        resolvedWakerId={resolvedTarget.wakerId}
        presentationScopeKey={scopeKey}
      />
    );
  }

  if (
    waitingForPreferred &&
    lastActiveQuery.isError &&
    lastActiveQuery.fetchStatus === "idle"
  ) {
    return (
      <ChatEntryState
        description={t("entryLoadFailed")}
        actionLabel={t("entryRetry")}
        onAction={() => void lastActiveQuery.refetch()}
      />
    );
  }

  if (wakersQuery.isError && !requestedWakerReady) {
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
    !latestChat &&
    !waitingForPreferred
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
    (!requestedWakerReady && !wakersQuery.isSuccess) ||
    waitingForPreferred ||
    (Boolean(wakerId) && !latestChat && chatsQuery.isPending)
  ) {
    return (
      <ChatPageContent
        routeTransitioning
        resolvedWakerId={wakerId || undefined}
        presentationScopeKey={scopeKey}
      />
    );
  }

  return (
    <ChatPageContent
      resolvedWakerId={wakerId || undefined}
      presentationScopeKey={scopeKey}
    />
  );
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
