import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { Button } from "@/shared/ui/button";
import type { ResourceRef } from "./knowledge-transfer";
import { knowledgeKeys } from "./knowledge-queries";
import { useCopy } from "./use-extended-copy";

/** 用户展开时才读取任务报告，尝试身份不符时不以新结果覆盖历史。 */
export function KnowledgeActivityTaskResult({
  resource,
  jobId,
  attempt,
}: {
  resource: ResourceRef;
  jobId: string;
  attempt: number;
}) {
  const t = useCopy();
  const active = useRouteSceneActive();
  const [opened, setOpened] = useState(false);
  const query = useQuery({
    queryKey: knowledgeKeys.job(resource, jobId),
    queryFn: async ({ signal }) =>
      (
        await knowledgeAdapter(resource).getJob(
          withLocalErrorSurface({
            path: { libraryId: resource.knowledgeBaseId, jobId },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
    enabled: opened && active,
  });
  const current = query.data?.job;
  const sameAttempt = current?.id === jobId && current.attempt === attempt;
  return (
    <div className="mt-2 grid justify-items-start gap-2">
      <Button
        type="button"
        variant="text"
        size="sm"
        aria-expanded={opened}
        onClick={() => setOpened(!opened)}
      >
        {t(opened ? "activityHideResult" : "activityViewResult")}
      </Button>
      {opened && (
        <div
          className="type-caption text-content-secondary max-h-64 overflow-y-auto whitespace-pre-wrap"
          role="region"
          aria-label={t("activityTaskResult")}
        >
          {query.isPending ? (
            <p role="status">{t("activityLoading")}</p>
          ) : (
            <>
              {query.error && (
                <>
                  <p role="alert">
                    {t(
                      sameAttempt
                        ? "activityResultStale"
                        : "activityResultFailed",
                    )}
                  </p>
                  <Button
                    type="button"
                    variant="text"
                    size="sm"
                    onClick={() => void query.refetch()}
                  >
                    {t("activityRetryResult")}
                  </Button>
                </>
              )}
              {sameAttempt ? (
                <p>
                  {current.diagnostic ||
                    query.data?.report ||
                    t("activityResultUnavailable")}
                </p>
              ) : query.data ? (
                t("activityResultAttemptChanged")
              ) : null}
            </>
          )}
        </div>
      )}
    </div>
  );
}
