import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

export type TeamWorkProgress = {
  activeMemberName?: string;
  activeTaskSubject?: string;
  phase:
    | "coordinating"
    | "executing"
    | "idle"
    | "review"
    | "stopping"
    | "summarizing";
  review: number;
  terminal: number;
  total: number;
};

export function TeamWorkStatus({
  activeMembers = [],
  onOpenMember,
  progress,
}: {
  activeMembers?: readonly {
    id: string;
    name: string;
    avatar?: string;
    activeTaskSubject?: string;
  }[];
  onOpenMember?: (memberId: string) => void;
  progress: TeamWorkProgress;
}) {
  const { t } = useTranslation("chat");
  if (progress.phase === "idle") return null;

  const title = t(`teamStatus.${progress.phase}.title`);
  const details: string[] = [];
  if (
    progress.phase === "executing" &&
    progress.activeMemberName &&
    progress.activeTaskSubject
  ) {
    details.push(
      t("teamStatus.executing.activeTask", {
        member: progress.activeMemberName,
        task: progress.activeTaskSubject,
      }),
    );
  }
  if (progress.phase === "review") {
    details.push(
      t("teamStatus.review.detail", {
        count: progress.review,
      }),
    );
  }
  if (progress.phase === "coordinating") {
    details.push(t("teamStatus.coordinating.detail"));
  }
  if (progress.phase === "summarizing") {
    details.push(t("teamStatus.summarizing.detail"));
  }
  if (progress.phase === "stopping") {
    details.push(t("teamStatus.stopping.detail"));
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className="flex min-w-0 flex-1 items-center gap-2"
    >
      <span
        aria-hidden="true"
        className={cn(
          "size-2 shrink-0 animate-pulse rounded-full",
          progress.phase === "review" || progress.phase === "stopping"
            ? "bg-warning"
            : "bg-success",
        )}
      />
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="shrink-0 font-medium">{title}</span>
        {activeMembers.length > 0 ? (
          <div className="flex max-w-full shrink-0 flex-wrap items-center">
            {activeMembers.map((member) => {
              const label = t("teamStatus.openMemberChat", {
                name: member.name,
              });
              const description = member.activeTaskSubject
                ? t("teamStatus.executing.activeTask", {
                    member: member.name,
                    task: member.activeTaskSubject,
                  })
                : details.length > 0 && progress.phase !== "executing"
                  ? `${member.name} · ${details.join(" · ")}`
                  : t("teamStatus.executing.activeMember", {
                      member: member.name,
                    });
              return (
                <Tooltip key={member.id}>
                  <TooltipTrigger asChild>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={label}
                      disabled={!onOpenMember}
                      onClick={() => onOpenMember?.(member.id)}
                    >
                      <Avatar
                        name={member.name}
                        src={member.avatar}
                        size="xs"
                        animated={false}
                        interactive={false}
                      />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{description}</TooltipContent>
                </Tooltip>
              );
            })}
          </div>
        ) : details.length > 0 ? (
          <span className="text-muted-foreground min-w-0 truncate">
            {details.join(" · ")}
          </span>
        ) : null}
      </div>
    </div>
  );
}
