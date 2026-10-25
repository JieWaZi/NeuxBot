import { useQueryEvents } from "@/shared/api/use-query-events";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { listLibrarySkillsQueryKey } from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import {
  ResourceCardGrid,
  ResourcePageToolbar,
  ResourcePageLibraryLabel,
} from "@/shared/ui/resource-page";
import { listLibrarySkills } from "@/shared/api/generated/sdk.gen";
import {
  useResourcePages,
  RESOURCE_BATCH_SIZE,
} from "@/shared/hooks/use-resource-pages";
import { useResourceWindow } from "@/shared/hooks/use-resource-window";
import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import { ResourceInfiniteScroll } from "@/shared/ui/resource-infinite-scroll";
import { ResourceSourceHeading } from "@/shared/ui/resource-source-heading";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  listWakerSkillsOptions,
  setWakerSkillEnabledMutation,
  invalidateVisibleSkillQueries,
} from "../api/skill-queries";
import { SkillCard } from "../components/skill-card";
import { SkillImportDialog } from "../components/skill-import-dialog";
import {
  SkillDeleteDialog,
  type DeletableSkill,
} from "../components/skill-delete-dialog";
import { toSkillSummary } from "../model/skill-view";

export function SkillResourceCollection({
  wakerId = "",
  search,
  adding,
  onAddingChange,
  source,
  onSourceChange,
  filters,
  pluginCount,
  pluginContent,
  pluginEmpty,
  onInstall,
}: {
  wakerId?: string;
  search: string;
  adding: boolean;
  onAddingChange: (open: boolean) => void;
  source: "all" | "plugins" | "independent";
  onSourceChange: (source: "all" | "plugins" | "independent") => void;
  filters: ReactNode;
  pluginCount: number;
  pluginContent: ReactNode;
  pluginEmpty: boolean;
  onInstall: (id: string, name: string) => void;
}) {
  const { t } = useTranslation("plugin");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const [targets, setTargets] = useState<DeletableSkill[]>([]);
  const libraryKey = listLibrarySkillsQueryKey({
    query: { query: search, pageSize: RESOURCE_BATCH_SIZE },
  });
  const library = useResourcePages({
    queryKey: libraryKey,
    fetchPage: async (pageNum, signal) => {
      const { data } = await listLibrarySkills(
        withLocalErrorSurface({
          query: { query: search, pageNum, pageSize: RESOURCE_BATCH_SIZE },
          signal,
          throwOnError: true as const,
        }),
      );
      return { items: data.resources ?? [], total: data.totalSize ?? 0 };
    },
    enabled: active && !wakerId && source !== "plugins",
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
      [...libraryKey, "infinite"],
      listWakerSkillsOptions({ path: { wakerId }, query: { query: search } })
        .queryKey,
    ],
    active,
  });
  const query = wakerId ? installed : library;
  const records = wakerId ? (installed.data ?? []) : library.items;
  const skills = records.flatMap((item) => toSkillSummary(item) ?? []);
  const total = wakerId ? skills.length : library.total;
  const window = useResourceWindow(
    skills,
    JSON.stringify([wakerId, search, source]),
  );
  const visible =
    source === "all"
      ? skills.slice(0, RESOURCE_BATCH_SIZE)
      : wakerId
        ? window.visible
        : skills;
  const selection = useResourceSelection(
    source === "plugins"
      ? []
      : visible.filter((item) => item.scope !== "built_in"),
    JSON.stringify([wakerId, search, source]),
  );
  const toggle = useMutation({
    ...withLocalErrorMutation(setWakerSkillEnabledMutation()),
    onSuccess: () => invalidateVisibleSkillQueries(client),
  });
  return (
    <div className="space-y-5">
      <ResourcePageToolbar filters={filters}>
        <ResourcePageLibraryLabel
          installed={t("allSkillsTitle")}
          count={t("resourceCount", {
            count:
              source === "plugins"
                ? pluginCount
                : source === "independent"
                  ? total
                  : total + pluginCount,
          })}
        />
        {source !== "plugins" && (
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        )}
      </ResourcePageToolbar>
      <ResourceSelectionActions
        count={selection.selected.length}
        onClear={selection.clear}
        onDelete={() =>
          setTargets(
            selection.selected.map((item) => ({
              ...item,
              scope: item.scope === "public" ? "public" : "private",
            })),
          )
        }
      />
      {source !== "plugins" &&
        (source === "independent" ||
          query.isPending ||
          query.isError ||
          total > 0) && (
          <section className="space-y-3" aria-label={t("independentResources")}>
            {source === "all" && total > 0 && (
              <ResourceSourceHeading
                title={t("independentResources")}
                count={total}
                onSelect={
                  source === "all"
                    ? () => onSourceChange("independent")
                    : undefined
                }
              />
            )}
            {query.isPending || visible.length === 0 ? (
              <QueryState
                loadingLayout="cards"
                pending={query.isPending}
                error={query.error}
                emptyLabel={t(search ? "noResults" : "empty")}
                onRetry={() => void query.refetch()}
              />
            ) : (
              <ResourceCardGrid>
                {visible.map((skill) => (
                  <SkillCard
                    compact
                    key={skill.id}
                    skill={skill}
                    selected={selection.selectedIds.has(skill.id)}
                    onSelectedChange={
                      skill.scope === "built_in"
                        ? undefined
                        : (selected) =>
                            selection.setSelected(skill.id, selected)
                    }
                    wakerId={wakerId}
                    disabled={toggle.isPending}
                    onEnabledChange={(enabled) =>
                      toggle.mutate({
                        path: { wakerId, skillId: skill.id },
                        body: { enabled },
                      })
                    }
                    onInstall={
                      skill.scope === "built_in"
                        ? undefined
                        : () => onInstall(skill.id, skill.name)
                    }
                    onDelete={
                      skill.scope === "built_in"
                        ? undefined
                        : () =>
                            setTargets([
                              {
                                ...skill,
                                scope:
                                  skill.scope === "public"
                                    ? "public"
                                    : "private",
                              },
                            ])
                    }
                  />
                ))}
              </ResourceCardGrid>
            )}
            {visible.length > 0 &&
              query.isError &&
              (Boolean(wakerId) || !library.isFetchNextPageError) && (
                <QueryRefreshError
                  error={query.error}
                  pending={query.isFetching}
                  onRetry={() => void query.refetch()}
                />
              )}
            {toggle.error && (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(toggle.error, t("failed"))}
              </p>
            )}
            {source === "independent" && (
              <ResourceInfiniteScroll
                hasMore={
                  active &&
                  (wakerId
                    ? window.hasMore
                    : Boolean(library.hasNextPage) && !library.isRefetchError)
                }
                pending={!wakerId && library.isFetching}
                error={
                  !wakerId && library.isFetchNextPageError
                    ? library.error
                    : undefined
                }
                onLoadMore={
                  wakerId ? window.loadMore : () => void library.fetchNextPage()
                }
              />
            )}
          </section>
        )}
      {pluginContent}
      {source === "all" && query.isSuccess && total === 0 && pluginEmpty && (
        <QueryState
          emptyLabel={t(search ? "noResults" : "empty")}
          onRetry={() => void query.refetch()}
        />
      )}
      <SkillImportDialog
        open={adding}
        wakerId={wakerId}
        onOpenChange={onAddingChange}
        onImported={() => {
          void invalidateVisibleSkillQueries(client);
          onAddingChange(false);
        }}
      />
      {targets.length > 0 && (
        <SkillDeleteDialog
          key={targets.map((item) => item.id).join(":")}
          open={targets.length > 0}
          onOpenChange={(open) => {
            if (!open) setTargets([]);
          }}
          skills={targets}
          wakerId={wakerId}
          onDeleted={selection.remove}
        />
      )}
    </div>
  );
}
