import { useMutation, useQuery } from "@tanstack/react-query";
import { ChevronRight, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useEffect, useId, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { ResourceCatalogGroup } from "@/shared/ui/resource-catalog";
import { listWakerPlugins } from "@/shared/api/generated/sdk.gen";
import {
  useResourcePages,
  RESOURCE_BATCH_SIZE,
} from "@/shared/hooks/use-resource-pages";
import { useResourceWindow } from "@/shared/hooks/use-resource-window";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import {
  refreshPluginCatalogMutation,
  getPluginOptions,
  getCatalogPluginOptions,
  listWakerPluginsQueryKey,
} from "../api/plugin-queries";
import {
  pluginInstalledVersion,
  pluginNeedsAuthorization,
  pluginLocale,
} from "../model/plugin-view";
import { usePluginBrowsePages } from "../api/use-plugin-browse";
import {
  pluginBrowseCategory,
  pluginBrowseGroups,
  PLUGIN_CATEGORY_LABELS,
  type PluginBrowseCategory,
  type PluginBrowseEntry,
} from "../model/plugin-browse";
import { PluginCard } from "../components/plugin-card";
import {
  PluginUpdateDialog,
  PluginInstallDialog,
} from "../components/deferred-plugin-dialogs";

const GROUP_LIMIT = 8;
const CATEGORY_BATCH_SIZE = 24;

type ManageTarget = { pluginId: string; versionId?: string };
type LoadIntent = { title: string; preview?: PluginBrowseEntry } & (
  | { kind: "install"; catalogKey?: string; target?: ManageTarget }
  | { kind: "update"; target: ManageTarget }
);
type Props = {
  wakerId?: string;
  targetPartnerId?: string;
  category?: PluginBrowseCategory;
  categoryHref: (category: PluginBrowseCategory) => string;
  search: string;
  searchField?: ReactNode;
};
export function PluginResourceCollection({
  wakerId = "",
  targetPartnerId,
  category,
  categoryHref,
  search,
  searchField,
}: Props) {
  const { t } = useTranslation("plugin");
  const active = useRouteSceneActive();
  const groupId = useId();
  const [intent, setIntent] = useState<LoadIntent | null>(null);
  const refresh = useMutation(refreshPluginCatalogMutation());
  const browse = usePluginBrowsePages({
    search,
    active: active && !wakerId,
  });
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
  const categoryEntries = category
    ? browse.entries.filter(
        (entry) => pluginBrowseCategory(entry.category) === category,
      )
    : [];
  const categoryWindow = useResourceWindow(
    categoryEntries,
    JSON.stringify([category, search]),
    CATEGORY_BATCH_SIZE,
  );
  const itemCount = category
    ? categoryEntries.length
    : wakerId
      ? details.length
      : browse.entries.length;
  const nextPageError = query.isFetchNextPageError ? query.error : undefined;
  const seekingCategory = Boolean(
    category && !itemCount && browse.hasNextPage && !browse.isError,
  );
  const { fetchNextPage, isFetching } = browse;
  const loadedPages = browse.data?.pages.length;
  useEffect(() => {
    // 空的匹配批次没有可滚动的列表，继续读取直到找到该分类或确认已读完。
    if (active && seekingCategory && !isFetching) void fetchNextPage();
  }, [active, fetchNextPage, isFetching, loadedPages, seekingCategory]);
  const hasCachedCategoryItems = Boolean(category && categoryWindow.hasMore);
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
  const browseRows = (entries: PluginBrowseEntry[]) => (
    <ResourceCardGrid appearance="catalog">
      {entries.map((entry) => (
        <PluginCard
          key={entry.sourceKey}
          entry={entry}
          headingLevel={category ? 2 : 3}
          onInstall={() => manage(entry, "install")}
          onUpdate={() => manage(entry, "update")}
        />
      ))}
    </ResourceCardGrid>
  );
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
        {query.isPending || itemCount === 0 ? (
          <QueryState
            loadingLayout="rows"
            pending={
              query.isPending ||
              Boolean(category && (isFetching || seekingCategory))
            }
            error={query.error}
            emptyLabel={t(search ? "noResults" : "empty")}
            onRetry={() =>
              void (nextPageError ? query.fetchNextPage() : query.refetch())
            }
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
        ) : category ? (
          browseRows(categoryWindow.visible)
        ) : (
          pluginBrowseGroups(browse.entries).map((group) => {
            const title = t(PLUGIN_CATEGORY_LABELS[group.category]);
            const titleId = `${groupId}-${group.category}`;
            return (
              <ResourceCatalogGroup
                key={group.category}
                title={<span id={titleId}>{title}</span>}
                count={
                  browse.total === null || browse.hasNextPage
                    ? undefined
                    : group.entries.length
                }
                aria-labelledby={titleId}
                actions={
                  group.entries.length > GROUP_LIMIT && (
                    <Button asChild variant="catalog-ghost" size="catalog">
                      <Link
                        to={categoryHref(group.category)}
                        aria-label={t("categoryMoreNamed", { name: title })}
                      >
                        {t("categoryMore")}
                        <ChevronRight aria-hidden="true" />
                      </Link>
                    </Button>
                  )
                }
              >
                {browseRows(group.entries.slice(0, GROUP_LIMIT))}
              </ResourceCatalogGroup>
            );
          })
        )}
        {itemCount > 0 && query.isError && !nextPageError && (
          <QueryRefreshError
            error={query.error}
            pending={query.isFetching}
            onRetry={() => void query.refetch()}
          />
        )}
        {(!category || itemCount > 0) && (
          <ResourceInfiniteScroll
            hasMore={
              active &&
              (hasCachedCategoryItems ||
                (Boolean(query.hasNextPage) && !query.isRefetchError))
            }
            pending={!hasCachedCategoryItems && query.isFetching}
            error={hasCachedCategoryItems ? undefined : nextPageError}
            showLoadMoreButton={false}
            onLoadMore={() => {
              if (category) categoryWindow.loadMore();
              if (!hasCachedCategoryItems) void query.fetchNextPage();
            }}
          />
        )}
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
