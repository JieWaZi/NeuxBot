import type { QueryClient } from "@tanstack/react-query";
import {
  resolveMainConversationSourceApproval,
  resolveMainConversationSourceElicitation,
} from "@/shared/api/generated/sdk.gen";
import type { GetMainConversationSourceResponse } from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import {
  mainConversationOptions,
  sourceHistoryOptions,
  sourceScopeOptions,
  type ConversationSubject,
} from "../api/main-conversation";
import type { ChatRequestSource } from "./use-conversation-interactions";

/** 请求来自已授权的来源快照，处理和回读始终携带同一公开主体与工作会话。 */
export function mainConversationSourceRequests(
  client: QueryClient,
  subject: ConversationSubject,
  workChatId: string,
  chat: NonNullable<GetMainConversationSourceResponse["chat"]>,
): ChatRequestSource {
  const path = { ...subject, workChatId };
  return {
    chatId: workChatId,
    pendingApprovals: chat.pendingApprovals ?? [],
    pendingElicitations: chat.pendingElicitations ?? [],
    resolveApproval: (_chatId, approvalId, optionId) =>
      resolveMainConversationSourceApproval(
        withLocalErrorSurface({
          path: { ...path, approvalId },
          body: { optionId },
          throwOnError: true,
        }),
      ),
    resolveElicitation: (_chatId, elicitationId, content, declined) =>
      resolveMainConversationSourceElicitation(
        withLocalErrorSurface({
          path: { ...path, elicitationId },
          body: { content, declined },
          throwOnError: true,
        }),
      ),
    onSettled: () => {
      for (const queryKey of [
        mainConversationOptions(subject).queryKey,
        sourceScopeOptions(subject, workChatId).queryKey,
        sourceHistoryOptions(subject, workChatId).queryKey,
      ]) {
        void refreshQueryFromEvent(client, queryKey).catch(() => {});
      }
    },
  };
}
