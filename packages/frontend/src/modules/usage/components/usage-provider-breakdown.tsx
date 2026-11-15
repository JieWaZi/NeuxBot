import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import type { ListUsageGroupsResponse } from "@/shared/api/generated/types.gen";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Button } from "@/shared/ui/button";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { Skeleton } from "@/shared/ui/skeleton";
import { formatUsageTokens } from "../model/usage-format";

type Group = NonNullable<ListUsageGroupsResponse["groups"]>[number];

const previewCount = 4;

export function UsageProviderBreakdown({
  groups,
  total,
  pending = false,
  emptyLabel,
}: {
  groups: readonly Group[];
  total: number | null | undefined;
  pending?: boolean;
  emptyLabel?: string;
}) {
  const { t, i18n } = useTranslation("usage");
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const percent = new Intl.NumberFormat(i18n.resolvedLanguage, {
    style: "percent",
    maximumFractionDigits: 1,
  });
  const rows = groups
    .map(({ key, metrics }) => ({
      id: key,
      label: runtimeDisplayName({ id: key }) || t("unknown"),
      tokens: metrics?.totalWithCacheRead ?? metrics?.knownTotalWithCacheRead,
    }))
    .sort((a, b) => (b.tokens ?? -1) - (a.tokens ?? -1));
  const visible = expanded ? rows : rows.slice(0, previewCount);
  const lines = visible.map(({ tokens }) =>
    tokens != null && total != null && total > 0 ? 2 : 1,
  );
  const toggle = rows.length > previewCount;
  const [layout, setLayout] = useState({ lines, toggle });
  if (
    !pending &&
    (layout.toggle !== toggle ||
      lines.length !== layout.lines.length ||
      lines.some((value, index) => value !== layout.lines[index]))
  ) {
    setLayout({ lines, toggle });
  }
  const skeletonRows = pending
    ? layout.lines.length
      ? layout.lines
      : [1]
    : [];

  return (
    <div
      className="min-w-0 space-y-2"
      role={pending ? "status" : undefined}
      aria-busy={pending || undefined}
      aria-label={pending ? t("providers") : undefined}
    >
      <ul id={listId} aria-label={t("providers")} className="grid gap-3">
        {skeletonRows.map((lineCount, index) => (
          <li
            key={`pending-${index}`}
            aria-hidden="true"
            className="flex min-w-0 items-start justify-between gap-3"
          >
            <div className="type-body min-w-0">
              <Skeleton className="h-[1lh] w-24 max-w-full" />
              {lineCount > 1 ? (
                <div className="type-ui mt-0.5">
                  <Skeleton className="h-[1lh] w-36 max-w-full" />
                </div>
              ) : null}
            </div>
            <div className="type-body">
              <Skeleton className="h-[1lh] w-12 max-w-full" />
            </div>
          </li>
        ))}
        {!pending && !visible.length && emptyLabel ? (
          <li className="text-content-secondary type-body">{emptyLabel}</li>
        ) : null}
        {(pending ? [] : visible).map(({ id, label, tokens }) => (
          <li
            key={id ?? ""}
            className="flex min-w-0 items-start justify-between gap-3"
          >
            <div className="min-w-0">
              <div className="type-body flex items-center gap-2 font-medium">
                <RuntimeCliIcon runtimeId={id} />
                <span className="wrap-anywhere">{label}</span>
              </div>
              {tokens != null && total != null && total > 0 ? (
                <p className="text-content-secondary type-ui mt-0.5">
                  {t("providerShare", {
                    share: percent.format(tokens / total),
                  })}
                </p>
              ) : null}
            </div>
            <span className="type-body shrink-0 text-right font-medium tabular-nums">
              {tokens == null
                ? t("unknown")
                : formatUsageTokens(tokens, i18n.resolvedLanguage)}
            </span>
          </li>
        ))}
      </ul>
      {(pending ? layout.toggle : toggle) ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-expanded={expanded}
          aria-controls={listId}
          aria-label={pending ? t("loadMoreProviders") : undefined}
          disabled={pending}
          onClick={() => setExpanded(!expanded)}
        >
          {pending ? (
            <Skeleton className="h-4 w-16" />
          ) : expanded ? (
            t("showFewerProviders")
          ) : (
            t("showOtherProviders", { count: rows.length - previewCount })
          )}
        </Button>
      ) : null}
    </div>
  );
}
