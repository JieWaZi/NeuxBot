import { queryOptions } from "@tanstack/react-query";
import {
  getChatReading,
  getMainConversationReading,
  getRunViewing,
  markChatRead as postChatRead,
  markChatUnread as postChatUnread,
  markMainConversationRead as postMainConversationRead,
  markMainConversationUnread as postMainConversationUnread,
  markRunViewed as postRunViewed,
} from "./generated/sdk.gen";
import type {
  GetChatReadingResponse,
  GetMainConversationReadingResponse,
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
export type MainConversationReadingSubject = {
  subjectType: "waker" | "team";
  subjectId: string;
};
export type ReadingTarget = string | MainConversationReadingSubject;
export type ConversationReadingState =
  ChatReadingState | GetMainConversationReadingResponse;
export type ChatReadCommand = MarkChatReadData["body"];
export type RunReadingKind = GetRunViewingData["path"]["ownerKind"];

export function chatReadingOptions(chatId: string) {
  return conversationReadingOptions(chatId);
}

export function conversationReadingOptions(target: ReadingTarget) {
  return queryOptions<ConversationReadingState | null>({
    queryKey:
      typeof target === "string"
        ? ["chat-reading", target]
        : ["main-conversation-reading", target.subjectType, target.subjectId],
    queryFn: async ({ signal }) => {
      try {
        if (typeof target !== "string") {
          const { data } = await getMainConversationReading(
            withLocalErrorSurface({
              path: target,
              signal,
              throwOnError: true as const,
            }),
          );
          return checkAttentionEpoch(
            "reading",
            { kind: "main_conversation", id: data.conversationId },
            data,
          );
        }
        const { data } = await getChatReading(
          withLocalErrorSurface({
            path: { chatId: target },
            signal,
            throwOnError: true as const,
          }),
        );
        return checkAttentionEpoch(
          "reading",
          { kind: "chat", id: target },
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

export async function markConversationRead(
  target: ReadingTarget,
  body: ChatReadCommand,
) {
  if (typeof target !== "string") {
    await postMainConversationRead(
      withLocalErrorSurface({
        path: target,
        body,
        throwOnError: true as const,
      }),
    );
    return;
  }
  await postChatRead(
    withLocalErrorSurface({
      path: { chatId: target },
      body,
      throwOnError: true as const,
    }),
  );
}

export async function markConversationUnread(
  target: ReadingTarget,
  body: MarkChatUnreadData["body"],
) {
  if (typeof target !== "string") {
    await postMainConversationUnread(
      withLocalErrorSurface({
        path: target,
        body,
        throwOnError: true as const,
      }),
    );
    return;
  }
  await postChatUnread(
    withLocalErrorSurface({
      path: { chatId: target },
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
