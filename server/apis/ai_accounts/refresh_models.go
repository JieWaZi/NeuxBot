package ai_accounts

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// AIAccountModel 是账号服务返回的模型目录项。
type AIAccountModel struct {
	// ID 是服务端接受的模型标识。
	ID string `json:"id"`
	// Name 是面向用户展示的模型名称。
	Name string `json:"name"`
}

// RefreshAIAccountModels 刷新指定账号的远端模型目录。
type RefreshAIAccountModels struct {
	// MethodPost 声明模型目录刷新使用 POST 提交。
	ginx.MethodPost
	// AccountID 是待探测的受管账号标识。
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
}

// RefreshAIAccountModelsResponse 是账号模型目录响应。
type RefreshAIAccountModelsResponse struct {
	// Resources 是去重后的远端模型列表。
	Resources []AIAccountModel `json:"resources"`
}

// PreviewAIAccountModels 使用尚未保存的表单地址和临时或已有凭据获取模型目录。
type PreviewAIAccountModels struct {
	// MethodPost 声明预览请求不会创建账号。
	ginx.MethodPost
	// Body 保存当前表单中的单个协议入口和临时或已有凭据引用。
	Body struct {
		// Endpoint 是当前表单填写的唯一协议入口。
		Endpoint models.AIAccountEndpoint `json:"endpoint" validate:"required"`
		// AccountID 在未重新输入 Key 时引用已有账号的保存凭据。
		AccountID string `json:"accountId" validate:"omitempty,len=32"`
		// APIKey 是仅用于本次目录获取的临时密钥。
		APIKey string `json:"apiKey" validate:"max=16384"`
	} `in:"body"`
}

// Path 返回未保存账号的模型目录预览路径。
func (*PreviewAIAccountModels) Path() string { return "/models/preview" }

// Validate 检查单协议地址并清理临时 Key。
func (r *PreviewAIAccountModels) Validate(*gin.Context) error {
	r.Body.APIKey = strings.TrimSpace(r.Body.APIKey)
	if r.Body.APIKey == "" && r.Body.AccountID == "" {
		return status_error.AIAccountConfigurationInvalid
	}
	return accountconfig.ValidateEndpoint(r.Body.Endpoint.Protocol, r.Body.Endpoint.BaseURL)
}

// Output 获取远端目录但不保存账号或凭据。
func (r *PreviewAIAccountModels) Output(ctx *gin.Context) (interface{}, error) {
	apiKey := r.Body.APIKey
	if apiKey == "" {
		account, err := global.AIAccountControllerManager.Get(ctx, r.Body.AccountID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if account.AuthMode != constants.AccountAuthModeAPIKey {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
		apiKey, err = global.AIAccountControllerManager.Credential(ctx, account.ID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, ginx.WithStack(status_error.AIAccountCredentialError)
	}
	items, err := aiaccount.DiscoverModels(ctx, r.Body.Endpoint, apiKey)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return RefreshAIAccountModelsResponse{Resources: modelResources(items)}, nil
}

// Path 返回账号模型目录刷新接口路径。
func (*RefreshAIAccountModels) Path() string { return "/:accountId/models/refresh" }

// Validate 规范化账号标识。
func (r *RefreshAIAccountModels) Validate(*gin.Context) error {
	r.AccountID = strings.TrimSpace(r.AccountID)
	return nil
}

// Output 使用账号凭据请求各协议入口的模型目录。
func (r *RefreshAIAccountModels) Output(ctx *gin.Context) (interface{}, error) {
	account, err := global.AIAccountControllerManager.Get(ctx, r.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if account.AuthMode != constants.AccountAuthModeAPIKey {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	apiKey, err := global.AIAccountControllerManager.Credential(ctx, account.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, ginx.WithStack(status_error.AIAccountCredentialError)
	}
	if len(account.Endpoints) == 0 {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}

	modelsByID := make(map[string]AIAccountModel)
	var lastErr error
	var successfulEndpoint bool
	for _, endpoint := range account.Endpoints[:1] {
		items, requestErr := aiaccount.DiscoverModels(ctx, endpoint, apiKey)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		successfulEndpoint = true
		for _, item := range items {
			if item.ID == "" {
				continue
			}
			if _, exists := modelsByID[item.ID]; !exists {
				modelsByID[item.ID] = AIAccountModel{ID: item.ID, Name: item.Name}
			}
		}
	}
	if !successfulEndpoint && lastErr != nil {
		return nil, ginx.WithStack(lastErr)
	}
	resources := make([]AIAccountModel, 0, len(modelsByID))
	for _, item := range modelsByID {
		resources = append(resources, item)
	}
	for index := 1; index < len(resources); index++ {
		for cursor := index; cursor > 0 && resources[cursor].ID < resources[cursor-1].ID; cursor-- {
			resources[cursor], resources[cursor-1] = resources[cursor-1], resources[cursor]
		}
	}
	return RefreshAIAccountModelsResponse{Resources: resources}, nil
}

// modelResources 将账号服务模型转换为 API 响应。
func modelResources(items []aiaccount.Model) []AIAccountModel {
	resources := make([]AIAccountModel, 0, len(items))
	for _, item := range items {
		resources = append(resources, AIAccountModel{ID: item.ID, Name: item.Name})
	}
	return resources
}
