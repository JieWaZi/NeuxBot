import { useResourceSelection } from "@/shared/hooks/use-resource-selection";
import {
  ResourceSelection,
  ResourceSelectionActions,
} from "@/shared/ui/resource-selection";
import {
  ResourceBatchDeleteDialog,
  type ResourceDeleteTarget,
} from "@/shared/ui/resource-batch-delete-dialog";
import {
  PageAction,
  PageActionLabel,
  ResourceCardGrid,
  ResourcePageHeader,
  ResourcePageSearchField,
  ResourcePageSurface,
  ResourcePageToolbar,
} from "@/shared/ui/resource-page";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { FolderPlus, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import {
  deleteProjectMutation,
  listProjectsOptions,
  listProjectsQueryKey,
} from "../api/project-queries";
import { ProjectCard } from "../components/project-card";
import { ProjectFormDialog } from "../components/project-form-dialog";
import { WakerDetailLayout } from "@/modules/waker";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { Skeleton } from "@/shared/ui/skeleton";

export function ProjectListPage() {
  const { t } = useTranslation("project");
  const { wakerId = "" } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("search") ?? "";
  const queryClient = useQueryClient();
  const deleteProject = useMutation(
    withLocalErrorMutation(deleteProjectMutation()),
  );
  const [batchTargets, setBatchTargets] = useState<ResourceDeleteTarget[]>([]);
  const [creating, setCreating] = useState(false);
  const query = useQuery(
    listProjectsOptions({
      path: { wakerId },
      query: { pageNum: 1, pageSize: 50, search: search || undefined },
    }),
  );
  const projects = query.data?.resources ?? [];
  const selection = useResourceSelection(
    projects.flatMap((project) =>
      project.id ? [{ ...project, id: project.id }] : [],
    ),
    JSON.stringify([wakerId, search]),
  );

  return (
    <WakerDetailLayout active="projects">
      <ResourcePageSurface>
        <ResourcePageHeader
          title={t("title")}
          description={t("description")}
          actions={
            <>
              <PageAction
                aria-label={t("newProject")}
                onClick={() => setCreating(true)}
              >
                <Plus />
                <PageActionLabel>{t("newProject")}</PageActionLabel>
              </PageAction>
            </>
          }
        />
        <ResourcePageToolbar
          filters={
            <ResourcePageSearchField
              aria-label={t("search")}
              placeholder={t("search")}
              value={search}
              onChange={(event) => {
                const next = new URLSearchParams(searchParams);
                if (event.target.value) next.set("search", event.target.value);
                else next.delete("search");
                setSearchParams(next, { replace: true });
              }}
            />
          }
        >
          <h2 className="type-body font-medium">{t("allProjects")}</h2>
          <span className="text-muted-foreground type-caption">
            {t("count", { count: query.data?.totalSize ?? 0 })}
          </span>
          <ResourceSelection
            checked={selection.checked}
            disabled={selection.disabled}
            onChange={selection.selectAll}
          />
        </ResourcePageToolbar>
        <ResourceSelectionActions
          count={selection.selected.length}
          onClear={selection.clear}
          onDelete={() =>
            setBatchTargets(
              selection.selected.map((project) => ({
                id: project.id,
                name: project.name ?? "",
              })),
            )
          }
        />
        <div className="mt-6">
          {query.isPending ? (
            <ResourceCardGrid>
              <Skeleton className="h-44 rounded-lg" />
              <Skeleton className="h-44 rounded-lg" />
            </ResourceCardGrid>
          ) : query.isError ? (
            <EmptyState
              density="compact"
              title={t("loadError")}
              actions={
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => void query.refetch()}
                >
                  {t("retry")}
                </Button>
              }
            />
          ) : projects.length === 0 ? (
            <EmptyState
              density="compact"
              icon={FolderPlus}
              title={search ? t("noResults") : t("emptyHint")}
            />
          ) : (
            <ResourceCardGrid>
              {projects.map((project) => (
                <ProjectCard
                  key={project.id}
                  project={project}
                  selected={selection.selectedIds.has(project.id ?? "")}
                  onSelectedChange={
                    project.id
                      ? (value) =>
                          selection.setSelected(project.id ?? "", value)
                      : undefined
                  }
                  wakerId={wakerId}
                />
              ))}
            </ResourceCardGrid>
          )}
        </div>
      </ResourcePageSurface>

      {batchTargets.length ? (
        <ResourceBatchDeleteDialog
          key={batchTargets.map((item) => item.id).join(":")}
          items={batchTargets}
          title={t("delete.batchTitle")}
          description={t("delete.description")}
          warning={t("delete.batchWarning")}
          onDelete={(projectId) =>
            deleteProject.mutateAsync({ path: { projectId, wakerId } })
          }
          onDeleted={async (ids) => {
            selection.remove(ids);
            await queryClient.invalidateQueries({
              queryKey: listProjectsQueryKey({ path: { wakerId } }),
            });
          }}
          onOpenChange={(open) => {
            if (!open) setBatchTargets([]);
          }}
        />
      ) : null}
      <ProjectFormDialog
        open={creating}
        onOpenChange={setCreating}
        wakerId={wakerId}
        onSaved={(projectId) => {
          if (projectId) {
            navigate(`/wakers/${wakerId}/projects/${projectId}`);
          }
        }}
      />
    </WakerDetailLayout>
  );
}
