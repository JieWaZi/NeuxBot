import {
  infiniteQueryOptions,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import {
  getMainConversation,
  getMainConversationSource,
} from "@/shared/api/generated/sdk.gen";
import type {
  GetMainConversationResponse,
  GetMainConversationSourceResponse,
} from "@/shared/api/generated/types.gen";
import { ApiError } from "@/shared/api/errors";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";

export type ConversationSubject = {
  subjectType: "waker" | "team";
  subjectId: string;
};
export type PublicMessage = NonNullable<
  GetMainConversationResponse["messages"]
>[number];

function earlier(page: { hasMore?: boolean; nextBeforeSequence?: number }) {
  return page.hasMore ? page.nextBeforeSequence : undefined;
}

export function conversationSubmissionKey(subject: ConversationSubject) {
  return [
    "main-conversation-submit",
    subject.subjectType,
    subject.subjectId,
  ] as const;
}

export function mainConversationOptions(path: ConversationSubject) {
  return infiniteQueryOptions({
    queryKey: ["main-conversation", path.subjectType, path.subjectId],
    initialPageParam: undefined as number | undefined,
    getNextPageParam: earlier,
    queryFn: async ({ pageParam, signal }) =>
      (
        await getMainConversation(
          withLocalErrorSurface({
            path,
            query: { limit: 50, beforeSequence: pageParam },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
  });
}

/** 新发言挪动分页窗口时，用同次权威读取覆盖正在阅读的引用原话。 */
export function refreshMainConversationHistory(
  client: QueryClient,
  options: ReturnType<typeof mainConversationOptions>,
  visibleTarget: () => string | undefined,
) {
  const { queryKey } = options;
  const targetId = visibleTarget();
  const current = targetId ? client.getQueryData(queryKey) : undefined;
  const sequence = current?.pages
    .flatMap((page) => page.messages ?? [])
    .find((row) => row.id === targetId)?.sequence;
  if (!current || sequence === undefined)
    return client.refetchQueries(
      { queryKey, exact: true, type: "active" },
      { cancelRefetch: false },
    );

  const loadedPages = current.pages.length;
  return client.fetchInfiniteQuery({
    ...options,
    staleTime: 0,
    pages: loadedPages + 20,
    getNextPageParam: (page, pages) => {
      const oldest = page.messages?.[0]?.sequence;
      if (
        pages.length >= loadedPages &&
        (visibleTarget() !== targetId ||
          (oldest !== undefined && oldest <= sequence))
      )
        return undefined;
      return earlier(page);
    },
  });
}

export function sourceHistoryOptions(
  subject: ConversationSubject,
  workChatId: string,
) {
  const path = { ...subject, workChatId };
  return infiniteQueryOptions({
    queryKey: [
      "main-conversation-source",
      subject.subjectType,
      subject.subjectId,
      workChatId,
    ],
    initialPageParam: undefined as number | undefined,
    getNextPageParam: earlier,
    queryFn: async ({ pageParam, signal }) =>
      (
        await getMainConversationSource(
          withLocalErrorSurface({
            path,
            query: { limit: 50, beforeSequence: pageParam },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
  });
}

/** 已确认来源在临时查询失败时仍可用，明确拒绝则撤销其缓存访问资格。 */
export function authorizedSourceChat(
  workChatId: string,
  chat: GetMainConversationSourceResponse["chat"],
  error: unknown,
): GetMainConversationSourceResponse["chat"] {
  const unavailable =
    error instanceof ApiError &&
    (error.code === "ChatNotFound" ||
      error.status === 401 ||
      error.status === 403 ||
      error.status === 404);
  return !unavailable && chat?.id === workChatId ? chat : undefined;
}

/** 读取来源 Chat 的目录和当前待处理请求，跳过工作历史分页。 */
export function sourceScopeOptions(
  subject: ConversationSubject,
  workChatId: string,
) {
  return queryOptions({
    queryKey: [
      "main-conversation-source-snapshot",
      subject.subjectType,
      subject.subjectId,
      workChatId,
    ],
    queryFn: async ({ signal }) =>
      (
        await getMainConversationSource(
          withLocalErrorSurface({
            path: { ...subject, workChatId },
            query: { limit: 1 },
            signal,
            throwOnError: true,
          }),
        )
      ).data.chat,
  });
}
