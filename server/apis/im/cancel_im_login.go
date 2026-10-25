package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// CancelIMLogin 取消扫码登录。
type CancelIMLogin struct {
	ginx.MethodDelete
	// 临时认证标识
	LoginID string `in:"path" name:"loginId" validate:"required"`
}

func (*CancelIMLogin) Path() string { return "/logins/:loginId" }
func (o *CancelIMLogin) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.IMService.CancelLogin(o.LoginID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
