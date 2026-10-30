export {
  listWorkflowsOptions,
  listWorkflowsQueryKey,
  createWorkflowMutation,
  getWorkflowOptions,
  getWorkflowQueryKey,
  getWorkflowSourceOptions,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsOptions,
  listWorkflowRevisionsInfiniteOptions,
  listWorkflowRevisionsQueryKey,
  restoreWorkflowRevisionMutation,
  listWorkflowRunsOptions,
  listWorkflowRunsInfiniteOptions,
  listWorkflowRunsQueryKey,
  startWorkflowRunMutation,
  saveWorkflowSourceMutation,
  deleteWorkflowRevisionMutation,
  deleteWorkflowRunMutation,
  getWorkflowRunOptions,
  getWorkflowRunQueryKey,
  getWorkflowInstanceOptions,
  getWorkflowInstanceQueryKey,
  getWorkflowArtifactOptions,
  cancelWorkflowRunMutation,
  answerWorkflowHumanMutation,
  sendWorkflowCreationMessageMutation,
  createWorkflowAnnotationMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

import { queryOptions } from "@tanstack/react-query";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listWorkflowEvents } from "@/shared/api/generated/sdk.gen";
import {
  getWorkflowQueryKey,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsQueryKey,
  listWorkflowRunsQueryKey,
  listWorkflowsQueryKey,
  listWorkflowEventsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";

/** 终态版本进入日志查询身份，保证结束时补读且不重取全部历史。 */
export function workflowRunLogsOptions({
  id,
  runId,
  after,
  completedAt,
}: {
  id: string;
  runId: string;
  after: number;
  completedAt?: number;
}) {
  const options = {
    path: { id },
    query: { runId, after, logsOnly: true },
  };
  return queryOptions({
    queryKey: [...listWorkflowEventsQueryKey(options), completedAt ?? 0],
    queryFn: async ({ signal }) =>
      (await listWorkflowEvents({ ...options, signal, throwOnError: true }))
        .data,
  });
}

/** 定义流只使实际依赖变化事实的查询失效；恢复由同一共享调度对账。 */
export function useWorkflowDefinitionEvents(
  id: string,
  active: boolean,
  enabled = true,
) {
  const resource = { kind: "workflow" as const, id };
  useQueryEvents({
    topic: "workflow",
    resource,
    active,
    enabled,
    queryKeys: [getWorkflowQueryKey({ path: { id } })],
    eventTypes: [
      "workflow.definition.",
      "workflow.creation.",
      "workflow.revision.",
      "workflow.annotation.",
    ],
  });
  useQueryEvents({
    topic: "workflow",
    resource,
    active,
    enabled,
    queryKeys: [
      getWorkflowSourceQueryKey({ path: { id } }),
      listWorkflowRevisionsQueryKey({ path: { id } }),
    ],
    eventTypes: ["workflow.revision."],
  });
  useQueryEvents({
    topic: "workflow",
    resource,
    active,
    enabled,
    queryKeys: [listWorkflowRunsQueryKey({ path: { id } })],
    eventTypes: ["workflow.run.created", "workflow.run.history."],
  });
  useQueryEvents({
    topic: "workflow",
    resource,
    active,
    enabled,
    queryKeys: [listWorkflowsQueryKey()],
    eventTypes: ["workflow.definition.", "workflow.revision.saved"],
  });
}
