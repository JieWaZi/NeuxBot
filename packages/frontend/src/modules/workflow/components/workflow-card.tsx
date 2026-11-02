import { MoreHorizontal, Pencil, Trash2, Workflow } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import { Card, CardBody, CardFooter } from "@/shared/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listWorkflowsQueryKey } from "../api/workflow-queries";
import { BasicInformationDialog } from "./basic-information-dialog";
import { WorkflowDeleteDialog } from "./workflow-delete-dialog";

export function WorkflowCard({
  definition,
}: {
  definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;
}) {
  const { t } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const id = definition.id ?? "";
  const revisionId = definition.currentRevisionId ?? "";
  const title = definition.title || t("newTitle");

  useQueryEvents({
    topic: "workflow",
    resource: { kind: "workflow", id },
    active,
    enabled: Boolean(id),
    queryKeys: [listWorkflowsQueryKey()],
    eventTypes: ["workflow.definition.", "workflow.revision.saved"],
  });

  return (
    <>
      <Card asChild variant="resource">
        <article>
          <CardBody asChild>
            <Link
              to={`/workflows/${id}`}
              className="focus-visible:ring-ring duration-quick grid min-h-32 min-w-0 grid-cols-[40px_minmax(0,1fr)] items-start gap-4 p-5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-inset motion-reduce:transition-none"
            >
              <Workflow
                className="text-muted-foreground size-icon-lg"
                aria-hidden="true"
                strokeWidth={1.5}
              />
              <div className="min-w-0">
                <h2 className="type-label font-medium">{title}</h2>
                {definition.description ? (
                  <p className="text-muted-foreground type-body mt-2 line-clamp-3">
                    {definition.description}
                  </p>
                ) : null}
              </div>
            </Link>
          </CardBody>
          <CardFooter className="grid grid-cols-[minmax(0,1fr)_44px] gap-0 px-0">
            <div className="text-muted-foreground type-caption flex min-w-0 items-center px-5">
              <span>
                {definition.ordinal
                  ? t("version", { version: definition.ordinal })
                  : t("draft")}
              </span>
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  aria-label={t("moreActions", { title })}
                  className="mx-auto"
                  size="icon"
                  variant="ghost"
                >
                  <MoreHorizontal aria-hidden="true" strokeWidth={1.5} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  disabled={!revisionId}
                  onSelect={() => setEditing(true)}
                >
                  <Pencil aria-hidden="true" />
                  {t("edit")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  onSelect={() => setDeleting(true)}
                >
                  <Trash2 aria-hidden="true" />
                  {t("deleteWorkflow")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </CardFooter>
        </article>
      </Card>
      {editing && revisionId ? (
        <BasicInformationDialog
          key={revisionId}
          id={id}
          revisionId={revisionId}
          title={title}
          description={definition.description ?? ""}
          open={active && editing}
          onOpenChange={setEditing}
        />
      ) : null}
      {deleting ? (
        <WorkflowDeleteDialog
          id={id}
          revisionId={revisionId}
          open={active && deleting}
          onOpenChange={setDeleting}
        />
      ) : null}
    </>
  );
}
