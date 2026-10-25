package ai_accounts

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"

	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 新增账号请求
type CreateAIAccountRequest struct {
	// 账号名称
	Name string `json:"name" validate:"required,max=120"`
	// 认证方式
	AuthMode constants.AccountAuthMode `json:"authMode" validate:"required"`

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
	// 接口密钥
	APIKey string `json:"apiKey" validate:"max=16384"`
}

// 新增账号
type CreateAIAccount struct {
	// 声明该账号操作使用 POST 提交。
	ginx.MethodPost
	// 请求体
	Body CreateAIAccountRequest `in:"body"`
}

// Path 返回新增账号路径。
func (*CreateAIAccount) Path() string { return "" }

// Validate 规范化名称并限制受支持的配置组合。
func (a *CreateAIAccount) Validate(*gin.Context) error {
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
	if a.Body.Name == "" || (a.Body.AuthMode == constants.AccountAuthModeAPIKey && a.Body.APIKey == "") {
		return status_error.AIAccountConfigurationInvalid
	}

	if a.Body.AuthMode == constants.AccountAuthModeSubscription && a.Body.APIKey != "" {
		return status_error.AIAccountConfigurationInvalid
	}
	if err := validateEndpoints(a.Body.AuthMode, a.Body.Endpoints); err != nil {
		return err
	}

	return nil
}

// Output 保存账号，返回订阅登录下一步需要的账号标识。
func (a *CreateAIAccount) Output(ctx *gin.Context) (interface{}, error) {
	id := util.New()
	account := models.AIAccount{
		ID:                   id,
		Name:                 a.Body.Name,
		AuthMode:             a.Body.AuthMode,
		Endpoints:            a.Body.Endpoints,
		ModelCatalog:         append([]string(nil), a.Body.ModelCatalog...),
		SelectedModels:       append([]string(nil), a.Body.SelectedModels...),
		CustomModels:         append([]string(nil), a.Body.CustomModels...),
		ModelContextSizes:    a.Body.ModelContextSizes,
		ModelDisplayNames:    a.Body.ModelDisplayNames,
		ModelReasoningLevels: a.Body.ModelReasoningLevels,
		ClaudeModelRoles:     a.Body.ClaudeModelRoles,
		DefaultModel:         a.Body.DefaultModel,
		DefaultReasoning:     a.Body.DefaultReasoning,
	}
	if err := global.AIAccountControllerManager.Create(ctx, account, a.Body.APIKey); err != nil {
		return nil, ginx.WithStack(err)
	}
	return CreateAIAccountResponse{ID: id}, nil
}

// 新增账号响应
type CreateAIAccountResponse struct {
	// 账号标识
	ID string `json:"id"`
}

func validateEndpoints(mode constants.AccountAuthMode, endpoints []models.AIAccountEndpoint) error {
	if mode != constants.AccountAuthModeAPIKey && mode != constants.AccountAuthModeSubscription {
		return status_error.AIAccountConfigurationInvalid
	}
	if len(endpoints) != 1 {
		return status_error.AIAccountConfigurationInvalid
	}
	seen := map[constants.AccountProtocol]bool{}
	for index := range endpoints {
		endpoint := &endpoints[index]
		if seen[endpoint.Protocol] {
			return status_error.AIAccountConfigurationInvalid
		}
		seen[endpoint.Protocol] = true
		if err := accountconfig.ValidateEndpoint(endpoint.Protocol, endpoint.BaseURL); err != nil {
			return err
		}
		endpoint.BaseURL = strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	}
	if mode == constants.AccountAuthModeSubscription {
		endpoint := endpoints[0]
		if (endpoint.Protocol != constants.AccountProtocolOpenAIResponses || endpoint.BaseURL != constants.AIAccountOpenAIURL) &&
			(endpoint.Protocol != constants.AccountProtocolOpenAICompletions || endpoint.BaseURL != constants.AIAccountKimiURL) {
			return status_error.AIAccountConfigurationInvalid
		}
	}
	return nil
}

func normalizeCustomModels(models []string) []string {
	result := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}
