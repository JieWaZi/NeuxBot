import { useMutationState } from "@tanstack/react-query";
import { useLayoutEffect } from "react";
import type { GithubComJieWaZiNeuxBotServerApisChatsMessageRequest as ChatMessageRequest } from "@/shared/api/generated/types.gen";
import {
  conversationSubmissionKey,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import type { BotChatMessage } from "../runtime/chat-projection";

type MainConversationSubmissionRequest = {
  message: ChatMessageRequest;
  submissionId: string;
  replyId?: string;
};

export type MainConversationSubmission = {
  request: MainConversationSubmissionRequest;
  preview?: PublicMessage;
};

// 预览由权威历史接管后，原请求的待确认状态与重试都不能再次补出该行。
const coveredByHistory = new WeakSet<MainConversationSubmissionRequest>();

type MainConversationSubmissionResult = {
  receipt?: PublicMessage;
  historyUpdates: number;
};

export async function mainConversationUserPreview(
  conversationId: string | undefined,
  submissionId: string,
  message: BotChatMessage,
  workChatId: string | undefined,
): Promise<PublicMessage> {
  let id = `submission:${submissionId}`;
  if (conversationId && crypto.subtle) {
    // 与 Core 的 UserMessageID 使用同一身份，使事件可以在 HTTP 回执之前接管该行。
    const parts = JSON.stringify(["user", conversationId, submissionId]);
    const digest = await crypto.subtle
      .digest("SHA-256", new TextEncoder().encode(parts))
      .catch(() => undefined);
    if (digest) {
      id = Array.from(new Uint8Array(digest, 0, 16), (byte) =>
        byte.toString(16).padStart(2, "0"),
      ).join("");
    }
  }
  return {
    id,
    conversationId,
    role: "user",
    content: message.content,
    createdAt: message.updatedAt,
    sourceChatId: workChatId,
    replyToMessageId: workChatId ? undefined : message.replyTo?.messageId,
    attachments: message.attachments?.map(({ name, mediaType }) => ({
      name,
      mediaType,
    })),
    attachmentOnly: message.attachmentOnly,
    codeReferences: message.codeReferences,
    codeReferenceOnly: message.codeReferenceOnly,
  };
}

/** 同一提交的预览及回执只补足权威历史还未覆盖的内容。 */
export function useMainConversationSubmissions(
  subject: ConversationSubject,
  conversationId: string | undefined,
  committed: PublicMessage[],
  hasEarlier: boolean,
  historyUpdates: number,
) {
  const submissions = useMutationState({
    filters: { mutationKey: conversationSubmissionKey(subject), exact: true },
    select: (mutation) => {
      const { state } = mutation;
      const result =
        state.status === "success"
          ? (state.data as MainConversationSubmissionResult | undefined)
          : undefined;
      const pending = state.status === "pending";
      return pending || result?.receipt
        ? { mutation, result, pending }
        : undefined;
    },
  });
  const messages = submissions.some(Boolean)
    ? new Map(committed.map((row) => [row.id, row]))
    : undefined;
  const confirmed: MainConversationSubmissionRequest[] = [];
  const appended = new Set<string>();
  const oldest = committed[0]?.sequence;
  let waiting = false;
  let changed = false;
  for (const submission of submissions) {
    if (!messages || !submission) continue;
    // 该键只由主会话的发送 mutation 使用。重试复用原请求对象。
    const input = submission.mutation.state.variables as
      MainConversationSubmission | undefined;
    const row = input?.preview;
    if (!input || !row?.id || row.conversationId !== conversationId) continue;
    const { request } = input;
    const receipt = submission.result?.receipt;
    const id = receipt?.id ?? row.id;
    const current = messages.get(id);
    if (current) {
      if (!appended.has(id) && !coveredByHistory.has(request))
        confirmed.push(request);
      if (
        current.inputContent === undefined &&
        (row.attachments?.length || row.codeReferences?.length)
      ) {
        messages.set(id, {
          ...current,
          inputContent: row.content,
          attachments: row.attachments,
          attachmentOnly: row.attachmentOnly,
          codeReferences: row.codeReferences,
          codeReferenceOnly: row.codeReferenceOnly,
        });
        changed = true;
      }
      continue;
    }
    // 确认后更新的权威历史决定消息是否仍可见，旧回执只能填补对账失败。
    if (
      coveredByHistory.has(request) ||
      (submission.result && historyUpdates > submission.result.historyUpdates)
    )
      continue;
    if (
      hasEarlier &&
      oldest !== undefined &&
      receipt?.sequence !== undefined &&
      receipt.sequence < oldest
    )
      continue;
    messages.set(id, {
      ...row,
      ...receipt,
      inputContent: row.content,
    });
    appended.add(id);
    changed = true;
    waiting ||= submission.pending;
  }
  useLayoutEffect(() => {
    for (const request of confirmed) coveredByHistory.add(request);
  });
  return {
    messages:
      changed && messages
        ? [...messages.values()].sort(
            (left, right) =>
              (left.sequence ?? Infinity) - (right.sequence ?? Infinity),
          )
        : committed,
    waiting,
  };
}
