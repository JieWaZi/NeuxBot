import { useQuery } from "@tanstack/react-query";
import { Folder, Pencil, Trash2, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";

import { getProjectOptions } from "../api/project-queries";
import { ProjectFormDialog } from "../components/project-form-dialog";
import { WakerDetailLayout } from "@/modules/waker";
import { PageHeader } from "@/shared/ui/page-header";
import { PageAction } from "@/shared/ui/resource-page";
import { Badge } from "@/shared/ui/badge";
import { QueryState } from "@/shared/ui/query-state";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { ProjectDeleteDialog } from "../components/project-delete-dialog";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";

export function ProjectDetailPage() {
  const { t } = useTranslation("project");
  const { projectId = "", wakerId = "" } = useParams();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const query = useQuery(
    getProjectOptions(withLocalErrorSurface({ path: { projectId, wakerId } })),
  );
  const project = query.data;

  return (
    <WakerDetailLayout active="projects">
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          backAction={{
            label: t("backToList"),
            onClick: () => navigate(`/wakers/${wakerId}/projects`),
          }}
          title={
            <span className="[overflow-wrap:anywhere]">
              {project?.name ?? t("title")}
            </span>
          }
          description={project ? project.brief || t("noBrief") : undefined}
          actions={
            <>
              <PageAction
                disabled={!project}
                intent="edit"
                label={t("editProject")}
                onClick={() => setEditing(true)}
              >
                <Pencil />
              </PageAction>
              <PageAction
                intent="delete"
                disabled={!project}
                label={t("deleteProject")}
                onClick={() => setDeleting(true)}
              >
                <Trash2 />
              </PageAction>
            </>
          }
        />

        <div className="mt-6 grid gap-6">
          {!project ? (
            <QueryState
              pending={query.isPending}
              error={query.error}
              emptyLabel={t("notFound")}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <>
              {query.isError ? (
                <QueryState
                  error={query.error}
                  onRetry={() => void query.refetch()}
                />
              ) : null}
              {!project.available ? (
                <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
                  <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
                  <p>{t("unavailableWarning")}</p>
                </div>
              ) : null}

              <section className="min-w-0">
                <h2 className="type-title font-semibold">
                  {t("directoriesTitle")}
                </h2>
                <ul className="mt-4 grid min-w-0 gap-2">
                  {(project.sources ?? []).map((source) => (
                    <li
                      key={source.id}
                      className="bg-muted/60 min-h-row-rich grid min-w-0 grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-3 py-2"
                    >
                      <Folder
                        className="text-muted-foreground size-icon-md"
                        strokeWidth={1.5}
                        aria-hidden="true"
                      />
                      <div className="min-w-0">
                        <p className="type-body font-medium [overflow-wrap:anywhere]">
                          {source.name}
                        </p>
                        <p className="text-muted-foreground type-caption [overflow-wrap:anywhere]">
                          {source.path}
                        </p>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        {source.isDefault ? (
                          <Badge>{t("defaultDirectory")}</Badge>
                        ) : null}
                        <span className="text-muted-foreground type-caption whitespace-nowrap">
                          {source.loadInstructions
                            ? t("instructionsEnabled")
                            : t("instructionsDisabled")}
                        </span>
                        {!source.available ? (
                          <span className="text-destructive type-caption whitespace-nowrap">
                            {t("sourceUnavailable")}
                          </span>
                        ) : null}
                      </div>
                    </li>
                  ))}
                </ul>
              </section>
            </>
          )}
        </div>
      </ResourcePageSurface>

      {project ? (
        <ProjectDeleteDialog
          open={deleting}
          onOpenChange={setDeleting}
          project={project}
          wakerId={wakerId}
          onDeleted={() => navigate(`/wakers/${wakerId}/projects`)}
        />
      ) : null}
      {project ? (
        <ProjectFormDialog
          open={editing}
          onOpenChange={setEditing}
          project={project}
          wakerId={wakerId}
        />
      ) : null}
    </WakerDetailLayout>
  );
}
