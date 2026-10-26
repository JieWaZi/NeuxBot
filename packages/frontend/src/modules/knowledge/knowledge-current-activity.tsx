import type { ListJobsResponse } from "@/shared/api/generated/types.gen";
import { Badge } from "@/shared/ui/badge";
import { Card } from "@/shared/ui/card";
import { Skeleton } from "@/shared/ui/skeleton";
import { isKnowledgeJobBusy } from "./knowledge-transfer";
import { useCopy } from "./use-extended-copy";

/** 当前任务读取核心快照，与不可变活动历史分别展示，不合成完成记录。 */
export function KnowledgeCurrentActivity({
  job,
  pending,
  error,
}: {
  job: ListJobsResponse["activeJob"];
  pending: boolean;
  error: boolean;
}) {
  const t = useCopy();
  if (pending)
    return (
      <div
        role="status"
        aria-busy="true"
        aria-label={t("activityCurrentTask")}
        className="grid gap-2"
      >
        <Skeleton className="h-4 w-1/3" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  const current =
    job && isKnowledgeJobBusy(job.state ?? "unknown") ? job : undefined;
  if (!current && !error) return null;
  const canceling =
    current?.cancelRequested || current?.state === "cancel_requested";
  const blocked = current?.hostState === "blocked";
  const status = canceling
    ? "activityCancelling"
    : blocked
      ? "activityBlocked"
      : current?.state === "queued"
        ? "activityQueued"
        : current?.state === "processing"
          ? "activityProcessing"
          : "activityStatusUnknown";
  const timestamp = current?.attemptStartedAt || current?.createdAt;
  const count = current?.sourceVersionIds?.length;
  return (
    <Card asChild variant="section">
      <section aria-label={t("activityCurrentTask")} className="grid gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="type-body font-medium">{t("activityCurrentTask")}</h3>
          {current && (
            <Badge variant={blocked ? "warning" : "secondary"}>
              {t(status)}
            </Badge>
          )}
        </div>
        {current && (
          <>
            <p className="type-body">
              {t(
                current.maintenance === true
                  ? "activityWikiMaintenance"
                  : current.maintenance === false
                    ? "activityOrganizeDocuments"
                    : "activityTask",
              )}
            </p>
            <p className="type-caption text-content-secondary">
              {current.maintenance === true
                ? t("activityExistingWikiScope")
                : count !== undefined && count > 0
                  ? t("activityDocumentScope", { count })
                  : t("activityScopeUnknown")}
            </p>
            {timestamp !== undefined && timestamp > 0 && (
              <p className="type-caption text-content-secondary">
                {t(
                  current.attemptStartedAt
                    ? "activityAcceptedAt"
                    : "activityCreatedAt",
                  { time: new Date(timestamp).toLocaleString() },
                )}
              </p>
            )}
            {blocked && current.hostDiagnostic && (
              <p className="type-caption text-content-secondary break-words">
                {current.hostDiagnostic}
              </p>
            )}
          </>
        )}
        {error && (
          <p role="alert" className="type-caption text-destructive">
            {t(current ? "activityCurrentStale" : "activityCurrentFailed")}
          </p>
        )}
      </section>
    </Card>
  );
}
