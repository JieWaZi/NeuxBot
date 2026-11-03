package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取工作流 SDK
type GetWorkflowSDK struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
}

// 工作流 SDK
type WorkflowSDKResponse struct {
	// SDK 标识
	SDK string `json:"sdk"`
	// 类型声明
	Declarations string `json:"declarations"`
	// 创作知识
	Knowledge string `json:"knowledge"`
	// 宿主契约
	Primitives []workflowdsl.PrimitiveContract `json:"primitives"`
}

func (*GetWorkflowSDK) Path() string { return "/sdk" }
func (*GetWorkflowSDK) Output(*gin.Context) (interface{}, error) {
	return WorkflowSDKResponse{SDK: constants.WorkflowSDKIdentifier, Declarations: workflowdsl.SDKArtifact("workflow.d.ts"), Knowledge: workflowdsl.SDKArtifact("creation-knowledge.md"), Primitives: workflowdsl.Registry()}, nil
}
