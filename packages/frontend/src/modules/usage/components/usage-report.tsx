import { Activity, Brain, LayoutGrid } from "lucide-react";
import { useTranslation } from "react-i18next";

import type {
  GetUsageSummaryResponse as Summary,
  ListUsageGroupsResponse,
  ListRuntimesResponse,
} from "@/shared/api/generated/types.gen";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/shared/ui/table";
import type { UsageGroupBy } from "../model/usage-filters";
import { formatUsageTokens } from "../model/usage-format";
import { usageSeries } from "../model/usage-series";

type Metrics = NonNullable<Summary["metrics"]>;
type Group = NonNullable<ListUsageGroupsResponse["groups"]>[number];

function TokenValue({ metrics }: { metrics: Metrics }) {
  const { t, i18n } = useTranslation("usage");
  const known = metrics.totalWithCacheRead ?? metrics.knownTotalWithCacheRead;
  return known == null ? (
    <span className="text-content-secondary">{t("unknown")}</span>
  ) : (
    <span className="tabular-nums">
      {formatUsageTokens(known, i18n.resolvedLanguage)}
    </span>
  );
}

const metricCell =
  "bg-surface-tertiary grid min-w-0 content-start gap-1 rounded-lg p-3";
const overviewCounters = [
  ...usageSeries,
  { key: "reasoning", color: "text-status-success-content", icon: Brain },
] as const;

export function UsageOverview({
  summary,
  provider,
}: {
  summary: Summary;
  provider?: string;
}) {
  const { t, i18n } = useTranslation("usage");
  const locale = i18n.resolvedLanguage;
  const { coverage, metrics = {} } = summary;
  const { invocations, missingUsageInvocations, incompleteUsageInvocations } =
    metrics;
  const missing = missingUsageInvocations ?? 0;
  const incomplete = incompleteUsageInvocations ?? 0;
  const partial = coverage?.reasons?.includes("native_projection_incomplete");
  const empty = !partial && invocations === 0;
  const total = empty
    ? 0
    : (metrics.totalWithCacheRead ?? metrics.knownTotalWithCacheRead);
  const recorded =
    !empty && total != null && (metrics.totalWithCacheRead == null || partial);
  const numbers = new Intl.NumberFormat(locale);
  const completeReasoning = metrics.tokens?.reasoning;
  const reasoning = empty
    ? 0
    : (completeReasoning ?? metrics.knownTokens?.reasoning);
  const reasoningLabel =
    !empty && (partial || completeReasoning == null)
      ? "recordedReasoning"
      : "reasoning";
  const notes = [
    partial && t("projectionPartial"),
    missing > 0 && t("tokenMissing", { count: missing }),
    incomplete > 0 && t("tokenIncomplete", { count: incomplete }),
  ].filter(Boolean);

  return (
    <div className="bg-surface-raised min-w-0 space-y-4 rounded-xl p-4 @[42.5rem]/resource-page:p-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="bg-surface-tertiary size-control-lg grid shrink-0 place-items-center rounded-lg">
            {provider ? (
              <RuntimeCliIcon runtimeId={provider} size={20} />
            ) : (
              <LayoutGrid aria-hidden="true" className="size-icon-lg" />
            )}
          </div>
          <div className="min-w-0 space-y-1">
            <p className="text-content-secondary type-ui flex flex-wrap items-baseline gap-x-2">
              {provider ? (
                <span className="text-content-primary font-medium wrap-anywhere">
                  {runtimeDisplayName({ id: provider })}
                </span>
              ) : null}
              <span>{t(recorded ? "recordedTokens" : "totalTokens")}</span>
            </p>
            <p className="type-usage-total font-semibold tracking-tight wrap-anywhere tabular-nums">
              {total == null ? t("unknown") : numbers.format(total)}
            </p>
          </div>
        </div>
        {invocations != null && (!partial || invocations > 0) ? (
          <p className="bg-surface-tertiary type-label flex items-center gap-2 rounded-lg px-3 py-2 font-medium tabular-nums">
            <Activity
              aria-hidden="true"
              className="text-status-info-content size-icon-md"
            />
            {t("invocationCount", { count: invocations })}
          </p>
        ) : null}
      </div>
      <dl
        className={cn(
          "grid grid-cols-2 gap-2",
          reasoning == null
            ? "@[38rem]/resource-page:grid-cols-4"
            : "@[38rem]/resource-page:grid-cols-3 @[52rem]/resource-page:grid-cols-5",
        )}
      >
        {overviewCounters.map(({ key, color, icon: Icon }) => {
          const value = empty ? 0 : metrics.knownTokens?.[key];
          const isReasoning = key === "reasoning";
          const count = isReasoning ? reasoning : value;
          if (isReasoning && count == null) return null;
          return (
            <div
              key={key}
              className={cn(
                metricCell,
                isReasoning && "col-span-2 @[38rem]/resource-page:col-span-1",
              )}
            >
              <dt className="text-content-secondary type-ui flex min-w-0 items-center gap-1.5">
                <Icon
                  aria-hidden="true"
                  className={cn("size-icon-sm shrink-0", color)}
                />
                {t(isReasoning ? reasoningLabel : key)}
              </dt>
              <dd className="type-label font-medium wrap-anywhere tabular-nums">
                {count == null
                  ? t("unknown")
                  : formatUsageTokens(count, locale)}
              </dd>
            </div>
          );
        })}
      </dl>
      {notes.length ? (
        <p className="text-content-secondary type-ui">{notes.join(" · ")}</p>
      ) : null}
    </div>
  );
}

