import { Workflow } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listWorkflowsQueryKey } from "../api/workflow-queries";

export function WorkflowCard({
  definition,
}: {
  definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;
}) {
  const { t } = useTranslation("workflow");
  const active = useRouteSceneActive();
  const id = definition.id ?? "";
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
    <ResourceListItem
      appearance="row"
      title={title}
      description={definition.description}
      icon={
        <span
          className="bg-resource-surface text-resource-muted size-resource-icon grid shrink-0 place-items-center rounded-lg"
          aria-hidden="true"
        >
          <Workflow className="size-resource-logo" strokeWidth={1.5} />
        </span>
      }
      to={`/workflows/${id}`}
      label={title}
    />
  );
}
