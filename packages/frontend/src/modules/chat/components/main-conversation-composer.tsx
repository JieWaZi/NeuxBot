import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getChannelOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  sendMainConversationMessage,
  sendMainConversationSourceMessage,
} from "@/shared/api/generated/sdk.gen";
import type { GetMainConversationSourceResponse } from "@/shared/api/generated/types.gen";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import { ApiError } from "@/shared/api/errors";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import {
  mainConversationOptions,
  sourceHistoryOptions,
  sourceScopeOptions,
  conversationSubmissionKey,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import {
  mainConversationUserPreview,
  type MainConversationSubmission,
} from "../model/main-conversation-submission";
import type { ChatReplyReference } from "../runtime/chat-projection";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { ChatComposerPanel } from "./chat-composer-panel";

type Props = {
  subject: ConversationSubject;
  conversationId?: string;
  reply?: ChatReplyReference;
  setReply?: (reply: ChatReplyReference | undefined) => void;
  /** 公开输入引用的原工作，或当前正在查看的工作会话。 */
  replySourceId?: string;
  workChatId?: string;
  sourceChat?: GetMainConversationSourceResponse["chat"];
  codeReferenceRequest?: ChatCodeReference;
  active: boolean;
  ready: boolean;
};
const clearNoReply = () => {};

/** 两个入口共用聊天编辑器，仅按当前视图选择接纳回址。 */
export function MainConversationComposer({
  subject,
  conversationId,
  reply,
  setReply = clearNoReply,
  replySourceId,
  workChatId,
  sourceChat,
  codeReferenceRequest,
  active,
  ready,
}: Props) {
  const client = useQueryClient();
  const sourceId = workChatId || replySourceId;
  const source = useQuery({
    ...sourceScopeOptions(subject, sourceId ?? ""),
    enabled: active && Boolean(replySourceId) && !sourceChat && !workChatId,
    staleTime: Infinity,
  });
  const chat = sourceChat ?? source.data;
  const isTeam = subject.subjectType === "team";
  const team = useQuery({
    ...getChannelOptions({ path: { channelId: subject.subjectId } }),
    enabled: active && isTeam,
  });
  const wakerId =
    chat?.wakerId ||
    (isTeam ? team.data?.channel?.leadWakerId : subject.subjectId) ||
    "";
  const member = team.data?.members?.find((item) => item.waker?.id === wakerId);
  const scopeKey = workChatId
    ? `source:${workChatId}`
    : `main:${subject.subjectType}:${subject.subjectId}`;
  const send = useMutation({
    mutationKey: conversationSubmissionKey(subject),
    mutationFn: async ({
      request: { message, submissionId, replyId },
      preview,
    }: MainConversationSubmission) => {
      const { queryKey } = mainConversationOptions(subject);
      let receipt: PublicMessage | undefined;
      try {
        if (workChatId) {
          await sendMainConversationSourceMessage(
            withLocalErrorSurface({
              path: { ...subject, workChatId },
              body: { ...message, submissionId, replyToMessageId: replyId },
              throwOnError: true,
            }),
          );
          // 来源回执确认原输入，但公开记录的顺序仍由主会话读取提供。
          if (preview?.id && !preview.id.startsWith("submission:"))
            receipt = preview;
        } else {
          const response = await sendMainConversationMessage(
            withLocalErrorSurface({
              path: subject,
              body: {
                message,
                submissionId,
                replyToMessageId: replyId,
              },
              throwOnError: true,
            }),
          );
          receipt = response.data.message;
        }
      } catch (error) {
        // 公开历史只返回已由工作日志接纳的用户消息，可以确认丢失的 HTTP 回执。
        const committed =
          preview?.id &&
          client
            .getQueryData(queryKey)
            ?.pages.flatMap((page) => page.messages ?? [])
            .find((row) => row.id === preview.id);
        if (committed) return { receipt: committed, historyUpdates: 0 };
        throw error;
      }
      const historyUpdates =
        client.getQueryState(queryKey)?.dataUpdateCount ?? 0;
      const affected = workChatId
        ? [queryKey, sourceHistoryOptions(subject, workChatId).queryKey]
        : [queryKey];
      await Promise.all(
        affected.map((key) =>
          refreshQueryFromEvent(client, key).catch(() => {}),
        ),
      );
      return { receipt, historyUpdates };
    },
  });
  return (
    <div className="shrink-0 px-6 pb-4">
      <ChatComposerPanel
        scopeKey={scopeKey}
        codeReferenceRequest={codeReferenceRequest}
        wakerId={wakerId}
        active={active}
        defaults={
          member
            ? {
                runtimeId: member.runtimeId,
                accountId: member.accountId,
                model: member.model,
                reasoning: member.reasoning,
                fast: member.fast,
                permissionLevel: member.permissionLevel,
              }
            : chat?.lastRuntimeId
              ? {
                  runtimeId: chat.lastRuntimeId,
                  accountId: chat.lastAccountId,
                  model: chat.lastModel,
                  reasoning: chat.lastReasoning,
                  fast: chat.lastFast,
                  contextWindow: chat.lastContextWindow,
                  permissionLevel: chat.lastPermissionLevel,
                }
              : undefined
        }
        isSendDisabled={
          !ready || !wakerId || Boolean(sourceId && !chat) || send.isPending
        }
        conversation={{
          reply,
          setReply,
          source: chat,
          view: workChatId ? "source" : "main",
          fixedMember: isTeam,
        }}
        onAccepted={() => {}}
        prepareSubmission={(message) => {
          const submissionId = crypto.randomUUID();
          const request = {
            message,
            submissionId,
            replyId: reply?.messageId,
          };
          let receipt:
            "rejected" | "preparation-failed" | "unknown" | undefined;
          return {
            submissionId,
            hasUnconfirmedReceipt: () => receipt === "unknown",
            canReturnToDraft: () => receipt === "preparation-failed",
            confirmRejected: () => !receipt || receipt === "rejected",
            accept: async (userMessage) => {
              const preview = userMessage
                ? await mainConversationUserPreview(
                    conversationId,
                    submissionId,
                    userMessage,
                    workChatId,
                  )
                : undefined;
              try {
                await send.mutateAsync({ request, preview });
                receipt = undefined;
                return { published: true };
              } catch (error) {
                // 本次准备失败无法证明之前丢失回执的尝试没有接纳。
                if (receipt === "unknown") throw error;
                const status =
                  error instanceof ApiError ? error.status : undefined;
                const key =
                  error instanceof ApiError
                    ? applicationErrorKey(error.code)
                    : undefined;
                if (
                  status === 500 &&
                  (key === "RuntimeSessionStartFailed" ||
                    key === "RuntimeSessionRestoreFailed")
                ) {
                  receipt = "preparation-failed";
                } else if (
                  status !== undefined &&
                  status >= 400 &&
                  status < 500 &&
                  status !== 408 &&
                  key !== "CreateRequestInProgress"
                ) {
                  receipt = "rejected";
                } else {
                  receipt = "unknown";
                }
                throw error;
              }
            },
          };
        }}
      />
    </div>
  );
}
