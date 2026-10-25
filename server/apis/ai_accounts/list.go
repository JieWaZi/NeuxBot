package ai_accounts

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 账号列表
type ListAIAccounts struct {
	// 声明账号列表使用 GET 查询。
	ginx.MethodGet
}

// 账号列表响应
type ListAIAccountsResponse struct {
	// 资源列表
	Resources []models.AIAccount `json:"resources"`
}

// Path 返回账号列表路径。
func (*ListAIAccounts) Path() string { return "" }

// Output 返回不含凭据的账号列表。
func (*ListAIAccounts) Output(ctx *gin.Context) (interface{}, error) {
	accounts, err := global.AIAccountControllerManager.List(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListAIAccountsResponse{Resources: accounts}, nil
}
