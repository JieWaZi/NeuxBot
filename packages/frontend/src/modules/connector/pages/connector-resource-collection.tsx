import { useQueryEvents } from "@/shared/api/use-query-events";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { listLibraryConnectorsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import {
  ResourceCardGrid,
  ResourcePageToolbar,
  ResourcePageLibraryLabel,
} from "@/shared/ui/resource-page";
import { listLibraryConnectors } from "@/shared/api/generated/sdk.gen";
import {
  useResourcePages,
  RESOURCE_BATCH_SIZE,
} from "@/shared/hooks/use-resource-pages";
import { useResourceWindow } from "@/shared/hooks/use-resource-window";
import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { ResourceSourceHeading } from "@/shared/ui/resource-source-heading";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  deleteWakerConnectorMutation,
  listWakerConnectorsOptions,
  setWakerConnectorEnabledMutation,
  invalidateVisibleConnectorQueries,
} from "../api/connector-queries";
import { ConnectorCard } from "../components/connector-card";
import { ConnectorFormDialog } from "../components/connector-form-dialog";
import { ConnectorDeleteDialog } from "../components/connector-delete-dialog";
import { toConnectorSummary } from "../model/connector-view";
import { ResourceBatchDeleteDialog } from "@/shared/ui/resource-batch-delete-dialog";
import type { ConnectorSummary } from "../model/types";

