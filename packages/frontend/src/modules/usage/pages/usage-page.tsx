import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@/shared/api/errors";
import type { ListUsageGroupsResponse } from "@/shared/api/generated/types.gen";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useVisibleScene } from "@/shared/api/work-result-reading";
import { DateRangePicker } from "@/shared/ui/date-range-picker";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  ResourcePageHeader,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Skeleton } from "@/shared/ui/skeleton";
import { TimeZonePicker } from "@/shared/ui/time-zone-picker";
import { usageGroupsOptions, usageSummaryOptions } from "../api/usage-queries";
import { UsageOverview, UsageReport } from "../components/usage-report";
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
    enabled: active && filters.groupBy === "model",
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
    initialPageParam: "",
    getNextPageParam: nextCursor,
    retry: retryGroups,
  });
  const retained = useInfiniteQuery({
    ...usageGroupsOptions(filters, previous),
    enabled: false,
    initialPageParam: "",
    getNextPageParam: nextCursor,
    retry: retryGroups,
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
  const pages = displayGroups.data?.pages;
  const rows = pages?.flatMap((page) => page.groups ?? []) ?? [];

  return (
    <ResourcePageSurface>
      <ResourcePageHeader
        title={t("title")}
        description={null}
        actions={null}
      />
      <ResourcePageToolbar
        filters={
          <>
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
            <div className="min-w-0">
              <TimeZonePicker
                aria-label={t("timezone")}
                value={filters.timezone}
                onChange={(timezone) => {
                  const range = rangeFromUsageDates(fromDate, toDate, timezone);
                  if (range) update({ ...range, timezone });
                }}
              />
            </div>
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
                      ? t("selectedWakers", { count: filters.wakerIds.length })
                      : t("wakers")
                  }
                  searchLabel={t("searchWakers")}
                  emptyLabel={t("noWakers")}
                  clearSearchLabel={t("clear")}
                  clearSelectionLabel={t("clear")}
                />
              </div>
            ) : null}
          </>
        }
      />
      <div className="min-w-0 space-y-4">
        {summary.isPending ? (
          <div
            className="grid grid-cols-1 gap-3 @[34rem]/resource-page:grid-cols-2"
            role="status"
            aria-busy="true"
            aria-label={t("title")}
          >
            {[0, 1].map((key) => (
              <Skeleton className="h-16" key={key} aria-hidden="true" />
            ))}
          </div>
        ) : displaySummary ? (
          <UsageOverview summary={displaySummary} />
        ) : (
          <QueryState
            error={summary.error}
            onRetry={() => void summary.refetch()}
          />
        )}
        {summary.data && summary.isError ? (
          <QueryRefreshError
            error={summary.error}
            pending={summary.isFetching}
            onRetry={() => void summary.refetch()}
          />
        ) : null}
        <SegmentedControl
          ariaLabel={t("groupBy")}
          value={filters.groupBy}
          options={[
            { value: "day", label: t("day") },
            { value: "model", label: t("model") },
            { value: "waker", label: t("waker") },
          ]}
          onValueChange={(groupBy) => update({ groupBy })}
        />
        {displaySummary && rows.length ? (
          <UsageReport
            groupBy={filters.groupBy}
            groups={rows}
            names={names}
            runtimes={runtimes.data?.resources}
            totalGroups={pages?.[0]?.totalGroups ?? 0}
            more={displayGroups.hasNextPage}
            loading={displayGroups.isFetchingNextPage}
            onLoadMore={() => void displayGroups.fetchNextPage()}
          />
        ) : displaySummary ? (
          <QueryState
            pending={
              displayGroups.isPending ||
              (!displayGroups.data && snapshotExpired && summary.isFetching)
            }
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
      </div>
    </ResourcePageSurface>
  );
}
