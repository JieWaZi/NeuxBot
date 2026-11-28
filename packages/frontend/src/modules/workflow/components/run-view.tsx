import { RetryRunDialog } from "./retry-run-dialog";
import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { ListChecks, RefreshCw, Square, WandSparkles } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router-dom";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { cn } from "@/shared/lib/cn";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  cancelWorkflowRunMutation,
  getWorkflowInstanceOptions,
  getWorkflowRunQueryKey,
  listWorkflowRunsQueryKey,
  retryWorkflowConclusionMutation,
  workflowRunDetailsOptions,
} from "../api/workflow-queries";
import { useResolveWorkflowRunDecisionDrafts } from "../model/human-answer-draft";
import { OperationRequestCard } from "./operation-request";
import { HumanRequestCard } from "./human-request";
import { WorkflowCanvas } from "./workflow-canvas";
import { WorkflowCanvasPlaceholder } from "./workflow-canvas-placeholder";
import { InstanceDetails } from "./instance-details";
import { RunResultDialog } from "./run-result-dialog";
import { RunInputValues } from "./run-input-values";
import { WorkflowInputNode } from "./workflow-input-node";
import { WorkflowQueryError } from "./query-feedback";

type RunViewNavigation = {
  params: URLSearchParams;
  replace: (update: (previous: URLSearchParams) => URLSearchParams) => void;
  chatId?: string;
  onRetryAccepted?: (runId: string) => void;
};

