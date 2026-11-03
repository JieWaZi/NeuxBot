// Package workflows 提供源码权威的工作流定义、创作及持久运行应用入口。
package workflows

import "github.com/shrewx/ginx"

// Router 是复用 Core 本地认证的工作流管理路由。
var Router = ginx.NewRouter(ginx.Group("/workflows"))

func init() {
	Router.Register(&UploadWorkflowFile{})
	Router.Register(&GetWorkflowTriggers{})
	Router.Register(&SaveWorkflowTriggers{})
	Router.Register(&EditWorkflowConfiguration{})
	Router.Register(&DeleteWorkflow{})
	Router.Register(&DeleteWorkflowRevision{})
	Router.Register(&DeleteWorkflowRun{})
	Router.Register(&RetryWorkflowRun{})
	Router.Register(&GetWorkflowRetryPlan{})
	Router.Register(&SkipWorkflowWorker{})
	Router.Register(&GetWorkflowInstance{})
	Router.Register(&SendWorkflowCreationMessage{})
	Router.Register(&CreateWorkflowAnnotation{})
	Router.Register(&StartWorkflowRun{})
	Router.Register(&ListWorkflowRuns{})
	Router.Register(&GetWorkflowRun{})
	Router.Register(&AnswerWorkflowHuman{})
	Router.Register(&AnswerWorkflowOperation{})
	Router.Register(&CancelWorkflowRun{})
	Router.Register(&ListWorkflowEvents{})
	Router.Register(&GetWorkflowSDK{})
	Router.Register(&ListWorkflows{})
	Router.Register(&CreateWorkflow{})
	Router.Register(&GetWorkflow{})
	Router.Register(&GetWorkflowSource{})
	Router.Register(&ValidateWorkflowSource{})
	Router.Register(&SaveWorkflowSource{})
	Router.Register(&ListWorkflowRevisions{})
	Router.Register(&RestoreWorkflowRevision{})
}
