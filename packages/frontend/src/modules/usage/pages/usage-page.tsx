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
    filters.groupBy,
  ]);
  const [published, setPublished] = useState<{
    scope: string;
    snapshot: string;
  }>();
  const previous = published?.scope === scope ? published.snapshot : "";
  const groups = useInfiniteQuery({
    ...usageGroupsOptions(filters, snapshot),
    enabled: active && Boolean(snapshot),
    ...groupPagination,
  });
  const retained = useInfiniteQuery({
    ...usageGroupsOptions(filters, previous),
    enabled: false,
    ...groupPagination,
  });
  const replacing = Boolean(previous && previous !== snapshot && retained.data);
  const targetPages = replacing ? (retained.data?.pages.length ?? 1) : 1;
  const loadedPages = groups.data?.pages.length ?? 0;
  const ready =
    loadedPages > 0 &&
    !groups.isError &&
    (loadedPages >= targetPages || !groups.hasNextPage);

  // Publish a matching summary and breakdown together. A replacement replays
  // its own cursors up to the previously visible depth.
  if (snapshot && ready && previous !== snapshot) {
    setPublished({ scope, snapshot });
  }
  const displayGroups = replacing && !ready ? retained : groups;
  const displaySummary =
    replacing && !ready ? retained.data?.pages[0] : summary.data;
  const dailySelected = filters.groupBy === "day";
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
    enabled: active && !dailySelected && Boolean(displayedSnapshot) && hasUsage,
  });
  const daily = dailySelected ? displayGroups : trend;
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
    displayGroups.data?.pages.flatMap((page) => page.groups ?? []) ?? [];
  const dailyRows = dailySelected
    ? rows
    : (daily.data?.pages.flatMap((page) => page.groups ?? []) ?? []);
  const breakdownPending =
    displayGroups.isPending ||
    (!displayGroups.data && snapshotExpired && summary.isFetching);
  const trendExpired =
    trend.error instanceof ApiError && trend.error.status === 409;

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
                pending={summary.isFetching}
                onRefresh={() => void refreshSummary()}
              />
            </>
          }
        />
        {summary.isPending ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("title")}
            className="space-y-6"
          >
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
                  "cacheHitRate",
                ].map((key) => (
                  <Skeleton key={key} className="h-16" />
                ))}
              </div>
            </div>
            <Skeleton
              className="h-72 @[42.5rem]/resource-page:h-88"
              aria-hidden="true"
            />
          </div>
        ) : displaySummary ? (
          <>
            <UsageOverview
              summary={displaySummary}
              provider={filters.provider}
            />
            <div className="bg-surface-raised min-w-0 space-y-3 rounded-xl p-4 @[42.5rem]/resource-page:p-6">
              {!hasUsage || dailyRows.length ? (
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
              ) : daily.isPending ? (
                <div
                  role="status"
                  aria-busy="true"
                  aria-label={t("dailyTokens")}
                >
                  <Skeleton
                    className="h-64 @[42.5rem]/resource-page:h-80"
                    aria-hidden="true"
                  />
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
              {hasUsage && !dailySelected && trend.isError ? (
                <QueryRefreshError
                  error={trend.error}
                  pending={trend.isFetching || summary.isFetching}
                  onRetry={() =>
                    void (trendExpired ? refreshSummary() : trend.refetch())
                  }
                />
              ) : null}
            </div>
            {hasUsage &&
            !filters.provider &&
            (providerRows.length > 0 ||
              providers.isPending ||
              providers.isError) ? (
              <section className="bg-surface-raised min-w-0 space-y-4 rounded-xl p-4 @[42.5rem]/resource-page:p-6">
                <h2 className="type-title font-semibold">{t("providers")}</h2>
                {providerRows.length ? (
                  <UsageProviderBreakdown
                    groups={providerRows}
                    total={
                      displaySummary.metrics?.totalWithCacheRead ??
                      displaySummary.metrics?.knownTotalWithCacheRead
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
          </>
        ) : (
          <QueryState
            error={summary.error}
            onRetry={() => void refreshSummary()}
          />
        )}
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
              appearance="underline"
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
          ) : displaySummary && breakdownPending ? (
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
              error={displayGroups.data ? null : groups.error}
              emptyLabel={t("empty")}
              onRetry={() =>
                void (snapshotExpired ? summary.refetch() : groups.refetch())
              }
            />
          ) : null}
          {displayGroups.data && (groups.isError || displayGroups.isError) ? (
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
