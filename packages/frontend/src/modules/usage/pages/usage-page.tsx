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
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Skeleton } from "@/shared/ui/skeleton";
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
  const replacing = Boolean(
    previous && previous.snapshot !== snapshot && retained.data,
  );
  const targetPages =
    replacing && sameGroup ? (retained.data?.pages.length ?? 1) : 1;
  const loadedPages = groups.data?.pages.length ?? 0;
  const ready =
    loadedPages > 0 &&
    !groups.isError &&
    (loadedPages >= targetPages || !groups.hasNextPage);

  // Changing the grouping keeps the published overview for the same filters.
  // A replacement for the same grouping replays its own cursors to the visible depth.
  if (snapshot && ready && (previous?.snapshot !== snapshot || !sameGroup)) {
    setPublished({ scope, snapshot, groupBy: filters.groupBy });
  }
  const showRetained = replacing && !ready;
  const displayGroups = showRetained && sameGroup ? retained : groups;
  const displayGroupsData =
    showRetained && !sameGroup ? undefined : displayGroups.data;
  const displaySummary = showRetained ? retained.data?.pages[0] : summary.data;
  const dailyInBreakdown =
    filters.groupBy === "day" && (!showRetained || sameGroup);
  const displayedSnapshot = displaySummary?.snapshot ?? "";
  const earliestRetainedAt = displaySummary?.coverage?.earliestRetainedAt;
  const hasUsage = displaySummary?.metrics?.invocations !== 0;
  const providers = useInfiniteQuery({
    ...usageGroupsOptions(
      { ...filters, groupBy: "provider" },
      displayedSnapshot,
    ),
    ...groupPagination,
    enabled:
      active && !filters.provider && Boolean(displayedSnapshot) && hasUsage,
  });
  const trend = useInfiniteQuery({
    ...usageGroupsOptions({ ...filters, groupBy: "day" }, displayedSnapshot),
    ...groupPagination,
    enabled:
      active && !dailyInBreakdown && Boolean(displayedSnapshot) && hasUsage,
  });
  const daily = dailyInBreakdown ? displayGroups : trend;
  const { fetchNextPage: extendReplacement } = groups;
  const needsMore =
    active &&
    replacing &&
    loadedPages > 0 &&
    loadedPages < targetPages &&
    groups.hasNextPage &&
    !groups.isFetching &&
    !groups.isError;
  useEffect(() => {
    if (needsMore) void extendReplacement();
  }, [needsMore, loadedPages, extendReplacement]);

  // Refresh the summary so its new snapshot replaces every grouped query key.
  useQueryEvents({
    topic: "usage",
    resource: { kind: "global" },
    eventTypes: ["usage.changed"],
    queryKeys: [options.queryKey],
    active,
  });
  const retried = useRef("");
  const snapshotExpired =
    groups.error instanceof ApiError && groups.error.status === 409;
  useEffect(() => {
    if (active && snapshotExpired && snapshot && retried.current !== snapshot) {
      retried.current = snapshot;
      void refreshSummary();
    }
  }, [active, snapshotExpired, snapshot, refreshSummary]);

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
  const providerRows =
    providers.data?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const providerIds = [
    ...(runtimes.data?.resources?.flatMap(({ id }) => (id ? [id] : [])) ?? []),
    ...providerRows.flatMap(({ key }) => (key ? [key] : [])),
  ];
  const rows =
    displayGroupsData?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const dailyRows = dailyInBreakdown
    ? rows
    : (daily.data?.pages.flatMap((page) => page.groups ?? []) ?? []);
  const breakdownPending =
    displayGroups.isPending ||
    (!displayGroupsData && snapshotExpired && summary.isFetching);
  const trendExpired =
    trend.error instanceof ApiError && trend.error.status === 409;

  return (
    <ResourcePageSurface>
      <div className="h-full min-w-0 space-y-6">
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
                pending={summary.isFetching}
                onRefresh={() => void refreshSummary()}
              />
            </>
          }
        />
        {summary.isPending ? (
          <div role="status" aria-busy="true" aria-label={t("title")}>
            <div
              className="bg-surface-raised space-y-4 rounded-xl p-4 @[42.5rem]/resource-page:p-6"
              aria-hidden="true"
            >
              <div className="flex flex-wrap items-center justify-between gap-4">
                <Skeleton className="h-control-lg w-52 max-w-full" />
                <Skeleton className="h-control-md w-24" />
              </div>
              <div className="grid grid-cols-2 gap-2 @[38rem]/resource-page:grid-cols-3 @[52rem]/resource-page:grid-cols-5">
                {[
                  "input",
                  "output",
                  "cacheWrite",
                  "cacheRead",
                  "reasoning",
                ].map((key) => (
                  <Skeleton key={key} className="h-16" />
                ))}
              </div>
            </div>
          </div>
        ) : displaySummary ? (
          <UsageOverview summary={displaySummary} provider={filters.provider} />
        ) : (
          <QueryState
            error={summary.error}
            onRetry={() => void refreshSummary()}
          />
        )}
        {summary.isPending || displaySummary ? (
          <div className="bg-surface-raised min-w-0 space-y-3 rounded-xl p-4 @[42.5rem]/resource-page:p-6">
            {displaySummary && (!hasUsage || dailyRows.length) ? (
              <UsageTrend
                groups={hasUsage ? dailyRows : []}
                from={fromDate}
                to={toDate}
                hasMore={hasUsage && daily.hasNextPage}
                projectionPartial={displaySummary.coverage?.reasons?.includes(
                  "native_projection_incomplete",
                )}
                earliestRetainedDay={
                  earliestRetainedAt == null
                    ? undefined
                    : usageDate(earliestRetainedAt, filters.timezone)
                }
              />
            ) : summary.isPending || daily.isPending ? (
              <div
                className="space-y-4"
                role="status"
                aria-busy="true"
                aria-label={t("dailyTokens")}
              >
                <div className="flex items-center justify-between gap-2">
                  <Skeleton className="h-6 w-20" />
                  <Skeleton className="h-4 w-12" />
                </div>
                <div className="space-y-2">
                  <Skeleton className="h-56 @[42.5rem]/resource-page:h-72" />
                  <Skeleton className="h-4" />
                </div>
                <Skeleton className="mx-auto h-4 w-48 max-w-full" />
              </div>
            ) : !daily.isError ? (
              <p className="text-content-secondary type-body">
                {t("noDailyUsage")}
              </p>
            ) : null}

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
            {hasUsage && !dailyInBreakdown && trend.isError ? (
              <QueryRefreshError
                error={trend.error}
                pending={trend.isFetching || summary.isFetching}
                onRetry={() =>
                  void (trendExpired ? refreshSummary() : trend.refetch())
                }
              />
            ) : null}
          </div>
        ) : null}
        {!filters.provider &&
        (summary.isPending ||
          (displaySummary &&
            hasUsage &&
            (providerRows.length > 0 ||
              providers.isPending ||
              providers.isError))) ? (
          <section className="bg-surface-raised min-w-0 space-y-4 rounded-xl p-4 @[42.5rem]/resource-page:p-6">
            <h2 className="type-title font-semibold">{t("providers")}</h2>
            {providerRows.length ? (
              <UsageProviderBreakdown
                groups={providerRows}
                total={
                  displaySummary?.metrics?.totalWithCacheRead ??
                  displaySummary?.metrics?.knownTotalWithCacheRead
                }
              />
            ) : providers.isPending ? (
              <div
                className="space-y-3"
                role="status"
                aria-busy="true"
                aria-label={t("providers")}
              >
                <Skeleton className="h-6" aria-hidden="true" />
                <Skeleton className="h-6" aria-hidden="true" />
              </div>
            ) : null}
            {providers.hasNextPage ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={providers.isFetchingNextPage}
                onClick={() => void providers.fetchNextPage()}
              >
                {t("loadMoreProviders")}
              </Button>
            ) : null}
            {providers.isError ? (
              <QueryRefreshError
                error={providers.error}
                pending={providers.isFetching || summary.isFetching}
                onRetry={() =>
                  void (providers.error instanceof ApiError &&
                  providers.error.status === 409
                    ? refreshSummary()
                    : providers.refetch())
                }
              />
            ) : null}
          </section>
        ) : null}
        {summary.data && summary.isError ? (
          <QueryRefreshError
            error={summary.error}
            pending={summary.isFetching}
            onRetry={() => void summary.refetch()}
          />
        ) : null}
        {/* Keep enough scroll range below the tabs when the report gets shorter. */}
        <section className="min-h-full min-w-0 space-y-3">
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
          {displaySummary && rows.length ? (
            <UsageReport
              groupBy={filters.groupBy}
              groups={rows}
              names={names}
              runtimes={runtimes.data?.resources}
              more={displayGroups.hasNextPage}
              loading={displayGroups.isFetchingNextPage}
              onLoadMore={() => void displayGroups.fetchNextPage()}
            />
          ) : summary.isPending || (displaySummary && breakdownPending) ? (
            <div
              className="space-y-2"
              role="status"
              aria-busy="true"
              aria-label={t("breakdown")}
            >
              <Skeleton className="h-row-default" aria-hidden="true" />
              <Skeleton className="h-row-default" aria-hidden="true" />
              <Skeleton className="h-row-default" aria-hidden="true" />
            </div>
          ) : displaySummary ? (
            <QueryState
              error={displayGroupsData ? null : groups.error}
              emptyLabel={t("empty")}
              onRetry={() =>
                void (snapshotExpired ? summary.refetch() : groups.refetch())
              }
            />
          ) : null}
          {displayGroupsData && (groups.isError || displayGroups.isError) ? (
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
