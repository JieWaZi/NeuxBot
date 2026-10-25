package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取对话节点产出
type GetWorkflowChatResults struct {
	// MethodGet 声明产出只读查询使用 GET 请求。
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"query" name:"chatId" validate:"required,max=32"`
}

func (*GetWorkflowChatResults) Path() string { return "/chat-results" }
func (o *GetWorkflowChatResults) Output(ctx *gin.Context) (interface{}, error) {
	effects, err := global.WorkflowRunControllerManager.ListChatAttempts(ctx, o.ChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	refs := []WorkflowNodeResultReference{}
	for _, effect := range effects {
		if effect.Status != constants.WorkflowStepStatusSucceeded || effect.Result == nil || effect.FinalMessageID == "" {
			continue
		}
		run, err := global.WorkflowRunControllerManager.GetRun(ctx, effect.RunID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		revision, err := global.WorkflowControllerManager.GetRevision(ctx, run.DefinitionID, run.RevisionID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if presentation, exists := nodePresentation(revision, effect.Boundary.NodeID); exists {
			refs = append(refs, WorkflowNodeResultReference{WorkflowID: run.DefinitionID, RunID: run.ID, InstanceID: effect.InstanceID, AttemptID: effect.ID, MessageID: effect.FinalMessageID, Title: presentation.title()})
		}
	}
	return refs, nil
}
