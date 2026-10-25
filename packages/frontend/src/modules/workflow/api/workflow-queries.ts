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

import { queryOptions, type QueryClient } from "@tanstack/react-query";
import { listWorkflowEvents } from "@/shared/api/generated/sdk.gen";
import {
  getWorkflowQueryKey,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsQueryKey,
  listWorkflowRunsQueryKey,
  listWorkflowsQueryKey,
  getWorkflowRunQueryKey,
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

export async function refreshWorkflow(
  client: QueryClient,
  id: string,
  runId?: string,
) {
  await Promise.all([
    client.invalidateQueries({
      queryKey: getWorkflowQueryKey({ path: { id } }),
    }),
    client.invalidateQueries({
      queryKey: getWorkflowSourceQueryKey({ path: { id } }),
    }),
    client.invalidateQueries({
      queryKey: listWorkflowRevisionsQueryKey({ path: { id } }),
    }),
    client.invalidateQueries({
      queryKey: listWorkflowRunsQueryKey({ path: { id } }),
    }),
    client.invalidateQueries({ queryKey: listWorkflowsQueryKey() }),
    ...(runId
      ? [
          client.invalidateQueries({
            queryKey: getWorkflowRunQueryKey({ path: { id, runId } }),
          }),
        ]
      : []),
  ]);
}
