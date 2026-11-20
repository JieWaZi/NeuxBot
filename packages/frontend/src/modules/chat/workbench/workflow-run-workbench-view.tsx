import { Suspense, useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { EmbeddedWorkflowRunView } from "@/modules/workflow";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";
import type { WorkbenchContentReference } from "./workbench-model";
import { openWorkflowRunTab } from "./workflow-run-reference";
import { useWorkbench } from "./workbench-store";

export function WorkflowRunWorkbenchView({
  active,
  navigationRevision,
  reference,
  scopeKey,
  title,
}: {
  active: boolean;
  navigationRevision: number;
  reference: WorkbenchContentReference;
  scopeKey: string;
  title: string;
}) {
  const { t } = useTranslation("common");
  const { dispatch } = useWorkbench(scopeKey);
  const { definitionId, runId, chatId, instanceId } = reference.ids;
  const [selection, setSelection] = useState(() => ({
    revision: navigationRevision,
    params: new URLSearchParams(instanceId ? { instance: instanceId } : {}),
  }));
  if (selection.revision !== navigationRevision) {
    setSelection({
      revision: navigationRevision,
      params: instanceId
        ? new URLSearchParams({ instance: instanceId })
        : selection.params,
    });
  }
  const replace = useCallback(
    (update: (previous: URLSearchParams) => URLSearchParams) => {
      setSelection((current) => {
        const params = update(current.params);
        return params === current.params ? current : { ...current, params };
      });
    },
    [],
  );
  if (!definitionId || !runId) return null;
  return (
    <div className="flex h-full min-h-0 min-w-0">
      <Suspense
        fallback={
          <ContentLoadingState label={t("pageLoading")} className="flex-1" />
        }
      >
        <EmbeddedWorkflowRunView
          id={definitionId}
          runId={runId}
          active={active}
          navigation={{
            params: selection.params,
            replace,
            chatId,
            onRetryAccepted: (nextRunId) =>
              dispatch(
                openWorkflowRunTab({
                  definitionId,
                  runId: nextRunId,
                  name: title,
                  chatId,
                }),
              ),
          }}
        />
      </Suspense>
    </div>
  );
}
