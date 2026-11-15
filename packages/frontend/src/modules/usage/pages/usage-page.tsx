import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useVisibleScene } from "@/shared/api/chat-reading";
import { ApiError } from "@/shared/api/errors";
import type { ListUsageGroupsResponse } from "@/shared/api/generated/types.gen";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import { PageHeader } from "@/shared/ui/page-header";
import { QueryRefreshError } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { usageGroupsOptions, usageSummaryOptions } from "../api/usage-queries";
import { UsageProviderBreakdown } from "../components/usage-provider-breakdown";
import { UsageOverview, UsageReport } from "../components/usage-report";
import { UsageTrend } from "../components/usage-trend";
import {
  UsageProviderFilter,
  UsageRefresh,
  UsageSourceFilter,
} from "../components/usage-toolbar";
import {
  parseUsageFilters,
  rangeFromUsageDates,
  usageDate,
  usageFiltersSearch,
  type UsageFilters,
} from "../model/usage-filters";

function nextCursor(page: ListUsageGroupsResponse) {
  return page.hasMore ? page.nextCursor : undefined;
}

function retryGroups(attempt: number, error: unknown) {
  return !(error instanceof ApiError && error.status === 409) && attempt < 2;
}

const groupPagination = {
  initialPageParam: "",
  getNextPageParam: nextCursor,
  retry: retryGroups,
};

function expiredSnapshot(error: unknown) {
  return error instanceof ApiError && error.status === 409;
}

type GroupQuery = {
  data?: { pages: ListUsageGroupsResponse[] };
  isError: boolean;
  isFetching: boolean;
  hasNextPage: boolean;
  fetchNextPage: () => Promise<unknown>;
};

/** 替换快照沿自己的游标补齐已经可见的深度，不复用旧快照游标。 */
function useSnapshotDepth(
  query: GroupQuery,
  depth: number,
  replacing: boolean,
) {
  const { fetchNextPage } = query;
  const loaded = query.data?.pages.length ?? 0;
  const needsMore =
    replacing &&
    loaded > 0 &&
    loaded < depth &&
    query.hasNextPage &&
    !query.isFetching &&
    !query.isError;
  useEffect(() => {
    if (needsMore) void fetchNextPage();
  }, [needsMore, loaded, fetchNextPage]);
  return (
    loaded > 0 && !query.isError && (loaded >= depth || !query.hasNextPage)
  );
}

