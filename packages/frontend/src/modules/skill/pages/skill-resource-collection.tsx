import { useQueryEvents } from "@/shared/api/use-query-events";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { listLibrarySkillsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { listLibrarySkills } from "@/shared/api/generated/sdk.gen";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  listWakerSkillsOptions,
  setWakerSkillEnabledMutation,
  invalidateVisibleSkillQueries,
} from "../api/skill-queries";
import { SkillCatalog } from "../components/skill-catalog";
import { toSkillSummary } from "../model/skill-view";

/** 分类概览只读取独立技能摘要；插件技能由所属插件展示。 */
export function SkillResourceCollection({
  wakerId = "",
  search,
  filters,
  category,
  onCategoryChange,
  onInstall,
}: {
  wakerId?: string;
  search: string;
  filters: ReactNode;
  category: string | null;
  onCategoryChange: (category: string | null) => void;
  onInstall: (id: string, name: string) => void;
}) {
  const { t } = useTranslation("plugin");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const libraryKey = [
    ...listLibrarySkillsQueryKey({
      query: { query: search, pageSize: 100 },
    }),
    "catalog",
  ];
  const library = useQuery({
    queryKey: libraryKey,
    queryFn: async ({ signal }) => {
      const items = [];
      for (let pageNum = 1; ; pageNum++) {
        const { data } = await listLibrarySkills(
          withLocalErrorSurface({
            query: { query: search, pageNum, pageSize: 100 },
            signal,
            throwOnError: true as const,
          }),
        );
        const page = data.resources ?? [];
        items.push(...page);
        if (items.length >= (data.totalSize ?? 0) || page.length === 0)
          return items;
      }
    },
    enabled: active && !wakerId,
  });
  const installed = useQuery({
    ...listWakerSkillsOptions(
      withLocalErrorSurface({ path: { wakerId }, query: { query: search } }),
    ),
    enabled: active && Boolean(wakerId),
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [
      libraryKey,
      listWakerSkillsOptions({ path: { wakerId }, query: { query: search } })
        .queryKey,
    ],
    active,
  });
  const query = wakerId ? installed : library;
  const needle = search.trim().toLocaleLowerCase();
  const skills = (query.data ?? []).flatMap((item) => {
    const skill = toSkillSummary(item);
    return skill &&
      (skill.source === "git" || skill.source === "zip") &&
      (!needle ||
        `${skill.name} ${skill.description}`
          .toLocaleLowerCase()
          .includes(needle))
      ? [skill]
      : [];
  });
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerSkillEnabledMutation()),
    onSuccess: () => invalidateVisibleSkillQueries(client),
  });
  const count =
    category === null
      ? skills.length
      : skills.filter((skill) =>
          category === ""
            ? !skill.categories?.length
            : skill.categories?.includes(category),
        ).length;
  return (
    <div className="grid min-w-0 gap-8">
      <div className="flex min-w-0 flex-wrap items-center gap-2">{filters}</div>
      <div className="grid min-w-0 gap-3">
        {wakerId && query.data !== undefined && (
          <p className="type-resource-body text-resource-muted">
            {t("skill:count", { count })}
          </p>
        )}
        {skills.length === 0 ? (
          <QueryState
            loadingLayout="rows"
            pending={query.isPending}
            error={query.error}
            emptyLabel={t(search ? "noResults" : "empty")}
            onRetry={() => void query.refetch()}
          />
        ) : (
          <SkillCatalog
            skills={skills}
            wakerId={wakerId}
            paginationKey={JSON.stringify([wakerId, search, category])}
            category={category}
            onCategoryChange={onCategoryChange}
            disabled={toggle.isPending}
            onEnabledChange={(skill, enabled) =>
              toggle.mutate({
                path: { wakerId, skillId: skill.id },
                body: { enabled },
              })
            }
            onInstall={onInstall}
          />
        )}
      </div>
      {skills.length > 0 && query.error && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
      {toggle.error && (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(toggle.error, t("skill:toggleFailed"))}
        </p>
      )}
    </div>
  );
}
