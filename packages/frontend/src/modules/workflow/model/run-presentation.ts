import type { WorkflowRun } from "./types";

export function runOriginKey(run: WorkflowRun) {
  const source = run.flowContext?.triggerSource;
  if (run.parentRunId) return "childRunOrigin";
  if (source === "manual" || source == null) return "manualRunOrigin";
  if (source === "retry") return "retryRunOrigin";
  if (source === "agent") return "chatRunOrigin";
  if (source === "automatic_task" || source === "schedule" || source === "api")
    return "automaticRunOrigin";
  return "otherRunOrigin";
}

export function runTime(createdAt: number | undefined, language: string) {
  if (!createdAt) return "";
  const minutes = Math.floor((Date.now() - createdAt) / 60_000);
  if (minutes >= 0 && minutes < 60)
    return new Intl.RelativeTimeFormat(language, { numeric: "auto" }).format(
      -minutes,
      "minute",
    );
  if (minutes >= 0 && minutes < 24 * 60)
    return new Intl.RelativeTimeFormat(language, { numeric: "auto" }).format(
      -Math.floor(minutes / 60),
      "hour",
    );
  return new Intl.DateTimeFormat(language, {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(createdAt);
}
