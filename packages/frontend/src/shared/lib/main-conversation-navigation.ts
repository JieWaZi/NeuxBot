import type { ChatSceneOwner } from "@/shared/workspace/app-scene-model";

export type ConversationDestination = ChatSceneOwner & {
  workChatId?: string;
  messageId?: string;
  publicationId?: string;
  recordMessageId?: string;
  request?: MainConversationRequestTarget;
};

export type MainConversationRequestTarget = {
  workChatId: string;
  requestId: string;
};

/** 来源隶属于主体的固定页签，切换工作只改变该页签中的地址。 */
export function mainConversationHref({
  kind,
  id,
  workChatId,
  messageId,
  publicationId,
  recordMessageId,
  request,
}: ConversationDestination) {
  const main = `/chat/main/${kind}/${encodeURIComponent(id)}`;
  if (workChatId) {
    const params = new URLSearchParams({ source: workChatId });
    if (publicationId) params.set("publication", publicationId);
    else if (messageId) params.set("message", messageId);
    return `${main}?${params}`;
  }
  if (recordMessageId)
    return `${main}?${new URLSearchParams({ record: recordMessageId })}`;
  if (request)
    return `${main}?${new URLSearchParams({ request: request.requestId, requestChat: request.workChatId })}`;
  if (messageId)
    return `${main}?${new URLSearchParams({ message: messageId })}`;
  return main;
}

export function mainConversationSourceId(params: URLSearchParams) {
  return params.get("source") || undefined;
}

export function mainConversationRecordId(params: URLSearchParams) {
  return params.get("record") || undefined;
}

export function mainConversationRequestTarget(params: URLSearchParams) {
  const requestId = params.get("request");
  const workChatId = params.get("requestChat");
  return requestId && workChatId ? { requestId, workChatId } : undefined;
}
