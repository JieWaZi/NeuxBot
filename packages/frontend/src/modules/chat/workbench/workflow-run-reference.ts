import type { WorkbenchCommand } from "./workbench-model";

export const workflowRunContentType = "workflow-run";

type RunTarget = {
  definitionId: string;
  runId: string;
  instanceId?: string;
  name: string;
  chatId?: string;
};

export function openWorkflowRunTab({
  definitionId,
  runId,
  instanceId,
  name,
  chatId,
}: RunTarget): WorkbenchCommand {
  return {
    type: "open-content",
    contentId: `${workflowRunContentType}:${runId}`,
    title: name,
    reference: {
      type: workflowRunContentType,
      ids: {
        definitionId,
        runId,
        ...(instanceId ? { instanceId } : {}),
        ...(chatId ? { chatId } : {}),
      },
    },
  };
}
