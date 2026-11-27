import { useState, type ReactNode } from "react";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import type {
  BotChatMessage,
  ChatPendingElicitation,
} from "../runtime/chat-projection";
import {
  approvalQueuePosition,
  ChatApprovalCard,
  nextApprovalSeenIds,
  type ApprovalSubmissionState,
} from "./chat-approval-card";
import {
  ChatElicitationCard,
  type ElicitationSubmissionState,
} from "./chat-elicitation-card";
import { useChatErrorNotices } from "./chat-error-card";
import { assistantMessageRowClassName } from "./chat-layout";
import type { ChatTimelineEvent } from "./chat-thread";

type ApprovalQueueProps = {
  pendingApprovals: readonly PendingApproval[];
  approvalSubmissionState?: ApprovalSubmissionState;
  requestedInteractionId?: string;
  onResolveApproval: (approvalId: string, optionId: string) => void;
  approvalHotkeys?: boolean;
};

type PendingQuestionProps = {
  pendingElicitations: readonly ChatPendingElicitation[];
  elicitationSubmissionState?: ElicitationSubmissionState;
  onResolveElicitation: (
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => void;
  firstQuestionActions?: ReactNode;
};

export function ChatQuestionRow({
  children,
  compact = false,
  embedded = false,
  interactionId,
}: {
  children: ReactNode;
  compact?: boolean;
  embedded?: boolean;
  interactionId?: string;
}) {
  if (embedded) {
    return (
      <div data-chat-interaction-id={interactionId} className="min-w-0">
        {children}
      </div>
    );
  }
  return (
    <div
      data-chat-interaction-id={interactionId}
      className={cn("py-2", compact ? "px-6" : assistantMessageRowClassName)}
    >
      <div className={compact ? undefined : "col-start-2 min-w-0"}>
        {children}
      </div>
    </div>
  );
}

export function SettledChatQuestion({
  question,
}: {
  question: NonNullable<BotChatMessage["question"]>;
}) {
  if (!question.result) return null;
  return (
    <ChatQuestionRow>
      <ChatElicitationCard elicitation={question} resolution={question} />
    </ChatQuestionRow>
  );
}

function TimelineInteractionErrors({
  scope,
  embedded,
}: {
  scope: string;
  embedded: boolean;
}) {
  const errors = useChatErrorNotices({ scopes: [scope] });
  return errors ? (
    <div className={embedded ? undefined : "px-6"}>{errors}</div>
  ) : null;
}

/** 在输入区与时间线使用同一审批队列位置和确认状态。 */
export function ChatApprovalQueue({
  pendingApprovals,
  approvalSubmissionState = "idle",
  requestedInteractionId,
  onResolveApproval,
  approvalHotkeys,
}: ApprovalQueueProps) {
  const pendingKey = pendingApprovals
    .map((approval) => approval.approvalId)
    .join("\0");
  const [seen, setSeen] = useState(() => ({
    pendingKey,
    ids: nextApprovalSeenIds(pendingApprovals, []),
  }));
  if (pendingKey !== seen.pendingKey) {
    setSeen({
      pendingKey,
      ids: nextApprovalSeenIds(pendingApprovals, seen.ids),
    });
  }
  const currentApproval = pendingApprovals[0];
  if (!currentApproval) return null;
  const queuedApproval = pendingApprovals.find(
    (item) =>
      item.approvalId === requestedInteractionId && item !== currentApproval,
  );
  return (
    <ChatApprovalCard
      approval={currentApproval}
      {...approvalQueuePosition(currentApproval.approvalId, seen.ids)}
      queuedApproval={queuedApproval}
      submissionState={approvalSubmissionState}
      hotkeys={approvalHotkeys}
      onResolve={onResolveApproval}
    />
  );
}

/** 将所属工作尚待处理的真实请求放在消息滚动区域。 */
export function ChatTimelineInteractions({
  pendingElicitations,
  elicitationSubmissionState,
  onResolveElicitation,
  approvalHotkeys = false,
  compact = false,
  embedded = false,
  firstQuestionActions,
  interactionErrorScope,
  ...approvalProps
}: ApprovalQueueProps &
  PendingQuestionProps & {
    compact?: boolean;
    embedded?: boolean;
    interactionErrorScope?: string;
  }) {
  const approval = approvalProps.pendingApprovals[0];
  const locatedApproval = approvalProps.pendingApprovals.find(
    (item) => item.approvalId === approvalProps.requestedInteractionId,
  );
  return (
    <>
      {approval ? (
        <ChatQuestionRow
          compact={compact}
          embedded={embedded}
          interactionId={locatedApproval?.approvalId ?? approval.approvalId}
        >
          <div className="bg-message-card-background rounded-xl">
            <ChatApprovalQueue
              {...approvalProps}
              approvalHotkeys={approvalHotkeys}
            />
          </div>
        </ChatQuestionRow>
      ) : null}
      {pendingElicitations.map((question, index) => (
        <PendingQuestionRow
          key={question.elicitationId}
          question={question}
          first={index === 0}
          compact={compact}
          embedded={embedded}
          elicitationSubmissionState={elicitationSubmissionState}
          onResolveElicitation={onResolveElicitation}
          firstQuestionActions={firstQuestionActions}
        />
      ))}
      {interactionErrorScope ? (
        <TimelineInteractionErrors
          scope={interactionErrorScope}
          embedded={embedded}
        />
      ) : null}
    </>
  );
}

function PendingQuestionRow({
  question,
  first,
  compact,
  embedded,
  elicitationSubmissionState,
  onResolveElicitation,
  firstQuestionActions,
}: Omit<PendingQuestionProps, "pendingElicitations"> & {
  question: ChatPendingElicitation;
  first: boolean;
  compact?: boolean;
  embedded?: boolean;
}) {
  return (
    <ChatQuestionRow
      compact={compact}
      embedded={embedded}
      interactionId={question.elicitationId}
    >
      <ChatElicitationCard
        elicitation={question}
        submissionState={first ? elicitationSubmissionState : "idle"}
        waitingForTurn={!first}
        onResolve={onResolveElicitation}
      />
      {first ? firstQuestionActions : null}
    </ChatQuestionRow>
  );
}

/** 分页前后保留同一提问组件的身份，历史定位补齐时保留已输入的答案。 */
export function pendingQuestionTimelineEvents(
  messages: readonly BotChatMessage[],
  { pendingElicitations, ...questionProps }: PendingQuestionProps,
): ChatTimelineEvent[] {
  const anchors = new Map<string, BotChatMessage>();
  for (const message of messages) {
    if (message.question?.elicitationId && message.id)
      anchors.set(message.question.elicitationId, message);
  }
  return pendingElicitations.flatMap((question, index) => {
    const id = question.elicitationId;
    if (!id) return [];
    const anchor = anchors.get(id);
    return [
      {
        id: `pending-question:${id}`,
        createdAt: anchor?.createdAt ?? Number.MAX_SAFE_INTEGER,
        beforeMessageId: anchor?.id,
        content: (
          <PendingQuestionRow
            {...questionProps}
            question={question}
            first={index === 0}
          />
        ),
      },
    ];
  });
}
