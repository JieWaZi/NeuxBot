import { ArrowUpRight, Clock3 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import {
  formatTime,
  scheduleSummary,
} from "@/shared/lib/automatic-task-schedule";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Card, CardTitle } from "@/shared/ui/card";

import type { BotChatMessage } from "../runtime/chat-projection";

type TaskReceipt = NonNullable<BotChatMessage["automaticTaskReceipts"]>[number];

export function AutomaticTaskReceiptCard({
  task,
  className,
  readingMessageId,
  readingSequence,
}: {
  task: TaskReceipt;
  className: string;
  readingMessageId?: string;
  readingSequence?: number;
}) {
  const { t, i18n } = useTranslation("automaticTask");
  const schedule = task.schedule;
  const timezone = schedule?.timezone;
  if (!task.taskId || !schedule || !timezone) return null;
  const nextRun = task.nextRunAt ? new Date(task.nextRunAt) : undefined;
  const once = schedule.kind === "once" && nextRun;
  const executorName = task.workflowName || task.wakerName;

  return (
    <Card
      asChild
      variant="section"
      className={cn(
        "bg-message-card-background w-full max-w-xl space-y-3 data-[chat-message-located]:bg-[color-mix(in_srgb,var(--message-card-background),var(--accent-default)_12%)]",
        className,
      )}
    >
      <article
        tabIndex={-1}
        data-chat-message-card
        data-chat-message-bubble
        data-chat-readable-body=""
        data-chat-reading-message-id={readingMessageId}
        data-chat-readable-sequence={readingSequence}
      >
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="flex min-w-0 items-start gap-2">
            <Clock3
              aria-hidden
              className="size-icon-md text-content-secondary mt-1 shrink-0"
            />
            <CardTitle className="break-words">{task.name}</CardTitle>
          </div>
          <Button asChild variant="ghost" size="sm" className="shrink-0">
            <Link to={`/automatic-tasks/${encodeURIComponent(task.taskId)}`}>
              {t("viewTask")}
              <ArrowUpRight aria-hidden />
            </Link>
          </Button>
        </div>
        <div className="type-ui text-content-secondary flex flex-wrap items-baseline gap-x-3 gap-y-1">
          {executorName && executorName !== task.name && (
            <span>{executorName}</span>
          )}
          <span>{once ? t("kinds.once") : scheduleSummary(schedule, t)}</span>
          <span>{timezone}</span>
          {task.enabled === false && <span>{t("disabled")}</span>}
          {nextRun && Number.isFinite(nextRun.getTime()) && (
            <span className="ms-auto flex max-w-full flex-wrap items-baseline justify-end gap-x-2 gap-y-1">
              <span>{t(task.updated ? "nextRun" : "firstRun")}</span>
              <time dateTime={nextRun.toISOString()}>
                {formatTime(task.nextRunAt, timezone, i18n.language)}
              </time>
            </span>
          )}
        </div>
        {task.instructionPreview && (
          <p className="type-body break-words whitespace-pre-wrap">
            {task.instructionPreview}
          </p>
        )}
      </article>
    </Card>
  );
}
