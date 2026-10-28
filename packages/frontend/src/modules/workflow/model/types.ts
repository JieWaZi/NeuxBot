export type {
  GithubComJieWaZiNeuxBotServerRepositoriesControllersWorkflowNodeCount as WorkflowNodeCount,
  GithubComJieWaZiNeuxBotServerInternalWorkflowDslCanvasManifest as Manifest,
  GithubComJieWaZiNeuxBotServerInternalWorkflowDslCanvasNode as WorkflowNode,
  GithubComJieWaZiNeuxBotServerInternalWorkflowDslCanvasPhase as WorkflowPhase,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowRun as WorkflowRun,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowNodeInstance as WorkflowInstance,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowHumanRequest as HumanRequest,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowDefinition as WorkflowDefinition,
  GithubComJieWaZiNeuxBotServerApisWorkflowsWorkflowRevisionResponse as WorkflowRevision,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWorkflowExecutionDefaults as ExecutionDefaults,
} from "@/shared/api/generated/types.gen";

export function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value))
    : undefined;
}
export function jsonText(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? "null";
}
