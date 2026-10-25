import { useTranslation } from "react-i18next";

import type { ChatRunMetrics } from "../runtime/chat-projection";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

const tokenCountFormat = new Intl.NumberFormat("zh-CN", {
  maximumFractionDigits: 1,
});

export function formatTokenCount(value: number) {
  if (value >= 1_000_000) {
    return `${tokenCountFormat.format(value / 1_000_000)}M`;
  }
  if (value >= 1_000) {
    return `${tokenCountFormat.format(value / 1_000)}k`;
  }
  return tokenCountFormat.format(value);
}

export function contextUsagePercent(metrics: ChatRunMetrics) {
  const used = metrics.contextUsedTokens ?? 0;
  const window = metrics.contextWindowTokens ?? 0;
  if (used <= 0 || window <= 0) return 0;
  return Math.min(100, Math.round((used / window) * 1000) / 10);
}

const contextUsageRingSize = 14;

function ContextUsageRing({ percent }: { percent: number }) {
  const radius = (contextUsageRingSize - 2) / 2;
  const center = contextUsageRingSize / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.max(0, Math.min(100, percent));
  const dashOffset = circumference * (1 - clamped / 100);

  return (
    <svg
      viewBox={`0 0 ${contextUsageRingSize} ${contextUsageRingSize}`}
      className="size-icon-sm shrink-0 -rotate-90"
      aria-hidden
    >
      <circle
        cx={center}
        cy={center}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        className="opacity-35"
      />
      {clamped > 0 ? (
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={dashOffset}
          className="duration-layout transition-[stroke-dashoffset] motion-reduce:transition-none"
        />
      ) : null}
    </svg>
  );
}

type ChatContextUsageProps = {
  metrics?: ChatRunMetrics;
};

export function ChatContextUsage({ metrics }: ChatContextUsageProps) {
  const { t } = useTranslation("chat");
  if (!metrics?.contextWindowTokens) return null;

  const used = metrics.contextUsedTokens ?? 0;
  const window = metrics.contextWindowTokens;
  const percent = contextUsagePercent(metrics);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          className="text-muted-foreground"
          aria-label={t("contextUsage.details")}
          title={t("contextUsage.details")}
        >
          <ContextUsageRing percent={percent} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="center" className="w-[280px]">
        <DropdownMenuLabel>{t("contextUsage.title")}</DropdownMenuLabel>
        <div className="type-body space-y-3 px-2 pb-2">
          <div className="space-y-1">
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">
                {t("contextUsage.context")}
              </span>
              <span className="tabular-nums">
                {formatTokenCount(used)} / {formatTokenCount(window)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">
                {t("contextUsage.occupancy")}
              </span>
              <span className="tabular-nums">{percent}%</span>
            </div>
            <div
              className="bg-muted h-1.5 overflow-hidden rounded-full"
              aria-hidden
            >
              <div
                className="bg-primary h-full rounded-full transition-[width]"
                style={{ width: `${percent}%` }}
              />
            </div>
          </div>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
