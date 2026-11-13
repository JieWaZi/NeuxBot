import {
  getUsageSummaryOptions,
  listUsageGroupsInfiniteOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { UsageFilters } from "../model/usage-filters";

type UsageRange = Omit<UsageFilters, "groupBy">;

function rangeQuery({
  from,
  to,
  timezone,
  wakerIds,
  provider,
  source,
}: UsageRange) {
  return {
    from,
    to,
    timezone,
    ...(wakerIds.length ? { wakerIds: wakerIds.join(",") } : {}),
    ...(provider ? { provider } : {}),
    ...(source ? { source } : {}),
  };
}

export function usageSummaryOptions(filters: UsageFilters) {
  return getUsageSummaryOptions(
    withLocalErrorSurface({ query: rangeQuery(filters) }),
  );
}

export function usageGroupsOptions(
  filters: UsageRange & {
    groupBy: UsageFilters["groupBy"] | "provider";
  },
  snapshot: string,
) {
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
