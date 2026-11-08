import { useMutation, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useId, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { ResourceCatalogGroup } from "@/shared/ui/resource-catalog";
import { listWakerPlugins } from "@/shared/api/generated/sdk.gen";
import {
  useResourcePages,
  RESOURCE_BATCH_SIZE,
} from "@/shared/hooks/use-resource-pages";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  refreshPluginCatalogMutation,
  getPluginOptions,
  getCatalogPluginOptions,
  listWakerPluginsQueryKey,
  invalidatePluginQueries,
} from "../api/plugin-queries";
import {
  pluginInstalledVersion,
  pluginNeedsAuthorization,
  pluginLocale,
  type PluginCatalogEntry,
  type PluginDetail,
} from "../model/plugin-view";
import { usePluginBrowsePages } from "../api/use-plugin-browse";
import {
  pluginBrowseGroups,
  PLUGIN_CATEGORY_LABELS,
  type PluginBrowseEntry,
} from "../model/plugin-browse";
import { StatusNotice } from "@/shared/ui/status-notice";
import { PluginCard } from "../components/plugin-card";
import {
  PluginUpdateDialog,
  PluginInstallDialog,
} from "../components/deferred-plugin-dialogs";

type ManageTarget = { pluginId: string; versionId?: string };
type LoadIntent =
  | { kind: "install"; catalogKey?: string; target?: ManageTarget }
  | { kind: "update"; target: ManageTarget };
type LoadResult =
  | {
      kind: "install";
      catalog?: PluginCatalogEntry;
      detail?: PluginDetail;
      authorizeOnly?: boolean;
    }
  | { kind: "update"; detail: PluginDetail };
