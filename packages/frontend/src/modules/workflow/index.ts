import { lazy } from "react";

export {
  WorkflowListPage,
  WorkflowResourceCollection,
} from "./pages/workflow-list-page";
export { WorkflowDetailPage } from "./pages/workflow-detail-page";

export const WorkflowRunPage = lazy(() =>
  import("./pages/workflow-run-page").then((module) => ({
    default: module.WorkflowRunPage,
  })),
);

export {
  WorkflowResultContent,
  WorkflowResultMessageScope,
} from "./presentation/node-result";
