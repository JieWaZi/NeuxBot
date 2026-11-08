import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { ResourceCardGrid } from "@/shared/ui/resource-page";
import { ResourceCatalogGroup } from "@/shared/ui/resource-catalog";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { EmptyState } from "@/shared/ui/empty-state";
import { SkillCard } from "./skill-card";
import type { SkillSummary } from "../model/types";

const GROUP_LIMIT = 6;
const PAGE_SIZE = 24;

/** SkillLibrary 的分类预览与独立分组页共用相同的两行条目。 */
export function SkillCatalog({
  skills,
  wakerId,
  paginationKey,
  category,
  onCategoryChange,
  disabled,
  onEnabledChange,
  onInstall,
}: {
  skills: SkillSummary[];
  wakerId: string;
  paginationKey: string;
  category: string | null;
  onCategoryChange: (category: string | null) => void;
  disabled?: boolean;
  onEnabledChange: (skill: SkillSummary, enabled: boolean) => void;
  onInstall?: (id: string, name: string) => void;
}) {
  const { t } = useTranslation("skill");
  const active = useRouteSceneActive();
  const [page, setPage] = useState({ paginationKey, shown: PAGE_SIZE });
  if (page.paginationKey !== paginationKey)
    setPage({ paginationKey, shown: PAGE_SIZE });
  const shown = page.paginationKey === paginationKey ? page.shown : PAGE_SIZE;
  const groups = new Map<string, SkillSummary[]>();
  for (const skill of skills) {
    for (const key of new Set(
      skill.categories?.length ? skill.categories : [""],
    )) {
      const list = groups.get(key) ?? [];
      list.push(skill);
      groups.set(key, list);
    }
  }
  for (const items of groups.values())
    items.sort((a, b) => a.name.localeCompare(b.name));
  const rows = (items: SkillSummary[]) => (
    <ResourceCardGrid appearance="catalog">
      {items.map((skill) => (
        <SkillCard
          key={skill.id}
          skill={skill}
          wakerId={wakerId}
          disabled={disabled}
          onEnabledChange={(enabled) => onEnabledChange(skill, enabled)}
          onInstall={
            onInstall ? () => onInstall(skill.id, skill.name) : undefined
          }
        />
      ))}
    </ResourceCardGrid>
  );
  if (category !== null) {
    const items = groups.get(category) ?? [];
    return (
      <div className="grid min-w-0 gap-3">
        {items.length ? (
          rows(items.slice(0, shown))
        ) : (
          <EmptyState density="compact" title={t("emptyTitle")} />
        )}
        <ResourceInfiniteScroll
          hasMore={active && items.length > shown}
          showLoadMoreButton={false}
          onLoadMore={() =>
            setPage({ paginationKey, shown: shown + PAGE_SIZE })
          }
        />
      </div>
    );
  }
  return (
    <div className="grid min-w-0 gap-8">
      {[...groups]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, items]) => {
          const title = key || t("category.uncategorized");
          return (
            <ResourceCatalogGroup
              key={key}
              title={title}
              count={items.length}
              aria-label={title}
              actions={
                items.length > GROUP_LIMIT ? (
                  <Button
                    variant="catalog-ghost"
                    size="catalog"
                    aria-label={t("category.seeAllName", { name: title })}
                    onClick={() => onCategoryChange(key)}
                  >
                    {t("category.seeAll")}
                    <ChevronRight />
                  </Button>
                ) : undefined
              }
            >
              {rows(items.slice(0, GROUP_LIMIT))}
            </ResourceCatalogGroup>
          );
        })}
    </div>
  );
}
