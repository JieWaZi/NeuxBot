import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Info, RefreshCw } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import {
  ResourceCardGrid,
  ResourcePageToolbar,
  ResourcePageLibraryLabel,
} from "@/shared/ui/resource-page";
import {
  listPluginCatalog,
  listPlugins,
  listWakerPlugins,
} from "@/shared/api/generated/sdk.gen";
import {
  useResourcePages,
  RESOURCE_BATCH_SIZE,
} from "@/shared/hooks/use-resource-pages";
import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { ResourceBatchDeleteDialog } from "@/shared/ui/resource-batch-delete-dialog";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  refreshPluginCatalogMutation,
  listPluginCatalogQueryKey,
  listPluginsQueryKey,
  listWakerPluginsQueryKey,
  setWakerPluginEnabledMutation,
  invalidatePluginQueries,
  deletePluginMutation,
  uninstallWakerPluginMutation,
} from "../api/plugin-queries";
import {
  pluginInstalledVersion,
  pluginNeedsAuthorization,
  pluginDisplayName,
  type PluginCatalogEntry,
  type PluginDetail,
} from "../model/plugin-view";
import { usePluginResources } from "../api/use-plugin-resources";
import { StatusNotice } from "@/shared/ui/status-notice";
import { PluginRemovalDialog } from "../components/plugin-removal-dialog";
import { PluginIcon } from "../components/plugin-icon";
import { PluginCard } from "../components/plugin-card";
import { PluginUpdateDialog } from "../components/deferred-plugin-dialogs";
import { PluginInstallDialog } from "../components/deferred-plugin-dialogs";

