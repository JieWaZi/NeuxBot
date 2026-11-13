package workspace_attention

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetExecutorAttention 分批查询原执行对象的关注与会话摘要。
type GetExecutorAttention struct {
	// MethodGet 声明只读请求。
	ginx.MethodGet
	// ExecutorIDs 是逗号分隔的原执行对象标识。
	ExecutorIDs string `in:"query" name:"executorIds" validate:"required,max=4223"`
}

// ExecutorAttentionResponse 返回同一快照内的执行对象摘要。
type ExecutorAttentionResponse struct {
	// Resources 是指定执行对象的摘要。
	Resources []models.ExecutorAttentionSummary `json:"resources"`
	// AttentionCompleteness 报告本次查询的投影完整性。
	models.AttentionCompleteness
}

func (*GetExecutorAttention) Path() string { return "/executors" }
func (d *GetExecutorAttention) Validate(*gin.Context) error {
	for _, id := range strings.Split(d.ExecutorIDs, ",") {
		if len(id) != 32 {
			return status_error.BadRequest
		}
	}
	return nil
}
func (d *GetExecutorAttention) Output(ctx *gin.Context) (interface{}, error) {
	resources, complete, err := global.WorkspaceAttention.ExecutorSummaries(ctx.Request.Context(), constants.LocalReaderID, strings.Split(d.ExecutorIDs, ","))
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ExecutorAttentionResponse{Resources: resources, AttentionCompleteness: complete}, nil
}
