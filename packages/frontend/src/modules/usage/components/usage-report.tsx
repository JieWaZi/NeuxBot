import { useTranslation } from "react-i18next";

import type {
  GetUsageSummaryResponse as Summary,
  ListUsageGroupsResponse,
} from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import type { UsageGroupBy } from "../model/usage-filters";

type Metrics = NonNullable<Summary["metrics"]>;
type Group = NonNullable<ListUsageGroupsResponse["groups"]>[number];

const numbers = new Intl.NumberFormat();
const counters = [
  "input",
  "reportedInput",
  "output",
  "cacheRead",
  "cacheWrite",
  "reasoning",
] as const;

function TokenValue({ metrics }: { metrics: Metrics }) {
  const { t } = useTranslation("usage");
  const known = metrics.totalWithCacheRead ?? metrics.knownTotalWithCacheRead;
  return known == null ? (
    <span className="text-muted-foreground">{t("unknown")}</span>
  ) : (
    <span className="tabular-nums">{numbers.format(known)}</span>
  );
}

export function UsageOverview({ summary }: { summary: Summary }) {
  const { t } = useTranslation("usage");
  const { coverage, metrics = {} } = summary;
  const missing = metrics.missingUsageInvocations ?? 0;
  const incomplete = metrics.incompleteUsageInvocations ?? 0;
  return (
    <div className="min-w-0 space-y-3">
      <dl className="grid grid-cols-1 gap-3 @[34rem]/resource-page:grid-cols-2">
        <div className="bg-muted/50 rounded-lg px-3 py-2.5">
          <dt className="text-muted-foreground type-ui">{t("tokens")}</dt>
          <dd className="type-title font-medium">
            <TokenValue metrics={metrics} />
          </dd>
        </div>
        <div className="bg-muted/50 rounded-lg px-3 py-2.5">
          <dt className="text-muted-foreground type-ui">{t("invocations")}</dt>
          <dd className="type-title font-medium tabular-nums">
            {metrics.invocations == null
              ? t("unknown")
              : numbers.format(metrics.invocations)}
          </dd>
        </div>
      </dl>
      {metrics.knownTotalWithCacheRead != null ||
      metrics.knownTokens?.reportedInput != null ? (
        <dl className="text-muted-foreground type-ui flex flex-wrap gap-x-4 gap-y-1">
          {counters.map((key) => {
            const value = metrics.knownTokens?.[key];
            if (value == null) return null;
            return (
              <div key={key} className="flex gap-1.5">
                <dt>{t(key)}</dt>
                <dd className="text-foreground tabular-nums">
                  {numbers.format(value)}
                </dd>
              </div>
            );
          })}
          {metrics.knownTotalWithoutCacheRead != null ? (
            <div className="flex gap-1.5">
              <dt>{t("withoutCache")}</dt>
              <dd className="text-foreground tabular-nums">
                {numbers.format(metrics.knownTotalWithoutCacheRead)}
              </dd>
            </div>
          ) : null}
        </dl>
      ) : null}
      <p className="text-muted-foreground type-ui">
        {[
          t("historyCoverage"),
          coverage?.reasons?.includes("native_projection_incomplete") &&
            t("projectionPartial"),
          missing > 0 && t("tokenMissing", { count: missing }),
          incomplete > 0 && t("tokenIncomplete", { count: incomplete }),
        ]
          .filter(Boolean)
          .join(" ")}
      </p>
    </div>
  );
}

export function UsageReport({
  groupBy,
  groups,
  names,
  totalGroups,
  more,
  loading,
  onLoadMore,
}: {
  groupBy: UsageGroupBy;
  groups: readonly Group[];
  names: ReadonlyMap<string, string>;
  totalGroups: number;
  more: boolean;
  loading: boolean;
  onLoadMore: () => void;
}) {
  const { t } = useTranslation("usage");
  const maxTokens = groups.reduce(
    (highest, group) =>
      Math.max(highest, group.metrics?.knownTotalWithCacheRead ?? 0),
    0,
  );
  return (
    <div className="min-w-0 space-y-2">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">{t(groupBy)}</TableHead>
            <TableHead scope="col">{t("tokens")}</TableHead>
            <TableHead scope="col" className="text-right">
              {t("invocations")}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {groups.map((group) => {
            const metrics = group.metrics ?? {};
            const value = metrics.knownTotalWithCacheRead;
            const label = group.key
              ? groupBy === "waker"
                ? (names.get(group.key) ?? group.key)
                : group.key
              : t("unknown");
            return (
              <TableRow key={JSON.stringify([group.provider, group.key])}>
                <TableCell>
                  {group.provider ? `${group.provider} / ${label}` : label}
                </TableCell>
                <TableCell>
                  <TokenValue metrics={metrics} />
                  {groupBy === "day" && value != null && maxTokens > 0 ? (
                    <div
                      aria-hidden="true"
                      className="bg-muted mt-1 h-1 max-w-44 overflow-hidden rounded-full"
                    >
                      <div
                        className="bg-primary/60 h-full"
                        style={{ width: `${(100 * value) / maxTokens}%` }}
                      />
                    </div>
                  ) : null}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {metrics.invocations == null
                    ? t("unknown")
                    : numbers.format(metrics.invocations)}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      <div className="flex items-center justify-between gap-3">
        <span className="text-muted-foreground type-ui">
          {t("groupCount", { count: totalGroups })}
        </span>
        {more ? (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={onLoadMore}
            disabled={loading}
          >
            {t("loadMore")}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
