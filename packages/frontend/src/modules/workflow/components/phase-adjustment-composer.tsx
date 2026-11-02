import { useMutation } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChatComposerPanel } from "@/modules/chat";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowCreationSession as ChatSession } from "@/shared/api/generated/types.gen";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { Badge } from "@/shared/ui/badge";
import {
  createWorkflowAnnotationMutation,
  sendWorkflowCreationMessageMutation,
} from "../api/workflow-queries";
import type { ExecutionDefaults, WorkflowPhase } from "../model/types";

/** 阶段内发送修改要求；批注固定用户阅读的版本与稳定阶段身份。 */
export function PhaseAdjustmentComposer({
  id,
  revisionId,
  phase,
  ordinal,
  session,
  defaults,
  onAccepted,
}: {
  id: string;
  revisionId: string;
  phase: WorkflowPhase;
  ordinal: number;
  session?: ChatSession;
  defaults?: ExecutionDefaults;
  onAccepted: (sessionId: string) => void;
}) {
  const { t } = useTranslation("workflow");
  const [isSubmitting, setSubmitting] = useState(false);
  const annotation = useMutation(
    withLocalErrorMutation(createWorkflowAnnotationMutation()),
  );
  const send = useMutation(
    withLocalErrorMutation(sendWorkflowCreationMessageMutation()),
  );
  const request = useRef<
    { signature: string; annotation: string; send: string } | undefined
  >(undefined);
  const acceptedSession = useRef<string | undefined>(undefined);

  return (
    <section
      className="grid gap-3 px-6 pt-2 pb-6"
      aria-label={t("phaseAdjustment")}
    >
      <Badge variant="secondary" className="justify-self-start">
        {t("phaseOrdinal", { ordinal: String(ordinal).padStart(2, "0") })}
      </Badge>
      <ChatComposerPanel
        scopeKey={`workflow:${id}:phase:${revisionId}:${phase.id}:${session?.chatId ?? "new"}`}
        chatId={session?.chatId || undefined}
        wakerId={PERSONAL_ASSISTANT_WAKER_ID}
        directoryLabel={t("defaultDirectory")}
        defaults={defaults}
        active={Boolean(revisionId && phase.id)}
        isSendDisabled={isSubmitting}
        onAccepted={() => {
          if (acceptedSession.current) onAccepted(acceptedSession.current);
        }}
        prepareSubmission={({
          text: messageText,
          runtimeId,
          accountId,
          model,
          reasoning,
          permissionLevel,
          ...input
        }) => {
          const text = messageText ?? "";
          const body = {
            ...input,
            sessionId: session?.id,
            defaults: {
              runtimeId,
              accountId,
              model,
              reasoning,
              permissionLevel,
            },
            content: t("phaseAdjustmentMessage", {
              title: phase.title,
              text,
            }),
          };
          const signature = JSON.stringify([revisionId, phase.id, body]);
          if (request.current?.signature !== signature)
            request.current = {
              signature,
              annotation: crypto.randomUUID(),
              send: crypto.randomUUID(),
            };
          const receipt = request.current;
          return {
            submissionId: receipt.send,
            confirmAccepted: () => {
              if (request.current === receipt) request.current = undefined;
            },
            accept: async () => {
              if (!phase.id || !revisionId)
                throw new Error("workflow_phase_not_available");
              setSubmitting(true);
              try {
                await annotation.mutateAsync({
                  path: { id },
                  body: {
                    requestId: receipt.annotation,
                    baseRevisionId: revisionId,
                    nodeId: phase.id,
                    text,
                  },
                });
                const accepted = await send.mutateAsync({
                  path: { id },
                  body: { ...body, requestId: receipt.send },
                });
                if (
                  !accepted.id ||
                  !accepted.chatId ||
                  !accepted.acceptedExecutionId
                )
                  throw new Error("workflow_conversation_not_accepted");
                acceptedSession.current = accepted.id;
                // 交付已接纳的权威快照，阶段批注和定义变化由原领域事件对账。
                return {
                  chat: accepted.chat,
                  execution: accepted.execution,
                };
              } finally {
                setSubmitting(false);
              }
            },
          };
        }}
      />
    </section>
  );
}
