import { NodeOutputCard } from "../presentation/node-result";
import { useNotificationPresentation } from "@/shared/api/notification-presentation";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Square, WandSparkles } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Link, useSearchParams } from "react-router-dom";
import { subscribeWorkflow } from "@/shared/api/events";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { cn } from "@/shared/lib/cn";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  cancelWorkflowRunMutation,
  getWorkflowRunOptions,
  getWorkflowRunQueryKey,
  listWorkflowRunsQueryKey,
} from "../api/workflow-queries";
import { record } from "../model/types";
import { OperationRequestCard } from "./operation-request";
import { HumanRequestCard } from "./human-request";
import { WorkflowCanvas } from "./workflow-canvas";
import { WorkflowCanvasPlaceholder } from "./workflow-canvas-placeholder";
import { InstanceDetails } from "./instance-details";
import { RunResultDialog } from "./run-result-dialog";
import { RunInputValues } from "./run-input-values";
import { WorkflowInputNode } from "./workflow-input-node";
import { WorkflowQueryError } from "./query-feedback";

export function WorkflowRunView({
  id,
  runId,
  actionsContainer,
  active: visible = true,
}: {
  id: string;
  runId: string;
  actionsContainer?: HTMLElement | null;
  active?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const { t: boardText } = useTranslation("workBoard");
  const client = useQueryClient();
  const sceneActive = useRouteSceneActive();
  const active = sceneActive && visible;
  const [params, setParams] = useSearchParams();
  const offset = Number(params.get("offset") ?? 0);
  const selected = params.get("instance");
  const query = useQuery({
    ...getWorkflowRunOptions({
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
  useEffect(() => {
    if (!active || !id || !runId) return;
    let frame = 0;
    const refresh = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        void client.invalidateQueries({
          queryKey: getWorkflowRunQueryKey({ path: { id, runId } }),
        });
        void client.invalidateQueries({
          queryKey: listWorkflowRunsQueryKey({ path: { id } }),
        });
        void client.invalidateQueries({
          predicate: (item) => {
            const key = record(item.queryKey[0]);
            return (
              key?._id === "getWorkflowInstance" &&
              record(key.path)?.runId === runId
            );
          },
        });
      });
    };
    const close = subscribeWorkflow(id, {
      runId,
      onEvent: refresh,
      onGap: refresh,
      onReconnect: refresh,
    });
    refresh();
    return () => {
      close();
      cancelAnimationFrame(frame);
    };
  }, [active, client, id, runId]);
  const switching = query.isPlaceholderData;
  const run = switching ? undefined : query.data?.run;
  const runRoot = useRef<HTMLElement>(null);
  useNotificationPresentation({
    target:
      run?.id === runId && query.data?.manifest
        ? { kind: "workflow_run", runId }
        : undefined,
    root: runRoot,
    active,
  });
  const instances = useMemo(
    () => (switching ? [] : (query.data?.instances ?? [])),
    [query.data?.instances, switching],
  );
  const instance = instances.find((item) => item.id === selected);
  const instanceId = selected || instance?.id;

  const resultOpen = params.get("result") === "1";
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
  const closeDetails = () => {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.delete("instance");
        next.delete("node");
        return next;
      },
      { replace: true },
    );
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
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("node", nodeId);
        next.delete("result");
        if (actual) next.set("instance", actual);
        else next.delete("instance");
        return next;
      },
      { replace: true },
    );
  };
  const prompted = useRef(new Set<string>());
  const requestedId = params.get("request")?.split(":").at(-1);
  const requestedGone = Boolean(
    requestedId &&
    run &&
    !query.isFetching &&
    !query.isError &&
    ![...waiting, ...operations].some((item) => item.id === requestedId),
  );
  useEffect(() => {
    if (!active || switching || params.has("node")) return;
    const request = requestedId
      ? [...waiting, ...operations].find((item) => item.id === requestedId)
      : [...waiting, ...operations].find(
          (item) => item.id && !prompted.current.has(item.id),
        );
    if (!request?.id || !request.instanceId) return;
    const nodeId =
      instances.find((item) => item.id === request.instanceId)?.nodeId ||
      query.data?.nodeCounts?.find(
        (item) => item.instanceId === request.instanceId,
      )?.nodeId;
    if (!nodeId) return;
    prompted.current.add(request.id);
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("node", nodeId);
        next.set("instance", request.instanceId ?? "");
        return next;
      },
      { replace: true },
    );
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
  ]);
  const actions = (
    <div className="flex items-center gap-2">
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
    <section
      ref={runRoot}
      className="@container/run-view relative flex min-h-0 min-w-0 flex-1"
    >
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {actionsContainer ? (
          createPortal(actions, actionsContainer)
        ) : (
          <div className="absolute top-4 right-4 z-10">{actions}</div>
        )}
        {query.isError ? (
          <WorkflowQueryError
            error={query.error}
            className="p-5"
            retrying={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        ) : null}
        {requestedGone ? (
          <div
            role="status"
            className="text-content-muted type-ui flex items-center gap-3 px-5 py-3"
          >
            {boardText("requestGone")}
            <Button variant="ghost" size="sm" asChild>
              <Link to="/work-board?mode=attention">
                {boardText("backToBoard")}
              </Link>
            </Button>
          </div>
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
                content: (
                  <RunInputValues
                    manifest={query.data.manifest}
                    args={run?.args}
                  />
                ),
              }}
              nodeCounts={query.data.nodeCounts ?? []}
              nodeOutput={(node) => {
                const page =
                  query.data?.manifest?.meta?.nodeResultPages?.[node.id ?? ""];
                const files =
                  query.data?.manifest?.meta?.nodeResultFiles?.[node.id ?? ""];
                return page || files ? (
                  <NodeOutputCard
                    id={id}
                    runId={runId}
                    nodeId={node.id ?? ""}
                    title={files?.title ?? page?.title}
                    files={Boolean(files)}
                    active={active && !switching}
                    version={query.data?.run?.version}
                  />
                ) : null;
              }}
              selected={
                active && !switching && !resultOpen
                  ? (params.get("node") ?? instance?.nodeId)
                  : undefined
              }
              onDismiss={closeDetails}
              onSelect={(node) => selectNode(node.id ?? "")}
              nodeHeading={(node) => {
                const actual = instances.find(
                  (item) => item.id === instanceId && item.nodeId === node.id,
                );
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
                        human={human}
                      />
                    ))}
                  {instanceId ? (
                    <InstanceDetails
                      key={instanceId}
                      id={id}
                      runId={runId}
                      instanceId={instanceId}
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
                        setParams(
                          (previous) => {
                            const next = new URLSearchParams(previous);
                            next.set("offset", String(query.data.nextOffset));
                            return next;
                          },
                          { replace: true },
                        )
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
        onOpenChange={(open) =>
          setParams(
            (previous) => {
              const next = new URLSearchParams(previous);
              if (open) next.set("result", "1");
              else next.delete("result");
              return next;
            },
            { replace: true },
          )
        }
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
                    void query.refetch();
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
