import { useQueryClient } from "@tanstack/react-query";
import type { GetMainConversationSourceResponse } from "@/shared/api/generated/types.gen";
import type { ConversationSubject } from "../api/main-conversation";
import {
  conversationKeyOf,
  useChatInvocationGuard,
} from "./conversation-identity";
import { mainConversationSourceRequests } from "./main-conversation-source-requests";
import {
  useChatInteractions,
  type ChatRequestSource,
} from "./use-conversation-interactions";

const emptyRequests: ChatRequestSource = {
  pendingApprovals: [],
  pendingElicitations: [],
  onSettled: () => {},
  resolveApproval: async () => {},
  resolveElicitation: async () => {},
};

/** 两个主会话入口都向同一经核验的工作提交请求。 */
export function useMainConversationInteractions(
  subject: ConversationSubject,
  workChatId: string,
  chat: GetMainConversationSourceResponse["chat"] | undefined,
  active: boolean,
) {
  const client = useQueryClient();
  const requests =
    active && chat?.id === workChatId
      ? mainConversationSourceRequests(client, subject, workChatId, chat)
      : emptyRequests;
  const queueKey = JSON.stringify([
    "main-conversation-source",
    subject.subjectType,
    subject.subjectId,
    workChatId,
  ]);
  const conversationKey = conversationKeyOf(queueKey, requests.chatId);
  const guard = useChatInvocationGuard(conversationKey);
  const {
    approvalSubmissionState,
    elicitationSubmissionState,
    resolveApprovalChoice,
    resolveElicitationChoice,
  } = useChatInteractions({ ...requests, conversationKey, guard, queueKey });

  return {
    pendingApprovals: requests.pendingApprovals,
    pendingElicitations: requests.pendingElicitations,
    approvalSubmissionState,
    elicitationSubmissionState,
    onResolveApproval: resolveApprovalChoice,
    onResolveElicitation: resolveElicitationChoice,
  };
}
