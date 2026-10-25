export {
  createAutomaticTaskMutation,
  createWorkflowAutomaticTaskMutation,
  updateWorkflowAutomaticTaskMutation,
  deleteAutomaticTaskMutation,
  getAutomaticTaskOptions,
  getAutomaticTaskQueryKey,
  listAutomaticTasksOptions,
  listGlobalAutomaticTasksOptions,
  getGlobalAutomaticTaskOptions,
  listAutomaticTasksQueryKey,
  listAutomaticRunsOptions,
  listAutomaticRunsQueryKey,
  listGlobalAutomaticTasksQueryKey,
  getGlobalAutomaticTaskQueryKey,
  setAutomaticTaskEnabledMutation,
  triggerAutomaticTaskMutation,
  updateAutomaticTaskMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";

import type { QueryClient } from "@tanstack/react-query";
import {
  getAutomaticTaskQueryKey,
  listAutomaticTasksQueryKey,
  listAutomaticRunsQueryKey,
  listGlobalAutomaticTasksQueryKey,
  getGlobalAutomaticTaskQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";

export async function refreshAutomaticTasks(
  client: QueryClient,
  wakerId: string,
  taskId?: string,
) {
  await Promise.all([
    client.invalidateQueries({ queryKey: listGlobalAutomaticTasksQueryKey() }),
    client.invalidateQueries({
      queryKey: listAutomaticTasksQueryKey({ path: { wakerId } }),
    }),
    ...(taskId
      ? [
          client.invalidateQueries({
            queryKey: getGlobalAutomaticTaskQueryKey({ path: { taskId } }),
          }),
          client.invalidateQueries({
            queryKey: getAutomaticTaskQueryKey({ path: { wakerId, taskId } }),
          }),
          client.invalidateQueries({
            queryKey: listAutomaticRunsQueryKey({ path: { wakerId, taskId } }),
          }),
        ]
      : []),
  ]);
}
