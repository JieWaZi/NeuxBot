import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getChannelOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  sendMainConversationMessage,
  sendMainConversationSourceMessage,
  setMainConversationSourceModel,
} from "@/shared/api/generated/sdk.gen";
import type { GetMainConversationSourceResponse } from "@/shared/api/generated/types.gen";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import { ApiError } from "@/shared/api/errors";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";
import {
  authorizedSourceChat,
  mainConversationOptions,
  sourceHistoryOptions,
  sourceScopeOptions,
  conversationSubmissionKey,
  type ConversationSubject,
  type PendingMainConversationInput,
  type PublicMessage,
} from "../api/main-conversation";
import {
  mainConversationInputId,
  mainConversationUserPreview,
  pendingMainConversationMessage,
  type MainConversationSubmission,
} from "../model/main-conversation-submission";
import type {
  BotChatMessage,
  ChatReplyReference,
} from "../runtime/chat-projection";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { ChatComposerPanel } from "./chat-composer-panel";
import { dismissExecutionFailure } from "./chat-error-card";

type Props = {
  subject: ConversationSubject;
  conversationId?: string;
  reply?: ChatReplyReference;
  contextReplyId?: string;
  replyHasTicket?: boolean;
  setReply?: (reply: ChatReplyReference | undefined) => void;
  /** 公开输入引用的原工作，或当前正在查看的工作会话。 */
  replySourceId?: string;
  workChatId?: string;
  privateContinuation?: boolean;
  sourceChat?: GetMainConversationSourceResponse["chat"];
  sourceModel?: string;
  executionFailure?: BotChatMessage;
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
  contextReplyId,
  replyHasTicket = false,
  setReply = clearNoReply,
  replySourceId,
  workChatId,
  privateContinuation = false,
  sourceChat,
  sourceModel,
  executionFailure,
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
  const sessionSource = sourceId
    ? authorizedSourceChat(sourceId, chat, source.error)
    : undefined;
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
  const updateModel = useMutation({
    scope: { id: scopeKey },
    mutationFn: async ({
      chatId,
      model,
    }: {
      chatId: string;
      model: string;
    }) => {
      await setMainConversationSourceModel({
        path: { ...subject, workChatId: chatId },
        body: { model },
        throwOnError: true,
      });
      await refreshQueryFromEvent(
        client,
        sourceHistoryOptions(subject, chatId).queryKey,
      );
    },
  });
  const send = useMutation({
    mutationKey: conversationSubmissionKey(subject),
    mutationFn: async ({
      request: { message, submissionId, replyId, historySourceChatId },
      preview,
    }: MainConversationSubmission) => {
      const { queryKey } = mainConversationOptions(subject);
      const sourceKey = workChatId
        ? sourceHistoryOptions(subject, workChatId).queryKey
        : undefined;
      let receipt: PublicMessage | undefined;
      let queued: PendingMainConversationInput | undefined;
      try {
        if (workChatId) {
          const response = await sendMainConversationSourceMessage(
            withLocalErrorSurface({
              path: { ...subject, workChatId },
              ...(historySourceChatId
                ? { query: { switchSession: true } }
                : {}),
              body: { ...message, submissionId, replyToMessageId: replyId },
              throwOnError: true,
            }),
          );
          queued = response.data.queuedInput;
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
                ...(historySourceChatId ? { historySourceChatId } : {}),
              },
              throwOnError: true,
            }),
          );
          receipt = response.data.message;
          queued = response.data.queuedInput;
        }
      } catch (error) {
        if (sourceKey && privateContinuation) {
          const pages = client.getQueryData(sourceKey)?.pages;
          if (
            pages?.some(({ resources }) =>
              resources?.some((item) => item.submissionId === submissionId),
            )
          )
            return { historyUpdates: 0 };
          const inputId = await mainConversationInputId(
            conversationId,
            submissionId,
          );
          queued = pages?.[0]?.pendingInputs?.find(
            (input) => input.id === inputId,
          );
        } else {
          const pages = client.getQueryData(queryKey)?.pages;
          const committed = preview?.id
            ? pages
                ?.flatMap((page) => page.messages ?? [])
                .find((row) => row.id === preview.id)
            : undefined;
          if (committed) return { receipt: committed, historyUpdates: 0 };
          queued = pages?.[0]?.pendingInputs?.find(
            (input) => input.id === preview?.id,
          );
        }
        if (!queued) throw error;
      }
      if (queued) {
        const input = queued;
        if (sourceKey && input.privateSource) {
          client.setQueryData(sourceKey, (current) => {
            if (!current) return current;
            const first = current.pages[0];
            if (
              !first ||
              first.pendingInputs?.some(({ id }) => id === input.id)
            )
              return current;
            return {
              ...current,
              pages: [
                {
                  ...first,
                  pendingInputs: [...(first.pendingInputs ?? []), input],
                },
                ...current.pages.slice(1),
              ],
            };
          });
        } else {
          receipt = pendingMainConversationMessage(input, conversationId);
          client.setQueryData(queryKey, (current) => {
            if (!current) return current;
            const first = current.pages[0];
            if (
              !first ||
              current.pages.some((page) =>
                page.messages?.some((row) => row.id === input.id),
              ) ||
              first.pendingInputs?.some((row) => row.id === input.id)
            )
              return current;
            return {
              ...current,
              pages: [
                {
                  ...first,
                  pendingInputs: [...(first.pendingInputs ?? []), input],
                },
                ...current.pages.slice(1),
              ],
            };
          });
        }
      }
      const historyUpdates =
        client.getQueryState(queryKey)?.dataUpdateCount ?? 0;
      const affectedSource = workChatId ?? queued?.workChatId;
      const affected = affectedSource
        ? [queryKey, sourceHistoryOptions(subject, affectedSource).queryKey]
        : [queryKey];
      const refresh = Promise.all(
        affected.map((key) =>
          refreshQueryFromEvent(client, key).catch(() => {}),
        ),
      );
      if (!queued) await refresh;
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
                  model: sourceModel || chat.lastModel,
                  reasoning: chat.lastReasoning,
                  fast: chat.lastFast,
                  contextWindow: chat.lastContextWindow,
                  permissionLevel: chat.lastPermissionLevel,
                }
              : undefined
        }
        isSendDisabled={
          !ready ||
          !wakerId ||
          Boolean(sourceId && !chat) ||
          send.isPending ||
          updateModel.isPending
        }
        onSourceModelChange={
          workChatId && !isTeam
            ? (model) => updateModel.mutate({ chatId: workChatId, model })
            : undefined
        }
        conversation={{
          reply,
          setReply,
          source: chat,
          sessionSource,
          executionFailure,
          view: workChatId ? "source" : "main",
          fixedMember: isTeam,
        }}
        workTicketTransfer={
          active && !workChatId && conversationId
            ? {
                origin: { originKind: "main", originId: conversationId },
                replyHasTicket,
                refresh: () =>
                  refreshQueryFromEvent(
                    client,
                    mainConversationOptions(subject).queryKey,
                  ),
              }
            : undefined
        }
        onAccepted={() => {}}
        prepareSubmission={(message) => {
          const submissionId = crypto.randomUUID();
          const failureId =
            executionFailure?.executionId || executionFailure?.id;
          const request = {
            message,
            submissionId,
            replyId: reply?.messageId ?? contextReplyId,
            historySourceChatId: message.startNewSession
              ? sessionSource?.id
              : undefined,
          };
          let receipt:
            "rejected" | "preparation-failed" | "unknown" | undefined;
          return {
            submissionId,
            confirmAccepted: () => {
              if (failureId) dismissExecutionFailure(failureId);
            },
            hasUnconfirmedReceipt: () => receipt === "unknown",
            canReturnToDraft: () => receipt === "preparation-failed",
            confirmRejected: () => !receipt || receipt === "rejected",
            accept: async (userMessage) => {
              const preview =
                userMessage && !privateContinuation
                  ? await mainConversationUserPreview(
                      conversationId,
                      submissionId,
                      userMessage,
                      workChatId,
                      request.replyId,
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
