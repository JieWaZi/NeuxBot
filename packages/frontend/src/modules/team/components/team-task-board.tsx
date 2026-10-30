import { notificationPresentationProps } from "@/shared/api/notification-presentation";
import { ChevronDown, ChevronUp, Columns3 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelTask as ChannelTask } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  formatTeamTaskTime,
  groupTeamTasksByStatus,
  teamTaskGroups,
  type TeamTaskMember,
  type TeamTaskStatus,
} from "../model/team-task-presentation";
import { TeamTaskReviewActions } from "./team-task-review-actions";

type TeamTaskBoardProps = {
  selectedTaskId?: string;
  actionsDisabled?: boolean;
  channelId: string;
  errorMessage?: string;
  loading?: boolean;
  members: TeamTaskMember[];
  onRetry?: () => void;
  tasks: ChannelTask[];
  threadId?: string;
};

const taskStatusDotClass: Record<TeamTaskStatus, string> = {
  pending: "bg-muted-foreground",
  in_progress: "bg-primary",
  in_review: "bg-warning",
  completed: "bg-success",
  failed: "bg-destructive",
  cancelled: "bg-border-strong",
};

function descriptionNeedsDisclosure(description: string) {
  return description.length > 120 || description.split("\n").length > 4;
}

function TeamTaskCard({
  selected,
  actionsDisabled,
  assignee,
  channelId,
  sender,
  task,
  threadId,
}: {
  selected?: boolean;
  actionsDisabled: boolean;
  assignee?: TeamTaskMember;
  channelId: string;
  sender?: TeamTaskMember;
  task: ChannelTask;
  threadId: string;
}) {
  const { t } = useTranslation("team");
  const [expanded, setExpanded] = useState(false);
  const target = useRef<HTMLElement>(null);
  useEffect(() => {
    if (selected) {
      target.current?.scrollIntoView({ block: "nearest" });
      target.current?.focus({ preventScroll: true });
    }
  }, [selected]);
  const description = task.description ?? "";
  const canExpand = descriptionNeedsDisclosure(description);
  const time = formatTeamTaskTime(task.createdAt);
  const senderName = task.senderWakerId
    ? (sender?.name ?? t("member"))
    : t("taskBoard.user");
  const assigneeName = assignee?.name ?? t("taskBoard.unassigned");

  return (
    <article
      {...(task.status === "in_review"
        ? notificationPresentationProps({
            kind: "request",
            noticeId: `review:${task.id}:${task.currentFence}`,
          })
        : {})}
      ref={target}
      tabIndex={selected ? -1 : undefined}
      className={cn(
        "bg-surface-elevated rounded-lg p-3",
        selected && "ring-ring ring-2",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <p className="text-muted-foreground type-caption min-w-0 flex-1 truncate">
          {t("taskBoard.initiator")}
          <span className="text-foreground ml-1 font-medium" title={senderName}>
            {senderName}
          </span>
        </p>
        {time ? (
          <time
            className="text-muted-foreground type-caption shrink-0 tabular-nums"
            dateTime={new Date(task.createdAt ?? 0).toISOString()}
          >
            {time}
          </time>
        ) : null}
      </div>
      <h3 className="type-label mt-2 font-medium break-words">
        {task.subject}
      </h3>
      {description ? (
        <div className="mt-2">
          <p
            className={cn(
              "text-muted-foreground type-body break-words whitespace-pre-wrap",
              canExpand && !expanded && "line-clamp-3",
            )}
          >
            {description}
          </p>
          {canExpand ? (
            <Button
              className="mt-1"
              onClick={() => setExpanded((current) => !current)}
              size="sm"
              type="button"
              variant="ghost"
            >
              {expanded ? <ChevronUp /> : <ChevronDown />}
              {expanded
                ? t("taskBoard.collapseContent")
                : t("taskBoard.expandContent")}
            </Button>
          ) : null}
        </div>
      ) : null}
      <div className="bg-surface-tertiary mt-3 flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5">
        <span className="text-muted-foreground type-caption shrink-0">
          {t("taskBoard.assignee")}
        </span>
        <span
          className="type-body min-w-0 truncate font-medium"
          title={assigneeName}
        >
          {assigneeName}
        </span>
      </div>
      {task.status === "in_review" ? (
        <TeamTaskReviewActions
          actionsDisabled={actionsDisabled}
          channelId={channelId}
          task={task}
          threadId={threadId}
        />
      ) : null}
    </article>
  );
}

function TeamTaskColumnLoading() {
  return (
    <div className="grid gap-2 p-2">
      <Skeleton className="h-32 w-full rounded-lg" />
      <Skeleton className="h-24 w-full rounded-lg" />
    </div>
  );
}

export function TeamTaskBoard({
  selectedTaskId,
  actionsDisabled = false,
  channelId,
  errorMessage,
  loading = false,
  members,
  onRetry,
  tasks,
  threadId,
}: TeamTaskBoardProps) {
  const { t } = useTranslation("team");
  const memberById = new Map(
    members.map((member) => [member.id, member] as const),
  );
  const tasksByStatus = groupTeamTasksByStatus(tasks);

  if (errorMessage) {
    return (
      <section
        aria-label={t("taskBoard.boardTitle")}
        className="bg-muted/20 grid h-full min-h-0 place-items-center p-6 text-center"
      >
        <div>
          <p className="text-destructive type-body" role="alert">
            {errorMessage}
          </p>
          {onRetry ? (
            <Button
              className="mt-3"
              onClick={onRetry}
              size="sm"
              variant="secondary"
            >
              {t("common:retry")}
            </Button>
          ) : null}
        </div>
      </section>
    );
  }

  if (!loading && !threadId) {
    return (
      <section
        aria-label={t("taskBoard.boardTitle")}
        className="bg-muted/20 grid h-full min-h-0 place-items-center p-6 text-center"
      >
        <p className="text-muted-foreground type-body max-w-96">
          {t("taskBoard.noThread")}
        </p>
      </section>
    );
  }

  if (!loading && tasks.length === 0) {
    return (
      <section
        aria-label={t("taskBoard.boardTitle")}
        className="bg-surface-panel grid h-full min-h-0 place-items-center p-6 text-center"
      >
        <div className="flex max-w-64 flex-col items-center gap-3">
          <span className="bg-surface-tertiary text-muted-foreground grid size-10 place-items-center rounded-xl">
            <Columns3 className="size-icon-md" />
          </span>
          <p className="text-muted-foreground type-body">
            {t("taskBoard.empty")}
          </p>
        </div>
      </section>
    );
  }

  return (
    <section
      aria-busy={loading || undefined}
      aria-label={t("taskBoard.boardTitle")}
      className="bg-surface-panel h-full min-h-0 min-w-0 overflow-y-auto p-3"
    >
      <div className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,17rem),1fr))] items-start gap-3">
        {teamTaskGroups.map((group) => {
          const groupTasks = tasksByStatus.get(group.id) ?? [];
          return (
            <section
              aria-labelledby={`team-task-board-column-${group.id}`}
              className="bg-surface-tertiary flex min-w-0 flex-col gap-3 rounded-xl p-3"
              key={group.id}
            >
              <header className="min-h-row-default flex shrink-0 items-center gap-2 px-1">
                <span
                  aria-hidden="true"
                  className={cn(
                    "size-2 shrink-0 rounded-full",
                    taskStatusDotClass[group.id],
                  )}
                />
                <h2
                  className="type-label min-w-0 flex-1 truncate font-medium"
                  id={`team-task-board-column-${group.id}`}
                >
                  {t(group.labelKey)}
                </h2>
                {loading ? (
                  <Skeleton className="h-4 w-5" />
                ) : (
                  <span className="bg-surface-panel text-muted-foreground type-caption min-w-5 rounded-full px-1.5 py-0.5 text-center tabular-nums">
                    {groupTasks.length}
                  </span>
                )}
              </header>
              <div className="grid gap-2">
                {loading ? <TeamTaskColumnLoading /> : null}
                {!loading && threadId && groupTasks.length > 0
                  ? groupTasks.flatMap((task) =>
                      task.id
                        ? [
                            <TeamTaskCard
                              selected={task.id === selectedTaskId}
                              actionsDisabled={actionsDisabled}
                              assignee={memberById.get(
                                task.assigneeWakerId ?? "",
                              )}
                              channelId={channelId}
                              key={task.id}
                              sender={memberById.get(task.senderWakerId ?? "")}
                              task={task}
                              threadId={threadId}
                            />,
                          ]
                        : [],
                    )
                  : null}
                {!loading && groupTasks.length === 0 ? (
                  <p className="text-muted-foreground type-caption px-1 py-2">
                    {t("taskBoard.emptyGroup")}
                  </p>
                ) : null}
              </div>
            </section>
          );
        })}
      </div>
    </section>
  );
}
