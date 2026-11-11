import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Workflow } from "lucide-react";
import { useEffect, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useSearchParams } from "react-router-dom";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { EmptyState } from "@/shared/ui/empty-state";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";
import { WorkflowCard } from "../components/workflow-card";
import { WorkflowQueryError } from "../components/query-feedback";
import { listWorkflowsQueryKey } from "../api/workflow-queries";
import {
  listWorkflowsInfiniteOptions,
  listResourceCategoriesOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { ResourceCatalogGroup } from "@/shared/ui/resource-catalog";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
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
  const [params, setParams] = useSearchParams();
  const category = params.get("workflowCategory");
  const categoryOptions = useQuery({
    ...listResourceCategoriesOptions(
      withLocalErrorSurface({ query: { type: "workflow" } }),
    ),
    enabled: active,
  });
  const categories = [
    ...new Set([
      ...(categoryOptions.data ?? []).flatMap((item) =>
        item.name ? [item.name] : [],
      ),
      ...(category ? [category] : []),
    ]),
  ].sort((a, b) => a.localeCompare(b));
  const filtersQuery = {
    search,
    limit: 24,
    ...(category === ""
      ? { uncategorized: true }
      : category
        ? { category }
        : {}),
  };
  useEffect(() => {
    if (active)
      void client.invalidateQueries({ queryKey: listWorkflowsQueryKey() });
  }, [active, client]);
  const query = useInfiniteQuery({
    ...listWorkflowsInfiniteOptions({ query: filtersQuery }),
    enabled: active,
    initialPageParam: { query: filtersQuery },
    getNextPageParam: (page) =>
      page.nextBefore && page.nextBeforeId
        ? {
            query: {
              ...filtersQuery,
              before: page.nextBefore,
              beforeId: page.nextBeforeId,
            },
          }
        : undefined,
  });
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const groups = new Map<string, typeof items>();
  for (const item of items) {
    for (const name of new Set(
      item.categories?.length ? item.categories : [""],
    )) {
      const group = groups.get(name) ?? [];
      group.push(item);
      groups.set(name, group);
    }
  }
  const rows = (definitions: typeof items, group?: string) => (
    <ResourceCardGrid appearance="catalog">
      {definitions.map((definition) => (
        <WorkflowCard
          key={definition.id}
          definition={definition}
          category={group}
        />
      ))}
    </ResourceCardGrid>
  );
  return (
    <div className="grid min-w-0 gap-8">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <ResourceFilterSelect
          size="catalog"
          ariaLabel={t("common:resourceCategory.label")}
          value={JSON.stringify(category)}
          options={[
            { value: "null", label: t("skill:category.allCategories") },
            {
              value: JSON.stringify(""),
              label: t("skill:category.uncategorized"),
            },
            ...categories.map((name) => ({
              value: JSON.stringify(name),
              label: name,
            })),
          ]}
          onValueChange={(value) =>
            setParams((previous) => {
              const next = new URLSearchParams(previous);
              if (value === "null") next.delete("workflowCategory");
              else {
                const name =
                  value === JSON.stringify("")
                    ? ""
                    : categories.find((name) => JSON.stringify(name) === value);
                if (name !== undefined) next.set("workflowCategory", name);
              }
              return next;
            })
          }
        />
        {filters}
      </div>
      {categoryOptions.isError ? (
        <WorkflowQueryError
          error={categoryOptions.error}
          retrying={categoryOptions.isFetching}
          onRetry={() => void categoryOptions.refetch()}
        />
      ) : null}
      {query.isError && !query.isFetchNextPageError ? (
        <WorkflowQueryError
          error={query.error}
          retrying={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? (
        <ResourceLoadingState layout="rows" label={t("loading")} />
      ) : !query.data ? null : !items.length ? (
        <EmptyState
          title={search ? t("plugin:noResults") : t("empty")}
          description={search ? undefined : t("emptyHelp")}
          icon={Workflow}
        />
      ) : category !== null ? (
        rows(items, category)
      ) : (
        [...groups]
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([name, definitions]) => (
            <ResourceCatalogGroup
              key={name}
              title={name || t("skill:category.uncategorized")}
              aria-label={name || t("skill:category.uncategorized")}
            >
              {rows(definitions, name)}
            </ResourceCatalogGroup>
          ))
      )}
      <ResourceInfiniteScroll
        hasMore={active && Boolean(query.hasNextPage)}
        pending={query.isFetchingNextPage}
        error={query.isFetchNextPageError ? query.error : undefined}
        onLoadMore={() => void query.fetchNextPage()}
        showLoadMoreButton={false}
      />
    </div>
  );
}
