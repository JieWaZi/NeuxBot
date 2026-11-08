import { useTranslation } from "react-i18next";
import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition } from "@/shared/api/generated/types.gen";
import { ResourceListItem } from "@/shared/ui/resource-list-item";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listWorkflowsQueryKey } from "../api/workflow-queries";

export function WorkflowCard({
  definition,
  category,
}: {
  definition: GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition;
  category?: string;
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
        <ResourceCategoryIcon
          category={category ?? definition.categories?.[0]}
        />
      }
      to={`/workflows/${id}`}
      label={title}
    />
  );
}