type Props = {
  wakerId?: string;
  targetPartnerId?: string;
  search: string;
  searchField?: ReactNode;
};
export function PluginResourceCollection({
  wakerId = "",
  targetPartnerId,
  search,
  searchField,
}: Props) {
  const { t, i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const groupId = useId();
  const [updating, setUpdating] = useState<PluginDetail | null>(null);
  const [installation, setInstallation] = useState<{
    catalog?: PluginCatalogEntry;
    detail?: PluginDetail;
    authorizeOnly?: boolean;
  } | null>(null);
  const refresh = useMutation({
    ...withLocalErrorMutation(refreshPluginCatalogMutation()),
    onSettled: () => invalidatePluginQueries(client),
  });
  const browse = usePluginBrowsePages({ search, active: active && !wakerId });
  const installedKey = listWakerPluginsQueryKey(
    withLocalErrorSurface({
      path: { wakerId },
      query: { query: search, pageSize: RESOURCE_BATCH_SIZE },
    }),
  );
  const installed = useResourcePages({
    queryKey: installedKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listWakerPlugins(
        withLocalErrorSurface({
          path: { wakerId },
          query: { query: search, pageNum, pageSize: RESOURCE_BATCH_SIZE },
          signal,
          throwOnError: true as const,
        }),
      );
      return {
        items: data.resources ?? [],
        total: data.totalSize ?? 0,
        metadata: data.catalogSource,
      };
    },
    enabled: active && Boolean(wakerId),
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [[...installedKey, "infinite"]],
    active,
    enabled: Boolean(wakerId),
  });
  const query = wakerId ? installed : browse;
  const details = installed.items.filter((detail) =>
    Boolean(detail.plugin?.id),
  );
  const source = wakerId ? installed.metadata : browse.source;
  const itemCount = wakerId ? details.length : browse.entries.length;
  const groups = pluginBrowseGroups(browse.entries);
  const load = useMutation({
    mutationFn: async (intent: LoadIntent): Promise<LoadResult> => {
      if (intent.kind === "install" && intent.catalogKey) {
        const catalog = await client.fetchQuery({
          ...getCatalogPluginOptions(
            withLocalErrorSurface({
              path: { key: intent.catalogKey },
              query: { locale },
            }),
          ),
          staleTime: 0,
        });
        return { kind: "install", catalog };
      }
      if (!intent.target) throw new Error(t("failed"));
      const detail = await client.fetchQuery({
        ...getPluginOptions(
          withLocalErrorSurface({
            path: { pluginId: intent.target.pluginId },
            query: { locale, versionId: intent.target.versionId },
          }),
        ),
        staleTime: 0,
      });
      return intent.kind === "install"
        ? {
            kind: "install",
            detail,
            authorizeOnly: pluginNeedsAuthorization(
              pluginInstalledVersion(detail)?.package,
              detail,
            ),
          }
        : { kind: "update", detail };
    },
    onSuccess: (result) => {
      if (result.kind === "install") setInstallation(result);
      else setUpdating(result.detail);
    },
  });
  const manage = (entry: PluginBrowseEntry, kind: "install" | "update") => {
    if (kind === "install" && !(entry.installedCount ?? 0) && entry.catalogKey)
      load.mutate({ kind, catalogKey: entry.catalogKey });
    else if (entry.pluginId)
      load.mutate({
        kind,
        target: { pluginId: entry.pluginId, versionId: entry.versionId },
      });
  };
  return (
    <div className="grid min-w-0 gap-8">
      <div className="flex min-w-0 items-center gap-2">
        {searchField}
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="catalog-ghost"
                size="catalog-icon"
                aria-label={t(
                  refresh.isPending ? "checkingUpdates" : "checkUpdates",
                )}
                aria-busy={refresh.isPending}
                disabled={refresh.isPending}
                onClick={() => refresh.mutate({})}
              >
                <RefreshCw
                  aria-hidden="true"
                  className={
                    refresh.isPending ? "motion-safe:animate-spin" : undefined
                  }
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("checkUpdates")}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
      <div className="grid min-w-0 gap-6">
        {source?.state === "stale" && (
          <StatusNotice>{t("catalogStale")}</StatusNotice>
        )}
        {source &&
          source.state !== "stale" &&
          (source.errorCode || source.state === "unavailable") && (
            <StatusNotice>{t("catalogUnavailable")}</StatusNotice>
          )}
        {refresh.error && (
          <p role="alert" className="type-resource-body text-destructive">
            {localApiErrorMessage(refresh.error, t("catalogUnavailable"))}
          </p>
        )}
        {load.isPending && (
          <p
            role="status"
            aria-label={t("busy")}
            className="type-resource-body text-resource-secondary"
          >
            {t("busy")}
          </p>
        )}
        {load.error && (
          <p role="alert" className="type-resource-body text-destructive">
            {localApiErrorMessage(load.error, t("failed"))}
          </p>
        )}
        {query.isPending || itemCount === 0 ? (
          <QueryState
            loadingLayout="rows"
            pending={query.isPending}
            error={query.error}
            emptyLabel={t(search ? "noResults" : "empty")}
            onRetry={() => void query.refetch()}
          />
        ) : wakerId ? (
          <ResourceCardGrid appearance="catalog">
            {details.map((detail) => (
              <PluginCard
                key={detail.plugin?.id}
                detail={detail}
                wakerId={wakerId}
              />
            ))}
          </ResourceCardGrid>
        ) : (
          groups.map((group) => (
            <ResourceCatalogGroup
              key={group.category}
              title={
                <span id={`${groupId}-${group.category}`}>
                  {t(PLUGIN_CATEGORY_LABELS[group.category])}
                </span>
              }
              count={
                browse.total === null || browse.hasNextPage
                  ? undefined
                  : group.entries.length
              }
              aria-labelledby={`${groupId}-${group.category}`}
            >
              <ResourceCardGrid appearance="catalog">
                {group.entries.map((entry) => (
                  <PluginCard
                    key={entry.sourceKey}
                    entry={entry}
                    pending={load.isPending}
                    onInstall={() => manage(entry, "install")}
                    onUpdate={() => manage(entry, "update")}
                  />
                ))}
              </ResourceCardGrid>
            </ResourceCatalogGroup>
          ))
        )}
        {itemCount > 0 && query.isError && !query.isFetchNextPageError && (
          <QueryRefreshError
            error={query.error}
            pending={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        )}
        <ResourceInfiniteScroll
          hasMore={
            active && Boolean(query.hasNextPage) && !query.isRefetchError
          }
          pending={query.isFetching}
          error={query.isFetchNextPageError ? query.error : undefined}
          showLoadMoreButton={false}
          onLoadMore={() => void query.fetchNextPage()}
        />
      </div>
      {updating && (
        <PluginUpdateDialog
          detail={updating}
          onClose={() => setUpdating(null)}
        />
      )}
      {installation && (
        <PluginInstallDialog
          {...installation}
          wakerId={targetPartnerId ?? wakerId}
          onClose={() => setInstallation(null)}
        />
      )}
    </div>
  );
}
