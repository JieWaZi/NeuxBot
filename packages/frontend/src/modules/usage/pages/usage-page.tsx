import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import { ApiError } from "@/shared/api/errors";
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
  const options = usageSummaryOptions(filters);
  const summary = useQuery({ ...options, enabled: active });
  const { refetch: refreshSummary } = summary;
  const snapshot = summary.data?.snapshot ?? "";
  const groups = useInfiniteQuery({
    ...usageGroupsOptions(filters, snapshot),
    enabled: active && Boolean(snapshot),
    initialPageParam: "",
    getNextPageParam: (page) => (page.hasMore ? page.nextCursor : undefined),
    retry: (attempt, error) =>
      !(error instanceof ApiError && error.status === 409) && attempt < 2,
  });

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
  const pages = groups.data?.pages;
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
            className="grid grid-cols-1 gap-3 @[34rem]/resource-page:grid-cols-3"
            role="status"
            aria-busy="true"
            aria-label={t("title")}
          >
            {[0, 1, 2].map((key) => (
              <Skeleton className="h-16" key={key} aria-hidden="true" />
            ))}
          </div>
        ) : summary.data ? (
          <UsageOverview summary={summary.data} />
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
        <div className="flex flex-wrap items-center justify-between gap-3">
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
          <SegmentedControl
            ariaLabel={t("metric")}
            value={filters.metric}
            options={[
              { value: "tokens", label: t("tokens") },
              { value: "cost", label: t("cost") },
            ]}
            onValueChange={(metric) => update({ metric })}
          />
        </div>
        {summary.data && rows.length ? (
          <UsageReport
            groupBy={filters.groupBy}
            groups={rows}
            metric={filters.metric}
            names={names}
            totalGroups={pages?.[0]?.totalGroups ?? 0}
            more={groups.hasNextPage}
            loading={groups.isFetchingNextPage}
            onLoadMore={() => void groups.fetchNextPage()}
          />
        ) : summary.data ? (
          <QueryState
            pending={
              groups.isPending || (snapshotExpired && summary.isFetching)
            }
            error={groups.error}
            emptyLabel={t("empty")}
            onRetry={() =>
              void (snapshotExpired ? summary.refetch() : groups.refetch())
            }
          />
        ) : null}
        {rows.length > 0 && groups.isError ? (
          <QueryRefreshError
            error={groups.error}
            pending={groups.isFetching}
            onRetry={() =>
              void (snapshotExpired ? summary.refetch() : groups.refetch())
            }
          />
        ) : null}
      </div>
    </ResourcePageSurface>
  );
}
