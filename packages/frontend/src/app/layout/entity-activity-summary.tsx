import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { CountBadge } from "@/shared/ui/count-badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

import type { NavigationEntity } from "./entity-navigation";

export type EntityActivityCounts = {
  runningCount: number;
  waitingCount: number;
};

export function entityActivityCounts(
  item: NavigationEntity,
): EntityActivityCounts {
  if (item.kind === "waker") {
    return {
      runningCount: item.waker.runningCount ?? 0,
      waitingCount: item.waker.waitingCount ?? 0,
    };
  }
  return {
    runningCount: item.team.runningCount ?? 0,
    waitingCount: item.team.waitingCount ?? 0,
  };
}

function activitySummaryLabel(
  counts: EntityActivityCounts,
  labels: {
    running: (count: number) => string;
    waiting: (count: number) => string;
  },
) {
  const parts: string[] = [];
  if (counts.runningCount > 0) {
    parts.push(labels.running(counts.runningCount));
  }
  if (counts.waitingCount > 0) {
    parts.push(labels.waiting(counts.waitingCount));
  }
  return parts.join(" · ");
}

export function entityActivitySummaryLabel(
  counts: EntityActivityCounts,
  t: (key: string, options?: { count: number }) => string,
) {
  return activitySummaryLabel(counts, {
    running: (count) => t("partnerNavigation.runningChatCount", { count }),
    waiting: (count) => t("partnerNavigation.waitingChatCount", { count }),
  });
}

export function EntityActivityBadges({
  className,
  counts,
  label,
}: {
  className?: string;
  counts: EntityActivityCounts;
  label?: string;
}) {
  if (counts.runningCount <= 0 && counts.waitingCount <= 0) return null;

  return (
    <span
      aria-label={label}
      className={cn("inline-flex items-center gap-1", className)}
      role={label ? "status" : undefined}
    >
      <CountBadge count={counts.runningCount} variant="success" />
      <CountBadge count={counts.waitingCount} variant="warning" />
    </span>
  );
}

export function EntityActivitySummary({
  className,
  counts,
}: {
  className?: string;
  counts: EntityActivityCounts;
}) {
  const { t } = useTranslation("common");
  const label = entityActivitySummaryLabel(counts, t);

  if (!label) return null;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <EntityActivityBadges
          className={className}
          counts={counts}
          label={label}
        />
      </TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}
