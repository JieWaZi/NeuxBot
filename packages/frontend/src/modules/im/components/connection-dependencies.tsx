import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsImConnectionDependencies as Dependencies } from "@/shared/api/generated/types.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";

export function connectionDeletionBlocked(data: Dependencies | undefined) {
  return (
    (data?.pendingDeliveries ?? 0) > 0 ||
    Boolean(data?.workflows?.some((item) => item.runId))
  );
}

/** 删除前展示当前版本及活跃运行引用，保留工作流和发送节点的业务名称。 */
export function ConnectionDependencies({
  data,
  loading,
  error,
  onRetry,
}: {
  data: Dependencies | undefined;
  loading: boolean;
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useTranslation("im");
  if (loading) return <ContentLoadingState label={t("checkingDependencies")} />;
  if (error)
    return (
      <div className="grid gap-2">
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(error, t("dependencyReadFailed"))}
        </p>
        <Button
          size="sm"
          variant="secondary"
          className="justify-self-start"
          onClick={onRetry}
        >
          {t("reload")}
        </Button>
      </div>
    );
  return (
    <div className="type-body grid gap-3">
      {data?.workflows?.length ? (
        <>
          <p>{t("workflowDependencyWarning")}</p>
          <ul className="grid gap-2">
            {data.workflows.map((item) => (
              <li
                key={`${item.definitionId}:${item.revisionId}:${item.nodeId}:${item.runId ?? ""}`}
                className="wrap-anywhere"
              >
                <Link
                  to={`/workflows/${item.definitionId}`}
                  className="text-link underline underline-offset-2"
                >
                  {item.title}
                </Link>{" "}
                · {item.label}
                {item.runId ? (
                  <span className="text-destructive">
                    {" "}
                    · {t("dependencyActiveRun")}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {connectionDeletionBlocked(data) ? (
        <p role="alert" className="text-destructive">
          {t("dependencyBlocksDeletion")}
        </p>
      ) : null}
    </div>
  );
}
