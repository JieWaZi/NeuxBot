import { Card, CardBody, CardFooter } from "@/shared/ui/card";
import { Checkbox } from "@/shared/ui/checkbox";
import { Folder, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import { ProjectDeleteDialog } from "./project-delete-dialog";
import { ProjectFormDialog } from "./project-form-dialog";
import type { Project } from "../model/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Button } from "@/shared/ui/button";

type Props = {
  project: Project;
  selected?: boolean;
  onSelectedChange?: (selected: boolean) => void;
  wakerId: string;
};

export function ProjectCard({
  project,
  wakerId,
  selected = false,
  onSelectedChange,
}: Props) {
  const { t } = useTranslation("project");
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  return (
    <>
      <Card
        asChild
        variant="resource"
        data-state={selected ? "selected" : undefined}
      >
        <article>
          <CardBody className="relative">
            <Link
              to={`/wakers/${wakerId}/projects/${project.id}`}
              aria-label={t("viewProject", { name: project.name })}
              title={[project.name, project.brief].filter(Boolean).join("\n")}
              className="focus-visible:ring-ring absolute inset-0 z-10 rounded-t-xl outline-none focus-visible:ring-2 focus-visible:ring-inset"
            />
            <div className="grid min-h-32 grid-cols-[40px_minmax(0,1fr)_auto] gap-4 p-5">
              <span className="bg-muted text-muted-foreground grid size-10 place-items-center rounded-md">
                <Folder className="size-icon-md" strokeWidth={1.5} />
              </span>
              <span className="min-w-0">
                <span className="flex items-center gap-2">
                  <strong
                    className="type-label min-w-0 truncate font-medium"
                    title={project.name}
                  >
                    {project.name}
                  </strong>
                  <span
                    className={
                      project.available
                        ? "bg-success/12 text-success type-caption rounded-xs px-1.5 py-0.5"
                        : "bg-destructive/10 text-destructive type-caption rounded-xs px-1.5 py-0.5"
                    }
                  >
                    {project.available ? t("available") : t("unavailable")}
                  </span>
                </span>
                <span className="text-muted-foreground type-caption mt-1 block">
                  {t("directoryCount", { count: project.sources?.length ?? 0 })}
                </span>
                <span className="text-muted-foreground type-body mt-2 block truncate">
                  {project.brief || t("noBrief")}
                </span>
              </span>
              {onSelectedChange ? (
                <Checkbox
                  className="relative z-20"
                  size="sm"
                  checked={selected}
                  aria-label={t("selectProject", { name: project.name })}
                  onCheckedChange={(checked) =>
                    onSelectedChange(checked === true)
                  }
                />
              ) : null}
            </div>
          </CardBody>

          <CardFooter className="grid grid-cols-[minmax(0,1fr)_44px] gap-0 px-0">
            <span className="text-muted-foreground type-caption truncate px-4">
              {t("createdAt", {
                date: project.createdAt
                  ? new Intl.DateTimeFormat("zh-CN").format(
                      new Date(project.createdAt),
                    )
                  : t("today"),
              })}
            </span>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  aria-label={t("moreActions")}
                  className="mx-auto"
                  size="icon"
                  variant="ghost"
                >
                  <MoreHorizontal className="size-icon-md" strokeWidth={1.5} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setEditing(true)}>
                  <Pencil />
                  {t("editProject")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive"
                  onSelect={() => setDeleting(true)}
                >
                  <Trash2 />
                  {t("deleteProject")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </CardFooter>
        </article>
      </Card>

      <ProjectFormDialog
        open={editing}
        onOpenChange={setEditing}
        project={project}
        wakerId={wakerId}
      />
      <ProjectDeleteDialog
        open={deleting}
        onOpenChange={setDeleting}
        project={project}
        wakerId={wakerId}
      />
    </>
  );
}
