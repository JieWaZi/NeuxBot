package terminals

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 关闭交互终端
type CloseTerminal struct {
	// ginx.MethodDelete 声明接口方法。
	ginx.MethodDelete
	// 内容标识
	OwnerKey string `in:"query" name:"ownerKey" validate:"required,max=256"`
}

func (*CloseTerminal) Path() string { return "" }
func (op *CloseTerminal) Output(ctx *gin.Context) (interface{}, error) {
	if global.TerminalRuntime != nil {
		if err := global.TerminalRuntime.CloseOwner(middleware.AuthSubject(ctx), op.OwnerKey); err != nil {
			return nil, status_error.TerminalInvalid
		}
	}
	return ginx.CommonSuccessResponse(), nil
}
