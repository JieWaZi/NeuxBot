package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetIMLogin 获取扫码进度。
type GetIMLogin struct {
	ginx.MethodGet
	// 临时认证标识
	LoginID string `in:"path" name:"loginId" validate:"required"`
}

func (*GetIMLogin) Path() string { return "/logins/:loginId" }
func (o *GetIMLogin) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.IMService.Login(o.LoginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
