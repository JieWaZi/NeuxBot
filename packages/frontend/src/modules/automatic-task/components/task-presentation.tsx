import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Badge } from "@/shared/ui/badge";
import type { PaginationLabels } from "@/shared/ui/pagination";
import type { TaskSchedule } from "../model/types";

export function RunState({ state }: { state?: string }) {
  const { t } = useTranslation("automaticTask");
  if (!state) return <>—</>;
  const variant =
    state === "completed"
      ? "success"
      : state === "failed" || state === "interrupted"
        ? "destructive"
        : state.startsWith("waiting_")
          ? "warning"
          : "secondary";
  return <Badge variant={variant}>{t(`state.${state}`)}</Badge>;
}

export function formatTime(
  value?: number | null,
  timezone?: string,
  language?: string,
) {
  if (!value) return "—";
  return new Intl.DateTimeFormat(language, {
    dateStyle: "medium",
    timeStyle: "short",
    ...(timezone ? { timeZone: timezone } : {}),
  }).format(value);
}

export function scheduleSummary(rule: TaskSchedule | undefined, t: TFunction) {
  if (!rule?.kind) return "—";
  if (rule.kind === "cron") return rule.cron || "—";
  if (rule.kind === "once") return rule.onceAt?.replace("T", " ") || "—";
  const time = `${String(rule.hour ?? 0).padStart(2, "0")}:${String(rule.minute ?? 0).padStart(2, "0")}`;
  if (rule.kind === "hourly")
    return `${t("kinds.hourly")} :${String(rule.minute ?? 0).padStart(2, "0")}`;
  if (rule.kind === "weekly")
    return `${t(`weekdays.${rule.weekday ?? 0}`)} ${time}`;
  if (rule.kind === "monthly")
    return `${t("kinds.monthly")} ${rule.dayOfMonth} · ${time}`;
  return `${t("kinds.daily")} ${time}`;
}

export function useTaskPaginationLabels(): PaginationLabels {
  const { t } = useTranslation("automaticTask");
  return {
    navigation: t("pagination.navigation"),
    next: t("pagination.next"),
    previous: t("pagination.previous"),
    page: (page) => t("pagination.page", { page }),
    pageSize: (count) => t("pagination.pageSize", { count }),
    pageSizeSelection: t("pagination.pageSizeSelection"),
    total: (count) => t("pagination.total", { count }),
  };
}
