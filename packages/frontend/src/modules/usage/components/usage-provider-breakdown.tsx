import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import type { ListUsageGroupsResponse } from "@/shared/api/generated/types.gen";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Button } from "@/shared/ui/button";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { formatUsageTokens } from "../model/usage-format";

type Group = NonNullable<ListUsageGroupsResponse["groups"]>[number];

const previewCount = 4;

export function UsageProviderBreakdown({
  groups,
  total,
}: {
  groups: readonly Group[];
  total: number | null | undefined;
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

  return (
    <div className="min-w-0 space-y-2">
      <ul id={listId} aria-label={t("providers")} className="grid gap-3">
        {(expanded ? rows : rows.slice(0, previewCount)).map(
          ({ id, label, tokens }) => (
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
          ),
        )}
      </ul>
      {rows.length > previewCount ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-expanded={expanded}
          aria-controls={listId}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded
            ? t("showFewerProviders")
            : t("showOtherProviders", { count: rows.length - previewCount })}
        </Button>
      ) : null}
    </div>
  );
}
