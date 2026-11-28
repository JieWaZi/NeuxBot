import type { TFunction } from "i18next";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsAutomaticTaskConfiguration } from "@/shared/api/generated/types.gen";

type TaskSchedule =
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAutomaticTaskConfiguration["schedule"];

export function formatTime(
  value?: number | null,
  timezone?: string,
  language?: string,
) {
  if (!value) return "—";
  try {
    return new Intl.DateTimeFormat(language, {
      dateStyle: "medium",
      timeStyle: "short",
      ...(timezone ? { timeZone: timezone } : {}),
    }).format(value);
  } catch (error) {
    if (!(error instanceof RangeError)) throw error;
    const date = new Date(value);
    return Number.isFinite(date.getTime()) ? date.toISOString() : "—";
  }
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
