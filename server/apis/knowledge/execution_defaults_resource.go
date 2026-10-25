package knowledge

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 知识库执行选择
type LibraryExecutionDefaultsInput struct {
	// 智能体
	RuntimeID string `json:"runtimeId" validate:"required"`
	// 账号标识
	AccountID string `json:"accountId"`
	// 模型
	Model string `json:"model"`
	// 推理强度
	Reasoning string `json:"reasoning" validate:"max=80"`
}

// resolveLibraryExecutionDefaults 复用既有执行准入解析，只保存选择引用，不启动模型。
func resolveLibraryExecutionDefaults(ctx context.Context, libraryID string, in LibraryExecutionDefaultsInput) (models.LibraryExecutionDefaults, error) {
	if global.KnowledgeService == nil {
		return models.LibraryExecutionDefaults{}, status_error.KnowledgeUnsupported
	}
	if strings.TrimSpace(in.RuntimeID) == "" {
		return models.LibraryExecutionDefaults{}, status_error.KnowledgeInvalid
	}
	if _, err := global.KnowledgeService.ResolveModel(ctx, libraryExecutionDefaultsValues(libraryID, in)); err != nil {
		return models.LibraryExecutionDefaults{}, err
	}

	// 保存用户的明确选择，空值继续表达使用智能体默认；接纳Job时才冻结解析结果。
	// 这样同一保存请求重放不会因全局默认后来变化而变成另一份请求。
	return libraryExecutionDefaultsValues(libraryID, in), nil
}

// libraryExecutionDefaultsValues 固定用户显式选择，用于持久偏好与稳定请求比较。
func libraryExecutionDefaultsValues(libraryID string, in LibraryExecutionDefaultsInput) models.LibraryExecutionDefaults {
	return models.LibraryExecutionDefaults{LibraryID: libraryID, RuntimeID: strings.TrimSpace(in.RuntimeID), AccountID: strings.TrimSpace(in.AccountID), Model: strings.TrimSpace(in.Model), Reasoning: strings.TrimSpace(in.Reasoning)}
}
