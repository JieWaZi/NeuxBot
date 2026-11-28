import { useQuery } from "@tanstack/react-query";
import { listWorkTicketActivityOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListWorkTicketActivityResponse } from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import type { ConversationSubject } from "./main-conversation";

export type WorkTicketActivity = ListWorkTicketActivityResponse[number];

/** 发起方与收件方按各自获授权的主会话读取同一工单事实，不在视图层推断执行状态。 */
export function useWorkTicketActivity(
  subjectType: ConversationSubject["subjectType"],
  subjectId?: string,
  active = true,
) {
  const enabled = Boolean(subjectId);
  const options = listWorkTicketActivityOptions(
    withLocalErrorSurface({
      query: { subjectType, subjectId: subjectId ?? "" },
    }),
  );
  const result = useQuery({ ...options, enabled: enabled && active });
  useQueryEvents({
    topic: "main-conversations",
    resource: { kind: subjectType, id: subjectId },
    eventTypes: ["main-conversations.changed"],
    queryKeys: [options.queryKey],
    active,
    enabled,
  });
  return result;
}
