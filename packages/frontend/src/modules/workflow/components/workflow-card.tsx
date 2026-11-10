import { MoreHorizontal } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listWorkflowsQueryKey } from "../api/workflow-queries";
import { WorkflowDeleteDialog } from "./workflow-delete-dialog";

export function WorkflowCard({
  definition,
  category,
}: {
  definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;
  category?: string;
}) {
  const { t } = useTranslation("workflow");
  const location = useLocation();
  const active = useRouteSceneActive();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const id = definition.id ?? "";
  const title = definition.title || t("newTitle");
  const moreActions = t("moreActionsLabel");

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
      <ResourceListItem
        appearance="row"
        title={title}
        description={definition.description}
        icon={
          <ResourceCategoryIcon
            category={category ?? definition.categories?.[0]}
          />
        }
        to={`/workflows/${id}`}
        label={title}
        actions={
          <TooltipProvider>
            <DropdownMenu>
              <Tooltip>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={moreActions}
                    >
                      <MoreHorizontal aria-hidden="true" strokeWidth={1.5} />
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent>{moreActions}</TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end">
                <DropdownMenuItem asChild>
                  <Link
                    to={`/workflows/${id}`}
                    state={{
                      resourceBack: location.pathname + location.search,
                    }}
                  >
                    {t("details")}
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant="destructive"
                  onSelect={() => setDeleteOpen(true)}
                >
                  {t("common:delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </TooltipProvider>
        }
      />
      <WorkflowDeleteDialog
        id={id}
        revisionId={definition.currentRevisionId ?? ""}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />
    </>
  );
}
