import type { QueryClient } from "@tanstack/react-query";
import {
  getWorkflowRunQueryKey,
  workflowRunDetailsOptions,
} from "./workflow-queries";
import type { ListChatWorkflowRunsResponse } from "@/shared/api/generated/types.gen";
import { chatWorkflowRunsScopeKey } from "@/shared/api/chat-workflow-query-keys";
import { applicationErrorKey } from "@/shared/api/error-presentation";
import { ApiError } from "@/shared/api/errors";
import { refreshQueryFromEvent } from "@/shared/api/use-query-events";

export function isWorkflowDecisionConflict(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    applicationErrorKey(error.code) === "WorkflowStateConflict"
  );
}

/** 拒绝旧快照后，让当前运行及其源聊天沿原查询重新对账。 */
export async function refreshWorkflowDecisionQueries(
  client: QueryClient,
  id: string,
  runId: string,
  chatId?: string,
) {
  const path = { id, runId };
  const cache = client.getQueryCache();
  const runQueries = cache.findAll({
    queryKey: getWorkflowRunQueryKey({ path }),
  });
  const chatQueries = cache.findAll({
    queryKey: chatWorkflowRunsScopeKey(chatId),
    predicate: ({ queryKey }) => {
      // 独立画布没有源聊天参数，只刷新已关联该运行的摘要。
      if (chatId) return true;
      const summary =
        client.getQueryData<ListChatWorkflowRunsResponse>(queryKey);
      return Boolean(
        summary?.resources?.some(
          (run) =>
            (run.definitionId === id && run.id === runId) ||
            [
              ...(run.activeRequests ?? []),
              ...(run.answeredRequests ?? []),
            ].some(
              (request) =>
                request.definitionId === id && request.runId === runId,
            ),
        ),
      );
    },
  });
  const refreshes = [...runQueries, ...chatQueries].map((query) => {
    const { queryKey } = query;
    const filters = { queryKey, exact: true };
    return query.isActive()
      ? refreshQueryFromEvent(client, queryKey, () =>
          client.refetchQueries(filters, {
            cancelRefetch: false,
            throwOnError: true,
          }),
        )
      : client.invalidateQueries({ ...filters, refetchType: "none" });
  });
  if (!runQueries.some((query) => query.isActive())) {
    // 聊天单独呈现请求时，仍更新画布首次打开会读取的运行快照。
    const input = { path, query: { offset: 0 } };
    const options = workflowRunDetailsOptions(input);
    refreshes.push(
      refreshQueryFromEvent(client, options.queryKey, () =>
        client.fetchQuery(options),
      ),
    );
  }
  await Promise.allSettled(refreshes);
}
