import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
import { Dialog, DialogContent } from "@/shared/ui/dialog";
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
type LoadIntent = { title: string; preview?: PluginBrowseEntry } & (
  | { kind: "install"; catalogKey?: string; target?: ManageTarget }
  | { kind: "update"; target: ManageTarget }
);
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
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const groupId = useId();
  const [intent, setIntent] = useState<LoadIntent | null>(null);
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
  const manage = (entry: PluginBrowseEntry, kind: "install" | "update") => {
    const title = t(
      kind === "update"
        ? "updateNamed"
        : entry.requiresAuthorization
          ? "authorizeNamed"
          : "installNamed",
      { name: entry.name ?? "" },
    );
    if (kind === "install" && !(entry.installedCount ?? 0) && entry.catalogKey)
      setIntent({ kind, catalogKey: entry.catalogKey, title, preview: entry });
    else if (entry.pluginId)
      setIntent({
        kind,
        title,
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
      {intent && (
        <PluginResourceActionDialog
          intent={intent}
          wakerId={targetPartnerId ?? wakerId}
          onClose={() => setIntent(null)}
        />
      )}
    </div>
  );
}

/** 操作先挂载弹窗，再在弹窗生命周期内读取所需资料；关闭会释放读取且不会被迟到响应重开。 */
function PluginResourceActionDialog({
  intent,
  wakerId,
  onClose,
}: {
  intent: LoadIntent;
  wakerId: string;
  onClose: () => void;
}) {
  const { i18n } = useTranslation("plugin");
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const catalogKey = intent.kind === "install" ? intent.catalogKey : undefined;
  const catalog = useQuery({
    ...getCatalogPluginOptions(
      withLocalErrorSurface({
        path: { key: catalogKey ?? "" },
        query: { locale },
      }),
    ),
    enabled: Boolean(catalogKey),
    staleTime: 0,
    retry: false,
  });
  const detail = useQuery({
    ...getPluginOptions(
      withLocalErrorSurface({
        path: { pluginId: intent.target?.pluginId ?? "" },
        query: { locale, versionId: intent.target?.versionId },
      }),
    ),
    enabled: Boolean(intent.target),
    staleTime: 0,
  });
  if (catalogKey)
    return (
      <PluginInstallDialog
        catalog={catalog.data}
        preview={intent.preview}
        catalogRead={{
          pending: catalog.isPending || catalog.isFetching,
          error: catalog.error,
          onRetry: () => void catalog.refetch(),
        }}
        wakerId={wakerId}
        onClose={onClose}
      />
    );
  if (!catalogKey && detail.data) {
    return intent.kind === "update" ? (
      <PluginUpdateDialog detail={detail.data} onClose={onClose} />
    ) : (
      <PluginInstallDialog
        detail={detail.data}
        authorizeOnly={pluginNeedsAuthorization(
          pluginInstalledVersion(detail.data)?.package,
          detail.data,
        )}
        wakerId={wakerId}
        onClose={onClose}
      />
    );
  }
  const query = catalogKey ? catalog : detail;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent title={intent.title}>
        <div className="px-6 pb-5">
          <QueryState
            loadingLayout="content"
            pending={query.isPending}
            error={query.error}
            onRetry={() => void query.refetch()}
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}
