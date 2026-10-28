import { useQuery, useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import {
  getWorkflowRetryPlanOptions,
  retryWorkflowRunMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";
import type { Manifest } from "../model/types";
export function RetryRunDialog({
  id,
  runId,
  manifest,
}: {
  id: string;
  runId: string;
  manifest?: Manifest;
}) {
  const { t } = useTranslation("workflow");
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [requestId, setRequestId] = useState("");
  const plan = useQuery({
    ...getWorkflowRetryPlanOptions(
      withLocalErrorSurface({ path: { id, runId }, throwOnError: true }),
    ),
    enabled: open,
  });
  const retry = useMutation(withLocalErrorMutation(retryWorkflowRunMutation()));
  const submit = async () => {
    if (!plan.data?.allowed) return;
    try {
      const run = await retry.mutateAsync({
        path: { id, runId },
        body: { requestId, expectedVersion: plan.data.expectedVersion },
      });
      if (run.id) {
        setOpen(false);
        navigate(`/workflows/${id}/runs/${run.id}`);
      }
    } catch {
      /* Preserve the plan and command identity for retry. */
    }
  };
  return (
    <>
      <Button
        size="sm"
        variant="surface"
        onClick={() => {
          retry.reset();
          setRequestId(crypto.randomUUID());
          setOpen(true);
        }}
      >
        {t("rerun")}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!retry.isPending) setOpen(next);
        }}
      >
        <DialogContent title={t("retryPlan")} description={t("retryPlanHelp")}>
          <div className="grid max-h-96 gap-3 overflow-y-auto px-6 pb-5">
            {plan.isPending ? <WorkflowLoadingState /> : null}
            {plan.isError ? (
              <WorkflowQueryError
                error={plan.error}
                retrying={plan.isFetching}
                onRetry={() => void plan.refetch()}
              />
            ) : null}
            {plan.data?.items?.map((item) => (
              <div
                key={item.instanceId}
                className="bg-surface-module grid gap-1 rounded-lg p-3"
              >
                <h3 className="type-body font-medium">
                  {manifest?.nodes?.find((node) => node.id === item.nodeId)
                    ?.label ?? item.nodeId}
                </h3>
                <p className="type-ui">
                  {item.action === "evaluate_reuse"
                    ? t("retryAction.evaluate_reuse")
                    : item.action === "reconcile"
                      ? t("retryAction.reconcile")
                      : t("retryAction.rerun")}
                </p>
                <p className="text-muted-foreground type-caption">
                  {item.reason}
                </p>
              </div>
            ))}
            {retry.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(retry.error, t("error"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              variant="secondary"
              disabled={retry.isPending}
              onClick={() => setOpen(false)}
            >
              {t("close")}
            </Button>
            <Button
              disabled={
                !plan.data?.allowed || retry.isPending || plan.isFetching
              }
              onClick={() => void submit()}
            >
              {t("rerun")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
