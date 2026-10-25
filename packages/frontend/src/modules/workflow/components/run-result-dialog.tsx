import { useQueryEvents } from "@/shared/api/use-query-events";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useVisibleWorkResult } from "@/shared/api/work-result-reading";
import { Copy } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getWorkflowRun } from "@/shared/api/generated/sdk.gen";
import { Button } from "@/shared/ui/button";
import { Badge } from "@/shared/ui/badge";
import { Card } from "@/shared/ui/card";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { ReadOnlyField } from "@/shared/ui/read-only-field";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { getWorkflowRunQueryKey } from "../api/workflow-queries";
import { jsonText, record } from "../model/types";
import { runOriginKey } from "../model/run-presentation";
import { InstanceDetails, WorkflowResult } from "./instance-details";
import { WorkflowFailure } from "./failure-details";
import { workflowStatusAppearance } from "../model/status-presentation";
import { cn } from "@/shared/lib/cn";
import { WorkflowArtifacts } from "./workflow-artifacts";
import { RunLogs } from "./run-logs";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";

const outputSelector = () => "[data-work-result-output]";

export function RunResultDialog({
  id,
  runId,
  open,
  onOpenChange,
}: {
  id: string;
  runId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t, i18n } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const query = useInfiniteQuery({
    queryKey: [
      ...getWorkflowRunQueryKey({ path: { id, runId } }),
      "result-pages",
    ],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      (
        await getWorkflowRun({
          path: { id, runId },
          query: { offset: pageParam },
          throwOnError: true,
        })
      ).data,
    getNextPageParam: (page) => page.nextOffset || undefined,
    enabled: active && open,
  });

  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id, parentId: runId },
    queryKeys: [getWorkflowRunQueryKey({ path: { id, runId } })],
    active,
    enabled: Boolean(id && runId),
  });
  const { hasNextPage, isFetching, isFetchNextPageError, fetchNextPage } =
    query;
  useEffect(() => {
    if (active && open && hasNextPage && !isFetching && !isFetchNextPageError)
      void fetchNextPage();
  }, [
    active,
    open,
    hasNextPage,
    isFetching,
    isFetchNextPageError,
    fetchNextPage,
  ]);
  const data = query.data?.pages[0];
  const run = data?.run;
  const resultRoot = useRef<HTMLDivElement>(null);
  useVisibleWorkResult({
    ownerKind: "workflow_run",
    ownerId: runId,
    root: resultRoot,
    selector: outputSelector,
    rendered:
      active &&
      open &&
      !query.isError &&
      run?.id === runId &&
      Boolean(run?.completedAt) &&
      (run?.result != null || run?.failure != null),
  });
  const manifest = data?.manifest;
  const instances =
    query.data?.pages.flatMap((page) => page.instances ?? []) ?? [];
  const input = record(run?.args) ?? {};
  return (
    <Dialog open={active && open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("executionDetail", { title: manifest?.meta?.title ?? "" })}
        description={t("executionDetailHelp")}
        size="xl"
        layout="form"
      >
        <div
          ref={resultRoot}
          className="grid min-h-0 content-start gap-6 overflow-y-auto px-6 pb-6"
        >
          {query.error ? (
            <WorkflowQueryError
              error={query.error}
              retrying={query.isFetching}
              onRetry={() => {
                if (query.isFetchNextPageError) void query.fetchNextPage();
                else void query.refetch();
              }}
            />
          ) : null}
          {!run ? (
            query.isPending ? (
              <WorkflowLoadingState />
            ) : null
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-3">
                <Badge variant={workflowStatusAppearance(run.status).badge}>
                  {run.status ? t(`status.${run.status}`) : ""}
                </Badge>
                <span className="text-muted-foreground type-caption">
                  {t(runOriginKey(run))}
                </span>
                {run.startedAt && run.completedAt ? (
                  <span className="text-muted-foreground type-caption">
                    {t("duration", {
                      seconds: (
                        (run.completedAt - run.startedAt) /
                        1000
                      ).toFixed(1),
                    })}
                  </span>
                ) : null}
                <Button
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  disabled={query.hasNextPage || query.isFetching}
                  onClick={() =>
                    void navigator.clipboard.writeText(
                      jsonText({
                        run,
                        phases: manifest?.meta?.phases,
                        instances,
                      }),
                    )
                  }
                >
                  <Copy />
                  {t("copyRunRecord")}
                </Button>
              </div>
              <p className="text-muted-foreground type-caption break-all">
                {run.createdAt
                  ? new Date(run.createdAt).toLocaleString(i18n.language)
                  : ""}
              </p>
              <section className="@container/run-inputs grid min-w-0 gap-3">
                <h3 className="type-title font-semibold">{t("runInputs")}</h3>
                {Object.keys(input).length ? (
                  <Card asChild variant="section">
                    <dl className="grid grid-cols-1 gap-x-6 gap-y-4 @min-[36rem]/run-inputs:grid-cols-2">
                      {Object.entries(input).map(([key, value]) => {
                        const field = record(
                          record(manifest?.meta?.inputSchema?.properties)?.[
                            key
                          ],
                        );
                        const structured =
                          value != null && typeof value === "object";
                        return (
                          <ReadOnlyField
                            key={key}
                            label={
                              typeof field?.title === "string"
                                ? field.title
                                : typeof field?.description === "string"
                                  ? field.description
                                  : key
                            }
                            className={cn(
                              (structured ||
                                (typeof value === "string" &&
                                  value.includes("\n"))) &&
                                "col-span-full",
                            )}
                          >
                            {value == null ? (
                              <span className="text-content-secondary">
                                {t("emptyValue")}
                              </span>
                            ) : typeof value === "string" ? (
                              value
                            ) : structured ? (
                              <pre className="type-body [overflow-wrap:anywhere] whitespace-pre-wrap">
                                {jsonText(value)}
                              </pre>
                            ) : (
                              jsonText(value)
                            )}
                          </ReadOnlyField>
                        );
                      })}
                    </dl>
                  </Card>
                ) : (
                  <p className="text-muted-foreground type-body">
                    {t("noInputs")}
                  </p>
                )}
              </section>
              {(manifest?.meta?.phases ?? []).map((phase, index) => {
                const steps = instances.filter(
                  (instance) => instance.phaseId === phase.id,
                );
                return (
                  <section key={phase.id} className="grid gap-3">
                    <h3 className="type-title font-semibold">
                      <span className="text-muted-foreground mr-2">
                        {String(index + 1).padStart(2, "0")}
                      </span>
                      {phase.title}
                    </h3>
                    {phase.detail ? (
                      <p className="text-muted-foreground type-body">
                        {phase.detail}
                      </p>
                    ) : null}
                    {steps.length ? (
                      steps.map((instance) => (
                        <details
                          key={instance.id}
                          className={cn(
                            "bg-surface-module rounded-lg p-4",
                            workflowStatusAppearance(instance.status).surface,
                          )}
                          onToggle={(event) => {
                            const open = event.currentTarget.open;
                            const instanceId = instance.id;
                            if (!instanceId) return;
                            setExpanded((previous) => {
                              const next = new Set(previous);
                              if (open) next.add(instanceId);
                              else next.delete(instanceId);
                              return next;
                            });
                          }}
                        >
                          <summary className="type-body cursor-pointer font-medium">
                            {instance.label}{" "}
                            <Badge
                              variant={
                                workflowStatusAppearance(instance.status).badge
                              }
                            >
                              {instance.status
                                ? t(`status.${instance.status}`)
                                : ""}
                            </Badge>
                          </summary>
                          {instance.id && expanded.has(instance.id) ? (
                            <div className="mt-4">
                              <InstanceDetails
                                id={id}
                                runId={runId}
                                instanceId={instance.id}
                              />
                            </div>
                          ) : null}
                        </details>
                      ))
                    ) : (
                      <p className="text-muted-foreground type-caption">
                        {t("noExecutionNodes")}
                      </p>
                    )}
                  </section>
                );
              })}
              {query.isFetchingNextPage ? <WorkflowLoadingState /> : null}
              <section className="grid gap-3">
                <div className="flex items-center justify-between gap-3">
                  <h3 className="type-title font-semibold">
                    {t("flowOutput")}
                  </h3>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={run.result == null && !run.failure}
                    onClick={() =>
                      void navigator.clipboard.writeText(
                        jsonText(run.failure?.data ?? run.result),
                      )
                    }
                  >
                    <Copy />
                    {t("copyOutput")}
                  </Button>
                </div>
                {run.result == null && !run.failure ? (
                  <p className="text-muted-foreground type-body">
                    {t("resultPending")}
                  </p>
                ) : (
                  <div data-work-result-output>
                    {run.failure ? (
                      <WorkflowFailure value={run.failure.data} />
                    ) : (
                      <WorkflowResult value={run.result} />
                    )}
                  </div>
                )}
                <WorkflowArtifacts
                  files={data?.files}
                  id={id}
                  runId={runId}
                  values={[run.result]}
                />
              </section>
              <RunLogs
                key={runId}
                id={id}
                runId={runId}
                completedAt={run.completedAt}
              />
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
