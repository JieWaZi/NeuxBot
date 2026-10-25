import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CalendarDays,
  Clock3,
  MessageCircle,
  ListTodo,
  type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { getWakerWorkRecordOptions } from "../api/waker-queries";
import { WakerActivityHeatmap } from "./waker-activity-heatmap";
import { WakerPatternPanel } from "./waker-pattern-panel";

const dayMs = 86_400_000;

export function daysSinceJoined(createdAt?: number) {
  if (!createdAt) {
    return 0;
  }
  return Math.max(0, Math.floor((Date.now() - createdAt) / dayMs));
}

type WakerWorkRecordSectionProps = {
  wakerId: string;
  createdAt?: number;
};

export function WakerWorkRecordSection({
  wakerId,
  createdAt,
}: WakerWorkRecordSectionProps) {
  const titleId = useId();
  const { t } = useTranslation("waker");
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const query = useQuery(
    getWakerWorkRecordOptions({
      path: { wakerId },
      query: { timezone },
    }),
  );

  const joinedDays = daysSinceJoined(createdAt);
  const daily = query.data?.dailyChats ?? [];
  const stats: {
    icon: LucideIcon;
    label: string;
    value: string;
  }[] = [
    {
      icon: CalendarDays,
      value: t("workRecord.daysValue", { count: joinedDays }),
      label: t("workRecord.daysSinceJoined"),
    },
    {
      icon: ListTodo,
      value: String(query.data?.activeChats ?? 0),
      label: t("workRecord.activeTasks"),
    },
    {
      icon: MessageCircle,
      value: String(query.data?.totalChats ?? 0),
      label: t("workRecord.chats"),
    },
    {
      icon: Clock3,
      value: String(query.data?.totalAutomaticTasks ?? 0),
      label: t("workRecord.automaticTasks"),
    },
  ];

  return (
    <section aria-labelledby={titleId}>
      <h2 className="sr-only" id={titleId}>
        {t("workRecord.title")}
      </h2>
      {query.isPending ? (
        <p className="text-muted-foreground type-body">
          {t("workRecord.loading")}
        </p>
      ) : query.isError ? (
        <p className="text-muted-foreground type-body">
          {t("workRecord.loadError")}
        </p>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-2 @[40rem]/resource-page:grid-cols-4">
            {stats.map(({ icon: Icon, label, value }) => (
              <WakerPatternPanel
                key={label}
                className="min-h-16 rounded-md p-3"
                pattern="dots"
                surface="card"
              >
                <div className="flex min-w-0 items-start gap-3">
                  <Icon
                    aria-hidden
                    className="text-muted-foreground size-icon-sm mt-0.5 shrink-0"
                    strokeWidth={1.5}
                  />
                  <div className="min-w-0">
                    <strong className="type-page-title block font-medium tabular-nums">
                      {value}
                    </strong>
                    <span className="text-muted-foreground type-caption mt-1 block">
                      {label}
                    </span>
                  </div>
                </div>
              </WakerPatternPanel>
            ))}
          </div>
          {daily.length > 0 ? (
            <div className="mt-6">
              <WakerActivityHeatmap daily={daily} />
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}
