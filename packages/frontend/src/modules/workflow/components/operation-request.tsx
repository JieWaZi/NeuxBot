import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { answerWorkflowOperationMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowOperationRequest as OperationRequest } from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { refreshWorkflow } from "../api/workflow-queries";
import { jsonText } from "../model/types";

export function OperationRequestCard({
  id,
  runId,
  operation,
}: {
  id: string;
  runId: string;
  operation: OperationRequest;
}) {
  const { t } = useTranslation("workflow");
  const client = useQueryClient();
  const answer = useMutation(
    withLocalErrorMutation(answerWorkflowOperationMutation()),
  );
  const [requests] = useState(() => ({
    allow: crypto.randomUUID(),
    deny: crypto.randomUUID(),
  }));
  const submit = async (allowed: boolean) => {
    try {
      await answer.mutateAsync({
        path: { id, runId, operationId: operation.id ?? "" },
        body: {
          requestId: allowed ? requests.allow : requests.deny,
          descriptorDigest: operation.descriptorDigest ?? "",
          allowed,
        },
      });
      await refreshWorkflow(client, id, runId);
    } catch {
      /* Mutation owns error. */
    }
  };
  return (
    <Card
      variant="section"
      {...notificationPresentationProps({
        kind: "request",
        noticeId: `operation:${operation.id}`,
      })}
    >
      <div className="grid gap-3">
        <div className="flex items-center gap-2">
          <ShieldCheck className="text-muted-foreground size-4" />
          <h3 className="type-body font-medium">{t("operationApproval")}</h3>
        </div>
        <p className="type-body">{operation.description}</p>
        <p className="text-muted-foreground type-caption">
          {t("operationApprovalHelp")}
        </p>
        <pre className="bg-muted type-caption max-h-64 overflow-auto rounded-lg p-3">
          {jsonText(operation.descriptor)}
        </pre>
        {answer.error ? (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(answer.error, t("answerError"))}
          </p>
        ) : null}
        {operation.status === "waiting" ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="secondary"
              disabled={answer.isPending}
              onClick={() => void submit(false)}
            >
              {t("denyOperation")}
            </Button>
            <Button
              disabled={answer.isPending}
              onClick={() => void submit(true)}
            >
              {t("allowOperation")}
            </Button>
          </div>
        ) : (
          <p className="text-muted-foreground type-body">
            {operation.allowed ? t("operationAllowed") : t("operationDenied")}
          </p>
        )}
      </div>
    </Card>
  );
}
