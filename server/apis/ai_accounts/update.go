package ai_accounts

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新账号请求
type UpdateAIAccountRequest struct {
	// 账号名称
	Name string `json:"name" validate:"required,max=120"`

	// 协议入口
	Endpoints []models.AIAccountEndpoint `json:"endpoints" validate:"required,len=1,dive"`
	// 最近一次获取的模型目录
	ModelCatalog []string `json:"modelCatalog" validate:"max=500,dive,max=200"`
	// 用户选择的账号模型
	SelectedModels []string `json:"selectedModels" validate:"max=500,dive,max=200"`
	// 自定义模型
	CustomModels []string `json:"customModels" validate:"max=50,dive,max=200"`
	// Kimi 模型的用户自定义上下文长度。
	ModelContextSizes map[string]int `json:"modelContextSizes" validate:"max=500,dive,min=1,max=4000000"`
	// 模型显示名称，以实际模型 ID 为键。
	ModelDisplayNames map[string]string `json:"modelDisplayNames" validate:"max=500,dive,max=200"`
	// 原生推理等级，以实际模型 ID 为键。
	ModelReasoningLevels map[string][]string `json:"modelReasoningLevels" validate:"max=500"`
	// Claude Code 各模型角色使用的实际请求模型 ID。
	ClaudeModelRoles map[string]string `json:"claudeModelRoles" validate:"max=5,dive,max=200"`
	// 默认模型
	DefaultModel string `json:"defaultModel" validate:"max=200"`
	// 默认推理强度
	DefaultReasoning string `json:"defaultReasoning" validate:"max=80"`
	// 替换密钥
	APIKey string `json:"apiKey" validate:"max=16384"`
	// 配置版本
	Revision int64 `json:"revision" validate:"min=1"`
}

// 更新账号
type UpdateAIAccount struct {
	// 声明账号配置通过 PUT 更新。
	ginx.MethodPut
	// 账号标识
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// 请求体
	Body UpdateAIAccountRequest `in:"body"`
}

// Path 返回更新账号路径。
func (*UpdateAIAccount) Path() string { return "/:accountId" }

// Validate 规范化账号名称和新凭据。
func (a *UpdateAIAccount) Validate(*gin.Context) error {
	a.Body.Name = util.NormalizeName(a.Body.Name)
	a.Body.APIKey = strings.TrimSpace(a.Body.APIKey)
	a.Body.CustomModels = normalizeCustomModels(a.Body.CustomModels)
	a.Body.ModelCatalog = normalizeCustomModels(a.Body.ModelCatalog)
	a.Body.SelectedModels = normalizeCustomModels(a.Body.SelectedModels)
	a.Body.DefaultModel = strings.TrimSpace(a.Body.DefaultModel)
	a.Body.DefaultReasoning = strings.TrimSpace(a.Body.DefaultReasoning)
	if err := validateModelMappings(a.Body.SelectedModels, a.Body.ModelDisplayNames, a.Body.ModelReasoningLevels, a.Body.ClaudeModelRoles); err != nil {
		return err
	}
	if a.Body.Name == "" {
		return status_error.AIAccountConfigurationInvalid
	}
	return nil
}

// Output 在账号空闲时更新配置，保留未替换的 Key。
func (a *UpdateAIAccount) Output(ctx *gin.Context) (interface{}, error) {
	release, err := global.AIAccountService.Exclusive(a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	current, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}

	if current.AuthMode == constants.AccountAuthModeSubscription && a.Body.APIKey != "" {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}
	if err := validateEndpoints(current.AuthMode, a.Body.Endpoints); err != nil {
		return nil, ginx.WithStack(err)
	}
	current.Name, current.Endpoints, current.Revision = a.Body.Name, a.Body.Endpoints, a.Body.Revision
	current.CustomModels = append([]string(nil), a.Body.CustomModels...)
	current.ModelContextSizes = a.Body.ModelContextSizes
	current.ModelDisplayNames = a.Body.ModelDisplayNames
	current.ModelReasoningLevels = a.Body.ModelReasoningLevels
	current.ClaudeModelRoles = a.Body.ClaudeModelRoles
	current.ModelCatalog = append([]string(nil), a.Body.ModelCatalog...)
	current.SelectedModels = append([]string(nil), a.Body.SelectedModels...)
	current.DefaultModel = a.Body.DefaultModel
	current.DefaultReasoning = a.Body.DefaultReasoning
	if err := global.AIAccountControllerManager.Update(ctx, current, a.Body.APIKey); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
