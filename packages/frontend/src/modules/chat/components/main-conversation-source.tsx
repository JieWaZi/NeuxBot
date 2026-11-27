import {
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { MainConversationComposer } from "./main-conversation-composer";
import { useMainConversationSourceHistory } from "./use-main-conversation-source-history";
import { useMainConversationInteractions } from "../model/use-main-conversation-interactions";
import type { ChatReplyReference } from "../runtime/chat-projection";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ConversationSubject } from "../api/main-conversation";
import { ChatThread, type ChatFileChangeOpenRequest } from "./chat-thread";
import type { ChatFileOpenRequest } from "./chat-attachment";
import type { ChatCodeReferenceOpenScope } from "./use-chat-workspace-bindings";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { MessageFailure } from "./chat-error-card";
import { NO_SEND_FAILURE } from "../runtime/chat-send-failure";

const emptyMessages: ThreadMessageLike[] = [];
/** 工作会话展示原始消息和过程，后续指示由来源输入框提交到同一工作。 */
export function MainConversationSource({
  subject,
  conversationId,
  workChatId,
  active,
  onOpenLink,
  onOpenFile,
  onOpenCodeReference,
  onOpenFileChange,
  codeReferenceRequest,
}: {
  subject: ConversationSubject;
  conversationId?: string;
  workChatId: string;
  active: boolean;
  onOpenLink?: (url: string) => boolean;
  onOpenFile: (file: ChatFileOpenRequest) => void;
  onOpenCodeReference?: (
    reference: ChatCodeReference,
    scope: ChatCodeReferenceOpenScope,
  ) => void;
  onOpenFileChange?: (file: ChatFileChangeOpenRequest) => void;
  codeReferenceRequest?: ChatCodeReference;
}) {
  const { t } = useTranslation("chat");
  const { history, messages, chat } = useMainConversationSourceHistory(
    subject,
    workChatId,
    active,
  );
  const interactions = useMainConversationInteractions(
    subject,
    workChatId,
    chat,
    active,
  );
  const [reply, setReply] = useState<ChatReplyReference>();
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: chat?.wakerId || "" } }),
    enabled: active && Boolean(chat?.wakerId),
  });
  const runtime = useExternalStoreRuntime<ThreadMessageLike>({
    messages: emptyMessages,
    convertMessage: (m) => m,
    isDisabled: true,
    onNew: async () => {},
  });
  const last = messages.at(-1);
  return (
    <>
      <ChatThread
        {...interactions}
        active={active}
        interactionsInTimeline
        interactionErrorScope={
          active && chat ? `chat:${workChatId}` : undefined
        }
        showComposer={false}
        onReplyToChange={chat ? setReply : undefined}
        runtime={runtime}
        messages={messages}
        wakerName={author.data?.name || t("wakerFallback")}
        wakerAvatar={author.data?.avatar}
        wakerId={chat?.wakerId}
        isHistoryLoading={history.isPending}
        isRunning={Boolean(chat?.activeExecutionId)}
        afterMessages={
          last?.status === "failed" ? (
            <div className="px-6" aria-live="polite">
              <MessageFailure last placement="composer" message={last} />
            </div>
          ) : null
        }
        hasExistingConversation
        historyError={
          history.isError
            ? {
                message: localApiErrorMessage(
                  history.error,
                  t("loadMessagesFailed"),
                ),
                onRetry: () => void history.refetch(),
              }
            : undefined
        }
        fileScopeChatId={workChatId}
        presentationScopeKey={workChatId}
        linkWorkspaceRoot={chat?.workspaceDirectory}
        linkAdditionalWorkingDirectories={chat?.additionalWorkingDirectories}
        onOpenLink={onOpenLink}
        onOpenFile={
          chat
            ? (file) => onOpenFile({ ...file, sourceChatId: workChatId })
            : undefined
        }
        onOpenCodeReference={
          chat && onOpenCodeReference
            ? (reference) =>
                onOpenCodeReference(reference, {
                  sourceChatId: workChatId,
                  rootPath: chat.workspaceDirectory,
                })
            : undefined
        }
        onOpenFileChange={
          chat && onOpenFileChange
            ? (file) => onOpenFileChange({ ...file, sourceChatId: workChatId })
            : undefined
        }
        emptyState={null}
        sendFailure={NO_SEND_FAILURE}
        hasEarlierMessages={history.hasNextPage}
        isLoadingEarlier={history.isFetchingNextPage}
        onLoadEarlier={async () => {
          await history.fetchNextPage({ throwOnError: true });
        }}
      />
      {chat && (
        <MainConversationComposer
          subject={subject}
          conversationId={conversationId}
          workChatId={workChatId}
          sourceChat={chat}
          codeReferenceRequest={codeReferenceRequest}
          reply={reply}
          setReply={setReply}
          ready={Boolean(chat.id && !chat.activeExecutionId)}
          active={active}
        />
      )}
    </>
  );
}
