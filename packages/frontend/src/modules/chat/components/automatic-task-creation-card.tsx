import { ArrowUpRight, Clock3 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import {
  scheduleSummary,
  formatTime,
} from "@/shared/lib/automatic-task-schedule";
import { Button } from "@/shared/ui/button";
import { Card, CardTitle } from "@/shared/ui/card";

import type { BotChatMessage } from "../runtime/chat-projection";

type TaskCreation = NonNullable<
  BotChatMessage["automaticTaskCreations"]
>[number];

export function AutomaticTaskCreationCard({
  task,
  readingMessageId,
  readingSequence,
}: {
  task: TaskCreation;
  readingMessageId?: string;
  readingSequence?: number;
}) {
  const { t, i18n } = useTranslation("automaticTask");
  const schedule = task.schedule;
  const timezone = schedule?.timezone;
  if (!task.taskId || !schedule || !timezone || !task.firstRunAt) return null;
  const firstRun = formatTime(task.firstRunAt, timezone, i18n.language);
  const once = schedule.kind === "once";

  return (
    <Card
      asChild
      variant="section"
      className="bg-message-card-background col-start-2 w-full max-w-xl space-y-3"
    >
      <article
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
          <span>{task.wakerName}</span>
          <span>{once ? t("kinds.once") : scheduleSummary(schedule, t)}</span>
          <span>{timezone}</span>
          <span className="ms-auto flex max-w-full flex-wrap items-baseline justify-end gap-x-2 gap-y-1">
            <span>{t("firstRun")}</span>
            <time dateTime={new Date(task.firstRunAt).toISOString()}>
              {firstRun}
            </time>
          </span>
        </div>
        <p className="type-body break-words whitespace-pre-wrap">
          {task.instructionPreview}
        </p>
      </article>
    </Card>
  );
}
