import { type ReactNode } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { listLibraryConnectorsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import { listLibraryConnectors } from "@/shared/api/generated/sdk.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { useResourcePages } from "@/shared/hooks/use-resource-pages";
import { useResourceWindow } from "@/shared/hooks/use-resource-window";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  listWakerConnectorsOptions,
  setWakerConnectorEnabledMutation,
  invalidateVisibleConnectorQueries,
} from "../api/connector-queries";
import { ConnectorCard } from "../components/connector-card";
import { toConnectorSummary } from "../model/connector-view";

export function ConnectorResourceCollection({
  wakerId = "",
  search,
  filters,
  onInstall,
}: {
  wakerId?: string;
  search: string;
  filters: ReactNode;
  onInstall: (id: string, name: string) => void;
}) {
  const { t } = useTranslation("plugin");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const libraryKey = listLibraryConnectorsQueryKey({
    query: { query: search, pageSize: 24 },
  });
  const library = useResourcePages({
    queryKey: libraryKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listLibraryConnectors(
        withLocalErrorSurface({
          query: { query: search, pageNum, pageSize: 24 },
          signal,
          throwOnError: true as const,
        }),
      );
      return { items: data.resources ?? [], total: data.totalSize ?? 0 };
    },
    enabled: active && !wakerId,
  });
  const installed = useQuery({
    ...listWakerConnectorsOptions(
      withLocalErrorSurface({ path: { wakerId }, query: { query: search } }),
    ),
    enabled: active && Boolean(wakerId),
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [
      [...libraryKey, "infinite"],
      listWakerConnectorsOptions({
        path: { wakerId },
        query: { query: search },
      }).queryKey,
    ],
    active,
  });
  const query = wakerId ? installed : library;
  const records = wakerId ? (installed.data ?? []) : library.items;
  const connectors = records.flatMap((item) => toConnectorSummary(item) ?? []);
  const window = useResourceWindow(
    connectors,
    JSON.stringify([wakerId, search]),
    24,
  );
  const visible = wakerId ? window.visible : connectors;
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerConnectorEnabledMutation()),
    onSuccess: () => invalidateVisibleConnectorQueries(client),
  });
  return (
    <div className="space-y-8">
      <div className="flex min-w-0 flex-wrap items-center gap-3">{filters}</div>
      <div className="space-y-4">
        {query.data !== undefined && (
          <p className="type-resource-body text-resource-muted">
            {t("connector:count", {
              count: wakerId ? connectors.length : library.total,
            })}
          </p>
        )}
        {query.isPending ||
        (query.isError && visible.length === 0) ||
        visible.length === 0 ? (
          <QueryState
            loadingLayout="rows"
            pending={query.isPending}
            error={query.error}
            emptyLabel={t(search ? "noResults" : "empty")}
            onRetry={() => void query.refetch()}
          />
        ) : null}
        <ResourceCardGrid appearance="catalog">
          {visible.map((connector) => (
            <ConnectorCard
              key={connector.id}
              connector={connector}
              wakerId={wakerId}
              disabled={toggle.isPending}
              onEnabledChange={(enabled) =>
                toggle.mutate({
                  path: { wakerId, connectorId: connector.id },
                  body: { enabled },
                })
              }
              onInstall={
                !wakerId && connector.scope !== "built_in"
                  ? () => onInstall(connector.id, connector.name)
                  : undefined
              }
            />
          ))}
        </ResourceCardGrid>
        {visible.length > 0 &&
          query.isError &&
          (Boolean(wakerId) || !library.isFetchNextPageError) && (
            <QueryRefreshError
              error={query.error}
              pending={query.isFetching}
              onRetry={() => void query.refetch()}
            />
          )}
        {toggle.error && (
          <p role="alert" className="text-destructive type-body">
            {localApiErrorMessage(toggle.error, t("failed"))}
          </p>
        )}
        <ResourceInfiniteScroll
          showLoadMoreButton={false}
          hasMore={
            active &&
            (wakerId
              ? window.hasMore
              : Boolean(library.hasNextPage) && !library.isRefetchError)
          }
          pending={!wakerId && library.isFetching}
          error={
            !wakerId && library.isFetchNextPageError ? library.error : undefined
          }
          onLoadMore={
            wakerId ? window.loadMore : () => void library.fetchNextPage()
          }
        />
      </div>
    </div>
  );
}
