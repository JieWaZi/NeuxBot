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
import { AssistantReplyWaitStatus, ChatReplyMessage } from "./chat-thread";
import { ChatTimelineInteractions } from "./chat-timeline-interactions";
import {
  MainConversationAvatar,
  type OpenMainConversationSource,
} from "./main-conversation-avatar";

type WaitProps = {
  subject: ConversationSubject;
  active: boolean;
  openSource: OpenMainConversationSource;
};

export function MainConversationWait({
  subject,
  active,
  work,
  continued,
  openSource,
  requestedInteractionId,
  settledQuestions,
}: WaitProps & {
  work?: MainConversationWork;
  continued?: boolean;
  requestedInteractionId?: string;
  settledQuestions?: PublicMessage["questions"];
}) {
  const { t } = useTranslation("chat");
  const owner = subject.subjectType === "waker" ? subject.subjectId : "";
  const actor = work?.sourceWakerId || owner;
  const author = useQuery({
    ...getWakerOptions({ path: { wakerId: actor } }),
    enabled: active && !continued && Boolean(actor),
  });
  const name = author.data?.name || t("wakerFallback");
  const sourceId = work?.workChatId ?? "";
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
            workChatId={sourceId}
            activity={request ?? "working"}
            openSource={openSource}
          />
        )
      }
      status={status}
    >
      {null}
    </ChatReplyMessage>
  );
  if (request && sourceId && work?.executionId) {
    return (
      <WorkRequests
        subject={subject}
        active={active}
        workChatId={sourceId}
        executionId={work.executionId}
        waitingForInput={request === "input"}
        requestedInteractionId={requestedInteractionId}
        settledQuestions={settledQuestions}
        renderRow={renderRow}
      />
    );
  }
  if (work?.hasPublicReply) return null;
  return renderRow(<AssistantReplyWaitStatus showWaitingText />);
}

function WorkRequests({
  subject,
  active,
  workChatId,
  executionId,
  waitingForInput,
  requestedInteractionId,
  settledQuestions,
  renderRow,
}: {
  subject: ConversationSubject;
  active: boolean;
  workChatId: string;
  executionId: string;
  waitingForInput: boolean;
  requestedInteractionId?: string;
  settledQuestions?: PublicMessage["questions"];
  renderRow: (status: ReactNode) => ReactNode;
}) {
  const { t } = useTranslation("chat");
  const snapshot = useQuery({
    ...sourceScopeOptions(subject, workChatId),
    enabled: active,
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
  if (pendingQuestions.length !== interactions.pendingElicitations.length)
    return null;
  return renderRow(
    <span
      className="text-content-secondary type-ui min-h-avatar-sm inline-flex items-center"
      role="status"
      aria-busy={snapshot.isFetching || undefined}
    >
      {t(waitingForInput ? "waitingElicitation" : "waitingApproval")}
    </span>,
  );
}