export function WorkflowRunView({
  id,
  runId,
  actionsContainer,
  active: visible = true,
  navigation,
}: {
  id: string;
  runId: string;
  actionsContainer?: HTMLElement | null;
  active?: boolean;
  navigation?: RunViewNavigation;
}) {
  const { t } = useTranslation("workflow");
  const sceneActive = useRouteSceneActive();
  const active = sceneActive && visible;
  const [routeParams, setRouteParams] = useSearchParams();
  const replaceRouteParams = useCallback<RunViewNavigation["replace"]>(
    (update) => setRouteParams(update, { replace: true }),
    [setRouteParams],
  );
  const params = navigation?.params ?? routeParams;
  const setParams = navigation?.replace ?? replaceRouteParams;
  const offset = Number(params.get("offset") ?? 0);
  const selected = params.get("instance");
  const query = useQuery({
    ...workflowRunDetailsOptions({
      path: { id, runId },
      query: {
        offset: Number.isSafeInteger(offset) && offset >= 0 ? offset : 0,
      },
    }),
    enabled: active && Boolean(runId),
    placeholderData: keepPreviousData,
  });
  const [stopRunId, setStopRunId] = useState<string>();
  const [inputRunId, setInputRunId] = useState<string>();
  const stop = useMutation(withLocalErrorMutation(cancelWorkflowRunMutation()));
  const retryConclusion = useMutation(
    withLocalErrorMutation(retryWorkflowConclusionMutation()),
  );
  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id, parentId: runId },
    queryKeys: [
      getWorkflowRunQueryKey({ path: { id, runId } }),
      listWorkflowRunsQueryKey({ path: { id } }),
    ],
    active,
    enabled: Boolean(id && runId),
    eventTypes: ["workflow.run.changed", "workflow.worker.skip_requested"],
  });
  const switching = query.isPlaceholderData;
  const snapshot = switching ? undefined : query.data;
  const run = snapshot?.run;
  useResolveWorkflowRunDecisionDrafts(id, runId, snapshot);
  const instances = useMemo(
    () => (switching ? [] : (query.data?.instances ?? [])),
    [query.data?.instances, switching],
  );
  const resultOpen = params.get("result") === "1";
  const instanceId = selected || undefined;
  const listedInstance = instanceId
    ? instances.find((item) => item.id === instanceId)
    : undefined;
  const listedNode =
    params.get("node") ??
    listedInstance?.nodeId ??
    (instanceId
      ? snapshot?.nodeCounts?.find((item) => item.instanceId === instanceId)
          ?.nodeId
      : undefined);
  const resolveExactInstance = Boolean(
    active && run && !resultOpen && instanceId && !listedNode,
  );
  const exactInstance = useQuery({
    ...getWorkflowInstanceOptions(
      withLocalErrorSurface({
        path: { id, runId, instanceId: instanceId ?? "" },
      }),
    ),
    enabled: resolveExactInstance,
  });
  const instance = listedInstance ?? exactInstance.data?.instance;
  const selectedNode = listedNode ?? instance?.nodeId;
  const waiting = useMemo(
    () =>
      (switching
        ? []
        : query.data?.humanRequests?.filter(
            (item) => item.status === "waiting",
          )) ?? [],
    [query.data?.humanRequests, switching],
  );
  const operations = useMemo(
    () =>
      (switching
        ? []
        : query.data?.operationRequests?.filter(
            (item) => item.status === "waiting",
          )) ?? [],
    [query.data?.operationRequests, switching],
  );
  const terminal = ["succeeded", "failed", "cancelled"].includes(
    run?.status ?? "",
  );
  const conclusion = run?.conclusion;
  const canRetryConclusion =
    terminal && conclusion && (!conclusion.settledAt || conclusion.errorCode);
  const isRetryingCurrentConclusion =
    retryConclusion.variables?.path.id === id &&
    retryConclusion.variables.path.runId === runId;
  const closeDetails = (
    expectedInstanceId?: string,
    answeredRequestId?: string,
  ) => {
    setParams((previous) => {
      if (expectedInstanceId && previous.get("instance") !== expectedInstanceId)
        return previous;
      const next = new URLSearchParams(previous);
      next.delete("instance");
      next.delete("node");
      if (answeredRequestId && next.get("request") === answeredRequestId)
        next.delete("request");
      return next;
    });
  };
  const selectNode = (nodeId: string, actualId?: string) => {
    const priority = [
      "unknown",
      "waiting",
      "running",
      "prepared",
      "failed",
      "succeeded",
      "skipped",
      "cancelled",
    ];
    const actual =
      actualId ||
      query.data?.nodeCounts
        ?.filter((item) => item.nodeId === nodeId && item.instanceId)
        .sort(
          (a, b) =>
            priority.indexOf(a.status ?? "") - priority.indexOf(b.status ?? ""),
        )[0]?.instanceId;
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      next.set("node", nodeId);
      next.delete("result");
      if (actual) next.set("instance", actual);
      else next.delete("instance");
      return next;
    });
  };
  const prompted = useRef(new Set<string>());
  const embedded = Boolean(navigation);
  const requestedId = params.get("request") ?? undefined;
  useEffect(() => {
    if (
      embedded ||
      !active ||
      switching ||
      params.has("node") ||
      params.has("result")
    )
      return;
    const request = requestedId
      ? [...waiting, ...operations].find((item) => item.id === requestedId)
      : [...waiting, ...operations].find(
          (item) => item.id && !prompted.current.has(item.id),
        );
    const instanceId = request?.instanceId;
    if (!request?.id || !instanceId) return;
    const nodeId =
      instances.find((item) => item.id === instanceId)?.nodeId ||
      query.data?.nodeCounts?.find((item) => item.instanceId === instanceId)
        ?.nodeId;
    if (!nodeId) return;
    prompted.current.add(request.id);
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      next.set("node", nodeId);
      next.set("instance", instanceId);
      return next;
    });
  }, [
    active,
    switching,
    params,
    requestedId,
    waiting,
    operations,
    instances,
    query.data?.nodeCounts,
    setParams,
    embedded,
  ]);
  const changeResult = (open: boolean) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (open) next.set("result", "1");
      else next.delete("result");
      return next;
    });
  const actions = (
    <div className="flex flex-wrap justify-end gap-2">
      {canRetryConclusion ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-content-secondary type-caption" role="status">
            {conclusion.errorCode
              ? t("conclusionDeliveryFailed")
              : t("conclusionDeliveryPending")}
          </span>
          <Button
            size="sm"
            variant="surface"
            disabled={isRetryingCurrentConclusion && retryConclusion.isPending}
            onClick={() =>
              retryConclusion.mutate(
                { path: { id, runId } },
                { onSuccess: () => void query.refetch() },
              )
            }
          >
            <RefreshCw />
            {t("retryConclusionDelivery")}
          </Button>
          {isRetryingCurrentConclusion && retryConclusion.error && (
            <span className="text-destructive type-caption" role="alert">
              {localApiErrorMessage(
                retryConclusion.error,
                t("conclusionDeliveryFailed"),
              )}
            </span>
          )}
        </div>
      ) : null}
      {embedded && terminal ? (
        <Button size="sm" variant="surface" onClick={() => changeResult(true)}>
          <ListChecks />
          {t("runResult")}
        </Button>
      ) : null}
      {terminal && run?.status !== "succeeded" ? (
        <RetryRunDialog
          id={id}
          runId={runId}
          manifest={query.data?.manifest}
          chatId={navigation?.chatId}
          onAccepted={navigation?.onRetryAccepted}
        />
      ) : null}
      {terminal && run?.status !== "succeeded" ? (
        <Button size="sm" variant="surface" asChild>
          <Link to={`/workflows/${id}?tab=canvas&optimizeRun=${runId}`}>
            <WandSparkles />
            {t("optimizeWorkflow")}
          </Link>
        </Button>
      ) : null}
      {!terminal && run ? (
        <Button
          size="sm"
          variant="surface"
          disabled={run.status === "cancelling"}
          onClick={() => {
            stop.reset();
            setStopRunId(runId);
          }}
        >
          <Square />
          {t("stop")}
        </Button>
      ) : null}
    </div>
  );
  return (
    <section className="@container/run-view relative flex min-h-0 min-w-0 flex-1">
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {actionsContainer ? (
          createPortal(actions, actionsContainer)
        ) : (
          <div className="absolute top-4 right-4 z-10 max-w-[calc(100%-2rem)]">
            {actions}
          </div>
        )}
        {query.isError ? (
          <WorkflowQueryError
            error={query.error}
            className="p-5"
            retrying={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        ) : resolveExactInstance && exactInstance.isError ? (
          <WorkflowQueryError
            error={exactInstance.error}
            className="p-5"
            retrying={exactInstance.isFetching}
            onRetry={() => void exactInstance.refetch()}
          />
        ) : null}
        {query.data?.manifest ? (
          <div
            className={cn(
              "flex min-h-0 min-w-0 flex-1",
              switching && "invisible",
            )}
            aria-hidden={switching || undefined}
            inert={switching}
          >
            <WorkflowCanvas
              active={active && !switching}
              manifest={query.data.manifest}
              inputNode={
                <WorkflowInputNode
                  manifest={query.data.manifest}
                  args={run?.args ?? {}}
                  open={
                    active && !switching && inputRunId === runId && !resultOpen
                  }
                />
              }
              inputDetails={{
                title: t("runInputs"),
                open:
                  active && !switching && inputRunId === runId && !resultOpen,
                onOpenChange: (open) => setInputRunId(open ? runId : undefined),
                content: <RunInputValues args={run?.args} />,
              }}
              nodeCounts={query.data.nodeCounts ?? []}
              selected={
                active && !switching && !resultOpen ? selectedNode : undefined
              }
              onDismiss={closeDetails}
              dismissWithinCanvas={embedded}
              onSelect={(node) => selectNode(node.id ?? "")}
              nodeHeading={(node) => {
                const actual =
                  instance?.nodeId === node.id ? instance : undefined;
                return { title: actual?.label, status: actual?.status };
              }}
              nodeContent={(node) => (
                <div className="grid gap-5 px-6 pb-6">
                  {instances.filter((item) => item.nodeId === node.id).length >
                  1 ? (
                    <div className="flex flex-wrap gap-2">
                      {instances
                        .filter((item) => item.nodeId === node.id)
                        .map((item) => (
                          <Button
                            key={item.id}
                            size="sm"
                            variant={
                              item.id === instanceId ? "secondary" : "ghost"
                            }
                            onClick={() => selectNode(node.id ?? "", item.id)}
                          >
                            {item.label || item.path}
                          </Button>
                        ))}
                    </div>
                  ) : null}
                  {operations
                    .filter((item) => item.instanceId === instanceId)
                    .map((operation) => (
                      <OperationRequestCard
                        key={operation.id}
                        id={id}
                        runId={runId}
                        chatId={navigation?.chatId}
                        operation={operation}
                      />
                    ))}
                  {waiting
                    .filter((item) => item.instanceId === instanceId)
                    .map((human) => (
                      <HumanRequestCard
                        key={human.id}
                        id={id}
                        runId={runId}
                        chatId={navigation?.chatId}
                        human={human}
                        onAnswered={() => {
                          if (human.id) prompted.current.add(human.id);
                          closeDetails(human.instanceId, human.id);
                        }}
                      />
                    ))}
                  {instanceId ? (
                    <InstanceDetails
                      key={instanceId}
                      id={id}
                      runId={runId}
                      instanceId={instanceId}
                      human={query.data?.humanRequests?.find(
                        (request) => request.instanceId === instanceId,
                      )}
                    />
                  ) : (
                    <p className="text-content-muted type-body">
                      {t("notExecuted")}
                    </p>
                  )}
                  {query.data?.nextOffset ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setParams((previous) => {
                          const next = new URLSearchParams(previous);
                          next.set("offset", String(query.data.nextOffset));
                          return next;
                        })
                      }
                    >
                      {t("nextPage")}
                    </Button>
                  ) : null}
                </div>
              )}
            />
          </div>
        ) : query.isPending ? (
          <WorkflowCanvasPlaceholder state="loading" active={active} />
        ) : query.isSuccess ? (
          <EmptyState title={t("noExecutionNodes")} />
        ) : null}
        {switching ? (
          <div className="absolute inset-0 flex min-h-0 min-w-0">
            <WorkflowCanvasPlaceholder state="loading" active={active} />
          </div>
        ) : null}
      </div>
      <RunResultDialog
        id={id}
        runId={runId}
        open={active && resultOpen}
        onOpenChange={changeResult}
      />
      <Dialog
        open={active && stopRunId === runId && !switching}
        onOpenChange={(open) => setStopRunId(open ? runId : undefined)}
      >
        <DialogContent title={t("stopTitle")} description={t("stopHelp")}>
          <div className="px-6 pb-4">
            {stop.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(stop.error, t("error"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setStopRunId(undefined)}>
              {t("cancel")}
            </Button>
            <Button
              disabled={stop.isPending}
              onClick={() =>
                void stop
                  .mutateAsync({ path: { id, runId } })
                  .then(() => {
                    setStopRunId(undefined);
                  })
                  .catch(() => {})
              }
            >
              {t("stop")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
