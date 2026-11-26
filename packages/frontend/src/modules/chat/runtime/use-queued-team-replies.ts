import { useLayoutEffect } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  teamLeadInputNoticeId,
  teamLeadInputReceiptsOptions,
} from "@/modules/team";
import { ApiError } from "@/shared/api/errors";
import {
  apiErrorNoticePresentation,
  reportErrorNotice,
} from "@/shared/api/error-notices";
import { i18n } from "@/shared/i18n";

import type { BotChatMessage } from "./chat-projection";
import { useChatQueueStore, useQueuedTeamReplies } from "./chat-queue-store";

/** 主聊天和成员页签共享原队列收据对账，恢复动作仍属于原编辑器。 */
export function useQueuedTeamReplyRecovery({
  channelId,
  threadId,
  chatId,
  active,
  messages,
}: {
  channelId: string;
  threadId?: string;
  chatId?: string;
  active: boolean;
  messages: readonly BotChatMessage[];
}) {
  const queue = useChatQueueStore();
  const tracked = useQueuedTeamReplies(chatId);
  const ids = [...new Set(tracked.map(({ inputId }) => inputId))].sort();
  const { data: receipts } = useQuery({
    ...teamLeadInputReceiptsOptions(channelId, threadId ?? "", ids),
    enabled: Boolean(active && threadId && ids.length),
  });
  useLayoutEffect(() => {
    if (!active || !chatId || !threadId || !tracked.length) return;
    const admitted = new Set(
      messages.flatMap((message) =>
        message.role === "user" && message.submissionId
          ? [message.submissionId]
          : [],
      ),
    );
    const byInputId = new Map(receipts?.map((item) => [item.id, item]));
    for (const { submissionId, inputId } of tracked) {
      const receipt = byInputId.get(inputId);
      if (
        admitted.has(submissionId) ||
        receipt?.status === "consumed" ||
        receipt?.status === "delivering"
      ) {
        queue.confirmTeamReply(chatId, submissionId);
        continue;
      }
      if (receipt?.status !== "failed" && receipt?.status !== "cancelled")
        continue;
      const reply = queue.takeTeamReply(chatId, submissionId);
      if (!reply) continue;
      const cancelled = receipt.status === "cancelled";
      const fallback = i18n.t("team:eventNotice.lead-input-failed");
      const error = new ApiError(
        fallback,
        cancelled ? "OperationCanceled" : receipt.errorCode,
        400,
        { messageSource: receipt.errorCode ? "localized" : "public" },
      );
      reply.prepared.confirmRejected?.(error);
      const restored = reply.snapshot.settleDraft?.(!cancelled, true);
      if (cancelled) continue;
      const noticeId = teamLeadInputNoticeId(inputId);
      reportErrorNotice(
        {
          id: noticeId,
          scope: chatId,
          lifetime: "session",
          operation: restored ? `send:${chatId}` : noticeId,
          ...apiErrorNoticePresentation(error, fallback),
          action: restored ? undefined : "retry",
          retainForRetry: !restored,
        },
        restored
          ? undefined
          : async () => {
              await queue.retryReply({
                ...reply,
                noticeId,
                reconcile: false,
                renewSubmission: true,
              });
            },
      );
    }
  }, [active, chatId, messages, queue, receipts, threadId, tracked]);
}
