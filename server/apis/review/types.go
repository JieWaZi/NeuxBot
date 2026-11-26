package review

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// ReviewSettingsInput 记忆与学习设置。
type ReviewSettingsInput struct {
	// 智能体来源
	Source string `json:"source,omitempty" validate:"omitempty,oneof=partner global"`
	// 智能体标识
	RuntimeID string `json:"runtimeId,omitempty" validate:"max=120"`
	// 账号标识
	AccountID string `json:"accountId,omitempty" validate:"max=120"`
	// 模型标识
	Model string `json:"model,omitempty" validate:"max=200"`
	// 整理频率（轮数）
	EveryTurns int `json:"everyTurns,omitempty" validate:"omitempty,oneof=5 10 20 30 50"`
}

// ReviewSettingsResponse 记忆与学习设置。
type ReviewSettingsResponse struct {
	// 复盘配置
	ReviewSettingsInput
}

// WakerReviewInput 伙伴复盘偏好。
type WakerReviewInput struct {
	// 自动记忆
	MemoryEnabled bool `json:"memoryEnabled"`
	// 学习模式
	SkillMode string `json:"skillMode" validate:"required,oneof=off ask automatic"`
}

// WakerReviewResponse 伙伴复盘状态。
type WakerReviewResponse struct {
	// 复盘偏好
	WakerReviewInput
	// 实际智能体
	RuntimeID string `json:"runtimeId,omitempty"`
	// 实际账号
	AccountID string `json:"accountId,omitempty"`
	// 实际模型
	Model string `json:"model,omitempty"`
	// 能力可用
	Available bool `json:"available"`
	// 暂停原因
	Reason string `json:"reason,omitempty"`
	// 处理状态
	Status string `json:"status"`
}

// SkillSuggestionResponse 聊天技能建议。
type SkillSuggestionResponse struct {
	// 建议标识
	ID string `json:"id"`
	// 技能名称
	Name string `json:"name"`
	// 学到的方法
	Lesson string `json:"lesson"`
	// 建议动作
	Action string `json:"action"`
	// 保存状态
	Status string `json:"status"`
	// 聊天时间线顺序所用的 Unix 毫秒时间
	OccurredAt int64 `json:"occurredAt"`
	// 技能标识
	SkillID string `json:"skillId,omitempty"`
	// 版本标识
	VersionID string `json:"versionId,omitempty"`
	// 影响伙伴
	SharedPartnerIDs []string `json:"sharedPartnerIds,omitempty"`
	// 修订确认凭据
	ApprovalToken string `json:"approvalToken,omitempty"`
	// 失败原因
	Error string `json:"error,omitempty"`
	// 来源消息
	SourceMessageIDs []string `json:"sourceMessageIds,omitempty"`
}

// SkillSuggestionsResponse 聊天技能建议列表。
type SkillSuggestionsResponse struct {
	// 建议列表
	Items []SkillSuggestionResponse `json:"items"`
	// 直接变更
	Changes []ManagedResourceChangeResponse `json:"changes,omitempty"`
	// 建议暂不可用
	SuggestionsUnavailable bool `json:"suggestionsUnavailable,omitempty"`
}

// ManagedResourceChangeResponse 聊天资源变更。
type ManagedResourceChangeResponse struct {
	// 资源名称
	Name string `json:"name"`
	// 变更动作
	Action string `json:"action"`
	// 技能标识
	SkillID string `json:"skillId,omitempty"`
	// 连接器标识
	ConnectorID string `json:"connectorId,omitempty"`
	// 自有伙伴标识
	OwnerWakerID string `json:"ownerWakerId,omitempty"`
	// 版本标识
	VersionID string `json:"versionId,omitempty"`
	// 日志游标
	Sequence int64 `json:"sequence"`
	// 变更时间
	OccurredAt int64 `json:"occurredAt"`
}

func settingsView(ctx context.Context) (ReviewSettingsResponse, error) {
	cfg, err := global.ReviewWorker.Settings(ctx)
	if err != nil {
		return ReviewSettingsResponse{}, err
	}
	return ReviewSettingsResponse{ReviewSettingsInput: ReviewSettingsInput{Source: cfg.Source, RuntimeID: cfg.RuntimeID, AccountID: cfg.AccountID, Model: cfg.Model, EveryTurns: cfg.EveryTurns}}, nil
}

func wakerView(ctx context.Context, id string) (WakerReviewResponse, error) {
	selection, cfg, reason := global.ReviewWorker.Effective(ctx, id)
	status, err := global.ReviewWorker.Status(ctx, id)
	view := WakerReviewResponse{WakerReviewInput: WakerReviewInput{MemoryEnabled: cfg.MemoryEnabled, SkillMode: cfg.SkillMode},
		RuntimeID: selection.RuntimeID, AccountID: selection.AccountID, Model: selection.Model, Available: reason == nil, Status: status}
	if util.HasStatusCode(reason, status_error.RuntimePolicyUnsupported) {
		view.Reason = "runtimeUnsupported"
	} else if reason != nil {
		view.Reason = "runtimeUnavailable"
	}
	return view, err
}

func suggestionView(v reviewstate.Suggestion) SkillSuggestionResponse {
	occurredAt := v.CreatedAt
	if v.Status == reviewstate.StatusSaved && v.PublishedAt > 0 {
		occurredAt = v.PublishedAt
	}
	return SkillSuggestionResponse{ID: v.ID, Name: v.Name, Lesson: v.Lesson, Action: v.Action, Status: v.Status, SkillID: v.SkillID,
		OccurredAt: occurredAt, VersionID: v.VersionID, SharedPartnerIDs: v.SharedPartnerIDs, ApprovalToken: v.ApprovalToken(), Error: v.Error, SourceMessageIDs: v.SourceMessageIDs}
}
