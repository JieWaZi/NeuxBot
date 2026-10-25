package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// VerifyIMLogin 提交验证码。
type VerifyIMLogin struct {
	ginx.MethodPost
	// 临时认证标识
	LoginID string `in:"path" name:"loginId" validate:"required"`
	// 一次性验证码
	Body VerifyIMLoginRequest `in:"body"`
}

func (*VerifyIMLogin) Path() string { return "/logins/:loginId/verify" }
func (o *VerifyIMLogin) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.IMService.VerifyLogin(o.LoginID, o.Body.Code); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
