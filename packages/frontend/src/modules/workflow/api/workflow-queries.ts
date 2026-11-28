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
  cancelWorkflowRunMutation,
  retryWorkflowConclusionMutation,
  answerWorkflowHumanMutation,
  sendWorkflowCreationMessageMutation,
  createWorkflowAnnotationMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

import { queryOptions, type QueryClient } from "@tanstack/react-query";
import {
  refreshQueryFromEvent,
  useQueryEvents,
} from "@/shared/api/use-query-events";
import { listWorkflowEvents } from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  getWorkflowQueryKey,
  getWorkflowRunOptions,
  getWorkflowRunQueryKey,
  getWorkflowSourceQueryKey,
  listWorkflowRevisionsQueryKey,
  listWorkflowRunsQueryKey,
  listWorkflowsQueryKey,
  listWorkflowEventsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";

/** 画布与决策恢复共用原运行缓存，局部反馈标记只用于请求。 */
export function workflowRunDetailsOptions(
  input: Parameters<typeof getWorkflowRunOptions>[0],
) {
  return {
    ...getWorkflowRunOptions(withLocalErrorSurface(input)),
    queryKey: getWorkflowRunQueryKey(input),
  };
}

function workflowRevisionQueryKeys(id: string) {
  const path = { id };
  return {
    detail: getWorkflowQueryKey({ path }),
    source: getWorkflowSourceQueryKey({ path }),
    revisions: listWorkflowRevisionsQueryKey({ path }),
    catalog: listWorkflowsQueryKey(),
  };
}

/** 保存回执和版本事件通过同一 Query 调度刷新，保留正在进行的分页。 */
export async function refreshWorkflowRevisionQueries(
  client: QueryClient,
  id: string,
) {
  const keys = Object.values(workflowRevisionQueryKeys(id));
  await Promise.all(
    keys.map((queryKey) =>
      client.invalidateQueries({ queryKey, refetchType: "none" }),
    ),
  );
  const activeQueries = new Set(
    keys.flatMap((queryKey) =>
      client.getQueryCache().findAll({ queryKey, type: "active" }),
    ),
  );
  // 已提交的保存回执不因刷新失败变成失败，所属查询保留错误并可对账。
  await Promise.allSettled(
    [...activeQueries].map(({ queryKey }) =>
      refreshQueryFromEvent(client, queryKey, () =>
        client.refetchQueries(
          { queryKey, exact: true, type: "active" },
          { cancelRefetch: false, throwOnError: true },
        ),
      ),
    ),
  );
}

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
  const keys = workflowRevisionQueryKeys(id);
  useQueryEvents({
    topic: "workflow",
    resource,
    active,
    enabled,
    queryKeys: [keys.detail],
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
    queryKeys: [keys.source, keys.revisions],
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
    queryKeys: [keys.catalog],
    eventTypes: ["workflow.definition.", "workflow.revision.saved"],
  });
}
