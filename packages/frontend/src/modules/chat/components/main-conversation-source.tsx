import {
  useExternalStoreRuntime,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ArrowUpRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { MainConversationComposer } from "./main-conversation-composer";
import { useMainConversationSourceHistory } from "./use-main-conversation-source-history";
import { useMainConversationInteractions } from "../model/use-main-conversation-interactions";
import type {
  BotChatMessage,
  ChatReplyReference,
} from "../runtime/chat-projection";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type {
  ConversationSubject,
  PendingMainConversationInput,
} from "../api/main-conversation";
import { ChatConversation } from "./chat-conversation";
import type {
  ChatFileChangeOpenRequest,
  ChatTimelineEvent,
} from "./chat-thread";
import type { ChatFileOpenRequest } from "./chat-attachment";
import type { ChatCodeReferenceOpenScope } from "./use-chat-workspace-bindings";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { NO_SEND_FAILURE } from "../runtime/chat-send-failure";
import { mainConversationHref } from "@/shared/lib/main-conversation-navigation";
import { Button } from "@/shared/ui/button";

const emptyMessages: ThreadMessageLike[] = [];

function SourceInputOrigin({
  subject,
  publicMessageId,
}: {
  subject: ConversationSubject;
  publicMessageId?: string;
}) {
  const { t } = useTranslation("chat");
  const label = t("sourceInputOrigin");
  if (!publicMessageId)
    return (
      <span className="text-action-quiet type-ui min-h-button-sm flex items-center px-3">
        {label}
      </span>
    );
  return (
    <Button asChild size="sm" variant="text" className="hover:underline">
      <Link
        preventScrollReset
        title={t("viewMainConversationMessage")}
        to={mainConversationHref({
          kind: subject.subjectType,
          id: subject.subjectId,
          messageId: publicMessageId,
        })}
      >
        {label}
        <ArrowUpRight aria-hidden="true" className="shrink-0" />
      </Link>
    </Button>
  );
}

/** 工作会话展示原始消息和过程，后续指示由来源输入框提交到同一工作。 */
export function MainConversationSource({
  subject,
  conversationId,
  workChatId,
  pendingInputs,
  publicMessageId,
  readOnlyPublicationId,
  executionId,
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
  pendingInputs?: PendingMainConversationInput[];
  publicMessageId?: string;
  readOnlyPublicationId?: string;
  executionId?: string;
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
  const readOnly = Boolean(readOnlyPublicationId);
  const {
    history,
    messages: native,
    chat,
    inputTriggers,
    privateContinuation,
    privatePendingInputs,
  } = useMainConversationSourceHistory(
    subject,
    workChatId,
    active,
    readOnlyPublicationId,
  );
  const interactions = useMainConversationInteractions(
    subject,
    workChatId,
    chat,
    active && !readOnly,
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
  const { messages, triggers } = useMemo(() => {
    const pendingSourceInputs = privatePendingInputs?.length
      ? [...(pendingInputs ?? []), ...privatePendingInputs]
      : pendingInputs;
    if (readOnly || !chat || !pendingSourceInputs?.length)
      return { messages: native, triggers: inputTriggers };
    const existing = new Set(native.map((message) => message.id));
    const pending: BotChatMessage[] = [];
    const triggers = new Map(inputTriggers);
    for (const input of pendingSourceInputs) {
      if (
        input.workChatId !== workChatId ||
        !input.sourceMessageId ||
        existing.has(input.sourceMessageId)
      )
        continue;
      existing.add(input.sourceMessageId);
      pending.push({
        id: input.sourceMessageId,
        role: "user",
        content: input.content,
        createdAt: input.createdAt,
        status: input.failureCode ? "failed" : undefined,
        errorCode: input.failureCode,
      });
      if (input.fromMain) {
        triggers.set(input.sourceMessageId, {
          sourceMessageId: input.sourceMessageId,
          publicMessageId: input.id,
          preview: input.content?.trim().split("\n", 1)[0] ?? "",
        });
      }
    }
    return {
      messages: pending.length ? [...native, ...pending] : native,
      triggers,
    };
  }, [
    chat,
    inputTriggers,
    native,
    pendingInputs,
    privatePendingInputs,
    readOnly,
    workChatId,
  ]);
  const last = messages.findLast((message) => !message.question);
  const executionFailure = last?.status === "failed" ? last : undefined;
  const modelChanges = useMemo(() => {
    if (readOnly || !chat) return [];
    const rows = new Map<number, ChatTimelineEvent>();
    for (const page of history.data?.pages ?? []) {
      for (const change of page.modelChanges ?? []) {
        if (
          change.actor !== "user" ||
          !change.sequence ||
          !change.createdAt ||
          !change.toModel
        )
          continue;
        const actor = t("replyAuthorYou");
        rows.set(change.sequence, {
          id: `model:${change.sequence}`,
          sequence: change.sequence,
          createdAt: change.createdAt,
          content: (
            <span className="type-caption text-content-muted">
              {change.fromModel
                ? t("sourceModelSelected", {
                    actor,
                    before: change.fromModel,
                    after: change.toModel,
                  })
                : t("sourceModelSelectedInitial", {
                    actor,
                    after: change.toModel,
                  })}
            </span>
          ),
        });
      }
    }
    return [...rows.values()];
  }, [chat, history.data, readOnly, t]);
  // 公开发言的标识由来源历史映射回原执行。权威回合标识允许直接定位旧页。
  const targetExecutionId =
    executionId ??
    (publicMessageId
      ? history.data?.pages
          .flatMap((page) => page.publishedMessages ?? [])
          .find(({ id }) => id === publicMessageId)?.executionId
      : undefined);
  const targetMessageId =
    chat?.activeExecutionId === targetExecutionId &&
    !messages.some(({ id }) => id === targetExecutionId)
      ? undefined
      : targetExecutionId;
  return (
    <ChatConversation
      {...interactions}
      active={active}
      interactionsInTimeline
      interactionErrorScope={
        active && chat && !readOnly ? `chat:${workChatId}` : undefined
      }
      forkSession={{
        chatId: chat?.id,
        wakerId: chat?.wakerId,
        messages: native,
        enabled: active && !readOnly && history.isSuccess,
        latestExecutionId: history.data?.pages[0]?.forkLatestExecutionId,
      }}
      showHistoricalExecutionFailures={false}
      onReplyToChange={chat && !readOnly ? setReply : undefined}
      runtime={runtime}
      messages={messages}
      timelineEvents={modelChanges}
      sourceWork
      renderUserMessageOrigin={(messageId) => {
        const trigger = triggers.get(messageId);
        return trigger ? (
          <SourceInputOrigin
            subject={subject}
            publicMessageId={trigger.publicMessageId}
          />
        ) : null;
      }}
      targetMessageId={chat && targetMessageId}
      wakerName={author.data?.name || t("wakerFallback")}
      wakerAvatar={author.data?.avatar}
      wakerId={chat?.wakerId}
      isHistoryLoading={history.isPending}
      isRunning={Boolean(chat?.activeExecutionId)}
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
      fileScopeChatId={readOnly ? undefined : workChatId}
      presentationScopeKey={workChatId}
      linkWorkspaceRoot={chat?.workspaceDirectory}
      linkAdditionalWorkingDirectories={chat?.additionalWorkingDirectories}
      onOpenLink={onOpenLink}
      onOpenFile={
        chat && !readOnly
          ? (file) => onOpenFile({ ...file, sourceChatId: workChatId })
          : undefined
      }
      onOpenCodeReference={
        chat && !readOnly && onOpenCodeReference
          ? (reference) =>
              onOpenCodeReference(reference, {
                sourceChatId: workChatId,
                rootPath: chat.workspaceDirectory,
              })
          : undefined
      }
      onOpenFileChange={
        chat && !readOnly && onOpenFileChange
          ? (file) => onOpenFileChange({ ...file, sourceChatId: workChatId })
          : undefined
      }
      emptyState={
        chat && !history.isError ? (
          <p className="type-ui text-content-muted px-6 py-2" role="status">
            {t(chat.activeExecutionId ? "awaitingReply" : "sourceEmptyHistory")}
          </p>
        ) : null
      }
      sendFailure={NO_SEND_FAILURE}
      hasEarlierMessages={history.hasNextPage}
      isLoadingEarlier={history.isFetchingNextPage}
      onLoadEarlier={async () => {
        await history.fetchNextPage({ throwOnError: true });
      }}
      composer={
        chat && !readOnly ? (
          <MainConversationComposer
            subject={subject}
            conversationId={conversationId}
            workChatId={workChatId}
            privateContinuation={privateContinuation}
            sourceChat={chat}
            sourceModel={history.data?.pages[0]?.selectedModel}
            executionFailure={executionFailure}
            codeReferenceRequest={codeReferenceRequest}
            reply={reply}
            setReply={setReply}
            ready={Boolean(chat.id)}
            active={active}
          />
        ) : null
      }
    />
  );
}
