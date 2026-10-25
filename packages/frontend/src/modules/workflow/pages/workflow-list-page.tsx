import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { Workflow } from "lucide-react";
import { useEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router-dom";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { EmptyState } from "@/shared/ui/empty-state";
import {
  ResourceCardGrid,
  ResourcePageLibraryLabel,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { WorkflowCard } from "../components/workflow-card";
import {
  WorkflowLoadingState,
  WorkflowQueryError,
} from "../components/query-feedback";
import { listWorkflowsQueryKey } from "../api/workflow-queries";
import { listWorkflowsInfiniteOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";

/** 旧列表地址继续进入资源库，并保留搜索条件。 */
export function WorkflowListPage() {
  const [params] = useSearchParams();
  const next = new URLSearchParams(params);
  next.set("category", "workflows");
  return <Navigate replace to={`/resources?${next}`} />;
}

export function WorkflowResourceCollection({
  search,
  filters,
}: {
  search: string;
  filters: ReactNode;
}) {
  const { t } = useTranslation("workflow");
  const client = useQueryClient();
  const active = useRouteSceneActive();
  useEffect(() => {
    if (active)
      void client.invalidateQueries({ queryKey: listWorkflowsQueryKey() });
  }, [active, client]);
  const query = useInfiniteQuery({
    ...listWorkflowsInfiniteOptions({ query: { search, limit: 9 } }),
    enabled: active,
    initialPageParam: { query: { search, limit: 9 } },
    getNextPageParam: (page) =>
      page.nextBefore && page.nextBeforeId
        ? {
            query: {
              search,
              limit: 9,
              before: page.nextBefore,
              beforeId: page.nextBeforeId,
            },
          }
        : undefined,
  });
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  return (
    <>
      <ResourcePageToolbar filters={filters}>
        <ResourcePageLibraryLabel
          installed={t("library")}
          count={t("count", { count: items.length })}
        />
      </ResourcePageToolbar>
      {query.isError ? (
        <WorkflowQueryError
          error={query.error}
          className="mt-6"
          retrying={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? (
        <WorkflowLoadingState className="mt-6" />
      ) : !query.data ? null : !items.length ? (
        <EmptyState
          className="mt-6"
          title={t("empty")}
          description={t("emptyHelp")}
          icon={Workflow}
        />
      ) : (
        <ResourceCardGrid className="mt-6">
          {items.map((definition) => (
            <WorkflowCard key={definition.id} definition={definition} />
          ))}
        </ResourceCardGrid>
      )}
      <ResourceInfiniteScroll
        hasMore={Boolean(query.hasNextPage)}
        pending={query.isFetchingNextPage}
        error={query.isFetchNextPageError ? query.error : undefined}
        onLoadMore={() => void query.fetchNextPage()}
      />
    </>
  );
}
