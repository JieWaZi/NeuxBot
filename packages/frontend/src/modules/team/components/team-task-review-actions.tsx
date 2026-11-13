import { useMutation } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { Check, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelTask as ChannelTask } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";
import { reviewChannelTaskMutation } from "../api/channel-queries";

type TeamTaskReviewActionsProps = {
  actionsDisabled: boolean;
  channelId: string;
  task: ChannelTask;
  threadId: string;
};

export function TeamTaskReviewActions({
  actionsDisabled,
  channelId,
  task,
  threadId,
}: TeamTaskReviewActionsProps) {
  const { t } = useTranslation("team");
  const review = useMutation(
    withLocalErrorMutation(reviewChannelTaskMutation()),
  );
  const [rejectOpen, setRejectOpen] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [feedbackTouched, setFeedbackTouched] = useState(false);
  const [actionError, setActionError] = useState("");
  const path = { channelId, threadId, taskId: task.id ?? "" };
  const expectedFence = task.currentFence ?? 0;
  const disabled = actionsDisabled || review.isPending || expectedFence <= 0;

  const run = async (action: () => Promise<unknown>) => {
    if (disabled) return false;
    setActionError("");
    try {
      await action();
      return true;
    } catch {
      setActionError(t("taskBoard.actionFailed"));
      return false;
    }
  };

  const rejectTask = async () => {
    const nextFeedback = feedback.trim();
    setFeedbackTouched(true);
    if (!nextFeedback) return;
    if (
      await run(() =>
        review.mutateAsync({
          path,
          body: { approved: false, feedback: nextFeedback, expectedFence },
        }),
      )
    ) {
      setRejectOpen(false);
      setFeedback("");
      setFeedbackTouched(false);
    }
  };

  return (
    <>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="success"
          disabled={disabled}
          onClick={() =>
            void run(() =>
              review.mutateAsync({
                path,
                body: { approved: true, expectedFence },
              }),
            )
          }
        >
          <Check />
          {t("taskBoard.approve")}
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={disabled}
          onClick={() => setRejectOpen(true)}
        >
          <X />
          {t("taskBoard.reject")}
        </Button>
      </div>
      {actionError ? (
        <p role="alert" className="text-destructive type-caption mt-3">
          {actionError}
        </p>
      ) : null}
      <Dialog
        open={rejectOpen}
        onOpenChange={(nextOpen) => {
          if (disabled) return;
          setRejectOpen(nextOpen);
          if (!nextOpen) {
            setFeedback("");
            setFeedbackTouched(false);
          }
        }}
      >
        <DialogContent
          size="sm"
          title={t("taskBoard.rejectTitle")}
          description={t("taskBoard.rejectDescription")}
        >
          <div className="grid gap-2 px-6 pb-5">
            <label className="type-label" htmlFor={`task-feedback-${task.id}`}>
              {t("taskBoard.rejectFeedback")}
            </label>
            <Textarea
              id={`task-feedback-${task.id}`}
              disabled={disabled}
              value={feedback}
              onChange={(event) => setFeedback(event.target.value)}
              placeholder={t("taskBoard.rejectPlaceholder")}
              aria-invalid={feedbackTouched && !feedback.trim()}
            />
            {feedbackTouched && !feedback.trim() ? (
              <p role="alert" className="text-destructive type-caption">
                {t("taskBoard.feedbackRequired")}
              </p>
            ) : null}
            {actionError ? (
              <p role="alert" className="text-destructive type-caption">
                {actionError}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              disabled={disabled}
              onClick={() => setRejectOpen(false)}
            >
              {t("common:cancel")}
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={disabled || !feedback.trim()}
              onClick={() => void rejectTask()}
            >
              {t("taskBoard.confirmReject")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
