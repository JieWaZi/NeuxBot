package runtime_options

import (
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 指定账号的原生模型目录
type GetRuntimeModelCatalog struct {
	// MethodGet 声明模型目录使用只读查询。
	ginx.MethodGet
	// 智能体标识
	RuntimeID string `in:"path" name:"runtimeId" validate:"required,max=120"`
	// 账号标识
	AccountID string `in:"query" name:"accountId" validate:"max=32"`
	// 主动刷新
	Refresh bool `in:"query" name:"refresh"`
}

// 原生模型目录
type RuntimeModelCatalogResponse struct {
	// 智能体标识
	RuntimeID string `json:"runtimeId"`
	// 账号标识
	AccountID string `json:"accountId"`
	// 认证代次
	AuthGeneration uint64 `json:"authGeneration"`
	// 发现状态
	State string `json:"state"`
	// 刷新错误
	ErrorCode string `json:"errorCode,omitempty"`
	// 原生默认模型
	DefaultModel string `json:"defaultModel,omitempty"`
	// 模型
	Models []RuntimeModelOption `json:"models"`
}

func (*GetRuntimeModelCatalog) Path() string { return "/:runtimeId/models" }

func (o *GetRuntimeModelCatalog) Output(ctx *gin.Context) (interface{}, error) {
	runtimeID := strings.TrimSpace(o.RuntimeID)
	result, err := global.RuntimeEnvironmentService.ReadModelCatalog(ctx, runtimeID, strings.TrimSpace(o.AccountID), o.Refresh)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	models := make([]RuntimeModelOption, 0, len(result.Models))
	for _, row := range result.Models {
		models = append(models, nativeModelOption(result.Provider, row))
	}
	return RuntimeModelCatalogResponse{
		RuntimeID: runtimeID, AccountID: result.AccountID, AuthGeneration: result.AuthGeneration,
		State:     result.State,
		ErrorCode: result.ErrorCode, DefaultModel: result.DefaultID, Models: models,
	}, nil
}

func nativeModelOption(provider string, row agent.AgentChoice) RuntimeModelOption {
	result := RuntimeModelOption{
		ID: row.ID, Name: row.Name, Description: row.Description, Group: row.Group, GroupName: row.GroupName,
		Free: row.Free, CreditMultiplier: row.CreditMultiplier,
		FastSupported: row.FastServiceTier != "", ReasoningOptions: []RuntimeReasoningOption{},
		PermissionLevels: runtimeenvironment.NativePermissionLevels(provider, row),
	}
	if result.FastSupported && row.DefaultServiceTier != nil {
		fast := *row.DefaultServiceTier == row.FastServiceTier
		result.DefaultFast = &fast
	}
	if row.Config == nil {
		return result
	}
	// A partial receipt may retain an earlier setting alongside an explicit
	// unknown marker. Keep the model and permissions, but never offer that override.
	reasoningUnknown := slices.ContainsFunc(row.Config.UnknownOptionIDs, func(id string) bool {
		return id == "reasoning" || id == "reasoning_effort" || id == "thinking_level" || id == "thought_level"
	})
	fastUnknown := slices.Contains(row.Config.UnknownOptionIDs, "fast") || slices.Contains(row.Config.UnknownOptionIDs, "service_tier")
	if fastUnknown {
		result.FastSupported, result.DefaultFast = false, nil
	}
	for _, setting := range row.Config.Options {
		if slices.Contains(row.Config.UnknownOptionIDs, setting.ID) {
			continue
		}
		switch {
		case agent.IsContextWindowOption(setting):
			result.ContextWindowTokens = agent.ContextWindowTokens(setting.Value.ID)
			result.MaxContextWindowTokens = result.ContextWindowTokens
			var options []RuntimeContextWindowOption
			for _, choice := range setting.Choices {
				tokens := agent.ContextWindowTokens(choice.ID)
				if tokens > 0 && !slices.ContainsFunc(options, func(option RuntimeContextWindowOption) bool { return option.ID == choice.ID }) {
					options = append(options, RuntimeContextWindowOption{ID: choice.ID, Tokens: tokens})
					result.MaxContextWindowTokens = max(result.MaxContextWindowTokens, tokens)
				}
			}
			if len(options) > 1 {
				result.ContextWindowOptions = options
				if slices.ContainsFunc(options, func(option RuntimeContextWindowOption) bool { return option.ID == setting.Value.ID }) {
					result.DefaultContextWindow = setting.Value.ID
				}
			}
		case (setting.Category == "thought_level" || setting.ID == "reasoning_effort") && !reasoningUnknown:
			for _, choice := range setting.Choices {
				if provider == constants.RuntimeIDWorkBuddy && (choice.ID == constants.RuntimeWorkBuddyThinkingEnabled || choice.ID == constants.RuntimeWorkBuddyThinkingDisabled) {
					continue
				}
				if choice.ID == setting.Value.ID {
					result.DefaultReasoning = setting.Value.ID
				}
				result.ReasoningOptions = append(result.ReasoningOptions, RuntimeReasoningOption{ID: choice.ID, Name: choice.Name})
			}
		case setting.ID == "fast" && !fastUnknown:
			result.FastSupported = true
			if setting.Value.Bool != nil {
				v := *setting.Value.Bool
				result.DefaultFast = &v
			} else if setting.Value.ID == "on" || setting.Value.ID == "off" || setting.Value.ID == "true" || setting.Value.ID == "false" {
				v := setting.Value.ID == "on" || setting.Value.ID == "true"
				result.DefaultFast = &v
			}
		}
	}
	return result
}
