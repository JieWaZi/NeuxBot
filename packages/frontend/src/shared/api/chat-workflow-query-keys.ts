import { listChatWorkflowRunsQueryKey } from "./generated/@tanstack/react-query.gen";

/** 工作流摘要的查询身份同时包含来源聊天和当前回合范围。 */
export function chatWorkflowRunsScopeKey(chatId?: string) {
  const [{ _id, baseUrl, path }] = listChatWorkflowRunsQueryKey({
    path: { chatId: chatId ?? "" },
    query: { executionIds: "" },
  });
  return [chatId ? { _id, baseUrl, path } : { _id, baseUrl }];
}
