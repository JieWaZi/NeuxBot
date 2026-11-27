import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { MarkdownVisibility } from "@/shared/markdown/visibility";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import {
  sourceScopeOptions,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import { runtimeFailurePresentation } from "../model/runtime-failure-presentation";
import type { MainConversationAvatarActivity } from "../model/main-conversation-message-group";
import { ChatErrorCard } from "./chat-error-card";
import { assistantMessageRowClassName } from "./chat-layout";
import { AssistantMarkdown } from "./chat-message-content";
import {
  ChatMessageAttachments,
  type ChatFileOpenRequest,
} from "./chat-attachment";
import { ChatReplyMessage, UserMessage } from "./chat-thread";
import { MessageReplyAction, MessageReplyQuote } from "./chat-message-reply";
import { ChatMessageActions } from "./chat-message-actions";
import { PublicMessageForkMenuItem } from "./chat-fork";
import {
  MainConversationAvatar,
  type OpenMainConversationSource,
} from "./main-conversation-avatar";
import type { ChatReplyReference } from "../runtime/chat-projection";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import type { ChatCodeReferenceOpenScope } from "./use-chat-workspace-bindings";

export function quotePublicMessage(
  row: PublicMessage,
): ChatReplyReference | undefined {
  if (!row.id || (row.role !== "user" && row.role !== "assistant")) return;
  const characters = Array.from((row.content ?? "").trim());
  return {
    messageId: row.id,
    role: row.role,
    preview:
      characters.length > 160
        ? characters.slice(0, 159).join("").trimEnd() + "…"
        : characters.join(""),
  };
}

export function PublicConversationMessage({
  subject,
  row,
  quoted,
  quoteFeedback,
  replyActivity,
  continued,
  active,
  select,
  openSource,
  showWorkRecord,
  sourceBusy,
  onMarkUnread,
  onDelete,
  openLink,
  openFile,
  openCodeReference,
}: {
  subject: ConversationSubject;
  row: PublicMessage;
  quoted?: PublicMessage;
  quoteFeedback?: ReactNode;
  replyActivity?: MainConversationAvatarActivity;
  continued?: boolean;
  active: boolean;
  select: (quote: ChatReplyReference) => void;
  openSource: OpenMainConversationSource;
  showWorkRecord?: boolean;
  sourceBusy?: boolean;
  onMarkUnread?: (messageId: string) => void;
  onDelete?: (messageId: string, trigger: HTMLButtonElement) => void;
  openLink?: (url: string) => boolean;
  openFile: (file: ChatFileOpenRequest) => void;
  openCodeReference?: (
    reference: ChatCodeReference,
    scope: ChatCodeReferenceOpenScope,
  ) => void;
}) {
  const { t } = useTranslation("chat");
  const workChatId = row.sourceChatId || "";
  const failureCode = row.role === "user" ? row.failureCode : undefined;
  const showIdentity = row.role === "assistant" && !continued;
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: row.sourceWakerId || "" } }),
    enabled:
      active &&
      (Boolean(failureCode) || showIdentity) &&
      Boolean(row.sourceWakerId),
  });
  const name = author.data?.name || t("wakerFallback");
  const content =
    row.role === "user" ? (row.inputContent ?? row.content) : row.content;
  const scope = useQuery({
    ...sourceScopeOptions(subject, workChatId),
    enabled:
      active &&
      Boolean(
        workChatId &&
        (row.attachments?.length ||
          row.codeReferences?.length ||
          (content && /\]\(|\]\[|^\s*\[[^\]]+\]:/m.test(content))),
      ),
    staleTime: Infinity,
  });
  const source =
    active && !scope.isError && scope.data?.id === workChatId
      ? scope.data
      : undefined;
  const openSourceFile = source
    ? (file: ChatFileOpenRequest) =>
        openFile({ ...file, sourceChatId: workChatId })
    : undefined;
  const ref = quotePublicMessage(row);
  if (!ref) return null;
  const selectReply =
    row.sequence || ref.role === "assistant" ? () => select(ref) : undefined;
  const deleteAction =
    row.sequence && onDelete
      ? (trigger: HTMLButtonElement) => onDelete(ref.messageId, trigger)
      : undefined;
  const message = {
    id: row.id,
    role: ref.role,
    content,
    createdAt: row.createdAt,
    createdSequence: row.sequence,
    readable: row.readable,
    attachments: row.attachments,
    codeReferences: row.codeReferences,
    attachmentOnly: row.attachmentOnly,
    codeReferenceOnly: row.codeReferenceOnly,
  };
  // 助手发言的关联 ID 指向原交办；用户的显式回复可以加载未显示的引用原话。
  const original = quoted && quotePublicMessage(quoted);
  const actions = (
    <>
      {selectReply && <MessageReplyAction onSelect={selectReply} />}
      <ChatMessageActions
        copyText={content ?? ""}
        onOpenWorkRecord={
          showWorkRecord && workChatId
            ? (trigger) => openSource(workChatId, trigger)
            : undefined
        }
        forkItem={
          ref.role === "assistant" &&
          workChatId &&
          row.sourceWakerId &&
          row.forkExecutionId ? (
            <PublicMessageForkMenuItem
              chatId={workChatId}
              wakerId={row.sourceWakerId}
              executionId={row.forkExecutionId}
              busy={sourceBusy}
            />
          ) : undefined
        }
        onMarkUnread={
          ref.role === "assistant" && row.readable && onMarkUnread
            ? () => onMarkUnread(ref.messageId)
            : undefined
        }
        onDelete={deleteAction}
      />
    </>
  );
  const identity =
    failureCode || showIdentity ? (
      <MainConversationAvatar
        name={name}
        src={author.data?.avatar}
        workChatId={workChatId}
        activity={replyActivity}
        openSource={openSource}
      />
    ) : null;
  return (
    <FileReferenceProvider
      context={{
        chatId: source?.id,
        baseDirectory: source?.workspaceDirectory || "",
        workspaceRoot: source?.workspaceDirectory || "",
        sourceRoots: source?.additionalWorkingDirectories ?? [],
      }}
      allowLocalReveal={Boolean(openSourceFile)}
      onOpenURL={openLink}
      onOpenFile={
        openSourceFile
          ? (file) => openSourceFile({ ...file, reference: file })
          : undefined
      }
    >
      <MarkdownVisibility value={active}>
        {ref.role === "user" ? (
          <UserMessage
            message={message}
            onOpenFile={openSourceFile}
            onOpenCodeReference={
              source && openCodeReference
                ? (reference) =>
                    openCodeReference(reference, {
                      sourceChatId: workChatId,
                      rootPath: source.workspaceDirectory,
                    })
                : undefined
            }
            sideAction={actions}
            replyQuote={
              row.replyToMessageId ? (
                <>
                  <MessageReplyQuote
                    replyTo={original ?? { messageId: row.replyToMessageId }}
                    replyingMessageId={row.id}
                  />
                  {quoteFeedback}
                </>
              ) : undefined
            }
          />
        ) : (
          <ChatReplyMessage
            message={message}
            variant="post"
            name={showIdentity ? name : undefined}
            continued={continued}
            identity={identity}
            sideAction={actions}
          >
            {row.readable ? (
              <div data-chat-readable-body>
                <AssistantMarkdown text={content || ""} />
              </div>
            ) : (
              <AssistantMarkdown text={content || ""} />
            )}
            {message.attachments?.length ? (
              <div className="flex flex-wrap items-start gap-1 py-1">
                <ChatMessageAttachments
                  attachments={message.attachments}
                  onOpenFile={openSourceFile}
                />
              </div>
            ) : null}
          </ChatReplyMessage>
        )}
        {failureCode && (
          <div className={assistantMessageRowClassName}>
            {identity}
            <div className="w-fit min-w-0">
              <ChatErrorCard
                id={`main-conversation:${row.id}`}
                message={runtimeFailurePresentation(failureCode).message}
                execution
              />
            </div>
          </div>
        )}
      </MarkdownVisibility>
    </FileReferenceProvider>
  );
}
