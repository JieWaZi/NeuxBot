import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { answerWorkflowOperationMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowOperationRequest as OperationRequest } from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { jsonText } from "../model/types";
import {
  useWorkflowDecisionDraft,
  workflowDecisionDraftKey,
} from "../model/human-answer-draft";
import {
  isWorkflowDecisionConflict,
  refreshWorkflowDecisionQueries,
} from "../api/workflow-decision-queries";

export function OperationRequestCard({
  id,
  runId,
  chatId,
  operation,
  onSettled,
  compact = false,
}: {
  id: string;
  runId: string;
  chatId?: string;
  operation: OperationRequest;
  onSettled?: () => void;
  compact?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const client = useQueryClient();
  const answer = useMutation(
    withLocalErrorMutation(answerWorkflowOperationMutation()),
  );
  const operationId = operation.id;
  const descriptorDigest = operation.descriptorDigest;
  const waiting = operation.status === "waiting";
  const { draft, start, finish } = useWorkflowDecisionDraft(
    workflowDecisionDraftKey(
      "operation",
      id,
      runId,
      operationId,
      descriptorDigest,
    ),
    operation.status,
  );
  const submission = draft.submission;
  const disabled =
    !operationId ||
    !descriptorDigest ||
    Boolean(submission && submission.status !== "failed");
  const submit = async (allowed: boolean) => {
    if (!waiting || disabled || !operationId || !descriptorDigest) return;
    const requestId = start(String(allowed));
    if (!requestId) return;
    try {
      await answer.mutateAsync({
        path: { id, runId, operationId },
        body: {
          requestId,
          descriptorDigest,
          allowed,
        },
      });
      finish(requestId, "accepted");
    } catch (error) {
      if (isWorkflowDecisionConflict(error)) {
        finish(requestId, "conflict", error);
        void refreshWorkflowDecisionQueries(client, id, runId, chatId);
        return;
      }
      finish(requestId, "failed", error);
    }
    onSettled?.();
  };
  const descriptor = (
    <pre className="bg-muted type-caption max-h-64 overflow-auto rounded-lg p-3">
      {jsonText(operation.descriptor)}
    </pre>
  );
  return (
    <Card variant="section">
      <div className="grid gap-3">
        <div className="flex items-center gap-2">
          <ShieldCheck className="text-muted-foreground size-4" />
          <h3 className="type-body font-medium">{t("operationApproval")}</h3>
        </div>
        <p className="type-body">{operation.description}</p>
        {compact ? (
          <details>
            <summary className="type-ui text-content-muted focus-visible:ring-ring mb-2 w-fit cursor-pointer rounded-sm outline-none focus-visible:ring-2">
              {t("details")}
            </summary>
            {descriptor}
          </details>
        ) : (
          <>
            <p className="text-muted-foreground type-caption">
              {t("operationApprovalHelp")}
            </p>
            {descriptor}
          </>
        )}
        {waiting &&
        (submission?.status === "failed" ||
          submission?.status === "conflict") ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(submission.error, t("answerError"))}
          </p>
        ) : null}
        {waiting ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="secondary"
              disabled={disabled}
              onClick={() => void submit(false)}
            >
              {t("denyOperation")}
            </Button>
            <Button disabled={disabled} onClick={() => void submit(true)}>
              {t("allowOperation")}
            </Button>
          </div>
        ) : (
          <p className="text-muted-foreground type-body">
            {operation.status === "answered"
              ? operation.allowed
                ? t("operationAllowed")
                : t("operationDenied")
              : t(`status.${operation.status ?? "unknown"}`)}
          </p>
        )}
      </div>
    </Card>
  );
}
