import {
  queryOptions,
  useQuery,
  useQueries,
  useQueryClient,
} from "@tanstack/react-query";
import {
  getExecutorAttention,
  getWorkspaceAttention,
} from "@/shared/api/generated/sdk.gen";
import type {
  GetWorkspaceAttentionResponse,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsExecutorAttentionChat,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsExecutorAttentionSummary,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  checkAttentionEpoch,
  workspaceAttentionResource,
} from "@/shared/api/reading-queries";
import {
  hasPendingQueryEventRefresh,
  useQueryEvents,
} from "@/shared/api/use-query-events";

const attentionKey = ["workspace-attention"] as const;

export function useWorkspaceAttention() {
  const client = useQueryClient();
  const query = useQuery(
    queryOptions({
      queryKey: attentionKey,
      queryFn: async ({ signal, client }) => {
        const { data } = await getWorkspaceAttention(
          withLocalErrorSurface({ signal, throwOnError: true as const }),
        );
        const snapshot = checkAttentionEpoch(
          "work-board",
          workspaceAttentionResource,
          data,
        );
        if (snapshot.complete) return snapshot;
        // 部分结果只确认已知为真的事项，不能否定此前已核实的状态。
        const previous =
          client.getQueryData<GetWorkspaceAttentionResponse>(attentionKey);
        return {
          ...snapshot,
          hasAction: snapshot.hasAction || previous?.hasAction === true,
          hasUnread: snapshot.hasUnread || previous?.hasUnread === true,
        };
      },
      retry: false,
      staleTime: Infinity,
    }),
  );
  const connection = useQueryEvents({
    topic: "work-board",
    resource: workspaceAttentionResource,
    queryKeys: [attentionKey],
    eventTypes: ["work-board.changed"],
    authority: true,
  });
  return {
    hasAction: query.data?.hasAction === true,
    hasUnread: query.data?.hasUnread === true,
    synced: Boolean(
      query.data?.complete &&
      !query.isError &&
      !query.isFetching &&
      !hasPendingQueryEventRefresh(client, attentionKey) &&
      connection.epoch &&
      connection.epoch === query.data.epoch,
    ),
  };
}

export type ChatAttention =
  GithubComJieWaZiNeuxBotServerRepositoriesModelsExecutorAttentionChat;
export type ExecutorAttention =
  GithubComJieWaZiNeuxBotServerRepositoriesModelsExecutorAttentionSummary;

const executorAttentionKey = ["workspace-attention-executors"] as const;

export function useExecutorAttention(executorIds: readonly string[]) {
  const ids = [...new Set(executorIds)].sort();
  const queries = useQueries({
    queries: Array.from({ length: Math.ceil(ids.length / 128) }, (_, index) => {
      const batch = ids.slice(index * 128, (index + 1) * 128).join(",");
      return queryOptions({
        queryKey: [...executorAttentionKey, batch],
        retry: false,
        staleTime: Infinity,
        queryFn: async ({ signal }) => {
          const { data } = await getExecutorAttention(
            withLocalErrorSurface({
              query: { executorIds: batch },
              signal,
              throwOnError: true as const,
            }),
          );
          return checkAttentionEpoch(
            "work-board",
            workspaceAttentionResource,
            data,
          );
        },
      });
    }),
  });
  useQueryEvents({
    topic: "work-board",
    resource: workspaceAttentionResource,
    queryKeys: [executorAttentionKey],
    authority: true,
    enabled: Boolean(ids.length),
    eventTypes: ["work-board.changed"],
  });
  return new Map(
    queries.flatMap(
      (query) =>
        query.data?.resources?.flatMap((entry) =>
          entry.executorId ? [[entry.executorId, entry] as const] : [],
        ) ?? [],
    ),
  );
}
