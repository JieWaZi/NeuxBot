import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { QueryRefreshError } from "@/shared/ui/query-state";
import {
  sourceScopeOptions,
  type ConversationSubject,
  type PublicMessage,
} from "../api/main-conversation";
import {
  mainConversationWorkRequest,
  type MainConversationWork,
} from "../model/main-conversation-message-group";
import { useMainConversationInteractions } from "../model/use-main-conversation-interactions";
import { ChatReplyMessage } from "./chat-thread";
import { ChatTimelineInteractions } from "./chat-timeline-interactions";
import {
  MainConversationAvatar,
  type OpenMainConversationSource,
} from "./main-conversation-avatar";

export function MainConversationRequests({
  subject,
  active,
  work,
  continued,
  openSource,
  requestedInteractionId,
  settledQuestions,
}: {
  subject: ConversationSubject;
  active: boolean;
  openSource: OpenMainConversationSource;
  work: MainConversationWork;
  continued?: boolean;
  requestedInteractionId?: string;
  settledQuestions?: PublicMessage["questions"];
}) {
  const { t } = useTranslation("chat");
  const { sourceWakerId, workChatId = "", executionId = "" } = work;
  const actor =
    sourceWakerId || (subject.subjectType === "waker" ? subject.subjectId : "");
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: actor } }),
    enabled: active && !continued && Boolean(actor),
  });
  const name = author.data?.name || t("wakerFallback");
  const request = mainConversationWorkRequest(work);
  const renderRow = (status: ReactNode) => (
    <ChatReplyMessage
      message={{ role: "assistant" }}
      continued={continued}
      identity={
        !continued && (
          <MainConversationAvatar
            name={name}
            src={author.data?.avatar}
            workChatId={workChatId}
            activity={request}
            openSource={openSource}
          />
        )
      }
      status={status}
    >
      {null}
    </ChatReplyMessage>
  );
  const snapshot = useQuery({
    ...sourceScopeOptions(subject, workChatId),
    enabled: active && Boolean(request && workChatId && executionId),
    staleTime: 0,
    retry: false,
  });
  const chat =
    snapshot.isSuccess &&
    snapshot.isFetchedAfterMount &&
    snapshot.data?.id === workChatId &&
    snapshot.data?.activeExecutionId === executionId
      ? snapshot.data
      : undefined;
  const interactions = useMainConversationInteractions(
    subject,
    workChatId,
    chat,
    active,
  );
  const pendingQuestions = interactions.pendingElicitations.filter(
    (pending) =>
      !settledQuestions?.some(
        (question) =>
          question.result && question.elicitationId === pending.elicitationId,
      ),
  );
  if (snapshot.isError) {
    return renderRow(
      <QueryRefreshError
        error={snapshot.error}
        pending={snapshot.isFetching}
        onRetry={() => void snapshot.refetch()}
      />,
    );
  }
  if (interactions.pendingApprovals.length || pendingQuestions.length) {
    return renderRow(
      <div data-chat-interaction-scope={workChatId}>
        <ChatTimelineInteractions
          {...interactions}
          pendingElicitations={pendingQuestions}
          embedded
          requestedInteractionId={requestedInteractionId}
          interactionErrorScope={`chat:${workChatId}`}
        />
      </div>,
    );
  }
  return null;
}
