import {
  reportChatError,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import { i18n } from "@/shared/i18n";
import { ApiError } from "@/shared/api/errors";
import { useCallback, useRef, useState } from "react";

import type { ApprovalSubmissionState } from "../components/chat-approval-card";
import type { ElicitationSubmissionState } from "../components/chat-elicitation-card";
import type {
  ChatInvocationGuard,
  ChatScopeKey,
} from "./conversation-identity";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsPendingApproval as PendingApproval } from "@/shared/api/generated/types.gen";
import type { ChatPendingElicitation } from "../runtime/chat-projection";

type ChatInteractionOptions = {
  chatId?: string;
  conversationKey: ChatScopeKey;
  guard: ChatInvocationGuard;
  onSettled: (chatId: string) => void;
  onElicitationSettled?: () => void;
  pendingApprovals: readonly PendingApproval[];
  pendingElicitations: readonly ChatPendingElicitation[];
  queueKey: string;
  resolveApproval: (
    chatId: string,
    approvalId: string,
    optionId: string,
  ) => Promise<unknown>;
  resolveElicitation: (
    chatId: string,
    elicitationId: string,
    content: Record<string, unknown>,
    declined: boolean,
  ) => Promise<unknown>;
};

type ApprovalSubmission = {
  approvalId?: string;
  conversationKey?: ChatScopeKey;
  state: ApprovalSubmissionState;
};

type ElicitationSubmission = {
  conversationKey?: ChatScopeKey;
  elicitationId?: string;
  state: ElicitationSubmissionState;
};

/** 审批与结构化输入共用同一 ChatScopeKey 提交和晚到结果隔离策略。 */
export function useChatInteractions({
  chatId,
  conversationKey,
  guard,
  onSettled,
  onElicitationSettled,
  pendingApprovals,
  pendingElicitations,
  queueKey,
  resolveApproval,
  resolveElicitation,
}: ChatInteractionOptions) {
  const [approvalSubmission, setApprovalSubmission] =
    useState<ApprovalSubmission>({ state: "idle" });
  const [elicitationSubmission, setElicitationSubmission] =
    useState<ElicitationSubmission>({ state: "idle" });
  const inFlightElicitations = useRef(new Set<string>());
  const elicitationInProgress =
    elicitationSubmission.state !== "idle" &&
    elicitationSubmission.conversationKey === conversationKey &&
    pendingElicitations.some(
      (item) => item.elicitationId === elicitationSubmission.elicitationId,
    );

  const resolveApprovalChoice = useCallback(
    async (approvalId: string, optionId: string) => {
      const currentIsSubmitting =
        approvalSubmission.state !== "idle" &&
        approvalSubmission.conversationKey === conversationKey &&
        pendingApprovals.some(
          (approval) => approval.approvalId === approvalSubmission.approvalId,
        );
      if (!chatId || currentIsSubmitting) return;
      const invocation = guard.capture(chatId, queueKey);
      if (!guard.isCurrent(invocation) || !invocation.chatId) return;
      const capturedChatId = invocation.chatId;
      const operation = `approval:${capturedChatId}:${approvalId}`;
      const recovery = captureErrorNoticeRecovery(operation);
      setApprovalSubmission({
        approvalId,
        conversationKey: invocation.key,
        state: "submitting",
      });
      try {
        await resolveApproval(capturedChatId, approvalId, optionId);
        resolveErrorNoticeRecovery(recovery);
        if (guard.isCurrent(invocation)) {
          setApprovalSubmission({
            approvalId,
            conversationKey: invocation.key,
            state: "confirming",
          });
        }
      } catch (error) {
        reportChatError(
          error,
          `chat:${capturedChatId}`,
          i18n.t("errorActionFailed", { ns: "chat" }),
          operation,
        );
        if (guard.isCurrent(invocation)) {
          setApprovalSubmission({ state: "idle" });
        }
      } finally {
        onSettled(capturedChatId);
      }
    },
    [
      approvalSubmission,
      chatId,
      conversationKey,
      guard,
      onSettled,
      pendingApprovals,
      queueKey,
      resolveApproval,
    ],
  );

  const resolveElicitationChoice = useCallback(
    async (
      elicitationId: string,
      content: Record<string, unknown>,
      declined: boolean,
    ) => {
      if (
        !chatId ||
        elicitationInProgress ||
        pendingElicitations[0]?.elicitationId !== elicitationId
      )
        return;
      const invocation = guard.capture(chatId, queueKey);
      if (!guard.isCurrent(invocation) || !invocation.chatId) return;
      const capturedChatId = invocation.chatId;
      const operation = `elicitation:${capturedChatId}:${elicitationId}`;
      if (inFlightElicitations.current.has(operation)) return;
      inFlightElicitations.current.add(operation);
      const recovery = captureErrorNoticeRecovery(operation);
      setElicitationSubmission({
        conversationKey: invocation.key,
        elicitationId,
        state: "submitting",
      });
      const settleSubmission = (state: ElicitationSubmissionState) => {
        if (!guard.isCurrent(invocation)) return;
        setElicitationSubmission((current) =>
          current.conversationKey === invocation.key &&
          current.elicitationId === elicitationId
            ? { ...current, state }
            : current,
        );
      };
      try {
        await resolveElicitation(
          capturedChatId,
          elicitationId,
          content,
          declined,
        );
        resolveErrorNoticeRecovery(recovery);
        onElicitationSettled?.();
        settleSubmission("confirming");
      } catch (error) {
        const resolvedElsewhere =
          error instanceof ApiError &&
          error.code === "ElicitationAlreadyResolved";
        if (resolvedElsewhere) onElicitationSettled?.();
        reportChatError(
          error,
          `chat:${capturedChatId}`,
          i18n.t("errorActionFailed", { ns: "chat" }),
          operation,
        );
        settleSubmission(resolvedElsewhere ? "confirming" : "idle");
      } finally {
        inFlightElicitations.current.delete(operation);
        onSettled(capturedChatId);
      }
    },
    [
      chatId,
      elicitationInProgress,
      guard,
      onElicitationSettled,
      onSettled,
      pendingElicitations,
      queueKey,
      resolveElicitation,
    ],
  );

  return {
    approvalSubmissionState:
      approvalSubmission.conversationKey === conversationKey &&
      approvalSubmission.approvalId === pendingApprovals[0]?.approvalId
        ? approvalSubmission.state
        : "idle",
    elicitationSubmissionState: elicitationInProgress
      ? elicitationSubmission.state
      : "idle",
    resolveApprovalChoice,
    resolveElicitationChoice,
  };
}
