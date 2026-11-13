import { Ellipsis, LayoutGrid, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { usageSources, type UsageSource } from "../model/usage-filters";

const primaryProviders = [
  "claude",
  "codex",
  "cursor",
  "grok",
  "opencode",
  "pi",
];

export function UsageProviderFilter({
  providers,
  value,
  onChange,
}: {
  providers: readonly string[];
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation("usage");
  const otherProviders = [...new Set([...providers, value])].filter(
    (id) => id && !primaryProviders.includes(id),
  );
  const otherSelected = otherProviders.includes(value);
  const moreLabel = otherSelected
    ? t("otherAgentSelected", { name: runtimeDisplayName({ id: value }) })
    : t("otherAgents");
  const select = (next: string) => {
    if (next !== value) onChange(next);
  };
  return (
    <SegmentedControl
      appearance="icons"
      ariaLabel={t("agentFilter")}
      value={value}
      onValueChange={select}
      options={[
        {
          value: "",
          label: t("allAgents"),
          icon: <LayoutGrid aria-hidden="true" />,
        },
        ...primaryProviders.map((id) => ({
          value: id,
          label: runtimeDisplayName({ id }),
          icon: <RuntimeCliIcon runtimeId={id} />,
        })),
      ]}
      trailingAction={
        otherProviders.length
          ? {
              selected: otherSelected,
              content: (
                <DropdownMenu>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <DropdownMenuTrigger asChild>
                        <Button
                          type="button"
                          variant="plain"
                          size="icon"
                          aria-label={moreLabel}
                        >
                          <Ellipsis aria-hidden="true" />
                        </Button>
                      </DropdownMenuTrigger>
                    </TooltipTrigger>
                    <TooltipContent>{moreLabel}</TooltipContent>
                  </Tooltip>
                  <DropdownMenuContent
                    align="end"
                    aria-label={t("otherAgents")}
                  >
                    <DropdownMenuRadioGroup
                      value={value}
                      onValueChange={select}
                    >
                      {otherProviders.map((id) => (
                        <DropdownMenuRadioItem key={id} value={id}>
                          <RuntimeCliIcon runtimeId={id} />
                          {runtimeDisplayName({ id })}
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              ),
            }
          : undefined
      }
    />
  );
}

export function UsageSourceFilter({
  value,
  onChange,
}: {
  value: UsageSource;
  onChange: (value: UsageSource) => void;
}) {
  const { t } = useTranslation("usage");
  return (
    <ResourceFilterSelect
      ariaLabel={t("sourceFilter")}
      options={[
        { value: "all", label: t("allSources") },
        ...usageSources.map(({ value, label }) => ({ value, label: t(label) })),
      ]}
      value={value || "all"}
      onValueChange={(next) => onChange(next === "all" ? "" : next)}
    />
  );
}

export function UsageRefresh({
  pending,
  onRefresh,
}: {
  pending: boolean;
  onRefresh: () => void;
}) {
  const { t } = useTranslation("usage");
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={t("refresh")}
            loading={pending}
            onClick={onRefresh}
          >
            <RefreshCw aria-hidden="true" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t("refresh")}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
