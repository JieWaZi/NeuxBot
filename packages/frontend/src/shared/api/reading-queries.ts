import { queryOptions } from "@tanstack/react-query";
import {
  getChatReading,
  getRunViewing,
  markChatRead as postChatRead,
  markChatUnread as postChatUnread,
  markRunViewed as postRunViewed,
} from "./generated/sdk.gen";
import type {
  GetChatReadingResponse,
  MarkChatReadData,
  MarkChatUnreadData,
  GetRunViewingData,
} from "./generated/types.gen";
import {
  getReadyEventEpoch,
  type GlobalEventResource,
  type GlobalEventTopic,
} from "./global-events";
import { withLocalErrorSurface } from "./local-error-surface";
import { ApiError } from "./errors";

export const workspaceAttentionResource = { kind: "global" } as const;

export function checkAttentionEpoch<T extends { epoch?: string }>(
  topic: GlobalEventTopic,
  resource: GlobalEventResource,
  snapshot: T,
): T {
  const epoch = getReadyEventEpoch(topic, resource);
  if (epoch && snapshot.epoch !== epoch)
    throw new Error("Attention response belongs to a previous Core process");
  return snapshot;
}

export type ChatReadingState = GetChatReadingResponse;
export type ChatReadCommand = MarkChatReadData["body"];
export type RunReadingKind = GetRunViewingData["path"]["ownerKind"];

export function chatReadingOptions(chatId: string) {
  return queryOptions({
    queryKey: ["chat-reading", chatId] as const,
    queryFn: async ({ signal }) => {
      try {
        const { data } = await getChatReading(
          withLocalErrorSurface({
            path: { chatId },
            signal,
            throwOnError: true as const,
          }),
        );
        return checkAttentionEpoch(
          "reading",
          { kind: "chat", id: chatId },
          data,
        );
      } catch (error) {
        // 隐藏于伙伴历史的公开 Chat 仍可读。非公开会话由 Core 拒绝阅读。
        if (error instanceof ApiError && error.code === "ChatNotFound")
          return null;
        throw error;
      }
    },
    retry: false,
    staleTime: Infinity,
  });
}

export async function markChatRead(chatId: string, body: ChatReadCommand) {
  await postChatRead(
    withLocalErrorSurface({
      path: { chatId },
      body,
      throwOnError: true as const,
    }),
  );
}

export async function markChatUnread(
  chatId: string,
  body: MarkChatUnreadData["body"],
) {
  await postChatUnread(
    withLocalErrorSurface({
      path: { chatId },
      body,
      throwOnError: true as const,
    }),
  );
}

export function runViewingOptions(ownerKind: RunReadingKind, ownerId: string) {
  return queryOptions({
    queryKey: ["run-viewing", ownerKind, ownerId] as const,
    queryFn: async ({ signal }) => {
      const { data } = await getRunViewing(
        withLocalErrorSurface({
          path: { ownerKind, ownerId },
          signal,
          throwOnError: true as const,
        }),
      );
      return checkAttentionEpoch(
        "reading",
        { kind: ownerKind, id: ownerId },
        data,
      );
    },
    retry: false,
    staleTime: Infinity,
  });
}

export async function markRunViewed(
  ownerKind: RunReadingKind,
  ownerId: string,
) {
  await postRunViewed(
    withLocalErrorSurface({
      path: { ownerKind, ownerId },
      throwOnError: true as const,
    }),
  );
}
