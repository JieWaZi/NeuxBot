import { lazy } from "react";

export {
  WorkflowListPage,
  WorkflowResourceCollection,
} from "./pages/workflow-list-page";
export { WorkflowDetailPage } from "./pages/workflow-detail-page";
export { WorkflowRequestLoadingState } from "./components/query-feedback";
export { workflowStatusAppearance } from "./model/status-presentation";

export const EmbeddedWorkflowRunView = lazy(() =>
  import("./components/run-view").then((module) => ({
    default: module.WorkflowRunView,
  })),
);

export const WorkflowChatQuestion = lazy(() =>
  import("./components/human-request").then((module) => ({
    default: module.WorkflowChatQuestion,
  })),
);

export const WorkflowOperationRequestCard = lazy(() =>
  import("./components/operation-request").then((module) => ({
    default: module.OperationRequestCard,
  })),
);

export const WorkflowWorkerRequest = lazy(() =>
  import("./components/instance-details").then((module) => ({
    default: module.InstanceDetails,
  })),
);

export const WorkflowRunPage = lazy(() =>
  import("./pages/workflow-run-page").then((module) => ({
    default: module.WorkflowRunPage,
  })),
);