export function UsageReport({
  groupBy,
  groups,
  names,
  runtimes = [],
  more,
  loading,
  onLoadMore,
}: {
  groupBy: UsageGroupBy;
  groups: readonly Group[];
  names: ReadonlyMap<string, string>;
  runtimes?: NonNullable<ListRuntimesResponse["resources"]>;
  more: boolean;
  loading: boolean;
  onLoadMore: () => void;
}) {
  const { t, i18n } = useTranslation("usage");
  const numbers = new Intl.NumberFormat(i18n.resolvedLanguage);
  return (
    <div className="min-w-0 space-y-3">
      <Table containerClassName="rounded-none border-0 bg-transparent">
        <TableHeader className="bg-transparent">
          <TableRow className="border-border-subtle">
            <TableHead scope="col" className="pl-0">
              {t(groupBy)}
            </TableHead>
            <TableHead scope="col" className="text-right">
              {t("tokens")}
            </TableHead>
            <TableHead scope="col" className="pr-0 text-right">
              {t("invocations")}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {groups.map((group) => {
            const metrics = group.metrics ?? {};
            const runtime = runtimes.find(({ id }) => id === group.provider);
            const label = group.key
              ? groupBy === "waker"
                ? (names.get(group.key) ?? group.key)
                : runtime?.models?.find(({ id }) => id === group.key)?.name ||
                  group.key
              : t("unknown");
            const recorded =
              metrics.totalWithCacheRead == null &&
              metrics.knownTotalWithCacheRead != null;
            return (
              <TableRow
                key={JSON.stringify([group.provider, group.key])}
                className="border-border-subtle"
              >
                <th scope="row" className="py-2 pr-3 text-left font-medium">
                  <div className="flex min-w-0 items-center gap-2">
                    {group.provider ? (
                      <RuntimeCliIcon runtimeId={group.provider} />
                    ) : null}
                    <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                      <span className="wrap-anywhere">{label}</span>{" "}
                      {group.provider ? (
                        <span className="text-content-secondary type-ui font-normal wrap-anywhere">
                          {group.provider}
                        </span>
                      ) : null}
                    </div>
                  </div>
                </th>
                <TableCell className="text-right whitespace-nowrap">
                  <TokenValue metrics={metrics} />
                  {recorded ? (
                    <span className="text-content-secondary type-caption block">
                      {t("recorded")}
                    </span>
                  ) : null}
                </TableCell>
                <TableCell className="pr-0 text-right whitespace-nowrap tabular-nums">
                  {metrics.invocations == null
                    ? t("unknown")
                    : numbers.format(metrics.invocations)}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      {more ? (
        <div className="flex justify-end">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            onClick={onLoadMore}
            disabled={loading}
          >
            {t("loadMore")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
