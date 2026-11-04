package ai_accounts

import (
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GetAIAccountThinking 查询当前 CLI、账号和模型的原生 Thinking 档位。
type GetAIAccountThinking struct {
	// MethodGet 声明能力探测不修改账号配置。
	ginx.MethodGet
	// AccountID 是聊天当前选择的受管账号。
	AccountID string `in:"path" name:"accountId" validate:"required,len=32"`
	// RuntimeID 是聊天当前选择的 CLI 环境。
	RuntimeID string `in:"query" name:"runtimeId" validate:"required,max=120"`
	// Model 是聊天当前选择的服务端模型 ID。
	Model string `in:"query" name:"model" validate:"required,max=200"`
}

// AccountThinkingOption 是原生 ACP 返回的模型专属档位。
type AccountThinkingOption struct {
	// ID 是 CLI 接受的推理强度标识。
	ID string `json:"id"`
	// Name 是 CLI 提供的展示名称。
	Name string `json:"name"`
}

// GetAIAccountThinkingResponse 区分探测成功与没有可靠模型专属回执。
type GetAIAccountThinkingResponse struct {
	// Options 只含当前选中模型原生返回的档位。
	Options []AccountThinkingOption `json:"options"`
	// Available 表示获得了当前模型的原生能力目录。
	Available bool `json:"available"`
}

func (*GetAIAccountThinking) Path() string { return "/:accountId/thinking" }

func (a *GetAIAccountThinking) Validate(*gin.Context) error {
	a.AccountID = strings.TrimSpace(a.AccountID)
	a.RuntimeID = strings.TrimSpace(a.RuntimeID)
	a.Model = strings.TrimSpace(a.Model)
	if a.RuntimeID == "" || a.Model == "" {
		return status_error.BadRequest
	}
	return nil
}

func (a *GetAIAccountThinking) Output(ctx *gin.Context) (interface{}, error) {
	account, err := global.AIAccountControllerManager.Get(ctx, a.AccountID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	environment, err := global.RuntimeEnvironmentControllerManager.Get(ctx, a.RuntimeID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if !environment.Enabled || len(account.Endpoints) != 1 || !accountconfig.Supports(environment.BaseRuntimeID, account.Endpoints[0].Protocol) {
		return nil, ginx.WithStack(status_error.AIAccountConfigurationInvalid)
	}
	if account.AuthMode == constants.AccountAuthModeAPIKey {
		selected := false
		for _, model := range account.SelectedModels {
			if model == a.Model {
				selected = true
				break
			}
		}
		if !selected {
			return nil, ginx.WithStack(status_error.BadRequest)
		}
	}
	response := GetAIAccountThinkingResponse{Options: []AccountThinkingOption{}}
	if environment.BaseRuntimeID == constants.RuntimeIDCodex {
		catalog, err := global.RuntimeEnvironmentService.ReadModelCatalog(ctx, environment.RuntimeID, account.ID, false)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		if !codexAccountCatalogAvailable(catalog, account.ID) {
			return response, nil
		}
		for _, row := range catalog.Models {
			if row.ID != a.Model || row.Config == nil || slices.Contains(row.Config.UnknownOptionIDs, "reasoning_effort") {
				continue
			}
			response.Available = true
			for _, setting := range row.Config.Options {
				if setting.ID != "reasoning_effort" {
					continue
				}
				for _, choice := range setting.Choices {
					if choice.ID != "" {
						response.Options = append(response.Options, AccountThinkingOption{ID: choice.ID, Name: choice.Name})
					}
				}
				break
			}
			break
		}
		return response, nil
	}
	result, err := global.AIAccountService.DetectModel(ctx.Request.Context(), global.RuntimeCatalog, account.ID, environment, a.Model)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if result.State != agentapi.DetectionStateReady {
		return response, nil
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == agentapi.DiagnosticCodeACPOptionsUnavailable {
			return response, nil
		}
	}
	nativeID := a.Model
	for _, model := range result.Models {
		if model.ID != nativeID {
			continue
		}
		response.Available = true
		for _, option := range model.ReasoningOptions {
			if option.ID != "" {
				response.Options = append(response.Options, AccountThinkingOption{ID: option.ID, Name: option.Name})
			}
		}
		break
	}
	return response, nil
}
