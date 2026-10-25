import {
  PageAction,
  PageActionLabel,
  ResourceCardGrid,
  ResourcePageHeader,
  ResourcePageToolbar,
  ResourcePageLibraryLabel,
  ResourcePageSearchField,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams, useSearchParams } from "react-router-dom";

import { WakerDetailLayout } from "@/modules/waker";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";

import {
  invalidateVisibleSkillQueries,
  listWakerSkillsOptions,
  setWakerSkillEnabledMutation,
} from "../api/skill-queries";
import { SkillCard } from "../components/skill-card";
import {
  SkillDeleteDialog,
  type DeletableSkill,
} from "../components/skill-delete-dialog";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import { SkillImportDialog } from "../components/skill-import-dialog";
import { toSkillSummary } from "../model/skill-view";
import type { SkillScope } from "../model/types";

type StatusFilter = "all" | "enabled" | "disabled";
type ScopeFilter = "all" | SkillScope;

export function WakerSkillsPage() {
  const { t } = useTranslation("skill");
  const { wakerId = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const search = searchParams.get("search") ?? "";
  const status = statusFilterOf(searchParams.get("status"));
  const scope = scopeFilterOf(searchParams.get("scope"));
  const [importOpen, setImportOpen] = useState(false);
  const [deleteTargets, setDeleteTargets] = useState<DeletableSkill[]>([]);
  const listOptions = listWakerSkillsOptions(
    withLocalErrorSurface({ path: { wakerId } }),
  );
  const skillsQuery = useQuery({ ...listOptions, enabled: Boolean(wakerId) });
  const toggleSkill = useMutation({
    ...withLocalErrorMutation(setWakerSkillEnabledMutation()),
    onSuccess: () => invalidateVisibleSkillQueries(queryClient),
  });
  const skills = useMemo(
    () =>
      (skillsQuery.data ?? []).flatMap((item) => toSkillSummary(item) ?? []),
    [skillsQuery.data],
  );
  const visibleSkills = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase();
    return skills.filter((skill) => {
      const matchesSearch = normalizedSearch
        ? `${skill.name} ${skill.description}`
            .toLocaleLowerCase()
            .includes(normalizedSearch)
        : true;
      const matchesStatus =
        status === "all" ||
        (status === "enabled" ? skill.enabled : !skill.enabled);
      const matchesScope = scope === "all" || skill.scope === scope;
      return matchesSearch && matchesStatus && matchesScope;
    });
  }, [search, scope, skills, status]);
  const uninstallableVisibleSkills = visibleSkills.filter(
    (skill) => skill.scope !== "built_in",
  );
  const selection = useResourceSelection(
    uninstallableVisibleSkills,
    JSON.stringify([wakerId, search, status, scope]),
  );
  const selectedSkills = selection.selected;
  const selectedSkillIds = selection.selectedIds;
  const clearSelection = selection.clear;
  const setSkillSelected = selection.setSelected;
  const refreshSkills = () => invalidateVisibleSkillQueries(queryClient);
  const updateSearchParam = (key: string, value: string, defaultValue = "") => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (!value || value === defaultValue) next.delete(key);
      else next.set(key, value);
      return next;
    });
  };

  return (
    <WakerDetailLayout active="skills">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <>
              <PageAction
                aria-label={t("add")}
                onClick={() => setImportOpen(true)}
              >
                <Plus />
                <PageActionLabel>{t("add")}</PageActionLabel>
              </PageAction>
            </>
          }
        />

        <ResourcePageToolbar
          filters={
            <>
              <ResourceFilterSelect
                value={status}
                onValueChange={(value) => {
                  updateSearchParam("status", value, "all");
                  clearSelection();
                }}
                ariaLabel={t("statusFilter")}
                options={[
                  { value: "all", label: t("allStatus") },
                  { value: "enabled", label: t("enabled") },
                  { value: "disabled", label: t("disabled") },
                ]}
              />
              <ResourceFilterSelect
                value={scope}
                onValueChange={(value) => {
                  updateSearchParam("scope", value, "all");
                  clearSelection();
                }}
                ariaLabel={t("scopeFilter")}
                options={(
                  ["all", "built_in", "public", "private"] as const
                ).map((value) => ({
                  value,
                  label: value === "all" ? t("allScopes") : t(`scope.${value}`),
                }))}
              />
              <ResourcePageSearchField
                value={search}
                onChange={(event) => {
                  updateSearchParam("search", event.target.value);
                  clearSelection();
                }}
                placeholder={t("search")}
                aria-label={t("search")}
              />
            </>
          }
        >
          <ResourcePageLibraryLabel
            installed={t("installed")}
            count={t("count", { count: visibleSkills.length })}
          />
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        </ResourcePageToolbar>

        {toggleSkill.isError ? (
          <p role="alert" className="text-destructive type-body mt-3">
            {localApiErrorMessage(toggleSkill.error, t("toggleFailed"))}
          </p>
        ) : null}

        <ResourceSelectionActions
          count={selectedSkills.length}
          onClear={selection.clear}
          onDelete={() =>
            setDeleteTargets(
              selectedSkills.map((skill) => ({
                affectedWakerCount: skill.affectedWakerCount,
                id: skill.id,
                name: skill.name,
                scope: skill.scope === "public" ? "public" : "private",
              })),
            )
          }
        />

        {skillsQuery.isPending ? (
          <ResourceLoadingState layout="cards" className="mt-6" />
        ) : skillsQuery.isError ? (
          <EmptyState
            className="mt-6"
            density="compact"
            title={t("loadError")}
            actions={
              <Button
                size="sm"
                variant="secondary"
                onClick={() => void skillsQuery.refetch()}
              >
                {t("common:retry")}
              </Button>
            }
          />
        ) : visibleSkills.length > 0 ? (
          <ResourceCardGrid className="mt-6">
            {visibleSkills.map((skill) => (
              <SkillCard
                key={skill.id}
                skill={skill}
                wakerId={wakerId}
                disabled={toggleSkill.isPending}
                selected={selectedSkillIds.has(skill.id)}
                onSelectedChange={
                  skill.scope === "built_in"
                    ? undefined
                    : (selected) => setSkillSelected(skill.id, selected)
                }
                onDelete={
                  skill.scope === "built_in"
                    ? undefined
                    : () =>
                        setDeleteTargets([
                          {
                            affectedWakerCount: skill.affectedWakerCount,
                            id: skill.id,
                            name: skill.name,
                            scope:
                              skill.scope === "public" ? "public" : "private",
                          },
                        ])
                }
                onEnabledChange={(enabled) => {
                  toggleSkill.mutate({
                    path: { wakerId, skillId: skill.id },
                    body: { enabled },
                  });
                }}
              />
            ))}
          </ResourceCardGrid>
        ) : (
          <EmptyState
            className="mt-6"
            density="compact"
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        )}
      </ResourcePageSurface>

      <SkillImportDialog
        open={importOpen}
        wakerId={wakerId}
        onImported={() => void refreshSkills()}
        onOpenChange={setImportOpen}
      />
      {deleteTargets.length > 0 ? (
        <SkillDeleteDialog
          key={deleteTargets.map((skill) => skill.id).join(":")}
          open
          skills={deleteTargets}
          wakerId={wakerId}
          onOpenChange={(open) => {
            if (!open) setDeleteTargets([]);
          }}
          onDeleted={selection.remove}
        />
      ) : null}
    </WakerDetailLayout>
  );
}

function statusFilterOf(value: string | null): StatusFilter {
  return value === "enabled" || value === "disabled" ? value : "all";
}

function scopeFilterOf(value: string | null): ScopeFilter {
  return value === "built_in" || value === "public" || value === "private"
    ? value
    : "all";
}
