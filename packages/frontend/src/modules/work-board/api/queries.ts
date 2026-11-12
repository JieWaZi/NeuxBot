import {
  infiniteQueryOptions,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import {
  getWorkBoardSummary,
  getWorkBoardViewTargets,
  listWorkBoard,
  markWorkBoardViewed,
} from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  checkAttentionEpoch,
  workspaceAttentionResource,
} from "@/shared/api/reading-queries";
import type {
  GetWorkBoardSummaryResponse,
  GetWorkBoardViewTargetsData,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardEntry,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkViewTarget,
  ListWorkBoardData,
} from "@/shared/api/generated/types.gen";

export type WorkBoardMode = "all" | "action" | "awaiting_view";
export type WorkBoardEntry =
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkBoardEntry;
export type WorkBoardSummary = GetWorkBoardSummaryResponse;
type BoardFilters = NonNullable<GetWorkBoardViewTargetsData["query"]>;
type BoardListFilters = Omit<
  NonNullable<ListWorkBoardData["query"]>,
  "mode"
> & { mode: WorkBoardMode };

export const workBoardKeys = {
  list: ["work-board", "list"] as const,
  focus: ["work-board", "focus"] as const,
  summary: ["work-board", "summary"] as const,
};
export const workBoardFocusHeadLimit = 100;

async function fetchWorkBoardPage(
  filters: BoardListFilters,
  signal: AbortSignal,
) {
  const { data } = await listWorkBoard(
    withLocalErrorSurface({
      query: filters,
      signal,
      throwOnError: true as const,
    }),
  );
  if (
    data.totalSize === undefined ||
    !Number.isSafeInteger(data.totalSize) ||
    data.totalSize < 0
  )
    throw new Error("Invalid work board page");
  return checkAttentionEpoch("work-board", workspaceAttentionResource, data);
}

export function workBoardListOptions(filters: BoardListFilters) {
  return queryOptions({
    queryKey: [...workBoardKeys.list, filters] as const,
    queryFn: ({ signal }) => fetchWorkBoardPage(filters, signal),
    retry: false,
    staleTime: Infinity,
  });
}

export function workBoardFocusOptions(mode: Exclude<WorkBoardMode, "all">) {
  return infiniteQueryOptions({
    queryKey: [...workBoardKeys.focus, mode] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      fetchWorkBoardPage({ mode, cursor: pageParam, limit: 50 }, signal),
    getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
    retry: false,
    // 自动重抓会重放所有已缓存页，关注区由握手和领域失效对账头部。
    staleTime: "static",
  });
}

export class WorkBoardFocusIncomplete extends Error {
  readonly complete = false;

  constructor(readonly incompleteReason?: string) {
    super("Work board focus is incomplete");
  }
}

export function refreshWorkBoardFocus(
  client: QueryClient,
  mode: Exclude<WorkBoardMode, "all">,
  isBrowsingDeep: () => boolean,
) {
  const options = workBoardFocusOptions(mode);
  return client.fetchInfiniteQuery({
    ...options,
    pages: 1,
    staleTime: 0,
    queryFn: async ({ signal }) => {
      const data = await fetchWorkBoardPage(
        { mode, limit: workBoardFocusHeadLimit },
        signal,
      );
      signal.throwIfAborted();
      // 读取期间也可能滚过刷新窗口。原查询回退，等待用户返回后再对账。
      if (isBrowsingDeep()) {
        await client.cancelQueries(
          { queryKey: options.queryKey, exact: true },
          { revert: true },
        );
        signal.throwIfAborted();
      }
      if (data.complete !== true)
        throw new WorkBoardFocusIncomplete(data.incompleteReason);
      return data;
    },
  });
}

export const workBoardSummaryOptions = queryOptions({
  queryKey: workBoardKeys.summary,
  queryFn: async ({ signal }) => {
    const { data } = await getWorkBoardSummary(
      withLocalErrorSurface({ signal, throwOnError: true as const }),
    );
    return checkAttentionEpoch("work-board", workspaceAttentionResource, data);
  },
  retry: false,
  staleTime: Infinity,
});

type ViewTarget = GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkViewTarget;
type BatchResult = {
  applied: number;
  skippedConflict: number;
  skippedDeleted: number;
};
export type ViewingOperation = BatchResult & {
  targets: ViewTarget[];
  completed: number;
};

export async function prepareViewingOperation(
  filters: BoardFilters,
  signal: AbortSignal,
): Promise<ViewingOperation> {
  const { data } = await getWorkBoardViewTargets(
    withLocalErrorSurface({
      query: filters,
      signal,
      throwOnError: true as const,
    }),
  );
  const snapshot = checkAttentionEpoch(
    "work-board",
    workspaceAttentionResource,
    data,
  );
  if (!snapshot.targets || snapshot.targets.length !== snapshot.targetCount)
    throw new Error("Invalid viewing targets");
  return {
    targets: snapshot.targets,
    completed: 0,
    applied: 0,
    skippedConflict: 0,
    skippedDeleted: 0,
  };
}

export async function applyViewingOperation(
  operation: ViewingOperation,
  signal: AbortSignal,
  onProgress: (operation: ViewingOperation) => void,
) {
  let current = operation;
  while (current.completed < current.targets.length) {
    signal.throwIfAborted();
    const targets = current.targets.slice(
      current.completed,
      current.completed + 128,
    );
    const { data } = await markWorkBoardViewed(
      withLocalErrorSurface({
        body: { targets },
        signal,
        throwOnError: true as const,
      }),
    );
    const { applied, skippedConflict, skippedDeleted } = data;
    if (
      applied === undefined ||
      skippedConflict === undefined ||
      skippedDeleted === undefined ||
      applied + skippedConflict + skippedDeleted !== targets.length
    )
      throw new Error("Invalid mark viewed result");
    current = {
      targets: current.targets,
      completed: current.completed + targets.length,
      applied: current.applied + applied,
      skippedConflict: current.skippedConflict + skippedConflict,
      skippedDeleted: current.skippedDeleted + skippedDeleted,
    };
    onProgress(current);
  }
}
