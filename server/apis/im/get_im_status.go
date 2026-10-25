package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetIMStatus 获取运行策略。
type GetIMStatus struct {
	ginx.MethodGet
}

func (*GetIMStatus) Path() string { return "/status" }
func (o *GetIMStatus) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.IMService.KeepAwake(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GetIMStatusResponse{KeepAwake: value}, nil
}