export function UsagePage({
  search,
  onSearchChange,
  wakerOptions = [],
}: {
  search: URLSearchParams;
  onSearchChange: (search: URLSearchParams) => void;
  wakerOptions?: readonly { value: string; label: string }[];
}) {
  const { t } = useTranslation("usage");
  const active = useVisibleScene();
  const filters = parseUsageFilters(search);
  const runtimes = useQuery({
    ...listRuntimesOptions(),
    enabled: active,
  });
  const options = usageSummaryOptions(filters);
  const summary = useQuery({ ...options, enabled: active });
  const { refetch: refreshSummary } = summary;
  const snapshot = summary.data?.snapshot ?? "";
  const scope = JSON.stringify([
    filters.from,
    filters.to,
    filters.timezone,
    filters.wakerIds,
    filters.provider,
    filters.source,
  ]);
  const [published, setPublished] = useState<{
    scope: string;
    snapshot: string;
    groupBy: UsageFilters["groupBy"];
  }>();
  const previous = published?.scope === scope ? published : undefined;
  const sameGroup = previous?.groupBy === filters.groupBy;
  const dayInReport = filters.groupBy === "day";
  const nextHasUsage = Boolean(
    summary.data && summary.data.metrics?.invocations !== 0,
  );
  const groups = useInfiniteQuery({
    ...usageGroupsOptions(filters, snapshot),
    enabled: active && Boolean(snapshot),
    ...groupPagination,
  });
  const retained = useInfiniteQuery({
    ...usageGroupsOptions(
      { ...filters, groupBy: previous?.groupBy ?? filters.groupBy },
      previous?.snapshot ?? "",
    ),
    enabled: false,
    ...groupPagination,
  });
  // 候选快照的各区并行读取，不能等明细发布后才让图表和占比重新加载。
  const trend = useInfiniteQuery({
    ...usageGroupsOptions({ ...filters, groupBy: "day" }, snapshot),
    ...groupPagination,
    enabled: active && !dayInReport && Boolean(snapshot) && nextHasUsage,
  });
  const retainedDays = useInfiniteQuery({
    ...usageGroupsOptions(
      { ...filters, groupBy: "day" },
      previous?.snapshot ?? "",
    ),
    ...groupPagination,
    enabled: false,
  });
  const providers = useInfiniteQuery({
    ...usageGroupsOptions({ ...filters, groupBy: "provider" }, snapshot),
    ...groupPagination,
    enabled: active && !filters.provider && Boolean(snapshot) && nextHasUsage,
  });
  const retainedProviders = useInfiniteQuery({
    ...usageGroupsOptions(
      { ...filters, groupBy: "provider" },
      previous?.snapshot ?? "",
    ),
    ...groupPagination,
    enabled: false,
  });
  const replacing = Boolean(
    previous && previous.snapshot !== snapshot && retained.data,
  );
  const groupsReady = useSnapshotDepth(
    groups,
    replacing && sameGroup ? (retained.data?.pages.length ?? 1) : 1,
    active && replacing,
  );
  const daysReady = useSnapshotDepth(
    trend,
    replacing ? (retainedDays.data?.pages.length ?? 1) : 1,
    active && replacing && !dayInReport && nextHasUsage,
  );
  const providersReady = useSnapshotDepth(
    providers,
    replacing ? (retainedProviders.data?.pages.length ?? 1) : 1,
    active && replacing && !filters.provider && nextHasUsage,
  );
  const ready =
    groupsReady &&
    (!nextHasUsage || dayInReport || daysReady) &&
    (!nextHasUsage || Boolean(filters.provider) || providersReady);

  // 首屏已显示的明细先固定身份；后续替换要等可见分组一起就绪。
  // 这里只保存 Query 身份，数据继续由各快照的 Query Cache 拥有。
  if (
    snapshot &&
    groupsReady &&
    (!replacing || ready) &&
    (previous?.snapshot !== snapshot || !sameGroup)
  ) {
    setPublished({ scope, snapshot, groupBy: filters.groupBy });
  }
  const showRetained = replacing && !ready;
  const displayGroups = showRetained && sameGroup ? retained : groups;
  const displayGroupsData =
    showRetained && !sameGroup ? undefined : displayGroups.data;
  const displaySummary = showRetained ? retained.data?.pages[0] : summary.data;
  const daily = showRetained ? retainedDays : dayInReport ? groups : trend;
  const displayProviders = showRetained ? retainedProviders : providers;
  const hasUsage = Boolean(
    displaySummary && displaySummary.metrics?.invocations !== 0,
  );
  const replacementFailed =
    groups.isError ||
    (!dayInReport && trend.isError) ||
    (!filters.provider && providers.isError);
  const refreshing =
    summary.isFetching || (active && replacing && !ready && !replacementFailed);

  useQueryEvents({
    topic: "usage",
    resource: { kind: "global" },
    eventTypes: ["usage.changed"],
    queryKeys: [options.queryKey],
    active,
  });
  const retried = useRef("");
  const snapshotExpired = [
    groups.error,
    !dayInReport && trend.error,
    !filters.provider && providers.error,
  ].some(expiredSnapshot);
  useEffect(() => {
    const identity = JSON.stringify([scope, snapshot]);
    if (active && snapshotExpired && snapshot && retried.current !== identity) {
      retried.current = identity;
      void refreshSummary();
    }
  }, [active, snapshotExpired, scope, snapshot, refreshSummary]);

  const fromDate = usageDate(filters.from, filters.timezone);
  const toDate = usageDate(filters.to - 1, filters.timezone);
  const update = (change: Partial<UsageFilters>) =>
    onSearchChange(usageFiltersSearch({ ...filters, ...change }));
  const selectRange = (from: string, to: string) => {
    if (!from || !to) {
      const recent = parseUsageFilters(
        new URLSearchParams({ timezone: filters.timezone }),
      );
      update({ from: recent.from, to: recent.to });
      return;
    }
    const range = rangeFromUsageDates(from, to, filters.timezone);
    if (range) update(range);
  };
  const names = new Map(wakerOptions.map(({ value, label }) => [value, label]));
  const confirmedEmpty =
    displaySummary?.metrics?.invocations === 0 &&
    !displaySummary.coverage?.reasons?.includes("native_projection_incomplete");
  const providerRows =
    displayProviders.data?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const providerIds = [
    ...(runtimes.data?.resources?.flatMap(({ id }) => (id ? [id] : [])) ?? []),
    ...providerRows.flatMap(({ key }) => (key ? [key] : [])),
  ];
  const rows =
    displayGroupsData?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const dailyRows =
    daily.data?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const breakdownPending =
    !displayGroupsData &&
    (displayGroups.isPending ||
      (showRetained && !sameGroup) ||
      (snapshotExpired && summary.isFetching));
  const providerPending =
    summary.isPending ||
    (!filters.provider && hasUsage && displayProviders.isPending);
  const trendError = trend.error ?? daily.error;
  const providerError = providers.error ?? displayProviders.error;

  return (
    <ResourcePageSurface>
      <div className="min-w-0 space-y-6">
        <PageHeader
          title={t("title")}
          layout="form"
          className="items-center"
          actions={
            <>
              <UsageProviderFilter
                providers={providerIds}
                value={filters.provider}
                onChange={(provider) => update({ provider })}
              />
              <UsageSourceFilter
                value={filters.source}
                onChange={(source) => update({ source })}
              />
              <DateRangePicker
                from={fromDate}
                to={toDate}
                labels={{
                  clear: t("clear"),
                  empty: t("recent"),
                  help: t("dateHelp"),
                  title: t("dateRange"),
                  trigger: t("dateRange"),
                }}
                onChange={selectRange}
              />
              {wakerOptions.length ? (
                <div className="min-w-0">
                  <SearchableMultiSelect
                    options={wakerOptions}
                    value={filters.wakerIds}
                    onValueChange={(wakerIds) =>
                      update({ wakerIds: wakerIds.slice(0, 100) })
                    }
                    triggerLabel={
                      filters.wakerIds.length
                        ? t("selectedWakers", {
                            count: filters.wakerIds.length,
                          })
                        : t("wakers")
                    }
                    searchLabel={t("searchWakers")}
                    emptyLabel={t("noWakers")}
                    clearSearchLabel={t("clear")}
                    clearSelectionLabel={t("clear")}
                  />
                </div>
              ) : null}
              <UsageRefresh
                pending={refreshing}
                onRefresh={() => void refreshSummary()}
              />
            </>
          }
        />
        <UsageOverview
          summary={displaySummary}
          provider={filters.provider}
          pending={summary.isPending}
        />
        {!displaySummary && summary.isError ? (
          <QueryRefreshError
            error={summary.error}
            pending={summary.isFetching}
            onRetry={() => void refreshSummary()}
          />
        ) : null}
        <div className="min-w-0 space-y-3">
          <UsageTrend
            groups={hasUsage ? dailyRows : []}
            from={fromDate}
            to={toDate}
            hasMore={hasUsage && daily.hasNextPage}
            pending={summary.isPending || (hasUsage && daily.isPending)}
            projectionPartial={
              !displaySummary ||
              (hasUsage && !dailyRows.length) ||
              displaySummary.coverage?.reasons?.includes(
                "native_projection_incomplete",
              )
            }
          />
          {hasUsage && daily.hasNextPage ? (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-content-secondary type-caption">
                {t("partialTrend")}
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={daily.isFetchingNextPage}
                onClick={() => void daily.fetchNextPage()}
              >
                {t("loadMoreDays")}
              </Button>
            </div>
          ) : null}
          {hasUsage && !dayInReport && (trend.isError || daily.isError) ? (
            <QueryRefreshError
              error={trendError}
              pending={trend.isFetching || summary.isFetching}
              onRetry={() =>
                void (expiredSnapshot(trendError)
                  ? refreshSummary()
                  : trend.refetch())
              }
            />
          ) : null}
        </div>
        <section
          hidden={Boolean(filters.provider)}
          className="min-w-0 space-y-4"
        >
          <h2 className="type-title font-semibold">{t("providers")}</h2>
          <UsageProviderBreakdown
            groups={providerRows}
            pending={Boolean(filters.provider) || providerPending}
            emptyLabel={
              displaySummary && !providerPending && !providerError
                ? t("noDailyUsage")
                : undefined
            }
            total={
              confirmedEmpty
                ? 0
                : (displaySummary?.metrics?.totalWithCacheRead ??
                  displaySummary?.metrics?.knownTotalWithCacheRead)
            }
          />
          {!filters.provider && displayProviders.hasNextPage ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={displayProviders.isFetchingNextPage}
              onClick={() => void displayProviders.fetchNextPage()}
            >
              {t("loadMoreProviders")}
            </Button>
          ) : null}
          {!filters.provider &&
          (providers.isError || displayProviders.isError) ? (
            <QueryRefreshError
              error={providerError}
              pending={providers.isFetching || summary.isFetching}
              onRetry={() =>
                void (expiredSnapshot(providerError)
                  ? refreshSummary()
                  : providers.refetch())
              }
            />
          ) : null}
        </section>
        {summary.data && summary.isError ? (
          <QueryRefreshError
            error={summary.error}
            pending={summary.isFetching}
            onRetry={() => void summary.refetch()}
          />
        ) : null}
        <section className="min-w-0 space-y-3">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
            <h2 className="type-title font-semibold">{t("breakdown")}</h2>
            <SegmentedControl
              ariaLabel={t("groupBy")}
              value={filters.groupBy}
              appearance="shell"
              size="md"
              options={[
                { value: "day", label: t("day") },
                { value: "model", label: t("model") },
                { value: "waker", label: t("waker") },
              ]}
              onValueChange={(groupBy) => update({ groupBy })}
            />
          </div>
          <UsageReport
            groupBy={filters.groupBy}
            groups={rows}
            names={names}
            runtimes={runtimes.data?.resources}
            more={displayGroups.hasNextPage}
            loading={displayGroups.isFetchingNextPage}
            pending={
              summary.isPending || Boolean(displaySummary && breakdownPending)
            }
            emptyLabel={
              displaySummary &&
              !breakdownPending &&
              !groups.isError &&
              !displayGroups.isError
                ? t("empty")
                : undefined
            }
            onLoadMore={() => void displayGroups.fetchNextPage()}
          />
          {groups.isError || displayGroups.isError ? (
            <QueryRefreshError
              error={groups.error ?? displayGroups.error}
              pending={groups.isFetching || displayGroups.isFetching}
              onRetry={() =>
                void (snapshotExpired
                  ? summary.refetch()
                  : (groups.isError ? groups : displayGroups).refetch())
              }
            />
          ) : null}
        </section>
      </div>
    </ResourcePageSurface>
  );
}
