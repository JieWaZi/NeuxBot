import { useTranslation } from "react-i18next";

import type { TeamMemberActivityPhase } from "../model/team-member-activity";
import { cn } from "@/shared/lib/cn";
import { Skeleton } from "@/shared/ui/skeleton";
import { Button } from "@/shared/ui/button";
import { Avatar } from "@/shared/ui/avatar";

export type TeamMemberStripItem = {
  avatar?: string;
  chatId?: string;
  id: string;
  name: string;
  phase: TeamMemberActivityPhase;
  selectable: boolean;
};

const phaseClassName: Record<TeamMemberActivityPhase, string> = {
  failed: "bg-destructive",
  idle: "bg-border",
  planning: "bg-primary",
  queued: "bg-muted-foreground",
  reviewing: "bg-warning",
  running: "bg-success",
  waiting_approval: "bg-warning",
  waiting_input: "bg-warning",
};

export function TeamMemberStrip({
  activeMemberId,
  loading = false,
  members,
  onOpenMember,
}: {
  activeMemberId?: string;
  loading?: boolean;
  members: TeamMemberStripItem[];
  onOpenMember: (member: TeamMemberStripItem) => void;
}) {
  const { t } = useTranslation("team");
  return (
    <div
      aria-busy={loading || undefined}
      aria-label={t("memberActivity.navigation")}
      className="grid min-w-0 shrink-0 gap-0.5"
      role="navigation"
    >
      {loading && members.length === 0
        ? Array.from({ length: 3 }, (_, index) => (
            <div
              key={index}
              className="min-h-row-compact flex items-center gap-3"
            >
              <Skeleton className="size-avatar-xs rounded-sm" />
              <Skeleton className="h-4 w-16" />
            </div>
          ))
        : null}
      {members.map((member) => {
        const status = t(`memberActivity.${member.phase}`);
        return (
          <Button
            key={member.id}
            type="button"
            size="row"
            variant="ghost"
            aria-label={t("memberActivity.open", {
              name: member.name,
              status,
            })}
            aria-pressed={activeMemberId === member.id}
            disabled={!member.selectable}
            onClick={() => onOpenMember(member)}
            className="min-w-0 text-left"
          >
            <Avatar
              name={member.name}
              src={member.avatar}
              size="xs"
              animated={false}
              interactive={false}
            />
            <span className="min-w-0 flex-1 truncate font-medium">
              {member.name}
            </span>
            <span className="text-muted-foreground type-caption flex shrink-0 items-center gap-1.5">
              <span
                aria-hidden="true"
                className={cn(
                  "size-2 shrink-0 rounded-full",
                  phaseClassName[member.phase],
                  (member.phase === "running" || member.phase === "planning") &&
                    "animate-pulse",
                )}
              />
              <span className="shrink-0">{status}</span>
            </span>
          </Button>
        );
      })}
    </div>
  );
}