type Props = {
  wakerId?: string;
  targetPartnerId?: string;
  search: string;
  view: "catalog" | "mine";
  onViewChange: (view: "catalog" | "mine") => void;
  searchField?: ReactNode;
};
export function PluginResourceCollection({
  wakerId = "",
  targetPartnerId,
  search,
  view,
  onViewChange,
  searchField,
}: Props) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const [batchTargets, setBatchTargets] = useState<
    (PluginDetail & { id: string })[]
  >([]);
  const remove = useMutation(withLocalErrorMutation(deletePluginMutation()));
  const uninstall = useMutation(
    withLocalErrorMutation(uninstallWakerPluginMutation()),
  );
  const [updating, setUpdating] = useState<PluginDetail | null>(null);
  const refresh = useMutation({
    ...withLocalErrorMutation(refreshPluginCatalogMutation()),
    onSettled: () => invalidatePluginQueries(client),
  });
  const [removing, setRemoving] = useState<PluginDetail | null>(null);
  const [installation, setInstallation] = useState<{
    catalog?: PluginCatalogEntry;
    detail?: PluginDetail;
    authorizeOnly?: boolean;
  } | null>(null);
  const catalogKey = listPluginCatalogQueryKey(
    withLocalErrorSurface({
      query: { query: search, pageSize: RESOURCE_BATCH_SIZE },
    }),
  );
  const libraryKey = listPluginsQueryKey(
    withLocalErrorSurface({
      query: {
        query: search,
        pageSize: RESOURCE_BATCH_SIZE,
        installedOnly: true,
      },
    }),
  );
  const installedKey = listWakerPluginsQueryKey(
    withLocalErrorSurface({
      path: { wakerId },
      query: { query: search, pageSize: RESOURCE_BATCH_SIZE },
    }),
  );
  const catalogResources = usePluginResources(
    undefined,
    active && !wakerId && view === "catalog",
  );
  const catalog = useResourcePages({
    queryKey: catalogKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listPluginCatalog(
        withLocalErrorSurface({
          query: { query: search, pageNum, pageSize: RESOURCE_BATCH_SIZE },
          signal,
          throwOnError: true as const,
        }),
      );
      return {
        items: data.entries ?? [],
        total: data.totalSize ?? 0,
        metadata: data.source,
      };
    },
    enabled: active && !wakerId && view === "catalog",
  });
  const resources = useResourcePages({
    queryKey: libraryKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listPlugins(
        withLocalErrorSurface({
          query: {
            query: search,
            pageNum,
            pageSize: RESOURCE_BATCH_SIZE,
            installedOnly: true,
          },
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
    enabled: active && !wakerId && view === "mine",
  });
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
    queryKeys: [
      [...catalogKey, "infinite"],
      [...libraryKey, "infinite"],
      [...installedKey, "infinite"],
    ],
    active,
  });
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerPluginEnabledMutation()),
    onSuccess: () => invalidatePluginQueries(client),
  });
  const query = wakerId ? installed : view === "catalog" ? catalog : resources;
  const entries = catalog.items;
  const details = (wakerId ? installed.items : resources.items).flatMap(
    (item) => (item.plugin?.id ? [{ ...item, id: item.plugin.id }] : []),
  );
  const total = query.total;
  const source = query.metadata;
  const selection = useResourceSelection(
    details,
    JSON.stringify([wakerId, search, view]),
  );
  return (
    <div className="space-y-5">
      <ResourcePageToolbar
        filters={
          <>
            <PluginCatalogUpdateInfo
              checkedAt={source?.fetchedAt}
              pending={refresh.isPending}
              onRefresh={() => refresh.mutate({})}
            />
            {searchField}
          </>
        }
      >
        {wakerId ? (
          <ResourcePageLibraryLabel
            installed={t("allPluginsTitle")}
            count={t("resourceCount", { count: total })}
          />
        ) : (
          <>
            <SegmentedControl
              appearance="underline"
              ariaLabel={t("plugins")}
              value={view}
              options={[
                { value: "catalog", label: t("catalog") },
                { value: "mine", label: t("mine") },
              ]}
              onValueChange={onViewChange}
            />
            <span className="type-caption text-content-secondary whitespace-nowrap">
              {t("resourceCount", { count: total })}
            </span>
          </>
        )}
        {(wakerId || view === "mine") && (
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        )}
      </ResourcePageToolbar>
      {source?.state === "stale" && (
        <StatusNotice>{t("catalogStale")}</StatusNotice>
      )}
      {source &&
        source.state !== "stale" &&
        (source.errorCode || source.state === "unavailable") && (
          <StatusNotice>{t("catalogUnavailable")}</StatusNotice>
        )}
      {refresh.error && (
        <p role="alert" className="type-body text-destructive">
          {localApiErrorMessage(refresh.error, t("catalogUnavailable"))}
        </p>
      )}
      <ResourceSelectionActions
        count={selection.selected.length}
        onClear={selection.clear}
        onDelete={() => setBatchTargets(selection.selected)}
      />
      {query.isPending || (query.isError && total === 0) || total === 0 ? (
        <QueryState
          loadingLayout="cards"
          pending={query.isPending}
          error={query.error}
          emptyLabel={t(search ? "noResults" : "empty")}
          onRetry={() => void query.refetch()}
        />
      ) : (
        <ResourceCardGrid>
          {!wakerId && view === "catalog"
            ? entries.map((entry) => {
                const detail = catalogResources.data?.find(
                  (item) =>
                    item.plugin?.sourceKey ===
                    (entry.sourceKey ?? `catalog:${entry.key}`),
                );
                return (
                  <PluginCard
                    key={entry.key}
                    catalog={entry}
                    detail={detail}
                    pending={
                      catalogResources.isPending || catalogResources.isError
                    }
                    onInstall={() =>
                      setInstallation({
                        catalog: entry,
                        detail,
                        authorizeOnly: pluginNeedsAuthorization(
                          entry.package,
                          detail,
                        ),
                      })
                    }
                  />
                );
              })
            : details.map((detail) => (
                <PluginCard
                  key={detail.plugin?.id}
                  detail={detail}
                  selected={selection.selectedIds.has(detail.id)}
                  onSelectedChange={(selected) =>
                    selection.setSelected(detail.id, selected)
                  }
                  wakerId={wakerId}
                  pending={toggle.isPending}
                  onInstall={() =>
                    setInstallation({
                      detail,
                      authorizeOnly: pluginNeedsAuthorization(
                        pluginInstalledVersion(detail, wakerId)?.package,
                        detail,
                      ),
                    })
                  }
                  onUpdate={() => setUpdating(detail)}
                  onRemove={() => setRemoving(detail)}
                  onToggle={
                    wakerId
                      ? (enabled) =>
                          toggle.mutate({
                            path: {
                              wakerId,
                              pluginId: detail.plugin?.id ?? "",
                            },
                            body: { enabled },
                          })
                      : undefined
                  }
                />
              ))}
        </ResourceCardGrid>
      )}
      {total > 0 && query.isError && !query.isFetchNextPageError && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
      {!wakerId && view === "catalog" && catalogResources.isError && (
        <QueryState
          error={catalogResources.error}
          onRetry={() => void catalogResources.refetch()}
        />
      )}
      {toggle.error && (
        <p role="alert" className="type-body text-destructive">
          {localApiErrorMessage(toggle.error, t("failed"))}
        </p>
      )}
      <ResourceInfiniteScroll
        hasMore={active && Boolean(query.hasNextPage) && !query.isRefetchError}
        pending={query.isFetching}
        error={query.isFetchNextPageError ? query.error : undefined}
        onLoadMore={() => void query.fetchNextPage()}
      />
      {batchTargets.length > 0 && (
        <ResourceBatchDeleteDialog
          key={batchTargets.map((item) => item.id).join(":")}
          items={batchTargets.map((detail) => ({
            id: detail.id,
            name: pluginDisplayName(
              pluginInstalledVersion(detail, wakerId)?.package,
              detail.plugin?.name,
            ),
            icon: (
              <PluginIcon
                pkg={pluginInstalledVersion(detail, wakerId)?.package}
              />
            ),
            description: t("partnerUsage", {
              count: detail.installations?.length ?? 0,
            }),
          }))}
          title={t(wakerId ? "batchUninstallTitle" : "batchDeleteTitle")}
          warning={
            wakerId
              ? t("uninstallHelp")
              : t("deleteHelp", {
                  count: new Set(
                    batchTargets.flatMap((item) =>
                      (item.installations ?? []).map(
                        (relation) => relation.wakerId,
                      ),
                    ),
                  ).size,
                })
          }
          onDelete={(pluginId) => {
            if (wakerId)
              return uninstall.mutateAsync({ path: { pluginId, wakerId } });
            const detail = batchTargets.find((item) => item.id === pluginId);
            return remove.mutateAsync({
              path: { pluginId },
              body: {
                confirmed: true,
                installations: (detail?.installations ?? []).map((item) => ({
                  wakerId: item.wakerId ?? "",
                  expectedVersionId: item.versionId ?? "",
                  versionId: item.versionId ?? "",
                })),
              },
            });
          }}
          onDeleted={async (ids) => {
            selection.remove(ids);
            await invalidatePluginQueries(client);
          }}
          onOpenChange={(open) => {
            if (!open) setBatchTargets([]);
          }}
        />
      )}
      {removing && (
        <PluginRemovalDialog
          detail={removing}
          mode={wakerId ? "installation" : "resource"}
          wakerId={wakerId}
          onClose={() => setRemoving(null)}
        />
      )}
      {updating && (
        <PluginUpdateDialog
          detail={updating}
          wakerId={wakerId}
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

/** 检查信息与手动刷新共用可交互浮层，悬停不抢占当前输入焦点。 */
function PluginCatalogUpdateInfo({
  checkedAt,
  pending,
  onRefresh,
}: {
  checkedAt?: number;
  pending: boolean;
  onRefresh: () => void;
}) {
  const { t } = useTranslation("plugin");
  const [open, setOpen] = useState(false);
  const content = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const focusOnOpen = useRef(false);
  const restoreOnClose = useRef(false);
  const suppressFocusOpen = useRef(false);
  function cancelClose() {
    clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }
  useEffect(
    () => () => {
      clearTimeout(closeTimer.current);
    },
    [],
  );
  function show() {
    cancelClose();
    setOpen(true);
  }
  function scheduleClose() {
    cancelClose();
    // 留出从图标跨过浮层间隔的时间；键盘正在操作浮层时保持展开。
    closeTimer.current = setTimeout(() => {
      closeTimer.current = undefined;
      if (
        document.activeElement === trigger.current ||
        content.current?.contains(document.activeElement)
      )
        return;
      setOpen(false);
    }, 120);
  }
  function enterContent() {
    focusOnOpen.current = true;
    show();
    content.current
      ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
      ?.focus();
  }
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        cancelClose();
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="plain"
          size="icon"
          aria-label={t("catalogUpdateInfo")}
          onPointerEnter={(event) => {
            trigger.current = event.currentTarget;
            focusOnOpen.current = false;
            show();
          }}
          onPointerLeave={scheduleClose}
          onFocus={(event) => {
            trigger.current = event.currentTarget;
            if (suppressFocusOpen.current) {
              suppressFocusOpen.current = false;
              return;
            }
            focusOnOpen.current = false;
            show();
          }}
          onBlur={scheduleClose}
          onClick={(event) => {
            event.preventDefault();
            trigger.current = event.currentTarget;
            enterContent();
          }}
          onKeyDown={(event) => {
            if (["ArrowDown", "Enter", " "].includes(event.key)) {
              event.preventDefault();
              enterContent();
            }
          }}
        >
          <Info aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        ref={content}
        width="default"
        side="top"
        align="end"
        aria-label={t("catalogUpdateInfo")}
        onPointerEnter={cancelClose}
        onPointerLeave={scheduleClose}
        onFocusCapture={cancelClose}
        onBlur={scheduleClose}
        onOpenAutoFocus={(event) => {
          if (!focusOnOpen.current) event.preventDefault();
          focusOnOpen.current = false;
        }}
        onEscapeKeyDown={() => {
          restoreOnClose.current = true;
          cancelClose();
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          if (restoreOnClose.current) {
            restoreOnClose.current = false;
            suppressFocusOpen.current =
              document.activeElement !== trigger.current;
            trigger.current?.focus();
          }
        }}
      >
        <div className="flex items-center gap-2">
          <p className="type-ui text-content-secondary min-w-0 flex-1">
            {checkedAt
              ? t("catalogCheckedAt", {
                  time: new Date(checkedAt).toLocaleString(),
                })
              : t("catalogUnchecked")}
          </p>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={t(pending ? "checkingUpdates" : "checkUpdates")}
            disabled={pending}
            onClick={onRefresh}
          >
            <RefreshCw
              aria-hidden="true"
              className={pending ? "motion-safe:animate-spin" : undefined}
            />
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
