import { useQuery } from "@tanstack/react-query";
import {
  listWorkflows,
  listWorkflowRevisions,
} from "@/shared/api/generated/sdk.gen";
import type {
  ListWorkflowRevisionsResponse,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition as Definition,
} from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";

/** 工作流选择器完整读取游标目录，包含后续分页中的可运行工作流。 */
export function useTaskWorkflows(enabled: boolean) {
  return useQuery({
    queryKey: ["automatic-task-workflows"],
    enabled,
    queryFn: async ({ signal }) => {
      const items: Definition[] = [];
      let before: number | undefined;
      let beforeId: string | undefined;
      do {
        const { data } = await listWorkflows(
          withLocalErrorSurface({
            query: { limit: 100, before, beforeId },
            signal,
            throwOnError: true,
          }),
        );
        items.push(...(data?.items ?? []));
        before = data?.nextBefore || undefined;
        beforeId = data?.nextBeforeId || undefined;
      } while (before && beforeId);
      return items;
    },
  });
}

/** 固定版本选择器也保留历史分页中的版本。 */
export function useTaskWorkflowVersions(id: string, enabled = true) {
  return useQuery({
    queryKey: ["automatic-task-workflow-versions", id],
    enabled: enabled && Boolean(id),
    queryFn: async ({ signal }) => {
      const items: NonNullable<ListWorkflowRevisionsResponse["items"]> = [];
      let before: number | undefined;
      do {
        const { data } = await listWorkflowRevisions(
          withLocalErrorSurface({
            path: { id },
            query: { limit: 100, before },
            signal,
            throwOnError: true,
          }),
        );
        items.push(...(data?.items ?? []));
        before = data?.nextBefore || undefined;
      } while (before);
      return items;
    },
  });
}