export function ConnectorResourceCollection({
  wakerId = "",
  search,
  adding,
  onAddingChange,
  source,
  onSourceChange,
  filters,
  pluginCount,
  pluginContent,
  pluginEmpty,
  onInstall,
}: {
  wakerId?: string;
  search: string;
  adding: boolean;
  onAddingChange: (open: boolean) => void;
  source: "all" | "plugins" | "independent";
  onSourceChange: (source: "all" | "plugins" | "independent") => void;
  filters: ReactNode;
  pluginCount: number;
  pluginContent: ReactNode;
  pluginEmpty: boolean;
  onInstall: (id: string, name: string) => void;
}) {
  const { t } = useTranslation("plugin");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [batchTargets, setBatchTargets] = useState<ConnectorSummary[]>([]);
  const remove = useMutation(
    withLocalErrorMutation(deleteWakerConnectorMutation()),
  );
  const [target, setTarget] = useState<ConnectorSummary | null>(null);
  const libraryKey = listLibraryConnectorsQueryKey({
    query: { query: search, pageSize: RESOURCE_BATCH_SIZE },
  });
  const library = useResourcePages({
    queryKey: libraryKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listLibraryConnectors(
        withLocalErrorSurface({
          query: { query: search, pageNum, pageSize: RESOURCE_BATCH_SIZE },
          signal,
          throwOnError: true as const,
        }),
      );
      return { items: data.resources ?? [], total: data.totalSize ?? 0 };
    },
    enabled: active && !wakerId && source !== "plugins",
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
  const total = wakerId ? connectors.length : library.total;
  const window = useResourceWindow(
    connectors,
    JSON.stringify([wakerId, search, source]),
  );
  const visible =
    source === "all"
      ? connectors.slice(0, RESOURCE_BATCH_SIZE)
      : wakerId
        ? window.visible
        : connectors;
  const selection = useResourceSelection(
    source === "plugins"
      ? []
      : visible.filter((item) => item.scope !== "built_in"),
    JSON.stringify([wakerId, search, source]),
  );
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerConnectorEnabledMutation()),
    onSuccess: () => invalidateVisibleConnectorQueries(client),
  });
  return (
    <div className="space-y-5">
      <ResourcePageToolbar filters={filters}>
        <ResourcePageLibraryLabel
          installed={t("allConnectorsTitle")}
          count={t("resourceCount", {
            count:
              source === "plugins"
                ? pluginCount
                : source === "independent"
                  ? total
                  : total + pluginCount,
          })}
        />
        {source !== "plugins" && (
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        )}
      </ResourcePageToolbar>
      <ResourceSelectionActions
        count={selection.selected.length}
        onClear={selection.clear}
        onDelete={() => setBatchTargets(selection.selected)}
      />
      {source !== "plugins" &&
        (source === "independent" ||
          query.isPending ||
          query.isError ||
          total > 0) && (
          <section className="space-y-3" aria-label={t("independentResources")}>
            {source === "all" && total > 0 && (
              <ResourceSourceHeading
                title={t("independentResources")}
                count={total}
                onSelect={
                  source === "all"
                    ? () => onSourceChange("independent")
                    : undefined
                }
              />
            )}
            {query.isPending || visible.length === 0 ? (
              <QueryState
                loadingLayout="cards"
                pending={query.isPending}
                error={query.error}
                emptyLabel={t(search ? "noResults" : "empty")}
                onRetry={() => void query.refetch()}
              />
            ) : (
              <ResourceCardGrid>
                {visible.map((connector) => (
                  <ConnectorCard
                    compact
                    key={connector.id}
                    connector={connector}
                    selected={selection.selectedIds.has(connector.id)}
                    onSelectedChange={
                      connector.scope === "built_in"
                        ? undefined
                        : (selected) =>
                            selection.setSelected(connector.id, selected)
                    }
                    wakerId={wakerId}
                    disabled={toggle.isPending}
                    onEnabledChange={(enabled) =>
                      toggle.mutate({
                        path: { wakerId, connectorId: connector.id },
                        body: { enabled },
                      })
                    }
                    onInstall={
                      connector.scope === "built_in"
                        ? undefined
                        : () => onInstall(connector.id, connector.name)
                    }
                    onDelete={
                      connector.scope === "built_in"
                        ? undefined
                        : () => setTarget(connector)
                    }
                  />
                ))}
              </ResourceCardGrid>
            )}
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
            {source === "independent" && (
              <ResourceInfiniteScroll
                hasMore={
                  active &&
                  (wakerId
                    ? window.hasMore
                    : Boolean(library.hasNextPage) && !library.isRefetchError)
                }
                pending={!wakerId && library.isFetching}
                error={
                  !wakerId && library.isFetchNextPageError
                    ? library.error
                    : undefined
                }
                onLoadMore={
                  wakerId ? window.loadMore : () => void library.fetchNextPage()
                }
              />
            )}
          </section>
        )}
      {pluginContent}
      {source === "all" && query.isSuccess && total === 0 && pluginEmpty && (
        <QueryState
          emptyLabel={t(search ? "noResults" : "empty")}
          onRetry={() => void query.refetch()}
        />
      )}
      <ConnectorFormDialog
        open={adding}
        wakerId={wakerId}
        onOpenChange={onAddingChange}
        onSaved={() => {
          void invalidateVisibleConnectorQueries(client);
          onAddingChange(false);
        }}
      />
      {batchTargets.length > 0 && (
        <ResourceBatchDeleteDialog
          key={batchTargets.map((item) => item.id).join(":")}
          items={batchTargets.map((item) => ({
            id: item.id,
            name: t("connector:delete.affected", {
              name: item.name,
              count: item.affectedWakerCount,
            }),
          }))}
          title={t("connector:delete.batchTitle")}
          description={t("connector:delete.description")}
          warning={t("connector:delete.batchWarning")}
          onDelete={(connectorId) => {
            const target = batchTargets.find((item) => item.id === connectorId);
            return remove.mutateAsync({
              path: { connectorId, wakerId },
              query: wakerId
                ? undefined
                : {
                    confirmed: true,
                    expectedAffectedCount: target?.affectedWakerCount,
                  },
            });
          }}
          onDeleted={async (ids) => {
            selection.remove(ids);
            await invalidateVisibleConnectorQueries(client);
          }}
          onOpenChange={(open) => {
            if (!open) setBatchTargets([]);
          }}
        />
      )}
      {target && (
        <ConnectorDeleteDialog
          open
          wakerId={wakerId}
          connectorId={target.id}
          name={target.name}
          affectedWakerCount={target.affectedWakerCount}
          onOpenChange={(open) => {
            if (!open) setTarget(null);
          }}
          onDeleted={() => {
            void invalidateVisibleConnectorQueries(client);
            setTarget(null);
          }}
        />
      )}
    </div>
  );
}
