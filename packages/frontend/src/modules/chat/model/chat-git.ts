import {
  queryOptions,
  useQueries,
  type QueryClient,
} from "@tanstack/react-query";

import { getScopedGitComparison, getScopedGitStatus } from "@/shared/platform";

export type ChatGitRepository = { root: string; path: string };

export function gitStatusOptions(
  chatId: string | undefined,
  root: string,
  repository?: string,
) {
  return queryOptions({
    queryKey: ["scoped-git-status", chatId, root, repository ?? ""],
    queryFn: () => getScopedGitStatus(chatId, root, repository),
  });
}

export function gitComparisonOptions(
  chatId: string | undefined,
  root: string,
  repository: string,
  base?: string,
  path?: string,
  ignoreWhitespace = false,
  previousPath?: string,
) {
  return queryOptions({
    queryKey: [
      "scoped-git-comparison",
      chatId,
      root,
      repository,
      base ?? "",
      path ?? "",
      ignoreWhitespace,
      previousPath ?? null,
    ],
    queryFn: ({ signal }) =>
      getScopedGitComparison(
        chatId,
        root,
        repository,
        base,
        path,
        ignoreWhitespace,
        previousPath,
        signal,
      ),
  });
}

export function useChatGitRepositories(
  chatId: string | undefined,
  roots: readonly string[],
  active: boolean,
) {
  const directories = [...new Set(roots.filter(Boolean))];
  const statuses = useQueries({
    queries: directories.map((root) => ({
      ...gitStatusOptions(chatId, root),
      enabled: active,
    })),
  });
  const repositories = new Map<string, ChatGitRepository>();
  directories.forEach((root, index) => {
    const status = statuses[index]?.data;
    const paths = status?.repositories?.length
      ? status.repositories
      : status?.isRepo && status.root
        ? [status.root]
        : [];
    paths.forEach((path) => {
      if (root === path || !repositories.has(path))
        repositories.set(path, { root, path });
    });
  });
  return { repositories: [...repositories.values()], statuses };
}

export function invalidateChatGit(
  queryClient: QueryClient,
  root: string,
  repository: string,
) {
  return queryClient.invalidateQueries({
    predicate: ({ queryKey }) =>
      typeof queryKey[0] === "string" &&
      queryKey[0].startsWith("scoped-git-") &&
      (queryKey[2] === root || queryKey[3] === repository),
  });
}
