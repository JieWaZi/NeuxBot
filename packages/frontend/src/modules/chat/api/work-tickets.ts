import { useQueries } from "@tanstack/react-query";
import { listWorkTicketsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListWorkTicketsResponse } from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";

export type WorkTicket = NonNullable<ListWorkTicketsResponse[number]["ticket"]>;
export type WorkTicketOrigin = {
  originKind: "chat" | "main";
  originId: string;
};

const messageLimit = 100;

/** 按现有历史页批读消息关联。流式正文更新不改变已加载消息的查询身份。 */
export function useWorkTickets(
  origin: WorkTicketOrigin | undefined,
  pages: readonly (readonly string[])[],
  active: boolean,
) {
  const queries = origin
    ? pages.flatMap((ids) => {
        const messages = [...new Set(ids)].sort();
        const batches = [];
        for (let start = 0; start < messages.length; start += messageLimit) {
          const messageIds = messages.slice(start, start + messageLimit);
          batches.push({
            messageIds,
            options: listWorkTicketsOptions(
              withLocalErrorSurface({
                query: {
                  ...origin,
                  messageId: messageIds,
                },
              }),
            ),
          });
        }
        return batches;
      })
    : [];
  const results = useQueries({
    queries: queries.map(({ options }) => ({ ...options, enabled: active })),
  });
  useQueryEvents({
    topic: "work-tickets",
    resource: { kind: origin?.originKind ?? "chat", id: origin?.originId },
    eventTypes: ["work-tickets.changed"],
    queryKeys: queries.map(({ options }) => options.queryKey),
    active,
    enabled: queries.length > 0,
  });
  const byMessage = new Map<string, WorkTicket>();
  const checkedMessages = new Set<string>();
  for (const [index, result] of results.entries()) {
    if (result.isSuccess)
      for (const id of queries[index]?.messageIds ?? [])
        checkedMessages.add(id);
    for (const { messageId, ticket } of result.data ?? []) {
      if (messageId && ticket) byMessage.set(messageId, ticket);
    }
  }
  const failed = results.filter((result) => result.isError);
  const loadError = failed[0]
    ? {
        error: failed[0].error,
        pending: failed.some(({ isFetching }) => isFetching),
        onRetry: () =>
          void Promise.all(failed.map((result) => result.refetch())),
      }
    : undefined;
  return { byMessage, checkedMessages, loadError };
}
