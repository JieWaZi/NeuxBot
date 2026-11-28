import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { openExternalURL } from "@/shared/platform";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import { formatTime } from "./task-presentation";
import type { TaskView } from "../model/types";

export function EventRuleDetails({ task }: { task: TaskView }) {
  const { t, i18n } = useTranslation("automaticTask");
  const cursor = task.eventState?.cursor;
  const observations = task.eventState?.observations ?? [];
  return (
    <div className="grid min-w-0 gap-4">
      <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(min(100%,16rem),1fr))] gap-x-8 gap-y-4">
        <ReadOnlyField label={t("eventRepository")}>
          {task.event?.repository}
        </ReadOnlyField>
        <ReadOnlyField label={t("eventTypes")}>
          {task.event?.kinds
            ?.map((kind) => t(`eventKind_${kind}`, { defaultValue: kind }))
            .join(" · ")}
        </ReadOnlyField>
        <ReadOnlyField label={t("eventLatestCheck")}>
          {cursor?.checkedAt
            ? formatTime(cursor.checkedAt, undefined, i18n.language)
            : t("eventNeverChecked")}
        </ReadOnlyField>
      </dl>
      {cursor?.outcome ? (
        <p role="status" className="type-body text-content-secondary">
          {t(`eventOutcome_${cursor.outcome}`, {
            defaultValue: cursor.outcome,
            count: cursor.matched ?? 0,
          })}
          {cursor.filtered && cursor.outcome === "matched"
            ? ` · ${t("eventFilteredCount", { count: cursor.filtered })}`
            : ""}
          {cursor.errorCode
            ? ` · ${applicationErrorMessage({ code: cursor.errorCode, fallback: t("actionFailed") })}`
            : ""}
        </p>
      ) : null}
      {observations.length ? (
        <ul className="grid gap-2" aria-label={t("eventRecentObservations")}>
          {observations.map((row) => (
            <li
              key={row.id}
              className="type-body flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1"
            >
              <span className="min-w-0 break-words">
                {row.event?.title || row.event?.topic}
              </span>
              <span className="type-caption text-content-secondary">
                {t(`eventDisposition_${row.disposition}`, {
                  defaultValue: row.disposition,
                })}
              </span>
              {row.errorCode ? (
                <span className="type-caption text-content-secondary">
                  {applicationErrorMessage({
                    code: row.errorCode,
                    fallback: t("eventFollowUpUnavailable"),
                  })}
                </span>
              ) : null}
              {row.event?.url ? (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    if (row.event?.url) void openExternalURL(row.event.url);
                  }}
                >
                  GitHub
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
