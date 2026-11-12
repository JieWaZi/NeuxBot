package workspace_attention

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetWorkspaceAttention 返回待操作与待查看状态。
type GetWorkspaceAttention struct {
	// MethodGet 声明只读请求。
	ginx.MethodGet
}

func (*GetWorkspaceAttention) Path() string { return "" }
func (*GetWorkspaceAttention) Output(ctx *gin.Context) (interface{}, error) {
	attention := global.WorkspaceAttention
	filter := controllers.WorkBoardQuery{}
	if attention.Isolated != nil {
		filter.IsolatedChats = attention.Isolated()
	}
	var value models.WorkspaceAttentionSnapshot
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		var err error
		value, err = global.WorkBoardControllerManager.HasAttention(tx, constants.LocalReaderID, filter)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	value.AttentionCompleteness, err = attention.Completeness(ctx.Request.Context(), filter, value.IncompleteReason)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
