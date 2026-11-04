import {
  getUsageSummaryOptions,
  listUsageGroupsInfiniteOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { UsageFilters } from "../model/usage-filters";

function rangeQuery({ from, to, timezone, wakerIds }: UsageFilters) {
  return {
    from,
    to,
    timezone,
    ...(wakerIds.length ? { wakerIds: wakerIds.join(",") } : {}),
  };
}

export function usageSummaryOptions(filters: UsageFilters) {
  return getUsageSummaryOptions(
    withLocalErrorSurface({ query: rangeQuery(filters) }),
  );
}

export function usageGroupsOptions(filters: UsageFilters, snapshot: string) {
  return listUsageGroupsInfiniteOptions(
    withLocalErrorSurface({
      query: {
        ...rangeQuery(filters),
        snapshot,
        groupBy: filters.groupBy,
        limit: 50,
      },
    }),
  );
}
