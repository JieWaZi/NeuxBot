package ai_accounts

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetAIAccountNativeModels 返回当前订阅账号原生目录中的模型。
type GetAIAccountNativeModels struct {
	// MethodGet 声明原生模型目录是只读查询。
	ginx.MethodGet
	// AccountID 是当前选择的订阅账号。
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// RuntimeID 是聊天当前选择的运行环境。
	RuntimeID string `in:"query" name:"runtimeId" validate:"required,max=120"`
}

// GetAIAccountNativeModelsResponse 是订阅账号本地模型目录。
type GetAIAccountNativeModelsResponse struct {
	// Models 是该账号原生配置可选的模型标识。
	Models []string `json:"models"`
	// DefaultModel 是该账号原生配置声明的默认模型。
	DefaultModel string `json:"defaultModel"`
}

func (*GetAIAccountNativeModels) Path() string { return "/:accountId/models/native" }

func (a *GetAIAccountNativeModels) Output(ctx *gin.Context) (interface{}, error) {
	account, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	environment, err := global.RuntimeEnvironmentControllerManager.Get(ctx, strings.TrimSpace(a.RuntimeID))
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.AuthMode != constants.AccountAuthModeSubscription || !environment.Enabled || len(account.Endpoints) != 1 || !accountconfig.Supports(environment.BaseRuntimeID, account.Endpoints[0].Protocol) {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}
	if !agent.SupportsProvider(environment.BaseRuntimeID) {
		return nil, ginx.WithStack(status_error.RuntimePolicyUnsupported)
	}
	result, err := global.RuntimeEnvironmentService.ReadModelCatalog(ctx, environment.RuntimeID, account.ID, false)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := GetAIAccountNativeModelsResponse{Models: []string{}}
	if !accountCatalogAvailable(result, account.ID) {
		return response, nil
	}
	for _, model := range result.Models {
		if model.ID != "" {
			response.Models = append(response.Models, model.ID)
		}
	}
	response.DefaultModel = result.DefaultID
	return response, nil
}

// The existing model endpoints may serve the last successful read for this exact account.
func accountCatalogAvailable(result runtimeenvironment.AccountModelCatalog, accountID string) bool {
	return result.AccountID == accountID && (result.State == agent.CatalogFresh || result.State == agent.CatalogStale)
}
