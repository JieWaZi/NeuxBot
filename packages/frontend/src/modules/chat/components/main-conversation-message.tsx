import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localizedDateTimeFormat,
  localizedRelativeTimeFormat,
} from "@/shared/i18n/format";
import { MarkdownVisibility } from "@/shared/markdown/visibility";
import { Button } from "@/shared/ui/button";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import {
  sourceScopeOptions,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
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

const conversationClock = localizedDateTimeFormat({
  hour: "2-digit",
  minute: "2-digit",
});
const conversationDateTime = localizedDateTimeFormat({
  dateStyle: "medium",
  timeStyle: "short",
});
const conversationDay = localizedRelativeTimeFormat({ numeric: "auto" });

export function MainConversationTimeDivider({
  date,
  messageId,
}: {
  date: Date;
  messageId: string;
}) {
  const now = new Date();
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  const calendarDate = date.toDateString();
  const exactTime = conversationDateTime.format(date);
  let label = exactTime;
  if (calendarDate === now.toDateString())
    label = `${conversationDay.format(0, "day")} ${conversationClock.format(date)}`;
  else if (calendarDate === yesterday.toDateString())
    label = `${conversationDay.format(-1, "day")} ${conversationClock.format(date)}`;

  return (
    <time
      dateTime={date.toISOString()}
      title={exactTime}
      data-chat-time-divider={messageId}
      className="type-caption text-content-secondary px-6 py-1 text-center tabular-nums"
    >
      {label}
    </time>
  );
}

export function quotePublicMessage(
  row: PublicMessage,
): ChatReplyReference | undefined {
  if (!row.id || (row.role !== "user" && row.role !== "assistant")) return;
  const characters = Array.from(
    row.content?.trim() || row.automaticTaskReceipts?.[0]?.name || "",
  );
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
  inTimeline = true,
  continued,
  active,
  select,
  openSource,
  openRecord,
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
  inTimeline?: boolean;
  continued?: boolean;
  active: boolean;
  select?: (quote: ChatReplyReference) => void;
  openSource?: OpenMainConversationSource;
  openRecord?: () => void;
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
  const showIdentity = row.role === "assistant" && !continued;
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: row.sourceWakerId || "" } }),
    enabled: active && showIdentity && Boolean(row.sourceWakerId),
  });
  const name = author.data?.name || t("wakerFallback");
  const originalWork = row.role === "assistant" ? row.originalWork : undefined;
  const originalTitle = originalWork?.title?.trim();
  const content =
    row.role === "user" ? (row.inputContent ?? row.content) : row.content;
  const workflowRun = row.workflowRun;
  const runLink =
    workflowRun?.definitionId && workflowRun.runId ? (
      <Button asChild size="sm" variant="link" className="w-fit">
        <Link
          to={`/workflows/${encodeURIComponent(workflowRun.definitionId)}/runs/${encodeURIComponent(workflowRun.runId)}`}
        >
          {t("openWorkflowRun")}
        </Link>
      </Button>
    ) : null;
  const scope = useQuery({
    ...sourceScopeOptions(subject, workChatId),
    enabled:
      active &&
      Boolean(
        openSource &&
        !row.sourceReadOnly &&
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
    select && (row.sequence || ref.role === "assistant")
      ? () => select(ref)
      : undefined;
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
    automaticTaskReceipts: row.automaticTaskReceipts,
    attachments: row.attachments,
    codeReferences: row.codeReferences,
    attachmentOnly: row.attachmentOnly,
    codeReferenceOnly: row.codeReferenceOnly,
  };
  // 助手发言默认只关联原交办，显式引用时才在公开时间线展示原话。
  const replyQuote =
    row.replyToMessageId &&
    (ref.role === "user" ||
      (ref.role === "assistant" && inTimeline && row.quoteReply === true)) ? (
      <>
        <MessageReplyQuote
          replyTo={
            (quoted && quotePublicMessage(quoted)) ?? {
              messageId: row.replyToMessageId,
            }
          }
          replyingMessageId={row.id}
          variant={ref.role}
        />
        {quoteFeedback}
      </>
    ) : undefined;
  const actions = (
    <>
      {selectReply && <MessageReplyAction onSelect={selectReply} />}
      <ChatMessageActions
        copyText={content ?? ""}
        onOpenWorkRecord={
          (showWorkRecord ||
            (row.sourceReadOnly && ref.role === "assistant")) &&
          workChatId &&
          openSource &&
          (!row.sourceReadOnly || ref.role === "assistant")
            ? (trigger) => openSource(workChatId, trigger)
            : undefined
        }
        forkItem={
          ref.role === "assistant" &&
          openSource &&
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
  const identity = showIdentity ? (
    <MainConversationAvatar
      name={name}
      src={author.data?.avatar}
      workChatId={workChatId}
      openSource={openSource}
      defaultConversation={!workChatId && !originalWork}
      openRecord={
        active && !workChatId && (row.replyToMessageId || originalWork)
          ? openRecord
          : undefined
      }
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
          <>
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
              sideAction={inTimeline ? actions : undefined}
              replyQuote={replyQuote}
            />
            {runLink && <div className="flex justify-end px-6">{runLink}</div>}
          </>
        ) : (
          <ChatReplyMessage
            message={message}
            variant={inTimeline ? "post" : undefined}
            name={inTimeline && showIdentity ? name : undefined}
            continued={continued}
            identity={identity}
            sideAction={inTimeline ? actions : undefined}
            footer={inTimeline ? undefined : actions}
          >
            {replyQuote ||
            content ||
            runLink ||
            message.attachments?.length ||
            originalWork?.ticketId ? (
              <>
                {replyQuote}
                {content &&
                  (row.readable ? (
                    <div data-chat-readable-body>
                      <AssistantMarkdown text={content} />
                    </div>
                  ) : (
                    <AssistantMarkdown text={content} />
                  ))}
                {runLink}
                {message.attachments?.length ? (
                  <div className="flex flex-wrap items-start gap-1 py-1">
                    <ChatMessageAttachments
                      attachments={message.attachments}
                      onOpenFile={openSourceFile}
                    />
                  </div>
                ) : null}
                {originalWork?.chatId && originalWork.ticketId && (
                  <p className="type-ui text-content-secondary wrap-anywhere">
                    {originalTitle
                      ? t("deletedWorkOriginWithTitle", {
                          title: originalTitle,
                        })
                      : t("deletedWorkOrigin")}
                  </p>
                )}
              </>
            ) : null}
          </ChatReplyMessage>
        )}
      </MarkdownVisibility>
    </FileReferenceProvider>
  );
}
